package skills

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPonytailV2SiempreActivaCompleta(t *testing.T) {
	r := Registry{Dir: filepath.Join(t.TempDir(), "skills")}
	list, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || !list[0].AlwaysActive || list[0].Name != Active {
		t.Fatalf("registro %v", list)
	}
	data, err := r.Read(Active, 0, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if data["total_bytes"].(int) < 80000 {
		t.Fatal("Ponytail v2 truncada")
	}
	hits, err := r.Search("Gradio")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("no se indexó extensión Kaggle/Gradio")
	}
	if err = r.Install(Active, "otro contenido"); err == nil {
		t.Fatal("Ponytail siempre activa sobrescrita")
	}
	if err = r.Install("custom", "Habilidad personalizada con instrucciones específicas."); err != nil {
		t.Fatal(err)
	}
	b, err := r.Read("custom", 0, 999)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b["content"].(string), "personalizada") {
		t.Fatal("skill custom no encontrada")
	}
	if _, err = r.Read("../escape", 0, 1024); err == nil {
		t.Fatal("path traversal aceptado")
	}
}
func TestPonytailV2PaginacionUTF8SinPerdidas(t *testing.T) {
	r := Registry{Dir: t.TempDir()}
	full, err := r.full(Active)
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	offset := 0
	for steps := 0; steps < 1000; steps++ {
		part, err := r.Read(Active, offset, 1023)
		if err != nil {
			t.Fatal(err)
		}
		content := part["content"].(string)
		if !utf8.ValidString(content) {
			t.Fatal("fragmento UTF8 inválido")
		}
		all.WriteString(content)
		offset = part["next_offset"].(int)
		if part["complete"].(bool) {
			break
		}
	}
	if all.String() != full {
		t.Fatal("Ponytail v2 perdió contenido al paginar")
	}
}
