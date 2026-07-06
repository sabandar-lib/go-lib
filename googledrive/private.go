package googledrive

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"google.golang.org/api/drive/v3"
)

func (gd *googleDriveClient) createFolder(svc *drive.Service, parentFolderID, name string) (string, error) {
	folder, err := svc.Files.Create(&drive.File{
		Name:     name,
		MimeType: "application/vnd.google-apps.folder",
		Parents:  []string{parentFolderID},
	}).Fields("id").Do()
	if err != nil {
		return "", fmt.Errorf("create folder %s: %w", name, err)
	}
	return folder.Id, nil
}

func (gd *googleDriveClient) uploadFile(svc *drive.Service, folderID, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = svc.Files.Create(&drive.File{
		Name:    filepath.Base(path),
		Parents: []string{folderID},
	}).Media(f).Do()
	return err
}

// listFiles lists backup folders in the main folder, newest first.
func (gd *googleDriveClient) listFiles(svc *drive.Service, pageSize int64) ([]*drive.File, error) {
	list, err := svc.Files.List().
		Q(fmt.Sprintf(
			"'%s' in parents and mimeType = 'application/vnd.google-apps.folder' and trashed = false",
			gd.cfg.FolderID,
		)).
		OrderBy("createdTime desc").
		PageSize(pageSize).
		Fields("files(id, name)").
		Do()
	if err != nil {
		return nil, fmt.Errorf("list backups: %w", err)
	}

	return list.Files, nil
}

// listFilesInFolder lists non-folder files inside a specific folder.
func (gd *googleDriveClient) listFilesInFolder(svc *drive.Service, folderID string) ([]*drive.File, error) {
	list, err := svc.Files.List().
		Q(fmt.Sprintf(
			"'%s' in parents and mimeType != 'application/vnd.google-apps.folder' and trashed = false",
			folderID,
		)).
		Fields("files(id, name)").
		Do()
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	return list.Files, nil
}

func (gd *googleDriveClient) downloadFile(svc *drive.Service, fileID, fileName, outDir string) error {
	resp, err := svc.Files.Get(fileID).Download()
	if err != nil {
		return fmt.Errorf("get file: %w", err)
	}
	defer resp.Body.Close()

	out, err := os.Create(filepath.Join(outDir, fileName))
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}
