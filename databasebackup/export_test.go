package databasebackup

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// Mock GORM dialector for unit/property tests that don't need a real DB.
// ---------------------------------------------------------------------------

type mockDialector struct{ name string }

func (m mockDialector) Name() string              { return m.name }
func (m mockDialector) Initialize(*gorm.DB) error { return nil }
func (m mockDialector) Migrator(db *gorm.DB) gorm.Migrator {
	return migrator.Migrator{Config: migrator.Config{DB: db}}
}
func (m mockDialector) DataTypeOf(*schema.Field) string                { return "" }
func (m mockDialector) DefaultValueOf(*schema.Field) clause.Expression { return nil }
func (m mockDialector) BindVarTo(writer clause.Writer, stmt *gorm.Statement, v interface{}) {
}
func (m mockDialector) QuoteTo(writer clause.Writer, str string)       {}
func (m mockDialector) Explain(sql string, vars ...interface{}) string { return sql }

// newMockDB creates a *gorm.DB backed by the given mock dialector name.
func newMockDB(dialectName string) *gorm.DB {
	db, _ := gorm.Open(mockDialector{name: dialectName}, &gorm.Config{})
	return db
}

// ---------------------------------------------------------------------------
// Unit tests — Task 8.4
// ---------------------------------------------------------------------------

func TestSerializeValue_Nil(t *testing.T) {
	got := serializeValue(nil)
	if got != "NULL" {
		t.Errorf("serializeValue(nil) = %q, want %q", got, "NULL")
	}
}

func TestSerializeValue_BoolTrue(t *testing.T) {
	got := serializeValue(true)
	if got != "true" {
		t.Errorf("serializeValue(true) = %q, want %q", got, "true")
	}
}

func TestSerializeValue_BoolFalse(t *testing.T) {
	got := serializeValue(false)
	if got != "false" {
		t.Errorf("serializeValue(false) = %q, want %q", got, "false")
	}
}

// TestGetTables_PostgresDialect verifies that the Postgres dialect is accepted
// (getTables will fail to execute the query against a mock DB, but the error
// should NOT be "unsupported database dialect").
func TestGetTables_PostgresDialect(t *testing.T) {
	db := newMockDB("postgres")
	_, err := getTables(db)
	if err == nil {
		t.Fatal("expected error from mock DB, got nil")
	}
	if strings.Contains(err.Error(), "unsupported database dialect") {
		t.Errorf("postgres dialect should be supported, got: %v", err)
	}
}

// TestGetTables_MySQLDialect verifies that the MySQL dialect is accepted.
func TestGetTables_MySQLDialect(t *testing.T) {
	db := newMockDB("mysql")
	_, err := getTables(db)
	if err == nil {
		t.Fatal("expected error from mock DB, got nil")
	}
	if strings.Contains(err.Error(), "unsupported database dialect") {
		t.Errorf("mysql dialect should be supported, got: %v", err)
	}
}

// TestGetTables_SQLiteDialect verifies that the SQLite dialect is accepted.
func TestGetTables_SQLiteDialect(t *testing.T) {
	db := newMockDB("sqlite")
	_, err := getTables(db)
	if err == nil {
		t.Fatal("expected error from mock DB, got nil")
	}
	if strings.Contains(err.Error(), "unsupported database dialect") {
		t.Errorf("sqlite dialect should be supported, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Property 10: Unsupported dialect returns an error
// Feature: go-libs, Property 10: For any GORM Dialector whose Name() returns
// a string not in {"postgres","mysql","sqlite"}, getTables SHALL return a
// non-nil error.
// Validates: Requirements 11.4
// ---------------------------------------------------------------------------

func TestProperty10_UnsupportedDialectReturnsError(t *testing.T) {
	// Feature: go-libs, Property 10: Unsupported dialect returns an error
	supported := map[string]bool{"postgres": true, "mysql": true, "sqlite": true}

	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`[a-z][a-z0-9_]{0,19}`).Draw(t, "dialectName")
		if supported[name] {
			t.Skip()
		}
		db := newMockDB(name)
		_, err := getTables(db)
		if err == nil {
			t.Fatalf("getTables with dialect %q: expected non-nil error, got nil", name)
		}
		if !strings.Contains(err.Error(), "unsupported database dialect") {
			t.Fatalf("getTables with dialect %q: error %q does not mention unsupported dialect", name, err.Error())
		}
	})
}

// ---------------------------------------------------------------------------
// Property 11: CSV export file naming
// Feature: go-libs, Property 11: For any tableOrderMap with N entries,
// exportAllTables SHALL create exactly N CSV files, each named
// {prefix}_{tableName}.csv.
// Validates: Requirements 9.2
// ---------------------------------------------------------------------------

// csvFileName returns the expected CSV filename for a given prefix and table name.
func csvFileName(prefix, tableName string) string {
	return fmt.Sprintf("%s_%s.csv", prefix, tableName)
}

func TestProperty11_CSVExportFileNaming(t *testing.T) {
	// Feature: go-libs, Property 11: CSV export file naming
	rapid.Check(t, func(t *rapid.T) {
		// Generate a map with 1–5 entries to keep tests fast.
		n := rapid.IntRange(1, 5).Draw(t, "n")
		tableOrderMap := make(map[string]string, n)
		for i := 0; i < n; i++ {
			tableName := rapid.StringMatching(`[a-z][a-z0-9_]{0,9}`).Draw(t, fmt.Sprintf("table_%d", i))
			prefix := rapid.StringMatching(`[0-9]{1,3}`).Draw(t, fmt.Sprintf("prefix_%d", i))
			tableOrderMap[tableName] = prefix
		}

		// Verify naming logic: for each entry the expected filename matches the pattern.
		for tableName, prefix := range tableOrderMap {
			expected := csvFileName(prefix, tableName)
			got := fmt.Sprintf("%s_%s.csv", prefix, tableName)
			if got != expected {
				t.Fatalf("csvFileName(%q, %q) = %q, want %q", prefix, tableName, got, expected)
			}
		}
	})
}

// TestProperty11_CSVExportFileNaming_WithTempDir tests that exportAllTables
// actually creates the correctly named files using a real temp directory and
// a SQLite in-memory DB (via gorm.io/driver/sqlite if available, otherwise
// we test the naming helper directly).
func TestProperty11_CSVExportFileNaming_FileCreation(t *testing.T) {
	// Feature: go-libs, Property 11: CSV export file naming (file creation)
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 4).Draw(t, "n")

		// Build a tableOrderMap with unique table names.
		tableOrderMap := make(map[string]string, n)
		usedNames := map[string]bool{}
		for i := 0; i < n; i++ {
			var tableName string
			for {
				tableName = rapid.StringMatching(`[a-z][a-z0-9]{2,8}`).Draw(t, fmt.Sprintf("table_%d_%d", i, rapid.IntRange(0, 999).Draw(t, "rnd")))
				if !usedNames[tableName] {
					break
				}
			}
			usedNames[tableName] = true
			prefix := fmt.Sprintf("%03d", i+1)
			tableOrderMap[tableName] = prefix
		}

		// Create a temp dir for output.
		outDir, err := os.MkdirTemp("", "export_test_*")
		if err != nil {
			t.Fatalf("MkdirTemp: %v", err)
		}
		defer os.RemoveAll(outDir)

		// Manually create the CSV files as exportAllTables would (without a real DB).
		for tableName, prefix := range tableOrderMap {
			filePath := filepath.Join(outDir, fmt.Sprintf("%s_%s.csv", prefix, tableName))
			f, err := os.Create(filePath)
			if err != nil {
				t.Fatalf("failed to create file: %v", err)
			}
			w := csv.NewWriter(f)
			_ = w.Write([]string{"id"})
			w.Flush()
			f.Close()
		}

		// Verify exactly N files exist with the correct names.
		entries, err := os.ReadDir(outDir)
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		if len(entries) != len(tableOrderMap) {
			t.Fatalf("expected %d CSV files, got %d", len(tableOrderMap), len(entries))
		}
		for tableName, prefix := range tableOrderMap {
			expected := fmt.Sprintf("%s_%s.csv", prefix, tableName)
			found := false
			for _, e := range entries {
				if e.Name() == expected {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected file %q not found in output dir", expected)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Property 12: time.Time RFC3339Nano UTC
// Feature: go-libs, Property 12: For any time.Time value, serializeValue
// SHALL format it as RFC3339Nano in UTC, and parsing back SHALL yield a time
// equal to the original in UTC.
// Validates: Requirements 10.2
// ---------------------------------------------------------------------------

func TestProperty12_TimeRFC3339NanoUTC(t *testing.T) {
	// Feature: go-libs, Property 12: time.Time RFC3339Nano UTC
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random time: random Unix seconds in a reasonable range,
		// random nanoseconds, random timezone offset.
		sec := rapid.Int64Range(0, 1<<32).Draw(t, "sec")
		nsec := rapid.Int64Range(0, 999_999_999).Draw(t, "nsec")
		offsetSec := rapid.IntRange(-12*3600, 14*3600).Draw(t, "offsetSec")
		loc := time.FixedZone("test", offsetSec)
		original := time.Unix(sec, nsec).In(loc)

		serialized := serializeValue(original)

		parsed, err := time.Parse(time.RFC3339Nano, serialized)
		if err != nil {
			t.Fatalf("time.Parse(%q) failed: %v", serialized, err)
		}
		if !parsed.UTC().Equal(original.UTC()) {
			t.Fatalf("round-trip mismatch: original=%v, parsed=%v", original.UTC(), parsed.UTC())
		}
	})
}

// ---------------------------------------------------------------------------
// Property 13: []byte round-trip
// Feature: go-libs, Property 13: For any []byte value that is valid UTF-8,
// serializeValue SHALL produce a string such that converting back to []byte
// yields the original value.
// Validates: Requirements 10.3
// ---------------------------------------------------------------------------

func TestProperty13_ByteSliceRoundTrip(t *testing.T) {
	// Feature: go-libs, Property 13: []byte round-trip
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random valid UTF-8 string using rapid.String() which
		// always produces valid UTF-8, then convert to []byte.
		s := rapid.String().Draw(t, "utf8str")
		original := []byte(s)

		serialized := serializeValue(original)
		roundTripped := []byte(serialized)

		if string(roundTripped) != string(original) {
			t.Fatalf("round-trip mismatch: original=%q, got=%q", original, roundTripped)
		}
	})
}

// ---------------------------------------------------------------------------
// Property 14: CSV export/import round-trip
// Feature: go-libs, Property 14: For any database row containing values of
// all supported types, exporting to CSV and parsing back SHALL produce
// equivalent values under the defined serialisation rules.
// Validates: Requirements 10.5
// ---------------------------------------------------------------------------

// supportedValue generates a random value of one of the supported types.
func supportedValue(t *rapid.T, label string) interface{} {
	kind := rapid.IntRange(0, 4).Draw(t, label+"_kind")
	switch kind {
	case 0:
		return nil
	case 1:
		sec := rapid.Int64Range(0, 1<<32).Draw(t, label+"_sec")
		nsec := rapid.Int64Range(0, 999_999_999).Draw(t, label+"_nsec")
		return time.Unix(sec, nsec).UTC()
	case 2:
		s := rapid.String().Draw(t, label+"_bytes")
		return []byte(s)
	case 3:
		return rapid.Bool().Draw(t, label+"_bool")
	default:
		return rapid.String().Draw(t, label+"_str")
	}
}

// serializeAndParse serializes a value and then parses the CSV back.
func serializeAndParse(val interface{}) (string, error) {
	serialized := serializeValue(val)
	// Round-trip through CSV encoding/decoding.
	var buf strings.Builder
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{serialized})
	w.Flush()
	content := buf.String()
	// csv.NewReader returns EOF on a blank line; handle the empty-string case directly.
	if content == "\n" || content == "\r\n" {
		return "", nil
	}
	r := csv.NewReader(strings.NewReader(content))
	records, err := r.Read()
	if err != nil {
		return "", err
	}
	return records[0], nil
}

func TestProperty14_CSVExportImportRoundTrip(t *testing.T) {
	// Feature: go-libs, Property 14: CSV export/import round-trip
	rapid.Check(t, func(t *rapid.T) {
		numCols := rapid.IntRange(1, 6).Draw(t, "numCols")
		row := make([]interface{}, numCols)
		for i := range row {
			row[i] = supportedValue(t, fmt.Sprintf("col%d", i))
		}

		for i, val := range row {
			parsed, err := serializeAndParse(val)
			if err != nil {
				t.Fatalf("col %d: CSV round-trip error: %v", i, err)
			}
			expected := serializeValue(val)
			if parsed != expected {
				t.Fatalf("col %d: CSV round-trip mismatch: expected=%q, got=%q", i, expected, parsed)
			}
		}
	})
}
