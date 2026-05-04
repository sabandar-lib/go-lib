// Package databasebackup provides a database backup and restore orchestrator
// that exports tables to CSV, uploads them to Google Drive, and restores them on demand.
package databasebackup

// DatabaseBackupConfig holds all configuration for the database backup client.
type DatabaseBackupConfig struct {
	DBBackupOutDirPrefix string // prefix for os.MkdirTemp
}
