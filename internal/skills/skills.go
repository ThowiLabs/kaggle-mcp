package skills

import (
	"embed"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

//go:embed ponytail-v2.md
var bundled embed.FS

const Active = "ponytail-v2"
const Summary = "Ponytail v2 SIEMPRE ACTIVA: antes de trabajar lee skills_read(ponytail-v2). Evita sobreingeniería; revisa contexto/tareas/Git; documenta cambios y prueba seguridad. En Kaggle el objetivo es entregar repositorio y notebook funcional, fijar dependencias y vigilar disco para impedir readonly; solo después de verificar crear interfaz Gradio con versiones fijadas."

type Registry struct{ Dir string }
type Info struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	AlwaysActive bool   `json:"always_active"`
}

func (r Registry) List() ([]Info, error) {
	result := []Info{{Name: Active, Description: "Metodología Ponytail v2 completa integrada y siempre activa", AlwaysActive: true}}
	files, err := os.ReadDir(r.Dir)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(f.Name(), ".md")
		if valid(name) {
			result = append(result, Info{Name: name, Description: "Skill personalizada"})
		}
	}
	return result, nil
}
func valid(s string) bool {
	if len(s) < 1 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func (r Registry) full(name string) (string, error) {
	if name == Active {
		b, err := bundled.ReadFile("ponytail-v2.md")
		return string(b), err
	}
	if !valid(name) {
		return "", errors.New("nombre de skill inválido")
	}
	path := filepath.Join(r.Dir, name+".md")
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return "", errors.New("skill inválida o demasiado grande")
	}
	b, err := os.ReadFile(path)
	return string(b), err
}
func (r Registry) Read(name string, offset, limit int) (map[string]any, error) {
	content, err := r.full(name)
	if err != nil {
		return nil, err
	}
	if offset < 0 || offset > len(content) {
		return nil, errors.New("offset inválido")
	}
	if limit <= 0 || limit > 12000 {
		limit = 12000
	}
	if offset < len(content) && !utf8.RuneStart(content[offset]) {
		return nil, errors.New("offset parte un carácter UTF-8")
	}
	end := min(offset+limit, len(content))
	for end < len(content) && end > offset && !utf8.RuneStart(content[end]) {
		end--
	}
	return map[string]any{"name": name, "always_active": name == Active, "total_bytes": len(content), "offset": offset, "next_offset": end, "complete": end == len(content), "content": content[offset:end]}, nil
}
func (r Registry) Search(query string) ([]map[string]any, error) {
	if len(query) < 2 || len(query) > 100 {
		return nil, errors.New("consulta inválida")
	}
	list, err := r.List()
	if err != nil {
		return nil, err
	}
	hits := []map[string]any{}
	q := strings.ToLower(query)
	for _, v := range list {
		body, err := r.full(v.Name)
		if err != nil {
			continue
		}
		pos := strings.Index(strings.ToLower(body), q)
		if pos < 0 && !strings.Contains(v.Name, q) {
			continue
		}
		if pos < 0 {
			pos = 0
		}
		start := max(pos-140, 0)
		end := min(pos+260, len(body))
		hits = append(hits, map[string]any{"name": v.Name, "always_active": v.AlwaysActive, "excerpt": body[start:end]})
		if len(hits) >= 20 {
			break
		}
	}
	return hits, nil
}
func (r Registry) Install(name, content string) error {
	if !valid(name) || name == Active {
		return errors.New("nombre protegido o inválido")
	}
	if len(content) < 20 || len(content) > 64<<10 {
		return errors.New("skill debe tener entre 20 bytes y 64KiB")
	}
	if err := os.MkdirAll(r.Dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(r.Dir, name+".md")
	if _, err := os.Lstat(path); err == nil {
		return errors.New("skill ya existe")
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = f.WriteString(content); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	return f.Close()
}
