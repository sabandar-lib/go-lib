# googledrive

A small OAuth2 Google Drive client for backup workflows. It supports uploading files to a timestamped folder, downloading the latest backup folder, and pruning old backup folders based on a retention count.

## When to use this package

Use this package when you need a Go service to:

- Upload backup archives to a specific Google Drive folder.
- Download the most recent backup archives from Google Drive.
- Automatically delete older backups while keeping a configurable number of recent ones.

## Installation

```bash
go get github.com/sabandar-lib/go-lib/googledrive
```

## Quick start

```go
package main

import (
    "context"
    "log"

    "github.com/sabandar-lib/go-lib/googledrive"
)

func main() {
    ctx := context.Background()

    store := myTokenStore{} // see TokenStore section below

    client, err := googledrive.New(ctx, googledrive.Config{
        ClientSecretFileDir: "/etc/backup/client_secret.json",
        FolderID:            "1BxiMVs0XRA5nFMdKvBdBZjgmUUqptlbs74OgvE2upms",
        RetainCount:         2,
    }, store)
    if err != nil {
        log.Fatalf("create client: %v", err)
    }

    if err := client.Upload(ctx, "backup_20260102_150405", []string{"/tmp/backup/users.csv"}); err != nil {
        log.Fatalf("upload: %v", err)
    }
}
```

## Configuration

The package is configured through `googledrive.Config`:

```go
type Config struct {
    // Path to the Google OAuth2 client secret JSON downloaded from Google Cloud Console.
    ClientSecretFileDir string

    // ID of the Google Drive folder where backups will be stored.
    FolderID string

    // How many recent backup folders to keep. Older folders are trashed by PruneOldFolders.
    RetainCount int
}
```

## TokenStore

The library never writes token files itself. You must provide a `TokenStore` implementation that tells the library how to load and save OAuth2 tokens.

```go
type TokenStore interface {
    Save(token *oauth2.Token) error
    Load() (*oauth2.Token, error)
}
```

A simple file-backed implementation:

```go
type fileTokenStore struct {
    path string
}

func (f *fileTokenStore) Save(token *oauth2.Token) error {
    file, err := os.Create(f.path)
    if err != nil {
        return err
    }
    defer file.Close()
    return json.NewEncoder(file).Encode(token)
}

func (f *fileTokenStore) Load() (*oauth2.Token, error) {
    file, err := os.Open(f.path)
    if err != nil {
        return nil, err
    }
    defer file.Close()

    token := &oauth2.Token{}
    if err := json.NewDecoder(file).Decode(token); err != nil {
        return nil, err
    }
    return token, nil
}
```

## Initial OAuth2 setup (interactive browser flow)

Before the service can use Google Drive, you need a valid OAuth2 token. The library provides `googledrive.Login` for this one-time interactive step.

1. Create a project in the [Google Cloud Console](https://console.cloud.google.com/).
2. Enable the **Google Drive API**.
3. Create OAuth2 credentials for a **Desktop app**. Leave the redirect URI at its default value (`http://localhost`); the `Login` helper uses the copy-paste authorization-code flow and does not need a local redirect server.
4. Download the client secret JSON and save it to the path configured as `ClientSecretFileDir`.
5. Create a `cmd/login-google/main.go` command in your repo:

```go
package main

import (
    "log"
    "os"

    "your-project/lib/googledrive"
)

func main() {
    clientSecretPath := os.Getenv("GDRIVE_CLIENT_SECRET_FILE_DIR")
    tokenPath := os.Getenv("GDRIVE_TOKEN_FILE_DIR")

    token, err := googledrivelib.Login(clientSecretPath)
    if err != nil {
        log.Fatalf("google drive login: %v", err)
    }

    store := googledrivelib.NewFileTokenStore(tokenPath)
    if err := store.Save(token); err != nil {
        log.Fatalf("save token: %v", err)
    }

    log.Printf("Token saved to %s", tokenPath)
}
```

> Replace `your-project` with your actual Go module path. Make sure the directory containing `GDRIVE_TOKEN_FILE_DIR` exists before running the command.

6. Run the login helper:

```bash
go run ./cmd/login-google
```

When you run this command:

1. It prints a Google authorization URL.
2. Open the URL in your browser and sign in with the Google account that owns the Drive folder.
3. Grant the requested Drive permissions.
4. Copy the authorization code from the browser.
5. Paste the code into the terminal prompt.
6. The helper exchanges the code for an access/refresh token and returns it.

Save the returned token with your `TokenStore`. The service will use the refresh token to obtain new access tokens automatically. If the token file is writable, refreshed tokens will be persisted on every refresh.

## Finding your Google Drive folder ID

The `FolderID` in `Config` is the ID of the Google Drive folder where backups will be stored.

1. Open [Google Drive](https://drive.google.com) in your browser.
2. Create or open the folder you want to use for backups.
3. Look at the URL. It will look like:
   ```text
   https://drive.google.com/drive/folders/1BxiMVs0XRA5nFMdKvBdBZjgmUUqptlbs74OgvE2upms
   ```
4. Copy the long string after `/folders/`. That is your `FolderID`.
5. Set it as the `GDRIVE_FOLDER_ID` environment variable.

## Recommended consumer setup (local facade)

We recommend creating a thin facade in your own repo so that only one local package imports `github.com/sabandar-lib/go-lib/googledrive`. This makes future upgrades much easier.

### Files to create in your repo

Create a directory `your-project/lib/googledrive/` with these files:

#### `lib/googledrive/remote.go`

The only file that imports the remote package.

```go
package googledrivelib

import (
    "context"

    "github.com/sabandar-lib/go-lib/googledrive"
    "golang.org/x/oauth2"
)

type remoteClient = googledrive.Client
type remoteConfig = googledrive.Config
type TokenStore = googledrive.TokenStore

func remoteNew(ctx context.Context, cfg remoteConfig, store TokenStore) (remoteClient, error) {
    return googledrive.New(ctx, cfg, store)
}

func remoteNewNop() remoteClient {
    return googledrive.NewNop()
}

func remoteLogin(clientSecretPath string) (*oauth2.Token, error) {
    return googledrive.Login(clientSecretPath)
}
```

#### `lib/googledrive/google_drive.go`

```go
package googledrivelib

import (
    "context"
    "fmt"
)

type GoogleDriveInterface interface {
    UploadToDrive(ctx context.Context, timeStampDir string, files []string) error
    DownloadFromDrive(ctx context.Context, outDir string, pageSize int64) (string, error)
    PruneOldBackups(ctx context.Context) error
}

type googleDriveClient struct {
    client remoteClient
}

func New(clientSecretFileDir, folderID string, retainCount int, store TokenStore) (GoogleDriveInterface, error) {
    client, err := remoteNew(context.Background(), remoteConfig{
        ClientSecretFileDir: clientSecretFileDir,
        FolderID:            folderID,
        RetainCount:         retainCount,
    }, store)
    if err != nil {
        return nil, fmt.Errorf("init google drive client: %w", err)
    }
    return &googleDriveClient{client: client}, nil
}

func NewNop() GoogleDriveInterface {
    return &googleDriveClient{client: remoteNewNop()}
}

func (g *googleDriveClient) UploadToDrive(ctx context.Context, timeStampDir string, files []string) error {
    return g.client.Upload(ctx, timeStampDir, files)
}

func (g *googleDriveClient) DownloadFromDrive(ctx context.Context, outDir string, pageSize int64) (string, error) {
    _ = pageSize // kept for interface compatibility; remote client uses pageSize = 1
    return g.client.DownloadLatest(ctx, outDir)
}

func (g *googleDriveClient) PruneOldBackups(ctx context.Context) error {
    return g.client.PruneOldFolders(ctx)
}
```

#### `lib/googledrive/token_store.go`

```go
package googledrivelib

import (
    "encoding/json"
    "fmt"
    "os"

    "golang.org/x/oauth2"
)

type fileTokenStore struct {
    path string
}

func NewFileTokenStore(path string) TokenStore {
    return &fileTokenStore{path: path}
}

func (f *fileTokenStore) Save(token *oauth2.Token) error {
    file, err := os.Create(f.path)
    if err != nil {
        return fmt.Errorf("create token file: %w", err)
    }
    defer file.Close()
    return json.NewEncoder(file).Encode(token)
}

func (f *fileTokenStore) Load() (*oauth2.Token, error) {
    file, err := os.Open(f.path)
    if err != nil {
        return nil, fmt.Errorf("open token file: %w", err)
    }
    defer file.Close()

    token := &oauth2.Token{}
    if err := json.NewDecoder(file).Decode(token); err != nil {
        return nil, fmt.Errorf("decode token file: %w", err)
    }
    return token, nil
}
```

#### `lib/googledrive/login.go`

```go
package googledrivelib

import "golang.org/x/oauth2"

func Login(clientSecretPath string) (*oauth2.Token, error) {
    return remoteLogin(clientSecretPath)
}
```

### Environment variables

Add these to your `.env` file or environment:

```bash
export GDRIVE_CLIENT_SECRET_FILE_DIR=/etc/backup/client_secret.json
export GDRIVE_FOLDER_ID=your-google-drive-folder-id
export GDRIVE_TOKEN_FILE_DIR=/etc/backup/token.json
export GDRIVE_RETAIN_COUNT=2
```

### Using the facade

```go
package main

import (
    "context"
    "log"
    "os"

    "your-project/lib/googledrive"
)

func main() {
    ctx := context.Background()

    gdrive, err := googledrivelib.New(
        os.Getenv("GDRIVE_CLIENT_SECRET_FILE_DIR"),
        os.Getenv("GDRIVE_FOLDER_ID"),
        2,
        googledrivelib.NewFileTokenStore(os.Getenv("GDRIVE_TOKEN_FILE_DIR")),
    )
    if err != nil {
        log.Fatalf("create google drive client: %v", err)
    }

    files := []string{"/tmp/backup/1_users.csv", "/tmp/backup/2_orders.csv"}
    if err := gdrive.UploadToDrive(ctx, "backup_20260102_150405", files); err != nil {
        log.Fatalf("upload: %v", err)
    }
}
```

> Replace `your-project` with your actual Go module path, then run `go mod tidy`.

## Complete consumer setup checklist

1. Add the remote module to your project:
   ```bash
   go get github.com/sabandar-lib/go-lib/googledrive
   ```

2. Create a Google Cloud project, enable the Google Drive API, and download the OAuth2 client secret JSON.

3. Create your local `lib/googledrive/` facade (the four files shown above).

4. Create `cmd/login-google/main.go` to generate the initial OAuth2 token.

5. Generate the initial OAuth2 token by running your login helper:
   ```bash
   go run ./cmd/login-google
   ```

5. Use the `googledrivelib.GoogleDriveInterface` in your application or cron job.

6. Run `go mod tidy` and build:
   ```bash
   go mod tidy
   go build ./...
   ```

## API reference

### `func New(ctx context.Context, cfg Config, store TokenStore) (Client, error)`

Creates a real Google Drive client. Requires a valid client secret file and a token store that can load an existing token (or a token generated by `Login`).

### `func NewNop() Client`

Returns a no-op client. All methods return nil/error-free zero values. Useful when Google Drive is disabled in a given environment.

### `func Login(clientSecretPath string) (*oauth2.Token, error)`

Interactive helper that performs the OAuth2 flow and returns a token. The caller is responsible for persisting the token through `TokenStore.Save`.

### `type Client`

```go
type Client interface {
    Upload(ctx context.Context, folderName string, files []string) error
    DownloadLatest(ctx context.Context, outDirPrefix string) (string, error)
    PruneOldFolders(ctx context.Context) error
}
```

- `Upload` creates a subfolder named `folderName` inside `FolderID` and uploads the given files.
- `DownloadLatest` downloads files from the most recent backup folder into a local directory prefixed with `outDirPrefix`.
- `PruneOldFolders` trashes old folders, keeping the most recent `RetainCount`.

## Dependencies

- `golang.org/x/oauth2`
- `google.golang.org/api/drive/v3`
- `golang.org/x/oauth2/google`

## Testing

The package includes unit tests for the no-op client and token persistence. It does not require a real Google Drive account in CI.

```bash
go test ./googledrive/...
```

## Notes

- The client requests `drive.DriveFileScope` (full Drive file access). Use a dedicated service account or restricted credentials if your security model requires it.
- Token refresh happens automatically. A refreshed token is saved through `TokenStore.Save` only when the access token changes.
- The library does not log to stdout. It returns errors and lets the caller decide how to log.
