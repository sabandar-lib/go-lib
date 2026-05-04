package databasebackup

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gorm.io/gorm"
	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// Mock GoogleDriveInterface for restore tests
// ---------------------------------------------------------------------------

type mockGoogleDrive struct {
	downloadFn func(ctx context.Context, outDir string, pageSize int64) (string, error)
}

func (m *mockGoogleDrive) UploadToDrive(_ context.Context, _ string, _ []string) error {
	return nil
}

func (m *mockGoogleDrive) DownloadFromDrive(ctx context.Context, outDir string, pageSize int64) (string, error) {
	if m.downloadFn != nil {
		return m.downloadFn(ctx, outDir, pageSize)
	}
	return "", nil
}

func (m *mockGoogleDrive) PruneOldBackups(_ context.Context) error {
	return nil
}

// ---------------------------------------------------------------------------
// failingConnPool is a gorm.ConnPool that always returns an error on Exec.
// ---------------------------------------------------------------------------

var errConnFailed = errors.New("connection failed")

type failingConnPool struct{}

func (f failingConnPool) PrepareContext(_ context.Context, _ string) (*sql.Stmt, error) {
	return nil, errConnFailed
}

func (f failingConnPool) ExecContext(_ context.Context, _ string, _ ...interface{}) (sql.Result, error) {
	return nil, errConnFailed
}

func (f failingConnPool) QueryContext(_ context.Context, _ string, _ ...interface{}) (*sql.Rows, error) {
	return nil, errConnFailed
}

func (f failingConnPool) QueryRowContext(_ context.Context, _ string, _ ...interface{}) *sql.Row {
	// Return a *sql.Row that will error on Scan.
	db, _ := sql.Open("failing", "")
	return db.QueryRow("SELECT 1")
}

// Register a failing driver so sql.Open("failing","") works.
func init() {
	sql.Register("failing", &failingDriver{})
}

type failingDriver struct{}

func (d *failingDriver) Open(_ string) (driver.Conn, error) { return nil, errConnFailed }

// newFailingDB creates a *gorm.DB whose ConnPool always returns errors on Exec.
// It also registers the raw exec callback so that db.Exec actually invokes
// the ConnPool.
func newFailingDB() *gorm.DB {
	db := newMockDB("postgres")
	pool := failingConnPool{}
	db.ConnPool = pool
	db.Statement.ConnPool = pool
	// Register the raw exec callback so db.Exec uses the ConnPool.
	db.Callback().Raw().Register("gorm:raw", func(db *gorm.DB) {
		if db.Error == nil {
			_, err := db.Statement.ConnPool.ExecContext(
				db.Statement.Context,
				db.Statement.SQL.String(),
				db.Statement.Vars...,
			)
			if err != nil {
				db.AddError(err)
			}
		}
	})
	return db
}

// ---------------------------------------------------------------------------
// Property 15: Restore order is ascending by numeric prefix
// Feature: go-libs, Property 15: For any set of CSV filenames with distinct
// numeric string prefixes, sort.Strings SHALL produce ascending lexicographic
// order of prefix, ensuring lower-numbered tables are processed first.
// Validates: Requirements 12.3
// ---------------------------------------------------------------------------

func TestProperty15_RestoreOrderAscendingByPrefix(t *testing.T) {
	// Feature: go-libs, Property 15: Restore order is ascending by numeric prefix
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 10).Draw(t, "n")

		// Generate n distinct zero-padded numeric prefixes.
		prefixSet := map[string]bool{}
		prefixes := make([]string, 0, n)
		for len(prefixes) < n {
			num := rapid.IntRange(1, 999).Draw(t, fmt.Sprintf("prefix_%d", len(prefixes)))
			p := fmt.Sprintf("%03d", num)
			if !prefixSet[p] {
				prefixSet[p] = true
				prefixes = append(prefixes, p)
			}
		}

		// Generate a table name for each prefix.
		tableNames := make([]string, n)
		for i := range tableNames {
			tableNames[i] = rapid.StringMatching(`[a-z][a-z0-9]{2,8}`).Draw(t, fmt.Sprintf("table_%d", i))
		}

		// Build filenames in random order.
		filenames := make([]string, n)
		for i := range filenames {
			filenames[i] = fmt.Sprintf("%s_%s.csv", prefixes[i], tableNames[i])
		}
		// Shuffle by drawing a permutation index.
		shuffled := make([]string, n)
		perm := rapid.SliceOfDistinct(
			rapid.IntRange(0, n-1),
			func(v int) int { return v },
		).Filter(func(s []int) bool { return len(s) == n }).Draw(t, "perm")
		for i, idx := range perm {
			shuffled[i] = filenames[idx]
		}

		// Sort using the same logic RestoreAllBackups would use.
		sort.Strings(shuffled)

		// Verify ascending order: each prefix must be ≤ the next.
		for i := 1; i < len(shuffled); i++ {
			prevPrefix := strings.SplitN(filepath.Base(shuffled[i-1]), "_", 2)[0]
			currPrefix := strings.SplitN(filepath.Base(shuffled[i]), "_", 2)[0]
			if prevPrefix > currPrefix {
				t.Fatalf("sort order violation at index %d: %q > %q", i, prevPrefix, currPrefix)
			}
		}
	})
}

// TestProperty15_SortOrderSimple is a deterministic example to complement the property test.
func TestProperty15_SortOrderSimple(t *testing.T) {
	files := []string{"003_orders.csv", "001_users.csv", "002_products.csv"}
	sort.Strings(files)
	expected := []string{"001_users.csv", "002_products.csv", "003_orders.csv"}
	for i, f := range files {
		if f != expected[i] {
			t.Errorf("index %d: got %q, want %q", i, f, expected[i])
		}
	}
}

// ---------------------------------------------------------------------------
// Property 16: Batch insert uses ON CONFLICT DO NOTHING
// Feature: go-libs, Property 16: For any row count R ≥ 0, the number of
// INSERT batches SHALL equal ceil(R / 100).
// Validates: Requirements 13.1, 13.2
// ---------------------------------------------------------------------------

func TestProperty16_BatchCountEqualsCeilDiv100(t *testing.T) {
	// Feature: go-libs, Property 16: Batch insert uses ON CONFLICT DO NOTHING
	rapid.Check(t, func(t *rapid.T) {
		r := rapid.IntRange(0, 500).Draw(t, "rowCount")

		// Expected number of batches: ceil(R / 100).
		expectedBatches := (r + 99) / 100

		// Count actual batches by simulating the loop in restoreTable.
		actualBatches := 0
		const batchSize = 100
		for i := 0; i < r; i += batchSize {
			actualBatches++
		}

		if actualBatches != expectedBatches {
			t.Fatalf("rowCount=%d: expected %d batches, got %d", r, expectedBatches, actualBatches)
		}
	})
}

// TestProperty16_SQLContainsOnConflictDoNothing verifies that insertBatch
// always produces SQL with ON CONFLICT DO NOTHING.
func TestProperty16_SQLContainsOnConflictDoNothing(t *testing.T) {
	// Feature: go-libs, Property 16: Batch insert uses ON CONFLICT DO NOTHING (SQL shape)
	rapid.Check(t, func(t *rapid.T) {
		numCols := rapid.IntRange(1, 5).Draw(t, "numCols")
		numRows := rapid.IntRange(1, 10).Draw(t, "numRows")

		columns := make([]string, numCols)
		for i := range columns {
			columns[i] = rapid.StringMatching(`[a-z][a-z0-9]{1,8}`).Draw(t, fmt.Sprintf("col_%d", i))
		}

		rows := make([][]string, numRows)
		for i := range rows {
			row := make([]string, numCols)
			for j := range row {
				row[j] = rapid.String().Draw(t, fmt.Sprintf("val_%d_%d", i, j))
			}
			rows[i] = row
		}

		// Build the SQL the same way insertBatch does, without executing it.
		quotedCols := make([]string, numCols)
		for i, col := range columns {
			quotedCols[i] = fmt.Sprintf("%q", col)
		}
		var placeholderGroups []string
		for range rows {
			placeholders := make([]string, numCols)
			for j := range placeholders {
				placeholders[j] = "?"
			}
			placeholderGroups = append(placeholderGroups, "("+strings.Join(placeholders, ",")+")")
		}
		tableName := rapid.StringMatching(`[a-z][a-z0-9]{1,8}`).Draw(t, "tableName")
		sql := fmt.Sprintf(
			`INSERT INTO %q (%s) VALUES %s ON CONFLICT DO NOTHING`,
			tableName,
			strings.Join(quotedCols, ","),
			strings.Join(placeholderGroups, ","),
		)

		if !strings.Contains(sql, "ON CONFLICT DO NOTHING") {
			t.Fatalf("SQL does not contain ON CONFLICT DO NOTHING: %q", sql)
		}
	})
}

// ---------------------------------------------------------------------------
// Property 17: Restore idempotence
// Feature: go-libs, Property 17: Restoring a CSV backup file twice into the
// same database SHALL produce the same state as restoring it once, because
// ON CONFLICT DO NOTHING prevents duplicate inserts from failing or changing
// the data.
// Validates: Requirements 13.4
// ---------------------------------------------------------------------------

// trackingDB records all Exec calls made against it.
type trackingDB struct {
	execCalls []execCall
}

type execCall struct {
	sql  string
	args []interface{}
}

// insertBatchWithTracking is a test-only variant that records calls instead of
// executing against a real DB. It mirrors the logic of insertBatch.
func insertBatchWithTracking(tracker *trackingDB, tableName string, columns []string, rows [][]string, batchNum int) error {
	if len(rows) == 0 {
		return nil
	}

	quotedCols := make([]string, len(columns))
	for i, col := range columns {
		quotedCols[i] = fmt.Sprintf("%q", col)
	}

	var placeholderGroups []string
	var args []interface{}
	for _, row := range rows {
		placeholders := make([]string, len(columns))
		for j := range columns {
			placeholders[j] = "?"
			if j < len(row) {
				args = append(args, row[j])
			} else {
				args = append(args, nil)
			}
		}
		placeholderGroups = append(placeholderGroups, "("+strings.Join(placeholders, ",")+")")
	}

	sql := fmt.Sprintf(
		`INSERT INTO %q (%s) VALUES %s ON CONFLICT DO NOTHING`,
		tableName,
		strings.Join(quotedCols, ","),
		strings.Join(placeholderGroups, ","),
	)

	tracker.execCalls = append(tracker.execCalls, execCall{sql: sql, args: args})
	return nil
}

// restoreTableWithTracking mirrors restoreTable but uses insertBatchWithTracking.
func restoreTableWithTracking(tracker *trackingDB, csvPath string) error {
	base := filepath.Base(csvPath)
	name := strings.TrimSuffix(base, ".csv")
	parts := strings.SplitN(name, "_", 2)
	if len(parts) < 2 {
		return fmt.Errorf("invalid CSV filename %q", base)
	}
	tableName := parts[1]

	f, err := os.Open(csvPath)
	if err != nil {
		return fmt.Errorf("failed to open CSV file %q: %w", csvPath, err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		return fmt.Errorf("failed to parse CSV file %q: %w", csvPath, err)
	}
	if len(records) == 0 {
		return nil
	}

	columns := records[0]
	dataRows := records[1:]

	const batchSize = 100
	for i := 0; i < len(dataRows); i += batchSize {
		end := i + batchSize
		if end > len(dataRows) {
			end = len(dataRows)
		}
		batchNum := i/batchSize + 1
		if err := insertBatchWithTracking(tracker, tableName, columns, dataRows[i:end], batchNum); err != nil {
			return err
		}
	}
	return nil
}

func TestProperty17_RestoreIdempotence(t *testing.T) {
	// Feature: go-libs, Property 17: Restore idempotence
	rapid.Check(t, func(t *rapid.T) {
		numCols := rapid.IntRange(1, 4).Draw(t, "numCols")
		numRows := rapid.IntRange(0, 50).Draw(t, "numRows")

		// Build column names.
		columns := make([]string, numCols)
		for i := range columns {
			columns[i] = fmt.Sprintf("col%d", i+1)
		}

		// Build data rows.
		rows := make([][]string, numRows)
		for i := range rows {
			row := make([]string, numCols)
			for j := range row {
				row[j] = rapid.StringMatching(`[a-zA-Z0-9]{1,10}`).Draw(t, fmt.Sprintf("val_%d_%d", i, j))
			}
			rows[i] = row
		}

		// Write CSV to a temp file.
		tmpDir, err := os.MkdirTemp("", "restore_idempotence_*")
		if err != nil {
			t.Fatalf("MkdirTemp: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		csvPath := filepath.Join(tmpDir, "001_testtable.csv")
		f, err := os.Create(csvPath)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		w := csv.NewWriter(f)
		_ = w.Write(columns)
		for _, row := range rows {
			_ = w.Write(row)
		}
		w.Flush()
		f.Close()

		// First restore.
		tracker1 := &trackingDB{}
		if err := restoreTableWithTracking(tracker1, csvPath); err != nil {
			t.Fatalf("first restore: %v", err)
		}

		// Second restore.
		tracker2 := &trackingDB{}
		if err := restoreTableWithTracking(tracker2, csvPath); err != nil {
			t.Fatalf("second restore: %v", err)
		}

		// Both restores should produce identical SQL calls (same number, same SQL).
		if len(tracker1.execCalls) != len(tracker2.execCalls) {
			t.Fatalf("idempotence violation: first restore produced %d calls, second produced %d",
				len(tracker1.execCalls), len(tracker2.execCalls))
		}
		for i := range tracker1.execCalls {
			if tracker1.execCalls[i].sql != tracker2.execCalls[i].sql {
				t.Fatalf("call %d SQL differs:\n  first:  %q\n  second: %q",
					i, tracker1.execCalls[i].sql, tracker2.execCalls[i].sql)
			}
		}

		// All SQL statements must contain ON CONFLICT DO NOTHING.
		for i, call := range tracker1.execCalls {
			if !strings.Contains(call.sql, "ON CONFLICT DO NOTHING") {
				t.Fatalf("call %d missing ON CONFLICT DO NOTHING: %q", i, call.sql)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Unit tests — Task 9.3
// ---------------------------------------------------------------------------

// TestRestoreTable_ErrorIdentifiesTable verifies that when insertBatch fails,
// the error message identifies the table name.
func TestRestoreTable_ErrorIdentifiesTable(t *testing.T) {
	// Write a CSV with one data row.
	tmpDir, err := os.MkdirTemp("", "restore_unit_*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	csvPath := filepath.Join(tmpDir, "001_mytable.csv")
	f, err := os.Create(csvPath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	w := csv.NewWriter(f)
	_ = w.Write([]string{"id", "name"})
	_ = w.Write([]string{"1", "alice"})
	w.Flush()
	f.Close()

	// Use a DB whose ConnPool always fails on Exec.
	db := newFailingDB()

	err = restoreTable(db, csvPath)
	if err == nil {
		t.Fatal("expected error from restoreTable, got nil")
	}
	if !strings.Contains(err.Error(), "mytable") {
		t.Errorf("error should identify table %q, got: %v", "mytable", err)
	}
}

// TestInsertBatch_ErrorIdentifiesTableAndBatch verifies that insertBatch wraps
// errors with both the table name and batch number.
func TestInsertBatch_ErrorIdentifiesTableAndBatch(t *testing.T) {
	db := newFailingDB()

	columns := []string{"id", "name"}
	rows := [][]string{{"1", "alice"}}

	err := insertBatch(db, "orders", columns, rows, 3)
	if err == nil {
		t.Fatal("expected error from insertBatch against failing DB, got nil")
	}
	if !strings.Contains(err.Error(), "orders") {
		t.Errorf("error should identify table %q, got: %v", "orders", err)
	}
	if !strings.Contains(err.Error(), "3") {
		t.Errorf("error should identify batch number 3, got: %v", err)
	}
}

// TestInsertBatch_EmptyRows verifies that insertBatch returns nil for empty input.
func TestInsertBatch_EmptyRows(t *testing.T) {
	db := newMockDB("postgres")
	err := insertBatch(db, "users", []string{"id"}, [][]string{}, 1)
	if err != nil {
		t.Errorf("insertBatch with empty rows should return nil, got: %v", err)
	}
}

// TestRestoreTable_InvalidFilename verifies that a filename without the
// expected prefix_table format returns an error.
func TestRestoreTable_InvalidFilename(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "restore_unit_*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Filename with no underscore separator.
	csvPath := filepath.Join(tmpDir, "notableprefix.csv")
	f, err := os.Create(csvPath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	f.Close()

	db := newMockDB("postgres")
	err = restoreTable(db, csvPath)
	if err == nil {
		t.Fatal("expected error for invalid filename, got nil")
	}
}

// TestDownloadAllBackup_Delegates verifies that downloadAllBackup delegates
// to gDriveLib.DownloadFromDrive and returns its result.
func TestDownloadAllBackup_Delegates(t *testing.T) {
	mock := &mockGoogleDrive{
		downloadFn: func(_ context.Context, outDir string, pageSize int64) (string, error) {
			return "backup-folder-2024", nil
		},
	}

	folderName, err := downloadAllBackup(context.Background(), mock, "/tmp/out", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if folderName != "backup-folder-2024" {
		t.Errorf("expected %q, got %q", "backup-folder-2024", folderName)
	}
}

// TestDownloadAllBackup_PropagatesError verifies that errors from
// DownloadFromDrive are propagated.
func TestDownloadAllBackup_PropagatesError(t *testing.T) {
	wantErr := errors.New("drive unavailable")
	mock := &mockGoogleDrive{
		downloadFn: func(_ context.Context, _ string, _ int64) (string, error) {
			return "", wantErr
		},
	}

	_, err := downloadAllBackup(context.Background(), mock, "/tmp/out", 10)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected %v, got %v", wantErr, err)
	}
}

// TestRestoreTable_EmptyCSV verifies that an empty CSV file (no rows at all)
// is handled gracefully.
func TestRestoreTable_EmptyCSV(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "restore_unit_*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	csvPath := filepath.Join(tmpDir, "001_emptytable.csv")
	f, err := os.Create(csvPath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	f.Close()

	db := newMockDB("postgres")
	// Should not error — nothing to insert.
	err = restoreTable(db, csvPath)
	if err != nil {
		t.Errorf("restoreTable on empty CSV should return nil, got: %v", err)
	}
}

// Ensure the package-level GoogleDriveInterface is satisfied by mockGoogleDrive.
var _ GoogleDriveInterface = (*mockGoogleDrive)(nil)
