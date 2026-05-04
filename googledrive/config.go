package googledrive

import "fmt"

// GoogleDriveConfig holds all configuration for the Google Drive client.
type GoogleDriveConfig struct {
	GDriveClientSecretFileDir string  // path to OAuth2 client_secret.json
	GDriveFolderID            string  // ID of the parent Drive folder
	GDriveTokenFileDir        string  // path to token.json (read/write)
	GDriveBackupCycle         string  // "daily" | "weekly" | "monthly"
	GDriveRetainMonths        float64 // months of backups to retain
}

// keep returns the number of backup folders to retain based on the backup
// cycle and retention period. Returns an error for unrecognised cycles.
func keep(cfg GoogleDriveConfig) (int, error) {
	switch cfg.GDriveBackupCycle {
	case "daily":
		return int(30 * cfg.GDriveRetainMonths), nil
	case "weekly":
		return int(4 * cfg.GDriveRetainMonths), nil
	case "monthly":
		return int(1 * cfg.GDriveRetainMonths), nil
	default:
		return 0, fmt.Errorf("invalid backup cycle %q: must be daily, weekly, or monthly", cfg.GDriveBackupCycle)
	}
}
