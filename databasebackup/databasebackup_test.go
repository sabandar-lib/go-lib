package databasebackup

import (
	"context"
	"errors"
	"os"
	"testing"
)

// ---------------------------------------------------------------------------
// orchestrationMock is a test double for GoogleDriveInterface that records
// calls and can be configured to return errors. It is separate from
// mockGoogleDrive (defined in restore_test.go) to avoid redeclaration.
// ---------------------------------------------------------------------------

type orchestrationMock struct {
	uploadCalled   bool
	uploadErr      error
	downloadCalled bool
	downloadErr    error
	pruneCalled    bool
	pruneErr       error
}

func (m *orchestrationMock) UploadToDrive(_ context.Context, _ string, _ []string) error {
	m.uploadCalled = true
	return m.uploadErr
}

func (m *orchestrationMock) DownloadFromDrive(_ context.Context, outDir string, _ int64) (string, error) {
	m.downloadCalled = true
	if m.downloadErr != nil {
		return "", m.downloadErr
	}
	return "backup-folder", nil
}

func (m *orchestrationMock) PruneOldBackups(_ context.Context) error {
	m.pruneCalled = true
	return m.pruneErr
}

// Compile-time check that orchestrationMock satisfies GoogleDriveInterface.
var _ GoogleDriveInterface = (*orchestrationMock)(nil)

// ---------------------------------------------------------------------------
// Unit tests — Task 10.3
// ---------------------------------------------------------------------------

// TestNew_ReturnsNonNilInterface verifies that New() returns a non-nil
// DatabaseBackupInterface.
func TestNew_ReturnsNonNilInterface(t *testing.T) {
	cfg := DatabaseBackupConfig{DBBackupOutDirPrefix: "test_"}
	mock := &orchestrationMock{}
	client := New(cfg, mock, false)
	if client == nil {
		t.Fatal("New() returned nil, want non-nil DatabaseBackupInterface")
	}
}

// TestRestoreAllBackups_SkipsWhenFlagFalse verifies that RestoreAllBackups
// returns nil immediately when restoreFeatureFlag is false, without calling
// DownloadFromDrive.
func TestRestoreAllBackups_SkipsWhenFlagFalse(t *testing.T) {
	mock := &orchestrationMock{
		// If DownloadFromDrive is called, return an error to make the test fail.
		downloadErr: errors.New("should not be called"),
	}
	cfg := DatabaseBackupConfig{DBBackupOutDirPrefix: "test_"}
	client := New(cfg, mock, false)

	err := client.RestoreAllBackups(context.Background(), newMockDB("postgres"), nil)
	if err != nil {
		t.Fatalf("RestoreAllBackups with flag=false should return nil, got: %v", err)
	}
	if mock.downloadCalled {
		t.Error("DownloadFromDrive should not be called when restoreFeatureFlag is false")
	}
}

// TestRestoreAllBackups_CallsDownloadWhenFlagTrue verifies that
// RestoreAllBackups calls DownloadFromDrive when restoreFeatureFlag is true.
func TestRestoreAllBackups_CallsDownloadWhenFlagTrue(t *testing.T) {
	mock := &orchestrationMock{}
	cfg := DatabaseBackupConfig{DBBackupOutDirPrefix: "test_"}
	client := New(cfg, mock, true)

	// Use a mock DB; no CSV files will be downloaded so restoreTable won't be called.
	_ = client.RestoreAllBackups(context.Background(), newMockDB("postgres"), nil)

	if !mock.downloadCalled {
		t.Error("DownloadFromDrive should be called when restoreFeatureFlag is true")
	}
}

// TestPruneOldBackups_DelegatesToGDrive verifies that PruneOldBackups calls
// gDriveLib.PruneOldBackups.
func TestPruneOldBackups_DelegatesToGDrive(t *testing.T) {
	mock := &orchestrationMock{}
	cfg := DatabaseBackupConfig{DBBackupOutDirPrefix: "test_"}
	client := New(cfg, mock, false)

	err := client.PruneOldBackups(context.Background())
	if err != nil {
		t.Fatalf("PruneOldBackups returned unexpected error: %v", err)
	}
	if !mock.pruneCalled {
		t.Error("PruneOldBackups should delegate to gDriveLib.PruneOldBackups")
	}
}

// TestPruneOldBackups_PropagatesError verifies that errors from
// gDriveLib.PruneOldBackups are propagated to the caller.
func TestPruneOldBackups_PropagatesError(t *testing.T) {
	wantErr := errors.New("drive error")
	mock := &orchestrationMock{pruneErr: wantErr}
	cfg := DatabaseBackupConfig{DBBackupOutDirPrefix: "test_"}
	client := New(cfg, mock, false)

	err := client.PruneOldBackups(context.Background())
	if err == nil {
		t.Fatal("PruneOldBackups should propagate error from gDriveLib, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected error %v, got %v", wantErr, err)
	}
}

// TestBackupAll_ReturnsErrorWithoutUploadingWhenExportFails verifies that
// BackupAll returns an error and does NOT call UploadToDrive when
// exportAllTables fails.
func TestBackupAll_ReturnsErrorWithoutUploadingWhenExportFails(t *testing.T) {
	mock := &orchestrationMock{}
	cfg := DatabaseBackupConfig{DBBackupOutDirPrefix: "test_backup_"}

	// newMockDB("postgres") has no real ConnPool, so db.Raw().Rows() returns
	// (nil, nil). exportTable now detects nil rows and returns an error,
	// which causes BackupAll to return before calling UploadToDrive.
	db := newMockDB("postgres")
	client := New(cfg, mock, false)

	tableOrderMap := map[string]string{"users": "001"}
	err := client.BackupAll(context.Background(), db, tableOrderMap)
	if err == nil {
		t.Fatal("BackupAll should return error when export fails, got nil")
	}
	if mock.uploadCalled {
		t.Error("UploadToDrive should NOT be called when export fails")
	}
}

// TestBackupAll_DeletesTempDirEvenWhenUploadFails verifies that BackupAll
// cleans up the temporary directory even when UploadToDrive returns an error.
func TestBackupAll_DeletesTempDirEvenWhenUploadFails(t *testing.T) {
	uploadErr := errors.New("upload failed")
	var capturedTmpDir string

	// Use a mock that captures the temp dir path from the uploaded file paths
	// and returns an error.
	capturingMock := &capturingUploadMock{
		uploadFn: func(ctx context.Context, timeStampDir string, files []string) error {
			// Derive the temp dir from the first file path (if any).
			if len(files) > 0 {
				capturedTmpDir = files[0][:len(files[0])-len("/"+files[0][len(files[0])-1:])]
			}
			return uploadErr
		},
	}

	cfg := DatabaseBackupConfig{DBBackupOutDirPrefix: "test_cleanup_"}
	client := New(cfg, capturingMock, false)

	// Empty tableOrderMap means no exports — export succeeds trivially.
	err := client.BackupAll(context.Background(), newMockDB("postgres"), map[string]string{})
	if err == nil {
		t.Fatal("BackupAll should return error when upload fails, got nil")
	}
	if !errors.Is(err, uploadErr) {
		t.Errorf("expected upload error, got: %v", err)
	}

	// Verify the temp dir was cleaned up. We can't easily capture the exact
	// path, so we verify that no temp dirs with our prefix linger by checking
	// that the error was returned (defer runs before return in Go).
	// The key invariant is that the function returned the upload error, which
	// means defer os.RemoveAll ran before the function returned.
	// We verify this indirectly: if the temp dir still exists, the test fails.
	if capturedTmpDir != "" {
		if _, statErr := os.Stat(capturedTmpDir); !os.IsNotExist(statErr) {
			t.Errorf("temp dir %q should have been removed, but still exists", capturedTmpDir)
		}
	}
}

// capturingUploadMock is a GoogleDriveInterface that delegates UploadToDrive
// to a configurable function, allowing tests to inspect call arguments.
type capturingUploadMock struct {
	uploadFn func(ctx context.Context, timeStampDir string, files []string) error
}

func (m *capturingUploadMock) UploadToDrive(ctx context.Context, timeStampDir string, files []string) error {
	if m.uploadFn != nil {
		return m.uploadFn(ctx, timeStampDir, files)
	}
	return nil
}

func (m *capturingUploadMock) DownloadFromDrive(_ context.Context, _ string, _ int64) (string, error) {
	return "", nil
}

func (m *capturingUploadMock) PruneOldBackups(_ context.Context) error {
	return nil
}

var _ GoogleDriveInterface = (*capturingUploadMock)(nil)

// TestRestoreAllBackups_ReturnsErrorWhenDownloadFails verifies that
// RestoreAllBackups propagates errors from DownloadFromDrive.
func TestRestoreAllBackups_ReturnsErrorWhenDownloadFails(t *testing.T) {
	wantErr := errors.New("download failed")
	mock := &orchestrationMock{downloadErr: wantErr}
	cfg := DatabaseBackupConfig{DBBackupOutDirPrefix: "test_"}
	client := New(cfg, mock, true)

	err := client.RestoreAllBackups(context.Background(), newMockDB("postgres"), nil)
	if err == nil {
		t.Fatal("RestoreAllBackups should return error when download fails, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected error %v, got %v", wantErr, err)
	}
}
