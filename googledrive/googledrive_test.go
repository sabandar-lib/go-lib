package googledrive

import (
	"testing"
)

// TestNewPanicsWhenTokenFileMissing verifies that New() panics with a
// descriptive message when the token file does not exist.
// This exercises the error path in newPersistentTokenSource.
// Validates: Requirements 2.1, 2.3
func TestNewPanicsWhenTokenFileMissing(t *testing.T) {
	cfg := GoogleDriveConfig{
		GDriveTokenFileDir:        "/tmp/nonexistent-token-xyz.json",
		GDriveClientSecretFileDir: "/tmp/nonexistent-secret-xyz.json",
		GDriveFolderID:            "some-folder-id",
		GDriveBackupCycle:         "daily",
		GDriveRetainMonths:        1,
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected New() to panic when token file is missing, but it did not")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected panic value to be a string, got %T: %v", r, r)
		}
		// The panic message should be descriptive.
		if msg == "" {
			t.Fatal("expected non-empty panic message")
		}
	}()

	New(cfg)
}
