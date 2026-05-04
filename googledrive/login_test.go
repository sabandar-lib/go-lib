package googledrive

import (
	"strings"
	"testing"
)

// TestLoginErrorWhenClientSecretMissing verifies that Login() returns a
// non-nil error when the client secret file does not exist.
// Validates: Requirements 4.4
func TestLoginErrorWhenClientSecretMissing(t *testing.T) {
	cfg := GoogleDriveConfig{
		GDriveClientSecretFileDir: "/tmp/nonexistent-client-secret-xyz.json",
		GDriveTokenFileDir:        "/tmp/nonexistent-token-xyz.json",
	}

	err := Login(cfg)
	if err == nil {
		t.Fatal("expected non-nil error when client secret file does not exist, got nil")
	}
}

// TestLoginErrorMessageContainsClientSecret verifies that the error returned
// by Login() when the client secret file is missing contains a meaningful
// description (not just a bare OS error).
// Validates: Requirements 4.4
func TestLoginErrorMessageContainsClientSecret(t *testing.T) {
	cfg := GoogleDriveConfig{
		GDriveClientSecretFileDir: "/tmp/nonexistent-client-secret-xyz.json",
		GDriveTokenFileDir:        "/tmp/nonexistent-token-xyz.json",
	}

	err := Login(cfg)
	if err == nil {
		t.Fatal("expected non-nil error, got nil")
	}

	// The error should mention the client secret context.
	if !strings.Contains(err.Error(), "client secret") {
		t.Fatalf("error message %q does not mention 'client secret'", err.Error())
	}
}
