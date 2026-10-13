package gradiolive

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"testing"
	"time"

	msg "github.com/ThowiLabs/kagssh-go/internal/frpmsg"
	frpcrypto "github.com/fatedier/golib/crypto"
	"github.com/hashicorp/yamux"
)

func localCertificate(t *testing.T) (tls.Certificate, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(17), Subject: pkix.Name{CommonName: "FRP test"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true,
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: parsed}
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	return cert, ca
}
func TestNativeFRPControlTLSAndEncryptedMessages(t *testing.T) {
	cert, ca := localCertificate(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	proxy := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	result := make(chan error, 1)
	go func() {
		fail := func(e error) { result <- e }
		conn, err := listener.Accept()
		if err != nil {
			fail(err)
			return
		}
		defer conn.Close()
		var first [1]byte
		if _, err = conn.Read(first[:]); err != nil {
			fail(err)
			return
		}
		if first[0] != gradioFRPTLSHeadByte {
			fail(fmt.Errorf("TLS preface inválida: %#x", first[0]))
			return
		}
		secured := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
		if err = secured.Handshake(); err != nil {
			fail(err)
			return
		}
		mux, err := yamux.Server(secured, nil)
		if err != nil {
			fail(err)
			return
		}
		defer mux.Close()
		ctrl, err := mux.AcceptStream()
		if err != nil {
			fail(err)
			return
		}
		defer ctrl.Close()
		var login msg.Login
		if err = msg.ReadMsgInto(ctrl, &login); err != nil {
			fail(err)
			return
		}
		if login.Version != "0.44.0" || login.PoolCount != 5 || login.PrivilegeKey != nativeAuthKey(login.Timestamp) {
			fail(errors.New("login FRP inválido"))
			return
		}
		if err = msg.WriteMsg(ctrl, &msg.LoginResp{Version: gradioFRPVersion, RunID: "test-session"}); err != nil {
			fail(err)
			return
		}
		reader := frpcrypto.NewReader(ctrl, []byte(gradioFRPToken))
		writer, err := frpcrypto.NewWriter(ctrl, []byte(gradioFRPToken))
		if err != nil {
			fail(err)
			return
		}
		var req msg.NewProxy
		if err = msg.ReadMsgInto(reader, &req); err != nil {
			fail(err)
			return
		}
		if req.ProxyType != "http" || req.ProxyName != proxy || !req.UseCompression || !req.UseEncryption {
			fail(fmt.Errorf("configuración FRP proxy inválida: %#v", req))
			return
		}
		sum := sha256.Sum256([]byte(proxy))
		if err = msg.WriteMsg(writer, &msg.NewProxyResp{ProxyName: hex.EncodeToString(sum[:])[:18], RemoteAddr: "https://test123.gradio.live"}); err != nil {
			fail(err)
			return
		}
		fail(nil)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	urlCh := make(chan string, 1)
	resultClient := make(chan error, 1)
	go func() {
		resultClient <- runNativeFRPSession(ctx, serverInfo{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, RootCA: ca}, 8181, proxy, func(url string) { urlCh <- url })
	}()
	select {
	case link := <-urlCh:
		if link != "https://test123.gradio.live" {
			t.Errorf("URL FRP incorrecta: %s", link)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no llegó respuesta FRP cifrada")
	}
	cancel()
	select {
	case <-resultClient:
	case <-time.After(2 * time.Second):
		t.Fatal("FRP nativo no terminó por contexto")
	}
	select {
	case e := <-result:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("servidor FRP simulado no terminó")
	}
}
