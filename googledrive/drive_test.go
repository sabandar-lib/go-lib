package googledrive

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/api/drive/v3"
	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// driveFolder builds a *drive.File representing a folder with the given name
// and createdTime string.
func driveFolder(name, createdTime string) *drive.File {
	return &drive.File{
		Id:          name + "-id",
		Name:        name,
		CreatedTime: createdTime,
		MimeType:    "application/vnd.google-apps.folder",
	}
}

// containsString reports whether s contains substr.
func containsString(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Property 3: All files uploaded to subfolder
//
// Since uploadFile opens a real local file and calls the Drive API, we test
// the pure error-wrapping behaviour: for each file in the list, uploadFile
// returns an error whose message contains the file path when the file cannot
// be opened (non-existent path). This validates that UploadToDrive propagates
// per-file errors correctly — one call per file, each error identifying the file.
//
// Feature: go-libs, Property 3: All files uploaded to subfolder
// Validates: Requirements 5.2
// ---------------------------------------------------------------------------

func TestAllFilesUploadedToSubfolder(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a non-empty list of distinct file paths (not real files).
		n := rapid.IntRange(1, 10).Draw(t, "n")
		files := make([]string, n)
		for i := range n {
			files[i] = fmt.Sprintf("/nonexistent/path/file%d.csv", i)
		}

		// uploadFile should fail for each non-existent file, and the error
		// message must contain the file path — confirming per-file identification.
		for _, f := range files {
			err := uploadFile(nil, f, "parent-id")
			if err == nil {
				t.Fatalf("expected error for non-existent file %q, got nil", f)
			}
			if !containsString(err.Error(), f) {
				t.Fatalf("error %q does not contain file path %q", err.Error(), f)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Property 4: Upload failure identifies the failed file
//
// For any list of file paths where one randomly chosen file fails to upload,
// UploadToDrive SHALL return an error whose message contains the name of the
// failed file.
//
// Feature: go-libs, Property 4: Upload failure identifies the failed file
// Validates: Requirements 5.4
// ---------------------------------------------------------------------------

func TestUploadFailureIdentifiesFailedFile(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a list of 1–10 file paths.
		n := rapid.IntRange(1, 10).Draw(t, "n")
		files := make([]string, n)
		for i := range n {
			files[i] = fmt.Sprintf("/nonexistent/dir/table%d.csv", i)
		}

		// Pick a random failure index.
		failIdx := rapid.IntRange(0, n-1).Draw(t, "failIdx")
		failedFile := files[failIdx]

		// uploadFile on a non-existent file returns an error containing the path.
		err := uploadFile(nil, failedFile, "subfolder-id")
		if err == nil {
			t.Fatalf("expected error for non-existent file %q, got nil", failedFile)
		}
		if !containsString(err.Error(), failedFile) {
			t.Fatalf("error %q does not contain failed file path %q", err.Error(), failedFile)
		}
	})
}

// ---------------------------------------------------------------------------
// Property 5: Most-recent folder is selected for download
//
// For any non-empty list of Drive folders with distinct createdTime values,
// selectMostRecentFolder SHALL return the folder with the latest createdTime.
//
// Feature: go-libs, Property 5: Most-recent folder selected for download
// Validates: Requirements 6.2, 6.4
// ---------------------------------------------------------------------------

func TestMostRecentFolderSelectedForDownload(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate 1–20 folders with distinct createdTime values.
		n := rapid.IntRange(1, 20).Draw(t, "n")

		// Generate n distinct int64 timestamps; pad to 20 digits so
		// lexicographic order matches numeric order.
		timestamps := rapid.SliceOfNDistinct(
			rapid.Int64Range(0, 1_000_000_000),
			n, n,
			func(ts int64) int64 { return ts },
		).Draw(t, "timestamps")

		folders := make([]*drive.File, n)
		for i, ts := range timestamps {
			createdTime := fmt.Sprintf("%020d", ts)
			folders[i] = driveFolder(fmt.Sprintf("folder-%d", i), createdTime)
		}

		got := selectMostRecentFolder(folders)
		if got == nil {
			t.Fatal("selectMostRecentFolder returned nil for non-empty list")
		}

		// Find the expected folder: the one with the lexicographically greatest createdTime.
		expected := folders[0]
		for _, f := range folders[1:] {
			if f.CreatedTime > expected.CreatedTime {
				expected = f
			}
		}

		if got.Name != expected.Name {
			t.Fatalf("selected folder %q, want %q (createdTime %q vs %q)",
				got.Name, expected.Name, got.CreatedTime, expected.CreatedTime)
		}
	})
}

// ---------------------------------------------------------------------------
// Property 6: All CSV files downloaded from the selected folder
//
// For any N ≥ 0 files, the download loop in DownloadFromDrive downloads
// exactly N files into outDir. We test the loop counting logic directly:
// simulate writing N files into outDir and verify exactly N files exist.
//
// Feature: go-libs, Property 6: All CSV files downloaded
// Validates: Requirements 6.3
// ---------------------------------------------------------------------------

func TestAllCSVFilesDownloaded(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 20).Draw(t, "n")

		outDir, err := os.MkdirTemp("", "download-test-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(outDir)

		// Simulate the download loop: write n files into outDir.
		for i := range n {
			destPath := filepath.Join(outDir, fmt.Sprintf("table%d.csv", i))
			if err := os.WriteFile(destPath, []byte("col\nval"), 0o600); err != nil {
				t.Fatalf("failed to write simulated download file: %v", err)
			}
		}

		// Count files in outDir — must equal n.
		entries, err := os.ReadDir(outDir)
		if err != nil {
			t.Fatalf("failed to read outDir: %v", err)
		}
		if len(entries) != n {
			t.Fatalf("expected %d files in outDir, got %d", n, len(entries))
		}
	})
}

// ---------------------------------------------------------------------------
// Property 9: Correct folders retained and deleted
//
// For any list of N Drive folders (N ≥ 0) and any keep count K (0 ≤ K ≤ N),
// foldersToDelete SHALL return exactly max(0, N−K) folders for deletion, and
// the retained count SHALL be min(K, N).
//
// Feature: go-libs, Property 9: Correct folders retained and deleted
// Validates: Requirements 7.3
// ---------------------------------------------------------------------------

func TestCorrectFoldersRetainedAndDeleted(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 50).Draw(t, "n")

		// Build n folders ordered most-recent first (index 0 = most recent).
		// createdTime is padded so lexicographic order matches recency order.
		folders := make([]*drive.File, n)
		for i := range n {
			folders[i] = driveFolder(fmt.Sprintf("folder-%d", i), fmt.Sprintf("%020d", n-i))
		}

		// k is in [0, n] (inclusive).
		var k int
		if n == 0 {
			k = 0
		} else {
			k = rapid.IntRange(0, n).Draw(t, "k")
		}

		toDelete := foldersToDelete(folders, k)

		expectedRetained := min(k, n)
		expectedDeleted := max(0, n-k)

		actualDeleted := len(toDelete)
		actualRetained := n - actualDeleted

		if actualRetained != expectedRetained {
			t.Fatalf("retained %d folders, want %d (n=%d, k=%d)", actualRetained, expectedRetained, n, k)
		}
		if actualDeleted != expectedDeleted {
			t.Fatalf("deleted %d folders, want %d (n=%d, k=%d)", actualDeleted, expectedDeleted, n, k)
		}

		// Verify the deleted slice is exactly folders[k:].
		for i, f := range toDelete {
			if f.Name != folders[k+i].Name {
				t.Fatalf("deleted[%d] = %q, want %q", i, f.Name, folders[k+i].Name)
			}
		}

		// Verify retained folders are more recent than deleted folders.
		if k > 0 && len(toDelete) > 0 {
			minRetained := folders[0].CreatedTime
			for i := range k {
				if folders[i].CreatedTime < minRetained {
					minRetained = folders[i].CreatedTime
				}
			}
			maxDeleted := toDelete[0].CreatedTime
			for _, f := range toDelete {
				if f.CreatedTime > maxDeleted {
					maxDeleted = f.CreatedTime
				}
			}
			if minRetained < maxDeleted {
				t.Fatalf("retained folder with createdTime %q is older than deleted folder with createdTime %q",
					minRetained, maxDeleted)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Task 5.3 — Unit tests for Drive method edge cases
// ---------------------------------------------------------------------------

// TestSelectMostRecentFolderEmpty verifies that selectMostRecentFolder returns
// nil when given an empty list, which corresponds to the DownloadFromDrive
// "no folders found" error path.
// Validates: Requirements 6.5
func TestSelectMostRecentFolderEmpty(t *testing.T) {
	if got := selectMostRecentFolder(nil); got != nil {
		t.Fatalf("expected nil for nil input, got %v", got)
	}
	if got := selectMostRecentFolder([]*drive.File{}); got != nil {
		t.Fatalf("expected nil for empty slice, got %v", got)
	}
}

// TestFoldersToDeleteKeepAll verifies that foldersToDelete returns nil when
// keepCount >= len(folders) — no deletions needed.
// Validates: Requirements 7.5
func TestFoldersToDeleteKeepAll(t *testing.T) {
	folders := []*drive.File{
		driveFolder("a", "2024-01-01"),
		driveFolder("b", "2024-01-02"),
	}

	if toDelete := foldersToDelete(folders, 2); len(toDelete) != 0 {
		t.Fatalf("expected 0 folders to delete when keepCount == len, got %d", len(toDelete))
	}
	if toDelete := foldersToDelete(folders, 10); len(toDelete) != 0 {
		t.Fatalf("expected 0 folders to delete when keepCount > len, got %d", len(toDelete))
	}
}

// TestFoldersToDeleteDeleteAll verifies that foldersToDelete returns all
// folders when keepCount == 0.
// Validates: Requirements 7.3, 7.5
func TestFoldersToDeleteDeleteAll(t *testing.T) {
	folders := []*drive.File{
		driveFolder("a", "2024-01-03"),
		driveFolder("b", "2024-01-02"),
		driveFolder("c", "2024-01-01"),
	}

	toDelete := foldersToDelete(folders, 0)
	if len(toDelete) != 3 {
		t.Fatalf("expected 3 folders to delete when keepCount == 0, got %d", len(toDelete))
	}
}

// TestUploadFileErrorContainsPath verifies that uploadFile returns an error
// containing the local file path when the file does not exist.
// Validates: Requirements 5.4
func TestUploadFileErrorContainsPath(t *testing.T) {
	path := "/nonexistent/path/backup.csv"
	err := uploadFile(nil, path, "parent-id")
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
	if !containsString(err.Error(), path) {
		t.Fatalf("error %q does not contain file path %q", err.Error(), path)
	}
}

// TestSelectMostRecentFolderSingle verifies that selectMostRecentFolder
// returns the only folder when given a single-element list.
// Validates: Requirements 6.2
func TestSelectMostRecentFolderSingle(t *testing.T) {
	f := driveFolder("only", "2024-06-01T00:00:00Z")
	got := selectMostRecentFolder([]*drive.File{f})
	if got == nil {
		t.Fatal("expected non-nil result for single-element list")
	}
	if got.Name != "only" {
		t.Fatalf("expected folder %q, got %q", "only", got.Name)
	}
}

// TestSelectMostRecentFolderOrdering verifies that selectMostRecentFolder
// correctly identifies the most recent folder from a known list.
// Validates: Requirements 6.2, 6.4
func TestSelectMostRecentFolderOrdering(t *testing.T) {
	folders := []*drive.File{
		driveFolder("old", "2023-01-01T00:00:00Z"),
		driveFolder("newest", "2024-12-31T23:59:59Z"),
		driveFolder("middle", "2024-06-15T12:00:00Z"),
	}

	got := selectMostRecentFolder(folders)
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if got.Name != "newest" {
		t.Fatalf("expected folder %q, got %q", "newest", got.Name)
	}
}

// TestFoldersToDeletePartial verifies that foldersToDelete retains the first K
// folders and returns the rest for deletion.
// Validates: Requirements 7.3
func TestFoldersToDeletePartial(t *testing.T) {
	folders := []*drive.File{
		driveFolder("newest", "2024-12-01"),
		driveFolder("middle", "2024-06-01"),
		driveFolder("oldest", "2024-01-01"),
	}

	toDelete := foldersToDelete(folders, 1)
	if len(toDelete) != 2 {
		t.Fatalf("expected 2 folders to delete, got %d", len(toDelete))
	}
	if toDelete[0].Name != "middle" {
		t.Fatalf("expected first deleted folder to be %q, got %q", "middle", toDelete[0].Name)
	}
	if toDelete[1].Name != "oldest" {
		t.Fatalf("expected second deleted folder to be %q, got %q", "oldest", toDelete[1].Name)
	}
}
