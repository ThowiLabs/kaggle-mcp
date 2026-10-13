package pinauth

import (
	"errors"
	"testing"
)

func TestGenerateAndValidate(t *testing.T) {
	one, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	two, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if one == two || len(one) != 20 || Validate(one) != nil {
		t.Fatalf("PIN temporal inseguro o inválido")
	}
	for _, tc := range []struct {
		pin  string
		pass bool
	}{
		{"", false}, {"12345", false}, {"123456", true}, {"abcdefghijkl", true}, {"123\n456", false},
	} {
		err := Validate(tc.pin)
		if (err == nil) != tc.pass {
			t.Errorf("Validate %q pass=%t err=%v", tc.pin, tc.pass, err)
		}
	}
}
func TestRotateOldToNewAndRollback(t *testing.T) {
	s, err := New("123456")
	if err != nil {
		t.Fatal(err)
	}
	revoked := 0
	if err = s.Change("bad", "654321", func() error { revoked++; return nil }); err == nil {
		t.Fatal("PIN actual falso aceptado")
	}
	if revoked != 0 {
		t.Fatal("revocación ejecutada con PIN incorrecto")
	}
	if err = s.Change("123456", "654321", func() error { revoked++; return errors.New("falló revocación") }); err == nil {
		t.Fatal("rotación aplicada pese al fallo de revocación")
	}
	if !s.Verify("123456") || s.Verify("654321") {
		t.Fatal("rotación parcial")
	}
	if err = s.Change("123456", "654321", func() error { revoked++; return nil }); err != nil {
		t.Fatal(err)
	}
	if !s.Verify("654321") || s.Verify("123456") {
		t.Fatal("PIN anterior sigue aceptándose")
	}
	if revoked != 2 {
		t.Fatalf("revoke se ejecutó %d veces", revoked)
	}
}
