package oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ThowiLabs/kagssh-go/internal/securefs"
)

type Client struct {
	ID           string    `json:"id"`
	Name         string    `json:"name,omitempty"`
	RedirectURIs []string  `json:"redirect_uris"`
	CreatedAt    time.Time `json:"created_at"`
}

type AuthorizationSession struct {
	ID               string    `json:"id"`
	CSRFHash         string    `json:"csrf_hash"`
	ClientID         string    `json:"client_id"`
	ClientName       string    `json:"client_name,omitempty"`
	RedirectURI      string    `json:"redirect_uri"`
	State            string    `json:"state,omitempty"`
	CodeChallenge    string    `json:"code_challenge"`
	Resource         string    `json:"resource"`
	Scope            string    `json:"scope"`
	ApprovedCode     string    `json:"approved_code,omitempty"`
	EnvironmentID    string    `json:"environment_id,omitempty"`
	EnvironmentRev   uint64    `json:"environment_revision,omitempty"`
	EnvironmentUntil time.Time `json:"environment_expires_at,omitempty"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type AuthorizationCode struct {
	CodeChallenge    string    `json:"code_challenge"`
	ClientID         string    `json:"client_id"`
	RedirectURI      string    `json:"redirect_uri"`
	Resource         string    `json:"resource"`
	Scope            string    `json:"scope"`
	EnvironmentID    string    `json:"environment_id,omitempty"`
	EnvironmentRev   uint64    `json:"environment_revision,omitempty"`
	EnvironmentUntil time.Time `json:"environment_expires_at,omitempty"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type Token struct {
	ClientID         string    `json:"client_id"`
	Resource         string    `json:"resource"`
	Scope            string    `json:"scope"`
	EnvironmentID    string    `json:"environment_id,omitempty"`
	EnvironmentRev   uint64    `json:"environment_revision,omitempty"`
	EnvironmentUntil time.Time `json:"environment_expires_at,omitempty"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type persistedState struct {
	OwnerClientID         string                          `json:"owner_client_id,omitempty"`
	Clients               map[string]Client               `json:"clients"`
	AuthorizationSessions map[string]AuthorizationSession `json:"authorization_sessions,omitempty"`
	Codes                 map[string]AuthorizationCode    `json:"codes"`
	AccessTokens          map[string]Token                `json:"access_tokens"`
	RefreshTokens         map[string]Token                `json:"refresh_tokens"`
	Environments          map[string]Environment          `json:"environments,omitempty"`
}

type Store struct {
	mu   sync.Mutex
	path string
	data persistedState
}

func OpenStore(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("state file path is required")
	}
	if err := securefs.EnsurePrivateDir(filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("secure state directory: %w", err)
	}

	s := &Store{path: path}
	s.data = emptyState()

	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read oauth state: %w", err)
	}
	if err := json.Unmarshal(raw, &s.data); err != nil {
		return nil, fmt.Errorf("decode oauth state: %w", err)
	}
	s.ensureMapsLocked()
	if err := securefs.RestrictPrivateFile(path); err != nil {
		return nil, fmt.Errorf("secure oauth state: %w", err)
	}
	return s, nil
}

func emptyState() persistedState {
	return persistedState{
		Clients:               make(map[string]Client),
		AuthorizationSessions: make(map[string]AuthorizationSession),
		Codes:                 make(map[string]AuthorizationCode),
		AccessTokens:          make(map[string]Token),
		RefreshTokens:         make(map[string]Token),
		Environments:          make(map[string]Environment),
	}
}

func (s *Store) ensureMapsLocked() {
	if s.data.Clients == nil {
		s.data.Clients = make(map[string]Client)
	}
	if s.data.AuthorizationSessions == nil {
		s.data.AuthorizationSessions = make(map[string]AuthorizationSession)
	}
	if s.data.Codes == nil {
		s.data.Codes = make(map[string]AuthorizationCode)
	}
	if s.data.AccessTokens == nil {
		s.data.AccessTokens = make(map[string]Token)
	}
	if s.data.RefreshTokens == nil {
		s.data.RefreshTokens = make(map[string]Token)
	}
	if s.data.Environments == nil {
		s.data.Environments = make(map[string]Environment)
	}
}

func (s *Store) saveLocked() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode oauth state: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("write oauth state: %w", err)
	}
	if err := securefs.RestrictPrivateFile(tmp); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("secure oauth state temp file: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace oauth state: %w", err)
	}
	return nil
}

func (s *Store) IsLinked(resource string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	owner := s.data.OwnerClientID
	if owner == "" {
		return false
	}
	for _, tok := range s.data.AccessTokens {
		if tok.ClientID == owner && tok.Resource == resource && now.Before(tok.ExpiresAt) && validateEnvironmentGrant(s.data.Environments, tok.EnvironmentID, tok.EnvironmentRev, tok.EnvironmentUntil, now) == nil {
			return true
		}
	}
	for _, tok := range s.data.RefreshTokens {
		if tok.ClientID == owner && tok.Resource == resource && now.Before(tok.ExpiresAt) && validateEnvironmentGrant(s.data.Environments, tok.EnvironmentID, tok.EnvironmentRev, tok.EnvironmentUntil, now) == nil {
			return true
		}
	}
	return false
}

func (s *Store) HasOwner() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.OwnerClientID != ""
}

func (s *Store) ResetOwner() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resetOwnerLocked()
	return s.saveLocked()
}

func (s *Store) ResetOwnerIfResourceChanged(resource string, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	owner := s.data.OwnerClientID
	if owner == "" {
		return false, nil
	}
	for _, tok := range s.data.AccessTokens {
		if tok.ClientID == owner && tok.Resource == resource && now.Before(tok.ExpiresAt) && validateEnvironmentGrant(s.data.Environments, tok.EnvironmentID, tok.EnvironmentRev, tok.EnvironmentUntil, now) == nil {
			return false, nil
		}
	}
	for _, tok := range s.data.RefreshTokens {
		if tok.ClientID == owner && tok.Resource == resource && now.Before(tok.ExpiresAt) && validateEnvironmentGrant(s.data.Environments, tok.EnvironmentID, tok.EnvironmentRev, tok.EnvironmentUntil, now) == nil {
			return false, nil
		}
	}
	s.resetOwnerLocked()
	return true, s.saveLocked()
}

func (s *Store) resetOwnerLocked() {
	s.data.OwnerClientID = ""
	s.data.AuthorizationSessions = make(map[string]AuthorizationSession)
	s.data.Codes = make(map[string]AuthorizationCode)
	s.data.AccessTokens = make(map[string]Token)
	s.data.RefreshTokens = make(map[string]Token)
}

// ReleaseStaleOwner clears legacy/provisional ownership when no live token exists
// for that client. v0.1.3 assigned ownership before the authorization code was
// redeemed, so a failed callback could otherwise leave the server stuck.
func (s *Store) ReleaseStaleOwner(now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	owner := s.data.OwnerClientID
	if owner == "" {
		return false, nil
	}
	for _, tok := range s.data.AccessTokens {
		if tok.ClientID == owner && now.Before(tok.ExpiresAt) && validateEnvironmentGrant(s.data.Environments, tok.EnvironmentID, tok.EnvironmentRev, tok.EnvironmentUntil, now) == nil {
			return false, nil
		}
	}
	for _, tok := range s.data.RefreshTokens {
		if tok.ClientID == owner && now.Before(tok.ExpiresAt) && validateEnvironmentGrant(s.data.Environments, tok.EnvironmentID, tok.EnvironmentRev, tok.EnvironmentUntil, now) == nil {
			return false, nil
		}
	}
	s.data.OwnerClientID = ""
	return true, s.saveLocked()
}
