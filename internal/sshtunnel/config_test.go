package sshtunnel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testPIN = "PIN-DE-PRUEBA-123"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	stored, err := Encrypt(testPIN, "usuario", "servidor.example.com", 22, 0, "contraseña-secreta", true, "")
	if err != nil {
		t.Fatal(err)
	}
	if stored.KDF != kdfID {
		t.Fatalf("KDF=%q", stored.KDF)
	}
	if stored.Salt == stored.Nonce || stored.Salt == "" || stored.Nonce == "" || stored.Ciphertext == "" {
		t.Fatal("faltan campos cifrados")
	}
	plain, err := stored.Decrypt(testPIN)
	if err != nil {
		t.Fatal(err)
	}
	if plain != "contraseña-secreta" {
		t.Fatalf("descifrado=%q", plain)
	}
}

func TestDecryptWrongPIN(t *testing.T) {
	stored, err := Encrypt(testPIN, "usuario", "servidor.example.com", 22, 2222, "contraseña-secreta", true, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stored.Decrypt("PIN-EQUIVOCADO-456"); err == nil {
		t.Fatal("un PIN distinto no debe descifrar")
	}
}

func TestDecryptTamperedCiphertext(t *testing.T) {
	stored, err := Encrypt(testPIN, "usuario", "servidor.example.com", 22, 2222, "contraseña-secreta", true, "")
	if err != nil {
		t.Fatal(err)
	}
	stored.Ciphertext = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	if _, err := stored.Decrypt(testPIN); err == nil {
		t.Fatal("un ciphertext manipulado no debe descifrar")
	}
}

func TestDecryptSameErrorForWrongPINAndTampered(t *testing.T) {
	stored, err := Encrypt(testPIN, "usuario", "servidor.example.com", 22, 2222, "x", true, "")
	if err != nil {
		t.Fatal(err)
	}
	_, errWrongPIN := stored.Decrypt("otro-pin")
	tampered := stored
	tampered.Salt = "AAAAAAAAAAAAAAAAAAAAAA=="
	_, errTampered := tampered.Decrypt(testPIN)
	if errWrongPIN == nil || errTampered == nil {
		t.Fatal("ambos casos deben fallar")
	}
	if errWrongPIN.Error() != errTampered.Error() {
		t.Fatalf("el error no debe distinguir la causa: %q vs %q", errWrongPIN, errTampered)
	}
}

func TestSaveLoadDoesNotContainPlaintextPassword(t *testing.T) {
	dir := t.TempDir()
	stored, err := Encrypt(testPIN, "usuario", "servidor.example.com", 22, 0, "contraseña-muy-secreta", true, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := stored.Save(dir); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "contraseña-muy-secreta") {
		t.Fatal("el archivo guardado contiene la contraseña en claro")
	}
	if strings.Contains(string(raw), testPIN) {
		t.Fatal("el archivo guardado contiene el PIN en claro")
	}
	loaded, ok, err := Load(dir)
	if err != nil || !ok {
		t.Fatalf("cargar: ok=%v err=%v", ok, err)
	}
	plain, err := loaded.Decrypt(testPIN)
	if err != nil || plain != "contraseña-muy-secreta" {
		t.Fatalf("descifrado tras cargar: %q %v", plain, err)
	}
	info, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("permisos del archivo=%o, se exige 0600", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = dirInfo
}

func TestLoadMissingIsNotConfigured(t *testing.T) {
	_, ok, err := Load(t.TempDir())
	if err != nil || ok {
		t.Fatalf("sin archivo: ok=%v err=%v", ok, err)
	}
}

func TestValidateInput(t *testing.T) {
	cases := []struct {
		name       string
		user       string
		host       string
		sshPort    int
		remotePort int
		password   string
	}{
		{"usuario vacío", "", "h.example.com", 22, 0, "x"},
		{"usuario con espacio", "a b", "h.example.com", 22, 0, "x"},
		{"host vacío", "u", "", 22, 0, "x"},
		{"host con puerto", "u", "h.example.com:22", 22, 0, "x"},
		{"host con espacios", "u", "h .example.com", 22, 0, "x"},
		{"puerto SSH 0", "u", "h.example.com", 0, 0, "x"},
		{"puerto SSH 70000", "u", "h.example.com", 70000, 0, "x"},
		{"puerto remoto negativo", "u", "h.example.com", 22, -1, "x"},
		{"puerto remoto 70000", "u", "h.example.com", 22, 70000, "x"},
		{"contraseña vacía", "u", "h.example.com", 22, 0, ""},
	}
	for _, c := range cases {
		if err := ValidateInput(c.user, c.host, c.sshPort, c.remotePort, c.password); err == nil {
			t.Errorf("%s: se aceptó una entrada inválida", c.name)
		}
	}
	valid := []struct {
		user       string
		host       string
		sshPort    int
		remotePort int
	}{
		{"u", "h.example.com", 22, 0},
		{"u", "192.0.2.10", 2222, 2222},
		{"u", "servidor", 22, 65535},
	}
	for _, c := range valid {
		if err := ValidateInput(c.user, c.host, c.sshPort, c.remotePort, "x"); err != nil {
			t.Errorf("%+v: %v", c, err)
		}
	}
}

func TestEncryptKeepsDifferentSaltPerSave(t *testing.T) {
	a, err := Encrypt(testPIN, "u", "h.example.com", 22, 0, "misma", true, "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encrypt(testPIN, "u", "h.example.com", 22, 0, "misma", true, "")
	if err != nil {
		t.Fatal(err)
	}
	if a.Salt == b.Salt || a.Nonce == b.Nonce || a.Ciphertext == b.Ciphertext {
		t.Fatal("dos cifrados iguales deben diferir en sal, nonce y ciphertext")
	}
}
