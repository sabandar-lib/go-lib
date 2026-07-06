package databasebackup

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gorm.io/gorm"
)

func (b *Backup) exportAllTables(outDir string) ([]string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}

	tables, err := getTables(b.db)
	if err != nil {
		return nil, err
	}

	if len(b.cfg.TableOrder) == 0 {
		sort.Strings(tables)
	}

	var files []string
	for _, table := range tables {
		prefix := b.cfg.TableOrder[table]
		if prefix == "" {
			prefix = "0"
		}
		path := filepath.Join(outDir, prefix+"_"+table+".csv")
		if err := exportTable(b.db, table, path); err != nil {
			return nil, fmt.Errorf("export table %s: %w", table, err)
		}
		files = append(files, path)
	}
	return files, nil
}

func getTables(db *gorm.DB) ([]string, error) {
	var tables []string

	switch db.Dialector.Name() {
	case "postgres":
		db.Raw("SELECT tablename FROM pg_tables WHERE schemaname = 'public'").Scan(&tables)
	case "mysql":
		db.Raw("SHOW TABLES").Scan(&tables)
	case "sqlite":
		db.Raw("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tables)
	default:
		return nil, fmt.Errorf("unsupported dialect: %s", db.Dialector.Name())
	}

	return tables, nil
}

func exportTable(db *gorm.DB, table, path string) error {
	rows, err := db.Raw(fmt.Sprintf("SELECT * FROM %q", table)).Rows()
	if err != nil {
		return err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write(cols); err != nil {
		return err
	}

	vals := make([]any, len(cols))
	valPtrs := make([]any, len(cols))
	for i := range vals {
		valPtrs[i] = &vals[i]
	}

	for rows.Next() {
		if err := rows.Scan(valPtrs...); err != nil {
			return err
		}
		row := make([]string, len(cols))
		for i, v := range vals {
			row[i] = serializeCSVValue(v)
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return rows.Err()
}

func serializeCSVValue(v any) string {
	if v == nil {
		return "NULL"
	}
	if t, ok := v.(time.Time); ok {
		return t.UTC().Format(time.RFC3339Nano)
	}
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	if b, ok := v.(bool); ok {
		if b {
			return "true"
		}
		return "false"
	}
	return fmt.Sprintf("%v", v)
}
