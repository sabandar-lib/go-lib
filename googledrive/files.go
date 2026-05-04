package googledrive

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"google.golang.org/api/drive/v3"
)

// createFolder creates a new Drive folder with the given name inside parentID
// and returns the new folder's ID.
func createFolder(svc *drive.Service, name, parentID string) (string, error) {
	folder := &drive.File{
		Name:     name,
		MimeType: "application/vnd.google-apps.folder",
		Parents:  []string{parentID},
	}
	created, err := svc.Files.Create(folder).Do()
	if err != nil {
		return "", fmt.Errorf("failed to create folder %q: %w", name, err)
	}
	return created.Id, nil
}

// uploadFile opens the file at localPath and uploads it to the Drive folder
// identified by parentID. Errors are wrapped with the local file path.
func uploadFile(svc *drive.Service, localPath, parentID string) error {
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to upload file %q: %w", localPath, err)
	}
	defer file.Close()

	filename := filepath.Base(localPath)
	meta := &drive.File{
		Name:    filename,
		Parents: []string{parentID},
	}
	_, err = svc.Files.Create(meta).Media(file).Do()
	if err != nil {
		return fmt.Errorf("failed to upload file %q: %w", localPath, err)
	}
	return nil
}

// listFiles lists files and folders inside parentID, applying the given
// pageSize and orderBy parameters. Only non-trashed items are returned.
func listFiles(svc *drive.Service, parentID string, pageSize int64, orderBy string) ([]*drive.File, error) {
	query := fmt.Sprintf("'%s' in parents and trashed = false", parentID)
	result, err := svc.Files.List().
		Q(query).
		PageSize(pageSize).
		OrderBy(orderBy).
		Fields("files(id, name, createdTime, mimeType)").
		Do()
	if err != nil {
		return nil, err
	}
	return result.Files, nil
}

// downloadFile downloads the Drive file identified by fileID and writes its
// contents to destPath. Errors are wrapped with the file ID.
func downloadFile(svc *drive.Service, fileID, destPath string) error {
	resp, err := svc.Files.Get(fileID).Download()
	if err != nil {
		return fmt.Errorf("failed to download file %q: %w", fileID, err)
	}
	defer resp.Body.Close()

	dest, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to download file %q: %w", fileID, err)
	}
	defer dest.Close()

	if _, err := io.Copy(dest, resp.Body); err != nil {
		return fmt.Errorf("failed to download file %q: %w", fileID, err)
	}
	return nil
}
