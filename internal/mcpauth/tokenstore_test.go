package mcpauth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
)

func TestServerTokenStore_ErrNoTokenWhenAbsent(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "mcp-auth.json"))
	ts := NewServerTokenStore(store, "srv", "https://example.com")

	_, err := ts.GetToken(context.Background())
	if !errors.Is(err, transport.ErrNoToken) {
		t.Fatalf("GetToken() error = %v, want transport.ErrNoToken", err)
	}
}

func TestServerTokenStore_ErrNoTokenOnURLMismatch(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "mcp-auth.json"))
	if err := store.UpdateTokens("srv", &Tokens{AccessToken: "at"}, "https://old.example.com"); err != nil {
		t.Fatalf("UpdateTokens: %v", err)
	}

	ts := NewServerTokenStore(store, "srv", "https://new.example.com")
	_, err := ts.GetToken(context.Background())
	if !errors.Is(err, transport.ErrNoToken) {
		t.Fatalf("GetToken() with mismatched URL error = %v, want transport.ErrNoToken", err)
	}
}

func TestServerTokenStore_RoundTrip(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "mcp-auth.json"))
	ts := NewServerTokenStore(store, "srv", "https://example.com")

	in := &transport.Token{
		AccessToken:  "at-1",
		TokenType:    "Bearer",
		RefreshToken: "rt-1",
		Scope:        "read",
		ExpiresAt:    time.Now().Add(time.Hour).Truncate(time.Second),
	}
	if err := ts.SaveToken(context.Background(), in); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	out, err := ts.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken: %v", err)
	}
	if out.AccessToken != in.AccessToken || out.RefreshToken != in.RefreshToken || out.Scope != in.Scope {
		t.Errorf("GetToken() = %+v, want fields matching %+v", out, in)
	}
	if !out.ExpiresAt.Equal(in.ExpiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", out.ExpiresAt, in.ExpiresAt)
	}
}

func TestServerTokenStore_ExpiresInConvertedToExpiresAt(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "mcp-auth.json"))
	ts := NewServerTokenStore(store, "srv", "https://example.com")

	before := time.Now()
	in := &transport.Token{AccessToken: "at-1", ExpiresIn: 3600}
	if err := ts.SaveToken(context.Background(), in); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	entry, ok, err := store.Get("srv")
	if err != nil || !ok {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	if entry.Tokens.ExpiresAt.IsZero() {
		t.Fatalf("expected ExpiresAt to be computed from ExpiresIn")
	}
	wantMin := before.Add(3599 * time.Second)
	wantMax := before.Add(3601 * time.Second)
	if entry.Tokens.ExpiresAt.Before(wantMin) || entry.Tokens.ExpiresAt.After(wantMax) {
		t.Errorf("ExpiresAt = %v, want within [%v, %v]", entry.Tokens.ExpiresAt, wantMin, wantMax)
	}

	// Round-tripping back out should re-derive a positive ExpiresIn.
	out, err := ts.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken: %v", err)
	}
	if out.ExpiresIn <= 0 {
		t.Errorf("ExpiresIn = %d, want > 0", out.ExpiresIn)
	}
}

func TestServerTokenStore_ContextCancellation(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "mcp-auth.json"))
	ts := NewServerTokenStore(store, "srv", "https://example.com")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := ts.GetToken(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("GetToken(cancelled) error = %v, want context.Canceled", err)
	}
	if err := ts.SaveToken(ctx, &transport.Token{AccessToken: "x"}); !errors.Is(err, context.Canceled) {
		t.Errorf("SaveToken(cancelled) error = %v, want context.Canceled", err)
	}
}
