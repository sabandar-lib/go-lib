package databasebackup

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gorm.io/gorm"
)

// GoogleDriveInterface is the interface for Google Drive operations used by
// the databasebackup package. It mirrors googledrive.GoogleDriveInterface to
// avoid a direct package dependency and maintain decoupling.
type GoogleDriveInterface interface {
	UploadToDrive(ctx context.Context, timeStampDir string, files []string) error
	DownloadFromDrive(ctx context.Context, outDir string, pageSize int64) (string, error)
	PruneOldBackups(ctx context.Context) error
}

// downloadAllBackup delegates to gDriveLib.DownloadFromDrive to fetch the
// most recent backup folder into outDir. Returns the folder name.
func downloadAllBackup(ctx context.Context, gDriveLib GoogleDriveInterface, outDir string, pageSize int64) (string, error) {
	return gDriveLib.DownloadFromDrive(ctx, outDir, pageSize)
}

// insertBatch builds and executes a single batched INSERT … ON CONFLICT DO NOTHING
// statement for the given rows. batchNum is used only for error reporting.
// Column names are quoted with double-quotes (ANSI SQL).
func insertBatch(db *gorm.DB, tableName string, columns []string, rows [][]string, batchNum int) error {
	if len(rows) == 0 {
		return nil
	}

	// Build quoted column list.
	quotedCols := make([]string, len(columns))
	for i, col := range columns {
		quotedCols[i] = fmt.Sprintf("%q", col)
	}

	// Build placeholder groups and collect args in row-major order.
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

	if err := db.Exec(sql, args...).Error; err != nil {
		return fmt.Errorf("failed to insert batch %d into table %q: %w", batchNum, tableName, err)
	}
	return nil
}

// restoreTable reads the CSV at csvPath and inserts all rows into the database
// in batches of 100. The table name is derived from the CSV filename by
// stripping the directory, extension, and numeric prefix (e.g. "001_users.csv"
// → "users").
func restoreTable(db *gorm.DB, csvPath string) error {
	// Derive table name from filename: "001_users.csv" → "users".
	base := filepath.Base(csvPath)
	name := strings.TrimSuffix(base, ".csv")
	parts := strings.SplitN(name, "_", 2)
	if len(parts) < 2 {
		return fmt.Errorf("invalid CSV filename %q: expected format {prefix}_{tableName}.csv", base)
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
		// Empty file — nothing to restore.
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
		if err := insertBatch(db, tableName, columns, dataRows[i:end], batchNum); err != nil {
			return err
		}
	}

	return nil
}
