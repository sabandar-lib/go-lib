# Implementation Plan: go-libs

## Overview

Implement the `go-libs` Go module at `github.com/sabandar-lib/go-libs`, providing two independent packages: `googledrive` (OAuth2-authenticated Google Drive client) and `databasebackup` (CSV-based database backup/restore orchestrator). Tasks proceed from module scaffolding through each package's core types, internal helpers, public API, and property-based tests using `pgregory.net/rapid`.

## Tasks

- [x] 1. Scaffold the Go module
  - Create `go.mod` declaring module path `github.com/sabandar-lib/go-libs` with Go 1.21 or later
  - Add direct dependencies: `gorm.io/gorm`, `golang.org/x/oauth2`, `google.golang.org/api/drive/v3`, `pgregory.net/rapid`
  - Create top-level directory structure: `googledrive/` and `databasebackup/`
  - Create a minimal `go.sum` by running `go mod tidy`
  - _Requirements: 1.1, 1.2, 1.3_

- [x] 2. Implement `googledrive` — config and Keep() helper
  - [x] 2.1 Create `googledrive/config.go`
    - Define `GoogleDriveConfig` struct with all five fields: `GDriveClientSecretFileDir`, `GDriveFolderID`, `GDriveTokenFileDir`, `GDriveBackupCycle`, `GDriveRetainMonths`
    - Implement unexported `keep(cfg GoogleDriveConfig) (int, error)` helper that applies the keep-count formula and returns an error for unrecognised cycles
    - _Requirements: 2.2, 7.2, 7.4_

  - [x]* 2.2 Write property tests for `keep()` (Properties 7 and 8)
    - **Property 7: Keep count formula is correct** — generate random `float64` retainMonths > 0 and each valid cycle; assert result equals `int(multiplier * retainMonths)`
    - **Property 8: Invalid backup cycle returns an error** — generate random strings filtered to exclude `"daily"`, `"weekly"`, `"monthly"`; assert non-nil error
    - Tag each test: `// Feature: go-libs, Property 7: ...` and `// Feature: go-libs, Property 8: ...`
    - Place in `googledrive/config_test.go`
    - _Requirements: 7.2, 7.4_

- [x] 3. Implement `googledrive` — persistent token source
  - [x] 3.1 Create `googledrive/token.go`
    - Define `persistentTokenSource` struct with `mu sync.Mutex`, `tokenFile string`, and `source oauth2.TokenSource`
    - Implement `Token() (*oauth2.Token, error)` — delegate to inner source, detect refresh by comparing expiry, write updated token to disk under mutex
    - Implement unexported `newPersistentTokenSource(cfg GoogleDriveConfig) (oauth2.TokenSource, error)` — reads token file, reads client secret, builds `oauth2.ReuseTokenSource`, wraps in `persistentTokenSource`
    - Return wrapped errors matching the error-handling table in the design
    - _Requirements: 3.1, 3.2, 3.3, 3.4_

  - [x]* 3.2 Write property tests for `persistentTokenSource` (Properties 1 and 2)
    - **Property 1: Token load round-trip** — generate random `oauth2.Token` values, serialise to a temp file, initialise source, call `Token()`, assert `AccessToken`, `RefreshToken`, and `Expiry` match
    - **Property 2: Token persistence after refresh** — generate random token pairs (original + refreshed); simulate refresh; assert token file contains refreshed values
    - Tag each test: `// Feature: go-libs, Property 1: ...` and `// Feature: go-libs, Property 2: ...`
    - Place in `googledrive/token_test.go`
    - _Requirements: 3.1, 3.2_

  - [x]* 3.3 Write unit tests for `persistentTokenSource` edge cases
    - Test that `newPersistentTokenSource` returns an error when the token file does not exist
    - Place in `googledrive/token_test.go`
    - _Requirements: 3.4_

- [x] 4. Implement `googledrive` — internal Drive file helpers
  - [x] 4.1 Create `googledrive/files.go`
    - Implement `createFolder(svc *drive.Service, name, parentID string) (string, error)` — creates a Drive folder inside `parentID`, returns the new folder ID
    - Implement `uploadFile(svc *drive.Service, localPath, parentID string) error` — opens the local file and uploads it to Drive; wraps errors with the file path
    - Implement `listFiles(svc *drive.Service, parentID string, pageSize int64, orderBy string) ([]*drive.File, error)` — lists files/folders with the given ordering
    - Implement `downloadFile(svc *drive.Service, fileID, destPath string) error` — downloads a single file by ID to `destPath`
    - _Requirements: 5.1, 5.2, 6.1, 6.3, 7.1_

- [x] 5. Implement `googledrive` — public Drive methods
  - [x] 5.1 Create `googledrive/drive.go`
    - Implement `UploadToDrive(ctx context.Context, timeStampDir string, files []string) error` — calls `createFolder` then `uploadFile` for each file; returns error identifying failed file
    - Implement `DownloadFromDrive(ctx context.Context, outDir string, pageSize int64) (string, error)` — lists folders ordered by `createdTime desc`, selects the first, downloads all CSV files into `outDir`, returns folder name
    - Implement `PruneOldBackups(ctx context.Context) error` — lists all folders, calls `keep()`, retains the N most recent, deletes the rest
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 6.1, 6.2, 6.3, 6.4, 6.5, 6.6, 7.1, 7.2, 7.3, 7.4, 7.5_

  - [x]* 5.2 Write property tests for Drive methods (Properties 3, 4, 5, 6, 9)
    - **Property 3: All files uploaded to subfolder** — generate random `[]string` of file paths with a mock Drive service; assert exactly one `uploadFile` call per file, all targeting the created subfolder
    - **Property 4: Upload failure identifies failed file** — generate random file lists and a random failure index; assert error message contains the failed file's name
    - **Property 5: Most-recent folder selected for download** — generate random folder lists with distinct `createdTime` values; assert the folder with the latest time is selected and its name returned
    - **Property 6: All CSV files downloaded** — generate random N (file count); assert exactly N files are downloaded into `outDir`
    - **Property 9: Correct folders retained and deleted** — generate random folder lists (N ≥ 0) and keep count K (0 ≤ K ≤ N); assert min(K, N) retained and max(0, N−K) deleted
    - Tag each test with its property number
    - Place in `googledrive/drive_test.go`
    - _Requirements: 5.2, 5.4, 6.2, 6.4, 7.3_

  - [x]* 5.3 Write unit tests for Drive method edge cases
    - Test `DownloadFromDrive` returns empty string and error when no folders exist
    - Test `PruneOldBackups` returns error when a folder deletion fails
    - Place in `googledrive/drive_test.go`
    - _Requirements: 6.5, 7.5_

- [x] 6. Implement `googledrive` — Login function and package entry point
  - [x] 6.1 Create `googledrive/login.go`
    - Implement standalone `Login(cfg GoogleDriveConfig) error` — reads client secret, runs `oauth2.Config.AuthCodeURL` + `Exchange` browser flow, writes resulting token to `GDriveTokenFileDir`
    - Return wrapped error on OAuth2 exchange failure
    - _Requirements: 4.1, 4.2, 4.3, 4.4_

  - [x] 6.2 Create `googledrive/googledrive.go`
    - Declare `GoogleDriveInterface` with methods `UploadToDrive`, `DownloadFromDrive`, `PruneOldBackups`
    - Define unexported `googleDriveClient` struct holding `cfg GoogleDriveConfig` and `service *drive.Service`
    - Implement `New(cfg GoogleDriveConfig) GoogleDriveInterface` — calls `newPersistentTokenSource`, builds `*drive.Service`, returns `*googleDriveClient`
    - _Requirements: 2.1, 2.3, 2.4_

  - [x]* 6.3 Write unit tests for `Login` and `New`
    - Test `New()` returns a non-nil `GoogleDriveInterface` given a valid config
    - Test `Login()` returns an error when the OAuth2 flow fails
    - Place in `googledrive/login_test.go` and `googledrive/googledrive_test.go`
    - _Requirements: 2.1, 2.3, 4.4_

- [x] 7. Checkpoint — googledrive package complete
  - Ensure all tests pass, ask the user if questions arise.

- [x] 8. Implement `databasebackup` — config and CSV serialisation helpers
  - [x] 8.1 Create `databasebackup/config.go`
    - Define `DatabaseBackupConfig` struct with field `DBBackupOutDirPrefix string`
    - _Requirements: 8.2_

  - [x] 8.2 Create `databasebackup/export.go` — serialisation and table listing
    - Implement unexported `serializeValue(val interface{}) string` — handles `nil` → `"NULL"`, `time.Time` → RFC3339Nano UTC, `[]byte` → UTF-8 string, `bool` → `"true"`/`"false"`, all others → `fmt.Sprintf("%v", val)`
    - Implement `getTables(db *gorm.DB) ([]string, error)` — switches on `db.Dialector.Name()` for `"postgres"`, `"mysql"`, `"sqlite"`; returns error for unsupported dialects
    - Implement `exportTable(db *gorm.DB, tableName, prefix, outDir string) error` — queries all rows, writes CSV with header row and serialised data rows to `{prefix}_{tableName}.csv`
    - Implement `exportAllTables(db *gorm.DB, tableOrderMap map[string]string, outDir string) error` — iterates `tableOrderMap`, calls `exportTable` for each entry
    - _Requirements: 9.2, 10.1, 10.2, 10.3, 10.4, 11.1, 11.2, 11.3, 11.4_

  - [x]* 8.3 Write property tests for serialisation and export (Properties 10, 11, 12, 13, 14)
    - **Property 10: Unsupported dialect returns an error** — generate random dialect name strings not in `{"postgres","mysql","sqlite"}`; assert `getTables` returns non-nil error
    - **Property 11: CSV export file naming** — generate random `map[string]string` tableOrderMaps; assert exactly N CSV files created, each named `{prefix}_{tableName}.csv`
    - **Property 12: time.Time RFC3339Nano UTC** — generate random `time.Time` values; assert `serializeValue` output parses back with `time.Parse(time.RFC3339Nano, s)` and equals original in UTC
    - **Property 13: []byte round-trip** — generate random valid UTF-8 byte slices; assert `serializeValue` output converted back to `[]byte` equals original
    - **Property 14: CSV export/import round-trip** — generate random rows with mixed supported types; assert export then parse produces equivalent values
    - Tag each test with its property number
    - Place in `databasebackup/export_test.go`
    - _Requirements: 10.2, 10.3, 10.5, 11.4_

  - [x]* 8.4 Write unit tests for serialisation and dialect examples
    - Test `nil` column value serialises to `"NULL"`
    - Test `bool` values serialise to `"true"` / `"false"`
    - Test Postgres dialect uses `information_schema.tables` query
    - Test MySQL dialect uses `information_schema.tables` query
    - Test SQLite dialect uses `sqlite_master` query
    - Place in `databasebackup/export_test.go`
    - _Requirements: 10.1, 10.4, 11.1, 11.2, 11.3_

- [x] 9. Implement `databasebackup` — restore helpers
  - [x] 9.1 Create `databasebackup/restore.go`
    - Implement `downloadAllBackup(ctx context.Context, gDriveLib GoogleDriveInterface, outDir string, pageSize int64) (string, error)` — delegates to `gDriveLib.DownloadFromDrive`
    - Implement `insertBatch(db *gorm.DB, tableName string, columns []string, rows [][]string) error` — builds `INSERT INTO "tableName" (...) VALUES (...) ON CONFLICT DO NOTHING` with positional parameters for up to 100 rows; wraps error with table name and batch number
    - Implement `restoreTable(db *gorm.DB, csvPath string) error` — reads CSV header and data rows, calls `insertBatch` in batches of 100
    - _Requirements: 12.2, 12.3, 13.1, 13.2, 13.3_

  - [x]* 9.2 Write property tests for restore helpers (Properties 15, 16, 17)
    - **Property 15: Restore order is ascending by numeric prefix** — generate random sets of CSV filenames with distinct numeric string prefixes; assert `RestoreAllBackups` processes files in ascending lexicographic order of prefix
    - **Property 16: Batch insert uses ON CONFLICT DO NOTHING** — generate random row counts R ≥ 0; assert generated SQL uses `ON CONFLICT DO NOTHING` and number of INSERT statements equals `ceil(R / 100)`
    - **Property 17: Restore idempotence** — generate random valid CSV backup files with a mock DB; assert restoring twice produces the same state as restoring once
    - Tag each test with its property number
    - Place in `databasebackup/restore_test.go`
    - _Requirements: 12.3, 13.1, 13.2, 13.4_

  - [x]* 9.3 Write unit tests for restore edge cases
    - Test `RestoreAllBackups` returns error identifying the table when restore fails
    - Test batch insert failure returns error identifying table and batch number
    - Place in `databasebackup/restore_test.go`
    - _Requirements: 12.6, 13.3_

- [x] 10. Implement `databasebackup` — BackupAll, RestoreAllBackups, PruneOldBackups, and package entry point
  - [x] 10.1 Create `databasebackup/backup.go`
    - Implement `BackupAll(ctx context.Context, db *gorm.DB, tableOrderMap map[string]string) error` — creates temp dir with `os.MkdirTemp` using `DBBackupOutDirPrefix`, calls `exportAllTables`, calls `UploadToDrive`, defers `os.RemoveAll`; returns error without uploading if export fails
    - Implement `RestoreAllBackups(ctx context.Context, db *gorm.DB, tableOrderMap map[string]string) error` — returns immediately if `restoreFeatureFlag` is false; otherwise creates temp dir, calls `downloadAllBackup`, restores each CSV in ascending prefix order, defers `os.RemoveAll`
    - Implement `PruneOldBackups(ctx context.Context) error` — delegates to `gDriveLib.PruneOldBackups(ctx)` and propagates any error
    - _Requirements: 9.1, 9.2, 9.3, 9.4, 9.5, 9.6, 12.1, 12.2, 12.3, 12.4, 12.5, 12.6, 14.1, 14.2_

  - [x] 10.2 Create `databasebackup/databasebackup.go`
    - Declare `GoogleDriveInterface` (re-export or import from `googledrive` package) for use in constructor signature
    - Declare `DatabaseBackupInterface` with methods `BackupAll`, `RestoreAllBackups`, `PruneOldBackups`
    - Define unexported `databaseBackupClient` struct with `cfg DatabaseBackupConfig`, `gDriveLib GoogleDriveInterface`, `restoreFeatureFlag bool`
    - Implement `New(cfg DatabaseBackupConfig, gDriveLib GoogleDriveInterface, restoreFeatureFlag bool) DatabaseBackupInterface`
    - _Requirements: 8.1, 8.3, 8.4, 8.5_

  - [x]* 10.3 Write unit tests for `databasebackup` orchestration
    - Test `New()` returns a non-nil `DatabaseBackupInterface`
    - Test `RestoreAllBackups` returns immediately when `restoreFeatureFlag` is false
    - Test `RestoreAllBackups` calls `DownloadFromDrive` when `restoreFeatureFlag` is true
    - Test `PruneOldBackups` delegates to the injected `GoogleDriveInterface`
    - Test `PruneOldBackups` propagates errors from the injected `GoogleDriveInterface`
    - Test `BackupAll` returns error without uploading when table export fails
    - Test `BackupAll` returns error but still deletes temp dir when upload fails
    - Test `RestoreAllBackups` returns error when download fails
    - Place in `databasebackup/databasebackup_test.go`
    - _Requirements: 8.3, 9.5, 9.6, 12.1, 12.5, 14.1, 14.2_

- [x] 11. Final checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Property tests use `pgregory.net/rapid` with a minimum of 100 iterations per property; each test is tagged with `// Feature: go-libs, Property N: {property_text}`
- Unit tests use standard `testing` and `testify/assert`
- `BackupAll` and `RestoreAllBackups` use `defer os.RemoveAll(tmpDir)` to guarantee temp directory cleanup
- Column names in batch INSERT statements are quoted with `"` (ANSI SQL, compatible with Postgres, MySQL ANSI mode, and SQLite)
- The `databasebackup` package depends on `googledrive` only through `GoogleDriveInterface` — never through the concrete type
