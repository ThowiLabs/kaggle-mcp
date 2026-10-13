package projectstate

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Project struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Repository   string        `json:"repository"`
	Notebook     string        `json:"notebook"`
	Memory       []string      `json:"memory"`
	Tasks        []Task        `json:"tasks"`
	Verification *Verification `json:"verification,omitempty"`
}
type Task struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}
type Event struct {
	Project     string    `json:"project"`
	When        time.Time `json:"when"`
	Description string    `json:"description"`
	Command     string    `json:"command"`
	Status      string    `json:"status"`
}
type State struct {
	Projects []Project `json:"projects"`
	History  []Event   `json:"history"`
}
type Store struct {
	mu    sync.Mutex
	file  string
	state State
}

func ID() string { var b [12]byte; _, _ = rand.Read(b[:]); return hex.EncodeToString(b[:]) }
func Open(file string) (*Store, error) {
	s := &Store{file: file}
	b, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) > 4<<20 {
		return nil, errors.New("archivo grande")
	}
	if err = json.Unmarshal(b, &s.state); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *Store) save() error {
	b, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	if len(b) > 4<<20 {
		return errors.New("límite de estado alcanzado")
	}
	dir := filepath.Dir(s.file)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".state-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), s.file)
}
func (s *Store) mutate(fn func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	before, _ := json.Marshal(s.state)
	if err := fn(&s.state); err != nil {
		return err
	}
	if err := s.save(); err != nil {
		_ = json.Unmarshal(before, &s.state)
		return err
	}
	return nil
}
func (s *Store) Snapshot() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.Marshal(s.state)
	var result State
	_ = json.Unmarshal(b, &result)
	return result
}
func (s *Store) Create(id, name, repo, notebook string) error {
	if id == "" || name == "" || len(id) > 64 || len(name) > 160 || len(repo) > 200 || len(notebook) > 300 {
		return errors.New("proyecto inválido")
	}
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return errors.New("ID inválido")
		}
	}
	return s.mutate(func(d *State) error {
		if len(d.Projects) >= 100 {
			return errors.New("máximo 100 proyectos")
		}
		for _, p := range d.Projects {
			if p.ID == id {
				return errors.New("proyecto existente")
			}
		}
		d.Projects = append(d.Projects, Project{ID: id, Name: name, Repository: repo, Notebook: notebook})
		return nil
	})
}
func (s *Store) Remember(id, note string) error {
	if note == "" || len(note) > 6000 {
		return errors.New("nota inválida")
	}
	return s.mutate(func(d *State) error {
		for i := range d.Projects {
			p := &d.Projects[i]
			if p.ID == id {
				p.Memory = append(p.Memory, note)
				if len(p.Memory) > 200 {
					p.Memory = p.Memory[len(p.Memory)-200:]
				}
				return nil
			}
		}
		return errors.New("proyecto no encontrado")
	})
}
func (s *Store) TaskAdd(id, title string) (string, error) {
	if title == "" || len(title) > 300 {
		return "", errors.New("tarea inválida")
	}
	taskID := ID()
	err := s.mutate(func(d *State) error {
		for i := range d.Projects {
			p := &d.Projects[i]
			if p.ID == id {
				if len(p.Tasks) >= 200 {
					return errors.New("máximo 200 tareas")
				}
				p.Tasks = append(p.Tasks, Task{ID: taskID, Title: title, Status: "pending"})
				return nil
			}
		}
		return errors.New("proyecto no encontrado")
	})
	return taskID, err
}
func (s *Store) TaskSet(id, taskID, status string) error {
	if status != "pending" && status != "in_progress" && status != "done" {
		return errors.New("estado inválido")
	}
	return s.mutate(func(d *State) error {
		for i := range d.Projects {
			p := &d.Projects[i]
			if p.ID == id {
				for j := range p.Tasks {
					if p.Tasks[j].ID == taskID {
						p.Tasks[j].Status = status
						return nil
					}
				}
				return errors.New("tarea no encontrada")
			}
		}
		return errors.New("proyecto no encontrado")
	})
}
func (s *Store) Record(project, description, command, status string) error {
	if len(command) > 4096 {
		command = command[:4096]
	}
	if description == "" {
		description = "Sin descripción"
	}
	if len(description) > 300 {
		description = description[:300]
	}
	return s.mutate(func(d *State) error {
		d.History = append(d.History, Event{Project: project, When: time.Now().UTC(), Description: description, Command: command, Status: status})
		if len(d.History) > 1000 {
			d.History = d.History[len(d.History)-1000:]
		}
		return nil
	})
}

func (s *Store) Get(id string) (Project, error) {
	state := s.Snapshot()
	for _, p := range state.Projects {
		if p.ID == id {
			return p, nil
		}
	}
	return Project{}, errors.New("proyecto no encontrado")
}
func (s *Store) Restore(p Project) error {
	// Un comprobante importado de Git no demuestra que esta sesión Kaggle haya ejecutado las pruebas.
	p.Verification = nil
	if p.ID == "" || p.Name == "" || len(p.ID) > 64 || len(p.Name) > 160 || len(p.Memory) > 200 || len(p.Tasks) > 200 {
		return errors.New("estado de proyecto inválido")
	}
	for _, c := range p.ID {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return errors.New("ID inválido")
		}
	}
	for _, n := range p.Memory {
		if len(n) > 6000 {
			return errors.New("nota muy larga")
		}
	}
	for _, t := range p.Tasks {
		if len(t.Title) > 300 || (t.Status != "pending" && t.Status != "in_progress" && t.Status != "done") {
			return errors.New("tarea inválida")
		}
	}
	return s.mutate(func(d *State) error {
		for i := range d.Projects {
			if d.Projects[i].ID == p.ID {
				d.Projects[i] = p
				return nil
			}
		}
		if len(d.Projects) >= 100 {
			return errors.New("máximo 100 proyectos")
		}
		d.Projects = append(d.Projects, p)
		return nil
	})
}
