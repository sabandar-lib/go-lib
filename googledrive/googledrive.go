// Package googledrive provides a client for uploading files to Google Drive
// and pruning old backup folders based on a configurable retention policy.
package googledrive

import (
	"context"
	"fmt"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

// Config holds the configuration required to interact with Google Drive.
type Config struct {
	ClientSecretFileDir string
	FolderID            string
	RetainCount         int
}

// TokenStore lets the caller decide how to persist refreshed tokens.
type TokenStore interface {
	Save(token *oauth2.Token) error
	Load() (*oauth2.Token, error)
}

// Client is the public interface for Google Drive backup operations.
type Client interface {
	Upload(ctx context.Context, folderName string, files []string) error
	DownloadLatest(ctx context.Context, outDirPrefix string) (string, error)
	PruneOldFolders(ctx context.Context) error
}

// nopClient is a no-op implementation of Client for callers that do not
// enable Google Drive.
type nopClient struct{}

func (nopClient) Upload(context.Context, string, []string) error         { return nil }
func (nopClient) DownloadLatest(context.Context, string) (string, error) { return "", nil }
func (nopClient) PruneOldFolders(context.Context) error                  { return nil }

// NewNop returns a Client whose methods do nothing and return no error.
func NewNop() Client { return nopClient{} }

type googleDriveClient struct {
	cfg      Config
	driveSvc *drive.Service
}

// New creates a real Google Drive client using the provided configuration and
// token store. The initial token is loaded from the store; refreshed tokens are
// saved back through the store.
func New(ctx context.Context, cfg Config, store TokenStore) (Client, error) {
	if cfg.ClientSecretFileDir == "" {
		return nil, fmt.Errorf("googledrive: ClientSecretFileDir is required")
	}
	if cfg.FolderID == "" {
		return nil, fmt.Errorf("googledrive: FolderID is required")
	}
	if store == nil {
		return nil, fmt.Errorf("googledrive: TokenStore is required")
	}

	driveSvc, err := newDriveService(ctx, cfg, store)
	if err != nil {
		return nil, fmt.Errorf("init google drive client: %w", err)
	}

	return &googleDriveClient{
		cfg:      cfg,
		driveSvc: driveSvc,
	}, nil
}

func newDriveService(ctx context.Context, cfg Config, store TokenStore) (*drive.Service, error) {
	data, err := os.ReadFile(cfg.ClientSecretFileDir)
	if err != nil {
		return nil, fmt.Errorf("read client secret: %w", err)
	}

	oauthCfg, err := google.ConfigFromJSON(data, drive.DriveFileScope)
	if err != nil {
		return nil, fmt.Errorf("parse client secret: %w", err)
	}

	token, err := store.Load()
	if err != nil {
		return nil, fmt.Errorf("load token (did you run the auth helper?): %w", err)
	}
	if token == nil {
		return nil, fmt.Errorf("token store returned nil token")
	}

	pts := &persistentTokenSource{
		source:  oauthCfg.TokenSource(ctx, token),
		store:   store,
		current: token,
	}

	httpClient := oauth2.NewClient(ctx, pts)
	return drive.NewService(ctx, option.WithHTTPClient(httpClient))
}

// Upload uploads files to a timestamped subfolder inside the configured Drive folder.
func (gd *googleDriveClient) Upload(ctx context.Context, folderName string, files []string) error {
	subFolderID, err := gd.createFolder(gd.driveSvc, gd.cfg.FolderID, folderName)
	if err != nil {
		return fmt.Errorf("create subfolder: %w", err)
	}

	for _, path := range files {
		if err := gd.uploadFile(gd.driveSvc, subFolderID, path); err != nil {
			return fmt.Errorf("upload %s: %w", path, err)
		}
	}
	return nil
}

// DownloadLatest downloads the most recent backup folder's CSV files into a
// local directory prefixed with outDirPrefix.
func (gd *googleDriveClient) DownloadLatest(ctx context.Context, outDirPrefix string) (string, error) {
	const pageSize = 1

	folders, err := gd.listFiles(gd.driveSvc, pageSize)
	if err != nil {
		return "", err
	}

	var folderOutDir string
	for _, folder := range folders {
		files, err := gd.listFilesInFolder(gd.driveSvc, folder.Id)
		if err != nil {
			return "", fmt.Errorf("list files in %s: %w", folder.Name, err)
		}

		folderOutDir = fmt.Sprintf("%s/restored_%s", outDirPrefix, folder.Name)
		if err := os.MkdirAll(folderOutDir, 0o755); err != nil {
			return "", fmt.Errorf("create output dir: %w", err)
		}

		for _, f := range files {
			if err := gd.downloadFile(gd.driveSvc, f.Id, f.Name, folderOutDir); err != nil {
				return "", fmt.Errorf("download %s: %w", f.Name, err)
			}
		}
	}

	return folderOutDir, nil
}

// PruneOldFolders keeps only the most recent RetainCount backup folders in the
// configured Drive folder.
func (gd *googleDriveClient) PruneOldFolders(ctx context.Context) error {
	list, err := gd.driveSvc.Files.List().
		Q(fmt.Sprintf(
			"'%s' in parents and mimeType = 'application/vnd.google-apps.folder' and trashed = false",
			gd.cfg.FolderID,
		)).
		OrderBy("createdTime").
		Fields("files(id, name)").
		Do()
	if err != nil {
		return fmt.Errorf("list folders: %w", err)
	}

	files := list.Files
	if len(files) <= gd.cfg.RetainCount {
		return nil
	}

	for _, f := range files[:len(files)-gd.cfg.RetainCount] {
		if _, err := gd.driveSvc.Files.Update(f.Id, &drive.File{Trashed: true}).Do(); err != nil {
			return fmt.Errorf("trash folder %s: %w", f.Name, err)
		}
	}
	return nil
}

// Login performs the OAuth2 flow and returns the resulting token. The caller is
// responsible for persisting the returned token via TokenStore.Save.
func Login(clientSecretPath string) (*oauth2.Token, error) {
	data, err := os.ReadFile(clientSecretPath)
	if err != nil {
		return nil, fmt.Errorf("read client secret: %w", err)
	}

	oauthCfg, err := google.ConfigFromJSON(data, drive.DriveFileScope)
	if err != nil {
		return nil, fmt.Errorf("parse client secret: %w", err)
	}

	authURL := oauthCfg.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Println("1. Open this URL in your browser:")
	fmt.Println()
	fmt.Println(authURL)
	fmt.Println()
	fmt.Print("2. Paste the authorization code here: ")

	var code string
	if _, err := fmt.Scan(&code); err != nil {
		return nil, fmt.Errorf("read authorization code: %w", err)
	}

	token, err := oauthCfg.Exchange(context.Background(), code)
	if err != nil {
		return nil, fmt.Errorf("exchange token: %w", err)
	}

	return token, nil
}
