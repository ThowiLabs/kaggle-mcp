package oauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	EnvironmentKindSandbox = "sandbox"
	EnvironmentKindMaster  = "master"

	minEnvironmentTTL        = time.Minute
	maxEnvironmentTTL        = 7 * 24 * time.Hour
	environmentPINLen        = 10
	environmentPINIterations = 100_000
	environmentPINSaltBytes  = 16
	maxEnvironments          = 256
)

var (
	ErrEnvironmentRequired = errors.New("MCP access required; request the current PIN from the KagMCP and reconnect")
	ErrEnvironmentExpired  = errors.New("MCP access expired; request a new PIN from the KagMCP and reconnect")
	ErrEnvironmentRevoked  = errors.New("MCP access revoked; request a new PIN from the KagMCP and reconnect")
	ErrEnvironmentRotated  = errors.New("MCP credentials changed; request the current PIN from the KagMCP and reconnect")
)

type Environment struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind,omitempty"`
	PINHash     string    `json:"pin_hash"`
	PINLookup   string    `json:"pin_lookup,omitempty"`
	Revision    uint64    `json:"revision"`
	OwnerUserID string    `json:"owner_user_id,omitempty"`
	SandboxID   string    `json:"sandbox_id,omitempty"`
	Permanent   bool      `json:"permanent,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	RevokedAt   time.Time `json:"revoked_at,omitempty"`
}

type EnvironmentSummary struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind,omitempty"`
	Revision    uint64    `json:"revision"`
	OwnerUserID string    `json:"owner_user_id,omitempty"`
	SandboxID   string    `json:"sandbox_id,omitempty"`
	Permanent   bool      `json:"permanent"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	Status      string    `json:"status"`
}

func (s *Server) ListEnvironments() []EnvironmentSummary {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	now := time.Now().UTC()
	out := make([]EnvironmentSummary, 0, len(s.store.data.Environments))
	for _, env := range s.store.data.Environments {
		out = append(out, summarizeEnvironment(env, now))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].Name < out[j].Name
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func (s *Server) HasEnvironments() bool {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	return len(s.store.data.Environments) > 0
}

func (s *Server) CreateEnvironment(name string, ttl time.Duration) (EnvironmentSummary, string, error) {
	return s.createEnvironment(name, "", "", "", ttl, false)
}

// CreateSandboxEnvironment creates an OAuth PIN bound to one user-owned sandbox.
// A permanent grant has no hard expiration but can always be rotated or revoked.
func (s *Server) CreateSandboxEnvironment(name, ownerUserID, sandboxID string, ttl time.Duration, permanent bool) (EnvironmentSummary, string, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	sandboxID = strings.TrimSpace(sandboxID)
	if ownerUserID == "" || sandboxID == "" {
		return EnvironmentSummary{}, "", errors.New("sandbox environment requires owner and sandbox identifiers")
	}
	return s.createEnvironment(name, EnvironmentKindSandbox, ownerUserID, sandboxID, ttl, permanent)
}

// CreateMasterEnvironment creates one control-plane PIN for an administrator.
// Its grant can manage LLM-created sandboxes without becoming a sandbox-specific credential.
func (s *Server) CreateMasterEnvironment(name, ownerUserID string, ttl time.Duration, permanent bool) (EnvironmentSummary, string, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	if ownerUserID == "" {
		return EnvironmentSummary{}, "", errors.New("master environment requires an owner identifier")
	}
	return s.createEnvironment(name, EnvironmentKindMaster, ownerUserID, "", ttl, permanent)
}

func (s *Server) MasterEnvironment() (EnvironmentSummary, bool) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	now := time.Now().UTC()
	for _, env := range s.store.data.Environments {
		if environmentKind(env) == EnvironmentKindMaster {
			return summarizeEnvironment(env, now), true
		}
	}
	return EnvironmentSummary{}, false
}

func (s *Server) createEnvironment(name, kind, ownerUserID, sandboxID string, ttl time.Duration, permanent bool) (EnvironmentSummary, string, error) {
	name = strings.TrimSpace(name)
	if err := validateEnvironmentInput(name, ttl, permanent); err != nil {
		return EnvironmentSummary{}, "", err
	}
	pin, lookup, err := newEnvironmentPIN(kind == EnvironmentKindSandbox || kind == EnvironmentKindMaster)
	if err != nil {
		return EnvironmentSummary{}, "", err
	}
	pinHash, err := hashEnvironmentPIN(pin)
	if err != nil {
		return EnvironmentSummary{}, "", fmt.Errorf("hash environment PIN: %w", err)
	}
	id, err := randomToken(18)
	if err != nil {
		return EnvironmentSummary{}, "", err
	}
	now := time.Now().UTC()
	env := Environment{ID: id, Name: name, Kind: kind, PINHash: pinHash, PINLookup: lookup, Revision: 1, OwnerUserID: ownerUserID, SandboxID: sandboxID, Permanent: permanent, CreatedAt: now}
	if !permanent {
		env.ExpiresAt = now.Add(ttl)
	}

	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if len(s.store.data.Environments) >= maxEnvironments {
		return EnvironmentSummary{}, "", fmt.Errorf("environment limit reached (%d)", maxEnvironments)
	}
	for _, current := range s.store.data.Environments {
		currentKind := environmentKind(current)
		if kind == EnvironmentKindSandbox && currentKind == EnvironmentKindSandbox && current.SandboxID == sandboxID {
			return EnvironmentSummary{}, "", errors.New("sandbox already has an OAuth environment")
		}
		if kind == EnvironmentKindMaster && currentKind == EnvironmentKindMaster {
			return EnvironmentSummary{}, "", errors.New("a master OAuth environment already exists")
		}
		if kind == "" && currentKind == "" && strings.EqualFold(current.Name, name) {
			return EnvironmentSummary{}, "", errors.New("an environment with that name already exists")
		}
	}
	s.store.data.Environments[id] = env
	if err := s.store.saveLocked(); err != nil {
		delete(s.store.data.Environments, id)
		return EnvironmentSummary{}, "", err
	}
	return summarizeEnvironment(env, now), pin, nil
}

func (s *Server) RotateEnvironment(id string, ttl time.Duration) (EnvironmentSummary, string, error) {
	return s.RotateEnvironmentAccess(id, ttl, false)
}

func (s *Server) RotateEnvironmentAccess(id string, ttl time.Duration, permanent bool) (EnvironmentSummary, string, error) {
	if err := validateEnvironmentTTL(ttl, permanent); err != nil {
		return EnvironmentSummary{}, "", err
	}
	pin, lookup, err := newEnvironmentPIN(true)
	if err != nil {
		return EnvironmentSummary{}, "", err
	}
	pinHash, err := hashEnvironmentPIN(pin)
	if err != nil {
		return EnvironmentSummary{}, "", fmt.Errorf("hash environment PIN: %w", err)
	}
	now := time.Now().UTC()

	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	env, ok := s.store.data.Environments[id]
	if !ok {
		return EnvironmentSummary{}, "", errors.New("environment not found")
	}
	previous := env
	env.PINHash = pinHash
	env.PINLookup = lookup
	env.Revision++
	env.Permanent = permanent
	if permanent {
		env.ExpiresAt = time.Time{}
	} else {
		env.ExpiresAt = now.Add(ttl)
	}
	env.RevokedAt = time.Time{}
	s.store.data.Environments[id] = env
	if err := s.store.saveLocked(); err != nil {
		s.store.data.Environments[id] = previous
		return EnvironmentSummary{}, "", err
	}
	return summarizeEnvironment(env, now), pin, nil
}

func (s *Server) RevokeEnvironment(id string) (EnvironmentSummary, error) {
	now := time.Now().UTC()
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	env, ok := s.store.data.Environments[id]
	if !ok {
		return EnvironmentSummary{}, errors.New("environment not found")
	}
	if env.RevokedAt.IsZero() {
		previous := env
		env.RevokedAt = now
		env.Revision++
		s.store.data.Environments[id] = env
		if err := s.store.saveLocked(); err != nil {
			s.store.data.Environments[id] = previous
			return EnvironmentSummary{}, err
		}
	}
	return summarizeEnvironment(env, now), nil
}

func validateEnvironmentInput(name string, ttl time.Duration, permanent bool) error {
	if name == "" {
		return errors.New("environment name is required")
	}
	if len(name) > 80 {
		return errors.New("environment name must contain at most 80 characters")
	}
	return validateEnvironmentTTL(ttl, permanent)
}

func validateEnvironmentTTL(ttl time.Duration, permanent bool) error {
	if permanent {
		return nil
	}
	if ttl < minEnvironmentTTL || ttl > maxEnvironmentTTL {
		return fmt.Errorf("environment TTL must be between %s and %s", minEnvironmentTTL, maxEnvironmentTTL)
	}
	return nil
}

func summarizeEnvironment(env Environment, now time.Time) EnvironmentSummary {
	status := "active"
	if !env.RevokedAt.IsZero() {
		status = "revoked"
	} else if !env.Permanent && !env.ExpiresAt.IsZero() && !now.Before(env.ExpiresAt) {
		status = "expired"
	}
	return EnvironmentSummary{
		ID: env.ID, Name: env.Name, Kind: environmentKind(env), Revision: env.Revision, OwnerUserID: env.OwnerUserID, SandboxID: env.SandboxID, Permanent: env.Permanent,
		CreatedAt: env.CreatedAt, ExpiresAt: env.ExpiresAt, Status: status,
	}
}

func environmentKind(env Environment) string {
	if env.Kind != "" {
		return env.Kind
	}
	if env.SandboxID != "" {
		return EnvironmentKindSandbox
	}
	return ""
}

func environmentSupportsSandboxAccess(env Environment) bool {
	if env.OwnerUserID == "" {
		return false
	}
	switch environmentKind(env) {
	case EnvironmentKindSandbox:
		return env.SandboxID != ""
	case EnvironmentKindMaster:
		return env.SandboxID == ""
	default:
		return false
	}
}

func (s *Server) authenticateEnvironmentPIN(pin string, now time.Time) (Environment, error) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if len(s.store.data.Environments) == 0 {
		return Environment{}, ErrEnvironmentRequired
	}
	var matched *Environment
	lookup := environmentPINLookup(pin)
	for _, env := range s.store.data.Environments {
		if s.requireSandbox && !environmentSupportsSandboxAccess(env) {
			continue
		}
		if env.PINLookup != "" && env.PINLookup != lookup {
			continue
		}
		if verifyEnvironmentPIN(env.PINHash, pin) {
			copy := env
			matched = &copy
		}
	}
	if matched == nil {
		return Environment{}, errors.New("incorrect environment PIN")
	}
	if !matched.RevokedAt.IsZero() {
		return Environment{}, ErrEnvironmentRevoked
	}
	if !matched.Permanent && !matched.ExpiresAt.IsZero() && !now.Before(matched.ExpiresAt) {
		return Environment{}, ErrEnvironmentExpired
	}
	return *matched, nil
}

func (s *Server) validateEnvironmentGrantLocked(environmentID string, revision uint64, grantExpiresAt, now time.Time) error {
	return validateEnvironmentGrantMode(s.store.data.Environments, environmentID, revision, grantExpiresAt, now, s.requireSandbox)
}

func validateEnvironmentGrant(environments map[string]Environment, environmentID string, revision uint64, grantExpiresAt, now time.Time) error {
	return validateEnvironmentGrantMode(environments, environmentID, revision, grantExpiresAt, now, false)
}

func validateEnvironmentGrantMode(environments map[string]Environment, environmentID string, revision uint64, grantExpiresAt, now time.Time, requireSandbox bool) error {
	if len(environments) == 0 && !requireSandbox {
		return nil
	}
	if environmentID == "" {
		return ErrEnvironmentRequired
	}
	env, ok := environments[environmentID]
	if !ok {
		return ErrEnvironmentRevoked
	}
	if requireSandbox && !environmentSupportsSandboxAccess(env) {
		return ErrEnvironmentRequired
	}
	if !env.RevokedAt.IsZero() {
		return ErrEnvironmentRevoked
	}
	if revision != env.Revision {
		return ErrEnvironmentRotated
	}
	if (!env.Permanent && !env.ExpiresAt.IsZero() && !now.Before(env.ExpiresAt)) || (!grantExpiresAt.IsZero() && !now.Before(grantExpiresAt)) {
		return ErrEnvironmentExpired
	}
	return nil
}

func hashEnvironmentPIN(pin string) (string, error) {
	salt := make([]byte, environmentPINSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := deriveEnvironmentPINHash(pin, salt, environmentPINIterations)
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s",
		environmentPINIterations,
		base64.RawURLEncoding.EncodeToString(salt),
		base64.RawURLEncoding.EncodeToString(hash),
	), nil
}

func verifyEnvironmentPIN(encoded, pin string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 100_000 || iterations > 1_000_000 {
		return false
	}
	salt, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(salt) < 16 || len(salt) > 64 {
		return false
	}
	want, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(want) != sha256.Size {
		return false
	}
	got := deriveEnvironmentPINHash(pin, salt, iterations)
	return subtle.ConstantTimeCompare(got, want) == 1
}

// deriveEnvironmentPINHash implements PBKDF2-HMAC-SHA256 for a single 32-byte block.
func deriveEnvironmentPINHash(pin string, salt []byte, iterations int) []byte {
	block := make([]byte, len(salt)+4)
	copy(block, salt)
	binary.BigEndian.PutUint32(block[len(salt):], 1)

	mac := hmac.New(sha256.New, []byte(pin))
	_, _ = mac.Write(block)
	u := mac.Sum(make([]byte, 0, sha256.Size))
	out := append([]byte(nil), u...)
	for i := 1; i < iterations; i++ {
		mac.Reset()
		_, _ = mac.Write(u)
		u = mac.Sum(u[:0])
		for j := range out {
			out[j] ^= u[j]
		}
	}
	return out
}

func newEnvironmentPIN(withLookup bool) (string, string, error) {
	secret, err := randomNumericPIN(environmentPINLen)
	if err != nil {
		return "", "", err
	}
	if !withLookup {
		return secret, "", nil
	}
	raw := make([]byte, 3)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	lookup := strings.ToUpper(hex.EncodeToString(raw))
	return lookup + "-" + secret, lookup, nil
}

func environmentPINLookup(pin string) string {
	parts := strings.SplitN(strings.TrimSpace(pin), "-", 2)
	if len(parts) != 2 {
		return ""
	}
	return strings.ToUpper(parts[0])
}

func randomNumericPIN(length int) (string, error) {
	if length <= 0 {
		return "", errors.New("PIN length must be positive")
	}
	buf := make([]byte, length)
	random := make([]byte, length*2)
	written := 0
	for written < length {
		if _, err := rand.Read(random); err != nil {
			return "", err
		}
		for _, value := range random {
			if value >= 250 {
				continue
			}
			buf[written] = '0' + value%10
			written++
			if written == length {
				break
			}
		}
	}
	return string(buf), nil
}

// ValidateMasterPIN verifies that pin belongs to the active master environment.
// In direct-host editions this credential is exclusive to OAuth/MCP.
func (s *Server) ValidateMasterPIN(pin string, now time.Time) error {
	env, err := s.authenticateEnvironmentPIN(strings.TrimSpace(pin), now)
	if err != nil {
		return err
	}
	if environmentKind(env) != EnvironmentKindMaster || env.SandboxID != "" {
		return errors.New("incorrect master PIN")
	}
	return nil
}
