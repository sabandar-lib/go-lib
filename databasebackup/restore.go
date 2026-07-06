package databasebackup

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"strings"

	"gorm.io/gorm"
)

func (b *Backup) restoreTable(ctx context.Context, table, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	r := csv.NewReader(f)

	headers, err := r.Read()
	if err != nil {
		return fmt.Errorf("read headers: %w", err)
	}

	rows, err := r.ReadAll()
	if err != nil {
		return fmt.Errorf("read rows: %w", err)
	}

	if len(rows) == 0 {
		return nil
	}

	for i := 0; i < len(rows); i += b.cfg.BatchSize {
		end := min(i+b.cfg.BatchSize, len(rows))
		batch := rows[i:end]

		if err := insertBatch(b.db, table, headers, batch, b.cfg.OnConflictClause); err != nil {
			return fmt.Errorf("insert batch at row %d: %w", i, err)
		}
	}
	return nil
}

func insertBatch(db *gorm.DB, table string, headers []string, rows [][]string, onConflict string) error {
	cols := `"` + strings.Join(headers, `", "`) + `"`
	placeholders := make([]string, 0, len(rows))
	values := make([]any, 0, len(rows)*len(headers))

	for _, row := range rows {
		rowPlaceholders := make([]string, len(row))
		for i, val := range row {
			rowPlaceholders[i] = "?"
			switch val {
			case "NULL":
				values = append(values, nil)
			default:
				values = append(values, val)
			}
		}
		placeholders = append(placeholders, "("+strings.Join(rowPlaceholders, ",")+")")
	}

	query := fmt.Sprintf(
		`INSERT INTO "%s" (%s) VALUES %s`,
		table,
		cols,
		strings.Join(placeholders, ","),
	)
	if onConflict != "" {
		query += " " + onConflict
	}

	return db.Exec(query, values...).Error
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
