package googledrive

import (
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"
	"pgregory.net/rapid"
)

// mockTokenSource is a simple oauth2.TokenSource that returns a predetermined token.
type mockTokenSource struct {
	mu    sync.Mutex
	token *oauth2.Token
}

func (m *mockTokenSource) Token() (*oauth2.Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.token, nil
}

// drawToken generates a random oauth2.Token using rapid generators.
func drawToken(t *rapid.T, label string) oauth2.Token {
	accessToken := rapid.String().Draw(t, label+"_access")
	refreshToken := rapid.String().Draw(t, label+"_refresh")
	ns := rapid.Int64Range(0, 1e15).Draw(t, label+"_expiry_ns")
	expiry := time.Unix(0, ns).UTC()
	return oauth2.Token{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		Expiry:       expiry,
	}
}

// Feature: go-libs, Property 1: Token load round-trip
// Validates: Requirements 3.1
func TestTokenLoadRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		tok := drawToken(t, "tok")

		// Write the token to a temp file.
		f, err := os.CreateTemp("", "token-*.json")
		if err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}
		defer os.Remove(f.Name())
		f.Close()

		if err := writeTokenFile(f.Name(), &tok); err != nil {
			t.Fatalf("writeTokenFile failed: %v", err)
		}

		// Read it back and unmarshal.
		data, err := os.ReadFile(f.Name())
		if err != nil {
			t.Fatalf("failed to read token file: %v", err)
		}

		var got oauth2.Token
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("failed to unmarshal token: %v", err)
		}

		// Assert the round-trip preserves AccessToken, RefreshToken, and Expiry.
		if got.AccessToken != tok.AccessToken {
			t.Fatalf("AccessToken mismatch: got %q, want %q", got.AccessToken, tok.AccessToken)
		}
		if got.RefreshToken != tok.RefreshToken {
			t.Fatalf("RefreshToken mismatch: got %q, want %q", got.RefreshToken, tok.RefreshToken)
		}
		if !got.Expiry.Equal(tok.Expiry) {
			t.Fatalf("Expiry mismatch: got %v, want %v", got.Expiry, tok.Expiry)
		}
	})
}

// Feature: go-libs, Property 2: Token persistence after refresh
// Validates: Requirements 3.2
func TestTokenPersistenceAfterRefresh(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		original := drawToken(t, "original")
		refreshed := drawToken(t, "refreshed")

		// Ensure the refreshed token has a different Expiry so the
		// persistentTokenSource detects a refresh.
		for refreshed.Expiry.Equal(original.Expiry) {
			ns := rapid.Int64Range(0, 1e15).Draw(t, "refreshed_expiry_retry")
			refreshed.Expiry = time.Unix(0, ns).UTC()
		}

		// Write the original token to a temp file.
		f, err := os.CreateTemp("", "token-*.json")
		if err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}
		defer os.Remove(f.Name())
		f.Close()

		if err := writeTokenFile(f.Name(), &original); err != nil {
			t.Fatalf("writeTokenFile failed: %v", err)
		}

		// Build a persistentTokenSource backed by a mock that returns the refreshed token.
		mock := &mockTokenSource{token: &refreshed}
		pts := &persistentTokenSource{
			tokenFile: f.Name(),
			source:    mock,
			lastToken: &original,
		}

		// Call Token() — this should detect the expiry change and persist the refreshed token.
		got, err := pts.Token()
		if err != nil {
			t.Fatalf("Token() returned error: %v", err)
		}

		// Verify the returned token matches the refreshed token.
		if got.AccessToken != refreshed.AccessToken {
			t.Fatalf("returned AccessToken mismatch: got %q, want %q", got.AccessToken, refreshed.AccessToken)
		}

		// Read the token file and verify it was updated with the refreshed values.
		data, err := os.ReadFile(f.Name())
		if err != nil {
			t.Fatalf("failed to read token file after refresh: %v", err)
		}

		var persisted oauth2.Token
		if err := json.Unmarshal(data, &persisted); err != nil {
			t.Fatalf("failed to unmarshal persisted token: %v", err)
		}

		if persisted.AccessToken != refreshed.AccessToken {
			t.Fatalf("persisted AccessToken mismatch: got %q, want %q", persisted.AccessToken, refreshed.AccessToken)
		}
		if persisted.RefreshToken != refreshed.RefreshToken {
			t.Fatalf("persisted RefreshToken mismatch: got %q, want %q", persisted.RefreshToken, refreshed.RefreshToken)
		}
		if !persisted.Expiry.Equal(refreshed.Expiry) {
			t.Fatalf("persisted Expiry mismatch: got %v, want %v", persisted.Expiry, refreshed.Expiry)
		}
	})
}

// TestNewPersistentTokenSourceMissingFile verifies that newPersistentTokenSource
// returns a non-nil error when the token file does not exist.
// Validates: Requirements 3.4
func TestNewPersistentTokenSourceMissingFile(t *testing.T) {
	cfg := GoogleDriveConfig{
		GDriveTokenFileDir:        "/tmp/nonexistent-token-file-xyz.json",
		GDriveClientSecretFileDir: "/tmp/nonexistent-secret-file-xyz.json",
	}

	_, err := newPersistentTokenSource(cfg)
	if err == nil {
		t.Fatal("expected non-nil error when token file does not exist, got nil")
	}
}
