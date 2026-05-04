package googledrive

import (
	"context"
	"fmt"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

// GoogleDriveInterface is the public contract for the Google Drive client.
// All Drive operations are performed through this interface, making it easy
// to mock in consumer tests.
type GoogleDriveInterface interface {
	UploadToDrive(ctx context.Context, timeStampDir string, files []string) error
	DownloadFromDrive(ctx context.Context, outDir string, pageSize int64) (string, error)
	PruneOldBackups(ctx context.Context) error
}

// googleDriveClient implements GoogleDriveInterface.
type googleDriveClient struct {
	cfg     GoogleDriveConfig
	service *drive.Service
}

// New constructs a GoogleDriveInterface backed by a real Google Drive service.
// It reads the OAuth2 token from cfg.GDriveTokenFileDir and the client secret
// from cfg.GDriveClientSecretFileDir.
//
// Panics if the token source or Drive service cannot be initialised — callers
// should ensure the token file exists (run Login first) before calling New.
func New(cfg GoogleDriveConfig) GoogleDriveInterface {
	tokenSource, err := newPersistentTokenSource(cfg)
	if err != nil {
		panic(fmt.Sprintf("googledrive: failed to initialise token source: %v", err))
	}

	svc, err := drive.NewService(context.Background(), option.WithTokenSource(tokenSource))
	if err != nil {
		panic(fmt.Sprintf("googledrive: failed to create Drive service: %v", err))
	}

	return &googleDriveClient{cfg: cfg, service: svc}
}
