# databasebackup

A database backup and restore library that exports every table to CSV, uploads the CSV files through a pluggable storage backend, and can restore the latest backup back into the database.

## When to use this package

Use this package when you need a Go service to:

- Periodically back up an entire database to CSV files.
- Store backups on a remote storage backend such as Google Drive, S3, or a custom destination.
- Restore the latest backup into a database.
- Prune old backups through the storage backend.

## Installation

```bash
go get github.com/sabandar-lib/go-lib/databasebackup
```

## Quick start

```go
package main

import (
    "context"
    "log"

    "github.com/sabandar-lib/go-lib/databasebackup"
    "gorm.io/gorm"
)

func main() {
    ctx := context.Background()

    var db *gorm.DB // initialize your GORM connection
    var storage databasebackup.Storage // implement or adapt your storage backend

    backup := databasebackup.New(databasebackup.Config{
        OutDirPrefix:     "/tmp/db-backup",
        TableOrder:       map[string]string{"users": "1", "orders": "2"},
        OnConflictClause: "ON CONFLICT DO NOTHING",
        BatchSize:        100,
        RetainCount:      2,
    }, storage, db)

    if err := backup.BackupAll(ctx); err != nil {
        log.Fatalf("backup failed: %v", err)
    }
}
```

## Configuration

The package is configured through `databasebackup.Config`:

```go
type Config struct {
    // Base directory for temporary local CSV files.
    OutDirPrefix string

    // Optional map of table name -> filename prefix (e.g. "users" -> "1").
    // During restore, files are processed in ascending numeric prefix order.
    // If empty, tables are processed alphabetically.
    TableOrder map[string]string

    // Optional SQL clause appended to INSERT statements, e.g. "ON CONFLICT DO NOTHING".
    // Leave empty if your dialect does not support conflict handling.
    OnConflictClause string

    // Number of rows per INSERT batch. Defaults to 100 if zero.
    BatchSize int

    // Retention count passed to Storage.Prune. Interpretation depends on the storage backend.
    RetainCount int
}
```

## Storage interface

The library does not know where backups are stored. You provide a `Storage` implementation:

```go
type Storage interface {
    Upload(ctx context.Context, folderName string, files []string) error
    DownloadLatest(ctx context.Context, outDirPrefix string) (string, error)
    Prune(ctx context.Context, retainCount int) error
}
```

- `Upload` receives the local CSV files and persists them under a folder named `folderName`.
- `DownloadLatest` downloads the most recent backup into a local directory and returns that directory path.
- `Prune` removes old backups, keeping `retainCount` recent ones.

## Supported databases

The library uses GORM and supports:

- PostgreSQL
- MySQL
- SQLite

Table discovery runs dialect-specific SQL:

| Dialect | Table list query |
|---|---|
| postgres | `SELECT tablename FROM pg_tables WHERE schemaname = 'public'` |
| mysql | `SHOW TABLES` |
| sqlite | `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'` |

## CSV format

Backup files are named `{prefix}_{table}.csv`.

Cell serialization rules:

| Go value | CSV value |
|---|---|
| `nil` | `NULL` |
| `time.Time` | RFC3339Nano in UTC |
| `[]byte` | string(bytes) |
| `bool` | `true` / `false` |
| anything else | `fmt.Sprintf("%v", v)` |

## Recommended consumer setup (local facade)

We recommend creating a thin facade in your own repo so that only one local package imports `github.com/sabandar-lib/go-lib/databasebackup`. This is especially useful when you also use `github.com/sabandar-lib/go-lib/googledrive` as the storage backend.

### Files to create in your repo

Create a directory `your-project/lib/databasebackup/` with these files.

> **Important:** set up your `your-project/lib/googledrive/` facade first (see the `googledrive` README). The `databasebackup` facade below depends on it.

#### `lib/databasebackup/remote.go`

The only file that imports the remote package.

```go
package databasebackuplib

import (
    "context"

    "github.com/sabandar-lib/go-lib/databasebackup"
    "gorm.io/gorm"
)

type remoteBackup = databasebackup.Backup
type remoteConfig = databasebackup.Config
type remoteStorage = databasebackup.Storage

func remoteNew(cfg remoteConfig, storage remoteStorage, db *gorm.DB) *remoteBackup {
    return databasebackup.New(cfg, storage, db)
}
```

#### `lib/databasebackup/storage_adapter.go`

If you use `go-lib/googledrive` as the storage backend, create this adapter. Replace `your-project` with your actual Go module path.

```go
package databasebackuplib

import (
    "context"

    "your-project/lib/googledrive"
)

type googleDriveStorageAdapter struct {
    client googledrivelib.GoogleDriveInterface
}

func newGoogleDriveStorageAdapter(client googledrivelib.GoogleDriveInterface) remoteStorage {
    return &googleDriveStorageAdapter{client: client}
}

func (a *googleDriveStorageAdapter) Upload(ctx context.Context, folderName string, files []string) error {
    return a.client.UploadToDrive(ctx, folderName, files)
}

func (a *googleDriveStorageAdapter) DownloadLatest(ctx context.Context, outDirPrefix string) (string, error) {
    return a.client.DownloadFromDrive(ctx, outDirPrefix, 1)
}

func (a *googleDriveStorageAdapter) Prune(ctx context.Context, retainCount int) error {
    return a.client.PruneOldBackups(ctx)
}
```

#### `lib/databasebackup/database_backup.go`

Replace `your-project` with your actual Go module path.

```go
package databasebackuplib

import (
    "context"

    "your-project/lib/googledrive"
    "gorm.io/gorm"
)

type DatabaseBackupInterface interface {
    BackupAll(ctx context.Context) error
    RestoreAllBackups(ctx context.Context) error
    PruneOldBackups(ctx context.Context) error
}

type databaseBackup struct {
    backup *remoteBackup
}

func New(
    outDirPrefix string,
    tableOrder map[string]string,
    onConflictClause string,
    batchSize int,
    retainCount int,
    gDriveLib googledrivelib.GoogleDriveInterface,
    db *gorm.DB,
) DatabaseBackupInterface {
    cfg := remoteConfig{
        OutDirPrefix:     outDirPrefix,
        TableOrder:       tableOrder,
        OnConflictClause: onConflictClause,
        BatchSize:        batchSize,
        RetainCount:      retainCount,
    }
    storage := newGoogleDriveStorageAdapter(gDriveLib)
    return &databaseBackup{backup: remoteNew(cfg, storage, db)}
}

func (d *databaseBackup) BackupAll(ctx context.Context) error {
    return d.backup.BackupAll(ctx)
}

func (d *databaseBackup) RestoreAllBackups(ctx context.Context) error {
    return d.backup.RestoreAll(ctx)
}

func (d *databaseBackup) PruneOldBackups(ctx context.Context) error {
    return d.backup.Prune(ctx)
}
```

### Environment variables

Add these to your `.env` file or environment:

```bash
# Required by databasebackup
export DB_BACKUP_OUT_DIR_PREFIX=/tmp/db-backup

# Required if you use Google Drive as the storage backend
export GDRIVE_CLIENT_SECRET_FILE_DIR=/etc/backup/client_secret.json
export GDRIVE_FOLDER_ID=your-google-drive-folder-id
export GDRIVE_TOKEN_FILE_DIR=/etc/backup/token.json
export GDRIVE_RETAIN_COUNT=2

# Optional: feature flags to control automated cron jobs (not the library itself)
export FEATURE_FLAG_BACKUP_DB=ON
export FEATURE_FLAG_PRUNE_DB_BACKUPS=ON
export FEATURE_FLAG_RESTORE_DB_BACKUPS=ON
export FEATURE_FLAG_CONNECT_GOOGLE_DRIVE_CLIENT=ON

# Optional: cron expressions if you run backups on a schedule
export DB_BACKUP_CRON_EXPRESSION="0 15 * * 1"
export DB_PRUNE_OLD_BACKUPS_CRON_EXPRESSION="0 15 * * 2"
```

### Table order map

Create an application-specific table order file, for example `your-project/internal/backup/table_order.go`:

```go
package backup

// TableOrder maps table names to filename prefixes so restore processes
// tables in the correct foreign-key dependency order.
var TableOrder = map[string]string{
    "roles":         "1",
    "users":         "2",
    "user_profiles": "3",
    "categories":    "4",
    "items":         "5",
    "orders":        "6",
    "order_items":   "7",
}
```

Pass this map into `databasebackuplib.New`.

> If a table is not present in `TableOrder`, the library assigns it the prefix `"0"` and processes it before any explicitly ordered tables.

### Creating the backup command

Create `cmd/backup-db/main.go` in your repo so you can run backups on demand:

```go
package main

import (
    "context"
    "log"
    "os"
    "strconv"

    "your-project/internal/backup"
    "your-project/lib/databasebackup"
    "your-project/lib/googledrive"
    "gorm.io/gorm"
    // "gorm.io/driver/postgres"
)

func main() {
    ctx := context.Background()

    // Initialize your GORM connection however your application does it.
    var db *gorm.DB
    _ = db // replace with your actual GORM initialization

    retainCount, err := strconv.Atoi(os.Getenv("GDRIVE_RETAIN_COUNT"))
    if err != nil {
        log.Fatalf("parse GDRIVE_RETAIN_COUNT: %v", err)
    }

    gdrive, err := googledrivelib.New(
        os.Getenv("GDRIVE_CLIENT_SECRET_FILE_DIR"),
        os.Getenv("GDRIVE_FOLDER_ID"),
        retainCount,
        googledrivelib.NewFileTokenStore(os.Getenv("GDRIVE_TOKEN_FILE_DIR")),
    )
    if err != nil {
        log.Fatalf("create google drive client: %v", err)
    }

    dbBackup := databasebackuplib.New(
        os.Getenv("DB_BACKUP_OUT_DIR_PREFIX"),
        backup.TableOrder,
        "ON CONFLICT DO NOTHING",
        0,
        retainCount,
        gdrive,
        db,
    )

    if err := dbBackup.BackupAll(ctx); err != nil {
        log.Fatalf("backup failed: %v", err)
    }

    log.Println("backup complete")
}
```

Run it with:

```bash
go run ./cmd/backup-db
```

### Wiring into your container

If your project uses a dependency container, construct the backup client there so it can be injected into cron jobs or HTTP handlers. Replace `your-project` with your actual Go module path.

```go
package container

import (
    "log"
    "os"
    "strconv"

    "your-project/internal/backup"
    "your-project/lib/databasebackup"
    "your-project/lib/googledrive"
    "gorm.io/gorm"
)

type ClientContainer struct {
    DatabaseBackup databasebackuplib.DatabaseBackupInterface
    GoogleDrive    googledrivelib.GoogleDriveInterface
    // ... other clients
}

func NewClientContainer(db *gorm.DB) ClientContainer {
    retainCount, err := strconv.Atoi(os.Getenv("GDRIVE_RETAIN_COUNT"))
    if err != nil {
        log.Fatalf("parse GDRIVE_RETAIN_COUNT: %v", err)
    }

    gdrive, err := googledrivelib.New(
        os.Getenv("GDRIVE_CLIENT_SECRET_FILE_DIR"),
        os.Getenv("GDRIVE_FOLDER_ID"),
        retainCount,
        googledrivelib.NewFileTokenStore(os.Getenv("GDRIVE_TOKEN_FILE_DIR")),
    )
    if err != nil {
        log.Fatalf("create google drive client: %v", err)
    }

    dbBackup := databasebackuplib.New(
        os.Getenv("DB_BACKUP_OUT_DIR_PREFIX"),
        backup.TableOrder,
        "ON CONFLICT DO NOTHING",
        0,
        retainCount,
        gdrive,
        db,
    )

    return ClientContainer{
        DatabaseBackup: dbBackup,
        GoogleDrive:    gdrive,
    }
}
```

Replace `your-project` with your actual Go module path.

```go
package main

import (
    "context"
    "log"
    "os"

    "your-project/internal/backup"
    "your-project/lib/databasebackup"
    "your-project/lib/googledrive"
    "gorm.io/gorm"
    // Also import your GORM driver, e.g.:
    // "gorm.io/driver/postgres"
)

func main() {
    ctx := context.Background()

    // Initialize your GORM connection however your application does it.
    // Example for Postgres:
    // db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    var db *gorm.DB
    _ = db // replace with your actual GORM initialization

    gdrive, err := googledrivelib.New(
        os.Getenv("GDRIVE_CLIENT_SECRET_FILE_DIR"),
        os.Getenv("GDRIVE_FOLDER_ID"),
        2,
        googledrivelib.NewFileTokenStore(os.Getenv("GDRIVE_TOKEN_FILE_DIR")),
    )
    if err != nil {
        log.Fatalf("create google drive client: %v", err)
    }

    dbBackup := databasebackuplib.New(
        os.Getenv("DB_BACKUP_OUT_DIR_PREFIX"),
        backup.TableOrder,
        "ON CONFLICT DO NOTHING",
        0,
        2,
        gdrive,
        db,
    )

    if err := dbBackup.BackupAll(ctx); err != nil {
        log.Fatalf("backup failed: %v", err)
    }

    log.Println("backup complete")
}
```

Then run `go mod tidy`.

### Restore example

To restore the latest backup, call `RestoreAllBackups` instead of `BackupAll`:

```go
if err := dbBackup.RestoreAllBackups(ctx); err != nil {
    log.Fatalf("restore failed: %v", err)
}
log.Println("restore complete")
```

This downloads the latest backup folder, processes CSV files in ascending prefix order, and inserts rows into the database using the configured `OnConflictClause`.

## Complete consumer setup checklist

If you are using Google Drive as the storage backend, follow these steps in order:

1. Add the remote module to your project:
   ```bash
   go get github.com/sabandar-lib/go-lib/googledrive
   go get github.com/sabandar-lib/go-lib/databasebackup
   ```

2. Create your local `lib/googledrive/` facade (see the `googledrive` README for the four files and OAuth2 setup).

3. Generate and save the Google Drive OAuth2 token:
   ```bash
   go run ./cmd/login-google  # or whatever command you create
   ```

4. Create your `internal/backup/table_order.go` file with the table order map.

5. Create your local `lib/databasebackup/` facade (the three files shown above).

6. Create `cmd/backup-db/main.go` and/or wire the backup client into your container (see the examples above).

7. Run `go mod tidy` and build:
   ```bash
   go mod tidy
   go build ./...
   ```

## Combining with `go-lib/googledrive`

If you use Google Drive, your storage adapter is the bridge between `databasebackup.Storage` and `googledrive.Client`. The adapter in the consumer facade section above is a complete example. In short:

- `databasebackup.Storage.Upload` → `googledrive.Client.Upload`
- `databasebackup.Storage.DownloadLatest` → `googledrive.Client.DownloadLatest`
- `databasebackup.Storage.Prune` → `googledrive.Client.PruneOldFolders`

The `RetainCount` for pruning is configured on the Google Drive client. The `databasebackup.Config.RetainCount` is passed through to the adapter but typically ignored by the Google Drive adapter.

## API reference

### `func New(cfg Config, storage Storage, db *gorm.DB) *Backup`

Creates a backup orchestrator. `storage` may be nil only if you never call `BackupAll`, `RestoreAll`, or `Prune`.

### `func (b *Backup) BackupAll(ctx context.Context) error`

Exports every table to a local CSV, uploads the files through `Storage.Upload`, then removes the local temporary directory.

### `func (b *Backup) RestoreAll(ctx context.Context) error`

Downloads the latest backup through `Storage.DownloadLatest`, then inserts each CSV back into the database in table-order prefix order.

### `func (b *Backup) Prune(ctx context.Context) error`

Delegates to `Storage.Prune` with `Config.RetainCount`.

## Dependencies

- `gorm.io/gorm`
- `github.com/sabandar-lib/go-lib/googledrive` (only if you use Google Drive as storage)

## Testing

The package includes unit tests for CSV round-trips, table ordering, batch inserts, and restore sorting. Tests use an in-memory SQLite database and a fake storage implementation.

```bash
go test ./databasebackup/...
```

## Notes

- `OnConflictClause` is dialect-specific. `"ON CONFLICT DO NOTHING"` works for PostgreSQL. For MySQL you might use `"ON DUPLICATE KEY UPDATE id=id"`. Leave it empty if you do not need conflict handling.
- Table names are quoted with double quotes in generated SQL, which is PostgreSQL-friendly. GORM translates placeholders for other dialects.
- Restore assumes backup filenames follow `{prefix}_{table}.csv`. If you change the storage backend, ensure downloaded filenames preserve this format.
- The library does not log to stdout. It returns errors and lets the caller decide how to log.
