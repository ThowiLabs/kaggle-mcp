package projectstate

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestHistoryPaginationLatestFirst(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 47; i++ {
		if err := s.Record("p", "Evento "+strconv.Itoa(i), "cmd", "ok"); err != nil {
			t.Fatal(err)
		}
	}
	a, b, c := s.HistoryPage(1), s.HistoryPage(2), s.HistoryPage(999)
	if a.Page != 1 || a.Total != 47 || a.TotalPages != 3 || len(a.Items) != 20 || a.Items[0].Description != "Evento 46" || a.Items[19].Description != "Evento 27" {
		t.Fatalf("página 1 inválida: %+v", a)
	}
	if b.Page != 2 || len(b.Items) != 20 || b.Items[0].Description != "Evento 26" {
		t.Fatalf("página 2: %+v", b)
	}
	if c.Page != 3 || len(c.Items) != 7 || c.Items[6].Description != "Evento 0" || !c.HasPrev || c.HasNext {
		t.Fatalf("página 3: %+v", c)
	}
	if a.HasPrev || !a.HasNext || a.NextPage != 2 || len(a.PageNumbers) == 0 {
		t.Fatal("controles incorrectos")
	}
	a.Items[0].Description = "editado"
	if s.HistoryPage(1).Items[0].Description != "Evento 46" {
		t.Fatal("resultado no es copia")
	}
	if s.HistoryPage(-1).Page != 1 {
		t.Fatal("page negativa sin normalizar")
	}
}

func TestHistorySortsUnorderedLegacyTimestamps(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "legacy.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	s.state.History = []Event{{Description: "old", When: now.Add(-time.Hour)}, {Description: "new", When: now}, {Description: "middle", When: now.Add(-time.Minute)}}
	result := s.HistoryPage(1)
	if len(result.Items) != 3 || result.Items[0].Description != "new" || result.Items[2].Description != "old" {
		t.Fatalf("orden: %+v", result.Items)
	}
}
func TestEmptyHistoryPage(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "empty.json"))
	if err != nil {
		t.Fatal(err)
	}
	p := s.HistoryPage(60)
	if p.Total != 0 || p.Page != 1 || p.TotalPages != 1 || len(p.Items) != 0 {
		t.Fatalf("vacío: %+v", p)
	}
}
