package databasebackup

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gorm.io/gorm"
)

// serializeValue converts a database column value to its CSV string representation.
// nil → "NULL", time.Time → RFC3339Nano UTC, []byte → UTF-8 string,
// bool → "true"/"false", all others → fmt.Sprintf("%v", val).
func serializeValue(val interface{}) string {
	if val == nil {
		return "NULL"
	}
	switch v := val.(type) {
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano)
	case []byte:
		return string(v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", val)
	}
}

// getTables returns the list of user table names for the given database connection.
// It detects the dialect via db.Dialector.Name() and issues the appropriate query.
func getTables(db *gorm.DB) ([]string, error) {
	var query string
	switch db.Dialector.Name() {
	case "postgres":
		query = "SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE'"
	case "mysql":
		query = "SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE()"
	case "sqlite":
		query = "SELECT name FROM sqlite_master WHERE type='table'"
	default:
		return nil, fmt.Errorf("unsupported database dialect: %q", db.Dialector.Name())
	}

	rows, err := db.Raw(query).Rows()
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}
	if rows == nil {
		return nil, fmt.Errorf("failed to list tables: query returned nil rows")
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, fmt.Errorf("failed to list tables: %w", err)
		}
		tables = append(tables, tableName)
	}
	return tables, nil
}

// exportTable queries all rows from tableName and writes them to a CSV file at
// filepath.Join(outDir, fmt.Sprintf("%s_%s.csv", prefix, tableName)).
func exportTable(db *gorm.DB, tableName, prefix, outDir string) error {
	rows, err := db.Raw(fmt.Sprintf("SELECT * FROM %q", tableName)).Rows()
	if err != nil {
		return fmt.Errorf("failed to export table %q: %w", tableName, err)
	}
	if rows == nil {
		return fmt.Errorf("failed to export table %q: query returned nil rows", tableName)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("failed to export table %q: %w", tableName, err)
	}

	filePath := filepath.Join(outDir, fmt.Sprintf("%s_%s.csv", prefix, tableName))
	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to export table %q: %w", tableName, err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	// Write header row.
	if err := w.Write(columns); err != nil {
		return fmt.Errorf("failed to export table %q: %w", tableName, err)
	}

	// Write data rows.
	vals := make([]interface{}, len(columns))
	valPtrs := make([]interface{}, len(columns))
	for i := range vals {
		valPtrs[i] = &vals[i]
	}

	for rows.Next() {
		if err := rows.Scan(valPtrs...); err != nil {
			return fmt.Errorf("failed to export table %q: %w", tableName, err)
		}
		record := make([]string, len(columns))
		for i, v := range vals {
			record[i] = serializeValue(v)
		}
		if err := w.Write(record); err != nil {
			return fmt.Errorf("failed to export table %q: %w", tableName, err)
		}
	}

	return nil
}

// exportAllTables iterates tableOrderMap and calls exportTable for each entry.
// tableOrderMap maps table names to their numeric string prefix.
// Returns the first error encountered.
func exportAllTables(db *gorm.DB, tableOrderMap map[string]string, outDir string) error {
	for tableName, prefix := range tableOrderMap {
		if err := exportTable(db, tableName, prefix, outDir); err != nil {
			return err
		}
	}
	return nil
}
