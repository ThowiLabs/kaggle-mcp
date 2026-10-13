package oauth

import (
	"path/filepath"
	"testing"
	"time"
)

func TestResetOwnerIfResourceChanged(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "oauth-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	store.data.OwnerClientID = "client-1"
	store.data.RefreshTokens["refresh"] = Token{
		ClientID:  "client-1",
		Resource:  "https://old.trycloudflare.com/mcp",
		Scope:     rootScope,
		ExpiresAt: now.Add(time.Hour),
	}
	if err := store.saveLocked(); err != nil {
		t.Fatal(err)
	}
	reset, err := store.ResetOwnerIfResourceChanged("https://new.trycloudflare.com/mcp", now)
	if err != nil {
		t.Fatal(err)
	}
	if !reset {
		t.Fatal("owner was not reset after resource changed")
	}
	if store.data.OwnerClientID != "" || len(store.data.RefreshTokens) != 0 {
		t.Fatalf("stale authorization remains: %#v", store.data)
	}
}

func TestResetOwnerIfResourceChangedKeepsCurrentOwner(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "oauth-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	resource := "https://same.example.com/mcp"
	store.data.OwnerClientID = "client-1"
	store.data.AccessTokens["access"] = Token{
		ClientID:  "client-1",
		Resource:  resource,
		Scope:     rootScope,
		ExpiresAt: now.Add(time.Hour),
	}
	if err := store.saveLocked(); err != nil {
		t.Fatal(err)
	}
	reset, err := store.ResetOwnerIfResourceChanged(resource, now)
	if err != nil {
		t.Fatal(err)
	}
	if reset || store.data.OwnerClientID != "client-1" {
		t.Fatal("current owner was reset unexpectedly")
	}
}

func TestResetOwnerClearsClientTokensButKeepsEnvironments(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "oauth-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	store.data.OwnerClientID = "client-1"
	store.data.AccessTokens["access"] = Token{ClientID: "client-1", Resource: "https://example.test/mcp", ExpiresAt: now.Add(time.Hour)}
	store.data.RefreshTokens["refresh"] = Token{ClientID: "client-1", Resource: "https://example.test/mcp", ExpiresAt: now.Add(time.Hour)}
	store.data.Environments["master"] = Environment{ID: "master", Name: "Master", PINHash: "pin-hash", Permanent: true}
	if err := store.saveLocked(); err != nil {
		t.Fatal(err)
	}
	if !store.HasOwner() {
		t.Fatal("owner should be reported before reset")
	}

	if err := store.ResetOwner(); err != nil {
		t.Fatal(err)
	}
	if store.HasOwner() {
		t.Fatal("owner still reported after reset")
	}
	if len(store.data.AccessTokens) != 0 || len(store.data.RefreshTokens) != 0 {
		t.Fatalf("tokens remain after reset: access=%d refresh=%d", len(store.data.AccessTokens), len(store.data.RefreshTokens))
	}
	env, ok := store.data.Environments["master"]
	if !ok || env.PINHash != "pin-hash" {
		t.Fatalf("master environment changed during owner reset: %#v", env)
	}
}
