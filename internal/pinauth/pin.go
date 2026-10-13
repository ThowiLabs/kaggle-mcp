package pinauth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
)

const MinLength = 6
const MaxLength = 128

func Validate(pin string) error {
	if len(pin) < MinLength || len(pin) > MaxLength || strings.ContainsAny(pin, "\x00\r\n") {
		return errors.New("PIN debe tener entre 6 y 128 caracteres sin saltos de línea")
	}
	return nil
}

// Generate entrega 120 bits de entropía usando exclusivamente crypto/rand.
func Generate() (string, error) {
	var bytes [15]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}

type State struct {
	mu   sync.RWMutex
	hash [sha256.Size]byte
}

func New(pin string) (*State, error) {
	if err := Validate(pin); err != nil {
		return nil, err
	}
	return &State{hash: sha256.Sum256([]byte(pin))}, nil
}
func (s *State) Verify(pin string) bool {
	if s == nil {
		return false
	}
	provided := sha256.Sum256([]byte(pin))
	s.mu.RLock()
	defer s.mu.RUnlock()
	return subtle.ConstantTimeCompare(provided[:], s.hash[:]) == 1
}

// Change valida, revoca las autorizaciones anteriores y publica el nuevo PIN.
// El PIN no se almacena ni se escribe en disco.
func (s *State) Change(current, next string, revoke func() error) error {
	if s == nil {
		return errors.New("PIN no inicializado")
	}
	if err := Validate(next); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	currentHash := sha256.Sum256([]byte(current))
	if subtle.ConstantTimeCompare(currentHash[:], s.hash[:]) != 1 {
		return errors.New("PIN actual incorrecto")
	}
	nextHash := sha256.Sum256([]byte(next))
	if subtle.ConstantTimeCompare(nextHash[:], s.hash[:]) == 1 {
		return errors.New("el nuevo PIN debe ser distinto")
	}
	if revoke != nil {
		if err := revoke(); err != nil {
			return err
		}
	}
	s.hash = nextHash
	return nil
}
