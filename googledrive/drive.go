package googledrive

import (
	"context"
	"fmt"
	"path/filepath"

	"google.golang.org/api/drive/v3"
)

// selectMostRecentFolder returns the folder with the latest createdTime from
// the provided list. Returns nil if the list is empty.
// This is a pure helper extracted for testability.
func selectMostRecentFolder(folders []*drive.File) *drive.File {
	if len(folders) == 0 {
		return nil
	}
	best := folders[0]
	for _, f := range folders[1:] {
		if f.CreatedTime > best.CreatedTime {
			best = f
		}
	}
	return best
}

// foldersToDelete returns the slice of folders that should be deleted given a
// keep count. The input slice is assumed to be ordered most-recent first.
// It retains the first keepCount entries and returns the rest for deletion.
// This is a pure helper extracted for testability.
func foldersToDelete(folders []*drive.File, keepCount int) []*drive.File {
	if keepCount >= len(folders) {
		return nil
	}
	return folders[keepCount:]
}

// UploadToDrive creates a subfolder named timeStampDir inside the configured
// parent folder, then uploads each file in files to that subfolder.
// Returns an error identifying the failed file if any upload fails.
func (c *googleDriveClient) UploadToDrive(ctx context.Context, timeStampDir string, files []string) error {
	subfolderID, err := createFolder(c.service, timeStampDir, c.cfg.GDriveFolderID)
	if err != nil {
		return fmt.Errorf("failed to create folder %q: %w", timeStampDir, err)
	}

	for _, file := range files {
		if err := uploadFile(c.service, file, subfolderID); err != nil {
			return err
		}
	}
	return nil
}

// DownloadFromDrive lists backup folders in the configured parent folder ordered
// by createdTime descending, selects the most recent one, downloads all CSV
// files from it into outDir, and returns the folder name.
func (c *googleDriveClient) DownloadFromDrive(ctx context.Context, outDir string, pageSize int64) (string, error) {
	// List only folders inside the parent, ordered most-recent first.
	folderQuery := fmt.Sprintf(
		"'%s' in parents and trashed = false and mimeType = 'application/vnd.google-apps.folder'",
		c.cfg.GDriveFolderID,
	)
	folderResult, err := c.service.Files.List().
		Q(folderQuery).
		PageSize(pageSize).
		OrderBy("createdTime desc").
		Fields("files(id, name, createdTime, mimeType)").
		Do()
	if err != nil {
		return "", err
	}

	if len(folderResult.Files) == 0 {
		return "", fmt.Errorf("no backup folders found in Drive folder %q", c.cfg.GDriveFolderID)
	}

	// Select the most recent folder (first in the createdTime desc list).
	selectedFolder := folderResult.Files[0]

	// List all non-folder (CSV) files inside the selected folder.
	fileQuery := fmt.Sprintf(
		"'%s' in parents and trashed = false and mimeType != 'application/vnd.google-apps.folder'",
		selectedFolder.Id,
	)
	fileResult, err := c.service.Files.List().
		Q(fileQuery).
		Fields("files(id, name)").
		Do()
	if err != nil {
		return "", err
	}

	// Download each file into outDir.
	for _, f := range fileResult.Files {
		destPath := filepath.Join(outDir, f.Name)
		if err := downloadFile(c.service, f.Id, destPath); err != nil {
			return "", err
		}
	}

	return selectedFolder.Name, nil
}

// PruneOldBackups lists all backup folders in the configured parent folder,
// computes the keep count from the config, retains the N most recent folders,
// and deletes the rest.
func (c *googleDriveClient) PruneOldBackups(ctx context.Context) error {
	keepCount, err := keep(c.cfg)
	if err != nil {
		return err
	}

	// List all folders inside the parent, ordered most-recent first.
	folderQuery := fmt.Sprintf(
		"'%s' in parents and trashed = false and mimeType = 'application/vnd.google-apps.folder'",
		c.cfg.GDriveFolderID,
	)
	folderResult, err := c.service.Files.List().
		Q(folderQuery).
		PageSize(1000).
		OrderBy("createdTime desc").
		Fields("files(id, name, createdTime)").
		Do()
	if err != nil {
		return err
	}

	folders := folderResult.Files

	// Retain the first keepCount folders; delete the rest.
	if keepCount >= len(folders) {
		return nil
	}

	toDelete := folders[keepCount:]
	for _, folder := range toDelete {
		if err := c.service.Files.Delete(folder.Id).Do(); err != nil {
			return fmt.Errorf("failed to delete folder %q: %w", folder.Id, err)
		}
	}

	return nil
}
