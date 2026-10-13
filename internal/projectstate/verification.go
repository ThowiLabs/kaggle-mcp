package projectstate

import (
	"errors"
	"regexp"
	"time"
)

type Verification struct {
	TestedAt              time.Time `json:"tested_at"`
	GitHead               string    `json:"git_head"`
	NotebookSHA256        string    `json:"notebook_sha256"`
	TestCommandSHA256     string    `json:"test_command_sha256"`
	NotebookCommandSHA256 string    `json:"notebook_command_sha256"`
}

var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var gitSHA = regexp.MustCompile(`^[a-f0-9]{40,64}$`)

func (v Verification) Valid() bool {
	return !v.TestedAt.IsZero() && gitSHA.MatchString(v.GitHead) && sha256Pattern.MatchString(v.NotebookSHA256) && sha256Pattern.MatchString(v.TestCommandSHA256) && sha256Pattern.MatchString(v.NotebookCommandSHA256)
}
func (s *Store) SetVerification(id string, v Verification) error {
	if !v.Valid() {
		return errors.New("comprobante de pruebas inválido")
	}
	return s.mutate(func(state *State) error {
		for i := range state.Projects {
			if state.Projects[i].ID == id {
				copy := v
				state.Projects[i].Verification = &copy
				return nil
			}
		}
		return errors.New("proyecto no encontrado")
	})
}
