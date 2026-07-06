package databasebackup

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeStorage struct {
	uploaded map[string][]string
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{uploaded: make(map[string][]string)}
}

func (f *fakeStorage) Upload(ctx context.Context, folderName string, files []string) error {
	destDir := filepath.Join(os.TempDir(), "fakestorage", folderName)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	copied := make([]string, len(files))
	for i, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		dest := filepath.Join(destDir, filepath.Base(file))
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return err
		}
		copied[i] = dest
	}
	f.uploaded[folderName] = copied
	return nil
}

func (f *fakeStorage) DownloadLatest(ctx context.Context, outDirPrefix string) (string, error) {
	if len(f.uploaded) == 0 {
		return "", fmt.Errorf("no backups")
	}

	var latest string
	for folder := range f.uploaded {
		if folder > latest {
			latest = folder
		}
	}

	outDir, err := os.MkdirTemp(outDirPrefix, "restored_*")
	if err != nil {
		return "", err
	}

	for _, file := range f.uploaded[latest] {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		dest := filepath.Join(outDir, filepath.Base(file))
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return "", err
		}
	}
	return outDir, nil
}

func (f *fakeStorage) Prune(ctx context.Context, retainCount int) error {
	return nil
}

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}

func TestBackupAll_RoundTrip(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)

	if err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`).Error; err != nil {
		t.Fatalf("create users: %v", err)
	}
	if err := db.Exec(`CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER)`).Error; err != nil {
		t.Fatalf("create orders: %v", err)
	}
	if err := db.Exec(`INSERT INTO users (id, name) VALUES (1, 'alice'), (2, 'bob')`).Error; err != nil {
		t.Fatalf("insert users: %v", err)
	}
	if err := db.Exec(`INSERT INTO orders (id, user_id) VALUES (10, 1), (20, 2)`).Error; err != nil {
		t.Fatalf("insert orders: %v", err)
	}

	outDir := t.TempDir()
	storage := newFakeStorage()
	backup := New(Config{OutDirPrefix: outDir, BatchSize: 10}, storage, db)

	if err := backup.BackupAll(ctx); err != nil {
		t.Fatalf("backup all: %v", err)
	}

	if len(storage.uploaded) != 1 {
		t.Fatalf("expected one uploaded backup, got %d", len(storage.uploaded))
	}

	// Clear tables and restore.
	if err := db.Exec(`DELETE FROM orders`).Error; err != nil {
		t.Fatalf("delete orders: %v", err)
	}
	if err := db.Exec(`DELETE FROM users`).Error; err != nil {
		t.Fatalf("delete users: %v", err)
	}

	if err := backup.RestoreAll(ctx); err != nil {
		t.Fatalf("restore all: %v", err)
	}

	var userCount, orderCount int64
	db.Raw(`SELECT count(*) FROM users`).Scan(&userCount)
	db.Raw(`SELECT count(*) FROM orders`).Scan(&orderCount)

	if userCount != 2 {
		t.Fatalf("expected 2 users, got %d", userCount)
	}
	if orderCount != 2 {
		t.Fatalf("expected 2 orders, got %d", orderCount)
	}
}

func TestBackupAll_TableOrder(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)

	if err := db.Exec(`CREATE TABLE zzz (id INTEGER PRIMARY KEY)`).Error; err != nil {
		t.Fatalf("create zzz: %v", err)
	}
	if err := db.Exec(`CREATE TABLE aaa (id INTEGER PRIMARY KEY)`).Error; err != nil {
		t.Fatalf("create aaa: %v", err)
	}

	outDir := t.TempDir()
	storage := newFakeStorage()
	backup := New(Config{
		OutDirPrefix: outDir,
		TableOrder: map[string]string{
			"zzz": "1",
			"aaa": "2",
		},
	}, storage, db)

	if err := backup.BackupAll(ctx); err != nil {
		t.Fatalf("backup all: %v", err)
	}

	var names []string
	for _, file := range storage.uploaded {
		for _, f := range file {
			names = append(names, filepath.Base(f))
		}
	}
	sort.Strings(names)

	expected := []string{"1_zzz.csv", "2_aaa.csv"}
	for i, exp := range expected {
		if names[i] != exp {
			t.Fatalf("expected %s, got %s", exp, names[i])
		}
	}
}

func TestInsertBatch_WithAndWithoutOnConflict(t *testing.T) {
	db := setupTestDB(t)
	if err := db.Exec(`CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT)`).Error; err != nil {
		t.Fatalf("create items: %v", err)
	}

	headers := []string{"id", "name"}
	rows := [][]string{{"1", "foo"}, {"2", "bar"}}

	tests := []struct {
		name         string
		onConflict   string
		wantConflict bool
	}{
		{
			name:         "without on conflict",
			onConflict:   "",
			wantConflict: false,
		},
		{
			name:         "with on conflict do nothing",
			onConflict:   "ON CONFLICT DO NOTHING",
			wantConflict: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := insertBatch(db, "items", headers, rows, tt.onConflict); err != nil {
				t.Fatalf("insert batch: %v", err)
			}

			var count int64
			db.Raw(`SELECT count(*) FROM items`).Scan(&count)
			if count != 2 {
				t.Fatalf("expected 2 rows, got %d", count)
			}

			// Run again. Without conflict clause this should fail; with it should succeed.
			err := insertBatch(db, "items", headers, rows, tt.onConflict)
			if tt.wantConflict {
				if err != nil {
					t.Fatalf("expected no error with ON CONFLICT, got %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("expected error without ON CONFLICT on duplicate")
				}
			}
		})
	}
}

func TestRestoreAll_FileSortingAndPrefixStripping(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)

	if err := db.Exec(`CREATE TABLE roles (id INTEGER PRIMARY KEY, name TEXT)`).Error; err != nil {
		t.Fatalf("create roles: %v", err)
	}
	if err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, role_id INTEGER)`).Error; err != nil {
		t.Fatalf("create users: %v", err)
	}

	outDir := t.TempDir()
	storage := newFakeStorage()

	// Manually create CSV files with out-of-order names.
	folder := filepath.Join(outDir, "restored_manual")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeCSV(t, filepath.Join(folder, "2_users.csv"), [][]string{{"id", "role_id"}, {"1", "1"}})
	writeCSV(t, filepath.Join(folder, "1_roles.csv"), [][]string{{"id", "name"}, {"1", "admin"}})

	// Override DownloadLatest to return the manual folder.
	storage.uploaded["manual"] = []string{
		filepath.Join(folder, "1_roles.csv"),
		filepath.Join(folder, "2_users.csv"),
	}
	backup := New(Config{OutDirPrefix: outDir, BatchSize: 10}, storage, db)

	if err := backup.RestoreAll(ctx); err != nil {
		t.Fatalf("restore all: %v", err)
	}

	var roleCount, userCount int64
	db.Raw(`SELECT count(*) FROM roles`).Scan(&roleCount)
	db.Raw(`SELECT count(*) FROM users`).Scan(&userCount)

	if roleCount != 1 {
		t.Fatalf("expected 1 role, got %d", roleCount)
	}
	if userCount != 1 {
		t.Fatalf("expected 1 user, got %d", userCount)
	}
}

func writeCSV(t *testing.T, path string, records [][]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create csv: %v", err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.WriteAll(records); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	w.Flush()
}
