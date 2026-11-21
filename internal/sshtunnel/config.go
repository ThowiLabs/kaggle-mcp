// Package sshtunnel guarda la configuración de un túnel SSH inverso
// opcional y la mantiene activa en segundo plano.
//
// La contraseña SSH nunca se guarda en claro: se cifra con AES-256-GCM
// con una clave derivada del PIN de administración mediante
// PBKDF2-HMAC-SHA256 (stdlib crypto/pbkdf2) con sal aleatoria.
// Solo los metadatos (usuario, host, puertos, huella del host) van en claro.
package sshtunnel

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// FileName es el archivo de configuración dentro de DataDir.
const FileName = "ssh-tunnel.json"

// kdfID identifica el KDF y sus parámetros guardados en el archivo.
const kdfID = "pbkdf2-sha256-600k"

const (
	kdfIterations = 600000
	saltLen       = 16
	nonceLen      = 12
	keyLen        = 32
	maxFileBytes  = 16 << 10
)

// Stored es la configuración guardada en disco. La contraseña solo existe
// cifrada; ningún campo en claro la contiene.
type Stored struct {
	User          string    `json:"user"`
	Host          string    `json:"host"`
	SSHPort       int       `json:"ssh_port"`
	RemotePort    int       `json:"remote_port"`
	HostKeySHA256 string    `json:"host_key_sha256,omitempty"`
	Enabled       bool      `json:"enabled"`
	KDF           string    `json:"kdf"`
	Salt          string    `json:"password_salt_b64"`
	Nonce         string    `json:"password_nonce_b64"`
	Ciphertext    string    `json:"password_ciphertext_b64"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Path devuelve la ruta del archivo de configuración.
func Path(dataDir string) string { return filepath.Join(dataDir, FileName) }

// ValidateInput valida los datos del panel antes de cifrar nada.
func ValidateInput(user, host string, sshPort, remotePort int, password string) error {
	if len(user) < 1 || len(user) > 128 || strings.ContainsAny(user, " \t\r\n\x00/@:") {
		return errors.New("usuario SSH inválido: 1-128 caracteres sin espacios ni @/:")
	}
	if err := validateHost(host); err != nil {
		return err
	}
	if sshPort < 1 || sshPort > 65535 {
		return errors.New("puerto SSH debe estar entre 1 y 65535")
	}
	if remotePort < 0 || remotePort > 65535 {
		return errors.New("puerto remoto debe ser 0 (asignado por el servidor) o estar entre 1 y 65535")
	}
	if len(password) < 1 || len(password) > 1024 {
		return errors.New("contraseña SSH inválida: 1-1024 caracteres")
	}
	return nil
}

func validateHost(host string) error {
	if len(host) < 1 || len(host) > 253 || strings.ContainsAny(host, " \t\r\n\x00/@:[]") {
		return errors.New("host SSH inválido")
	}
	if ip := net.ParseIP(host); ip != nil {
		return nil
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if len(label) < 1 || len(label) > 63 {
			return errors.New("host SSH inválido")
		}
		for i, c := range label {
			ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-'
			if !ok || i == 0 && c == '-' || i == len(label)-1 && c == '-' {
				return errors.New("host SSH inválido")
			}
		}
	}
	return nil
}

// Validate comprueba una configuración cargada de disco.
func (s Stored) Validate() error {
	if err := ValidateInput(s.User, s.Host, s.SSHPort, s.RemotePort, "x"); err != nil {
		return err
	}
	if s.KDF != kdfID {
		return errors.New("KDF de túnel SSH no reconocido")
	}
	salt, err := base64.StdEncoding.DecodeString(s.Salt)
	if err != nil || len(salt) != saltLen {
		return errors.New("sal de túnel SSH inválida")
	}
	nonce, err := base64.StdEncoding.DecodeString(s.Nonce)
	if err != nil || len(nonce) != nonceLen {
		return errors.New("nonce de túnel SSH inválido")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(s.Ciphertext)
	if err != nil || len(ciphertext) == 0 {
		return errors.New("contraseña cifrada de túnel SSH inválida")
	}
	if s.HostKeySHA256 != "" {
		fp, err := base64.StdEncoding.DecodeString(s.HostKeySHA256)
		if err != nil || len(fp) != sha256.Size {
			return errors.New("huella de host SSH inválida")
		}
	}
	return nil
}

func deriveKey(pin string, salt []byte) ([]byte, error) {
	if pin == "" {
		return nil, errors.New("PIN no configurado: el túnel SSH requiere un PIN propio")
	}
	return pbkdf2.Key(sha256.New, pin, salt, kdfIterations, keyLen)
}

// Encrypt cifra la contraseña con una clave derivada del PIN y sal
// aleatoria nueva. Cada guardado genera sal y nonce nuevos.
func Encrypt(pin, user, host string, sshPort, remotePort int, password string, enabled bool, hostKeySHA256 string) (Stored, error) {
	if err := ValidateInput(user, host, sshPort, remotePort, password); err != nil {
		return Stored{}, err
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return Stored{}, err
	}
	key, err := deriveKey(pin, salt)
	if err != nil {
		return Stored{}, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return Stored{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return Stored{}, err
	}
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return Stored{}, err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(password), nil)
	return Stored{
		User: user, Host: host, SSHPort: sshPort, RemotePort: remotePort,
		HostKeySHA256: hostKeySHA256, Enabled: enabled, KDF: kdfID,
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
		UpdatedAt:  time.Now().UTC(),
	}, nil
}

// Decrypt descifra la contraseña con la clave derivada del PIN. Cualquier
// fallo (PIN distinto, archivo manipulado) devuelve el mismo error
// genérico, sin distinguir la causa.
func (s Stored) Decrypt(pin string) (string, error) {
	const generic = "no se pudo descifrar la configuración del túnel SSH"
	if err := s.Validate(); err != nil {
		return "", errors.New(generic)
	}
	salt, err := base64.StdEncoding.DecodeString(s.Salt)
	if err != nil {
		return "", errors.New(generic)
	}
	key, err := deriveKey(pin, salt)
	if err != nil {
		return "", errors.New(generic)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", errors.New(generic)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", errors.New(generic)
	}
	nonce, err := base64.StdEncoding.DecodeString(s.Nonce)
	if err != nil {
		return "", errors.New(generic)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(s.Ciphertext)
	if err != nil {
		return "", errors.New(generic)
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", errors.New(generic)
	}
	return string(plain), nil
}

// Load lee la configuración guardada. Devuelve configured=false si el
// archivo no existe. Un archivo corrupto es un error, no silencio.
func Load(dataDir string) (Stored, bool, error) {
	raw, err := os.ReadFile(Path(dataDir))
	if errors.Is(err, os.ErrNotExist) {
		return Stored{}, false, nil
	}
	if err != nil {
		return Stored{}, false, fmt.Errorf("leer túnel SSH: %w", err)
	}
	if len(raw) > maxFileBytes {
		return Stored{}, false, errors.New("túnel SSH: archivo demasiado grande")
	}
	var stored Stored
	if err := json.Unmarshal(raw, &stored); err != nil {
		return Stored{}, false, errors.New("túnel SSH: configuración inválida")
	}
	if err := stored.Validate(); err != nil {
		return Stored{}, false, fmt.Errorf("túnel SSH: %w", err)
	}
	return stored, true, nil
}

// Save escribe la configuración con escritura atómica (temp+rename),
// directorio 0700 y archivo 0600, como SaveSettings.
func (s Stored) Save(dataDir string) error {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dataDir, 0700); err != nil {
		return err
	}
	content, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	path := Path(dataDir)
	temp, err := os.CreateTemp(dataDir, ".ssh-tunnel-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(content); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

// Fingerprint devuelve la huella SHA-256 en base64 estándar de una clave
// pública de host, sin prefijo. Es el valor guardado en HostKeySHA256.
func Fingerprint(key ssh.PublicKey) string {
	sum := sha256.Sum256(key.Marshal())
	return base64.StdEncoding.EncodeToString(sum[:])
}
