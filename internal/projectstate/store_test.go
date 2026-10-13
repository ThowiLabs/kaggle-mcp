package projectstate

import (
	"path/filepath"
	"sync"
	"testing"
)

func TestStore_PersistenciaConcurrenciaYEstados(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "projects.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Create("alpha", "Proyecto A", "owner/repo", "notebooks/run.ipynb"); err != nil {
		t.Fatal(err)
	}
	if err = s.Create("alpha", "duplicado", "", ""); err == nil {
		t.Fatal("ID duplicado aceptado")
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Remember("alpha", "Decisión compartida"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	id, err := s.TaskAdd("alpha", "Verificar notebook")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.TaskSet("alpha", id, "done"); err != nil {
		t.Fatal(err)
	}
	if err = s.Record("alpha", "Comprobar tests", "go test ./...", "ok"); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	data := reopened.Snapshot()
	if len(data.Projects) != 1 || len(data.Projects[0].Memory) != 20 || len(data.Projects[0].Tasks) != 1 || data.Projects[0].Tasks[0].Status != "done" || len(data.History) != 1 {
		t.Fatalf("persistencia inesperada: %+v", data)
	}
	if err = reopened.TaskSet("alpha", id, "invalid"); err == nil {
		t.Fatal("estado inválido aceptado")
	}
	if err = reopened.Remember("missing", "nota"); err == nil {
		t.Fatal("proyecto inexistente aceptado")
	}
}
func TestStore_RestaurarProyectoVersionado(t *testing.T) {
	first, err := Open(filepath.Join(t.TempDir(), "first.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = first.Create("beta", "Restaurable", "owner/repo", "notebooks/main.ipynb"); err != nil {
		t.Fatal(err)
	}
	if err = first.Remember("beta", "Decisión entre sesiones"); err != nil {
		t.Fatal(err)
	}
	exported := first.Snapshot().Projects[0]
	recovered, err := Open(filepath.Join(t.TempDir(), "second.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = recovered.Restore(exported); err != nil {
		t.Fatal(err)
	}
	got, err := recovered.Get("beta")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Memory) != 1 || got.Memory[0] != "Decisión entre sesiones" {
		t.Fatalf("memoria perdida: %+v", got)
	}
	exported.Tasks = []Task{{ID: "x", Title: "tarea", Status: "desconocido"}}
	if err = recovered.Restore(exported); err == nil {
		t.Fatal("tarea inválida restaurada")
	}
}
