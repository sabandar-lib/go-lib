// Package databasebackup provides functionality to back up and restore an
// entire database to and from CSV files through a configurable storage backend.
package databasebackup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

const storageFull = "databasebackup: storage is nil"

// Storage abstracts the destination used to persist and retrieve backup archives.
type Storage interface {
	Upload(ctx context.Context, folderName string, files []string) error
	DownloadLatest(ctx context.Context, outDirPrefix string) (string, error)
	Prune(ctx context.Context, retainCount int) error
}

// Config controls backup/restore behavior.
type Config struct {
	OutDirPrefix     string
	TableOrder       map[string]string // table -> filename prefix, e.g. "users" -> "2"
	OnConflictClause string            // e.g. "ON CONFLICT DO NOTHING"; empty for none
	BatchSize        int               // default 100 if zero
	RetainCount      int               // passed to Storage.Prune
}

// Backup orchestrates database backup and restore.
type Backup struct {
	cfg     Config
	storage Storage
	db      *gorm.DB
}

// New creates a Backup instance. A nil storage is accepted for callers that only
// need local CSV export, but BackupAll will fail if storage is nil.
func New(cfg Config, storage Storage, db *gorm.DB) *Backup {
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 100
	}
	return &Backup{
		cfg:     cfg,
		storage: storage,
		db:      db,
	}
}

// BackupAll exports every table to CSV and uploads the resulting files.
func (b *Backup) BackupAll(ctx context.Context) error {
	timeStampDir := time.Now().Format("20060102_150405")
	outDir := filepath.Join(b.cfg.OutDirPrefix, "backup_"+timeStampDir)

	files, err := b.exportAllTables(outDir)
	if err != nil {
		return err
	}
	defer os.RemoveAll(outDir)

	if b.storage == nil {
		return fmt.Errorf(storageFull)
	}
	if err := b.storage.Upload(ctx, timeStampDir, files); err != nil {
		return fmt.Errorf("upload backup: %w", err)
	}
	return nil
}

// RestoreAll downloads the latest backup and restores every CSV file into the
// database.
func (b *Backup) RestoreAll(ctx context.Context) error {
	if b.storage == nil {
		return fmt.Errorf(storageFull)
	}

	folderOutDir, err := b.storage.DownloadLatest(ctx, b.cfg.OutDirPrefix)
	if err != nil {
		return fmt.Errorf("download backup: %w", err)
	}
	if folderOutDir == "" {
		return fmt.Errorf("databasebackup: download returned empty directory")
	}
	defer os.RemoveAll(folderOutDir)

	entries, err := os.ReadDir(folderOutDir)
	if err != nil {
		return fmt.Errorf("read restore dir: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return extractNumericPrefix(entries[i].Name()) < extractNumericPrefix(entries[j].Name())
	})

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".csv" {
			continue
		}

		table := stripPrefixAndExtension(entry.Name())
		path := filepath.Join(folderOutDir, entry.Name())

		if err := b.restoreTable(ctx, table, path); err != nil {
			return fmt.Errorf("restore table %s: %w", table, err)
		}
	}
	return nil
}

// Prune delegates retention cleanup to the configured storage.
func (b *Backup) Prune(ctx context.Context) error {
	if b.storage == nil {
		return fmt.Errorf(storageFull)
	}
	return b.storage.Prune(ctx, b.cfg.RetainCount)
}

func extractNumericPrefix(name string) int {
	parts := strings.SplitN(name, "_", 2)
	num, _ := strconv.Atoi(parts[0])
	return num
}

func stripPrefixAndExtension(name string) string {
	name = strings.TrimSuffix(name, ".csv")
	idx := strings.Index(name, "_")
	if idx == -1 {
		return name
	}
	return name[idx+1:]
}
