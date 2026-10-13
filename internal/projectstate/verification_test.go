package projectstate

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVerificationPersistsAndImportedVerificationRevoked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "projects.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Create("alpha", "A", "owner/repo", "run.ipynb"); err != nil {
		t.Fatal(err)
	}
	receipt := Verification{TestedAt: time.Now().UTC(), GitHead: strings.Repeat("a", 40), NotebookSHA256: strings.Repeat("b", 64), TestCommandSHA256: strings.Repeat("c", 64), NotebookCommandSHA256: strings.Repeat("d", 64)}
	if err = store.SetVerification("alpha", receipt); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := restored.Get("alpha")
	if err != nil || p.Verification == nil || !p.Verification.Valid() {
		t.Fatalf("no persistió comprobante: %#v %v", p, err)
	}
	if err = restored.Restore(p); err != nil {
		t.Fatal(err)
	}
	imported, _ := restored.Get("alpha")
	if imported.Verification != nil {
		t.Fatal("un archivo importado no puede verificar pruebas de la sesión actual")
	}
	bad := receipt
	bad.NotebookSHA256 = "erroneo"
	if err = restored.SetVerification("alpha", bad); err == nil {
		t.Fatal("hash inválido aceptado")
	}
}
