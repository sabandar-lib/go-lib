package databasebackup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gorm.io/gorm"
)

// BackupAll exports all tables to CSV files in a temporary directory, uploads
// them to Google Drive under a timestamped folder, and cleans up the temp dir.
// If export fails, the upload is skipped and the error is returned.
func (c *databaseBackupClient) BackupAll(ctx context.Context, db *gorm.DB, tableOrderMap map[string]string) error {
	tmpDir, err := os.MkdirTemp("", c.cfg.DBBackupOutDirPrefix)
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := exportAllTables(db, tableOrderMap, tmpDir); err != nil {
		return fmt.Errorf("failed to export tables: %w", err)
	}

	timeStampDir := time.Now().UTC().Format("2006-01-02T15-04-05Z")

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return fmt.Errorf("failed to read temp dir: %w", err)
	}
	var csvFiles []string
	for _, e := range entries {
		if !e.IsDir() {
			csvFiles = append(csvFiles, filepath.Join(tmpDir, e.Name()))
		}
	}

	if err := c.gDriveLib.UploadToDrive(ctx, timeStampDir, csvFiles); err != nil {
		return fmt.Errorf("failed to upload backup: %w", err)
	}

	return nil
}

// RestoreAllBackups downloads the most recent backup from Google Drive and
// restores each CSV file into the database in ascending prefix order.
// If restoreFeatureFlag is false, the method returns immediately without
// performing any work.
func (c *databaseBackupClient) RestoreAllBackups(ctx context.Context, db *gorm.DB, tableOrderMap map[string]string) error {
	if !c.restoreFeatureFlag {
		return nil
	}

	tmpDir, err := os.MkdirTemp("", c.cfg.DBBackupOutDirPrefix)
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	_, err = downloadAllBackup(ctx, c.gDriveLib, tmpDir, 100)
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return fmt.Errorf("failed to read download dir: %w", err)
	}
	var csvFiles []string
	for _, e := range entries {
		if !e.IsDir() {
			csvFiles = append(csvFiles, filepath.Join(tmpDir, e.Name()))
		}
	}
	sort.Strings(csvFiles)

	for _, csvPath := range csvFiles {
		if err := restoreTable(db, csvPath); err != nil {
			return err
		}
	}

	return nil
}

// PruneOldBackups delegates to the injected Google Drive client to remove
// backup folders that exceed the configured retention policy.
func (c *databaseBackupClient) PruneOldBackups(ctx context.Context) error {
	return c.gDriveLib.PruneOldBackups(ctx)
}
