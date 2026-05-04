package databasebackup

import (
	"context"

	"gorm.io/gorm"
)

// DatabaseBackupInterface is the public contract for the database backup client.
type DatabaseBackupInterface interface {
	BackupAll(ctx context.Context, db *gorm.DB, tableOrderMap map[string]string) error
	RestoreAllBackups(ctx context.Context, db *gorm.DB, tableOrderMap map[string]string) error
	PruneOldBackups(ctx context.Context) error
}

// databaseBackupClient implements DatabaseBackupInterface.
type databaseBackupClient struct {
	cfg                DatabaseBackupConfig
	gDriveLib          GoogleDriveInterface
	restoreFeatureFlag bool
}

// New constructs a DatabaseBackupInterface with the given config, Google Drive
// client, and restore feature flag.
func New(cfg DatabaseBackupConfig, gDriveLib GoogleDriveInterface, restoreFeatureFlag bool) DatabaseBackupInterface {
	return &databaseBackupClient{
		cfg:                cfg,
		gDriveLib:          gDriveLib,
		restoreFeatureFlag: restoreFeatureFlag,
	}
}
