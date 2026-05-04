# Requirements Document

## Introduction

This document defines the requirements for `go-libs`, a public Go module library hosted at `github.com/sabandar-lib/go-libs`. The library provides two reusable, fully decoupled packages — `googledrive` and `databasebackup` — intended to be imported by multiple independent Go projects. All external dependencies are injected via constructors or function parameters; the library never imports from any consuming project's internal packages.

## Glossary

- **Library**: The Go module `github.com/sabandar-lib/go-libs` as a whole.
- **GoogleDrive_Package**: The `googledrive` sub-package providing Google Drive integration via OAuth2.
- **DatabaseBackup_Package**: The `databasebackup` sub-package providing database backup and restore functionality.
- **GoogleDriveConfig**: The configuration struct passed to the `googledrive.New()` constructor.
- **DatabaseBackupConfig**: The configuration struct passed to the `databasebackup.New()` constructor.
- **GoogleDriveInterface**: The interface exposing `UploadToDrive`, `DownloadFromDrive`, and `PruneOldBackups`.
- **DatabaseBackupInterface**: The interface exposing `BackupAll`, `RestoreAllBackups`, and `PruneOldBackups`.
- **PersistentTokenSource**: An OAuth2 token source that automatically refreshes and persists the updated token to disk.
- **TimestampedFolder**: A Google Drive subfolder named after the current timestamp, created inside the configured parent folder during upload.
- **BackupCycle**: The configured rotation period for backups — one of `"daily"`, `"weekly"`, or `"monthly"`.
- **RetainMonths**: A `float64` value representing how many months of backups to retain, used to compute the keep count.
- **TableOrderMap**: A `map[string]string` mapping table names to numeric string prefixes that determine export and restore order.
- **GORM**: The Go ORM library (`gorm.io/gorm`) used for database access; the Library never manages its own DB connection.
- **Dialect**: The database engine type — one of Postgres, MySQL, or SQLite.
- **CSV**: Comma-separated values file format used for table export and import.

---

## Requirements

### Requirement 1: Module Structure and Decoupling

**User Story:** As a Go developer, I want to import `go-libs` packages independently, so that I can use only the functionality I need without pulling in unrelated dependencies.

#### Acceptance Criteria

1. THE Library SHALL declare its module path as `github.com/sabandar-lib/go-libs` in `go.mod`.
2. THE Library SHALL expose the `googledrive` package at the import path `github.com/sabandar-lib/go-libs/googledrive`.
3. THE Library SHALL expose the `databasebackup` package at the import path `github.com/sabandar-lib/go-libs/databasebackup`.
4. THE Library SHALL NOT import from any consuming project's internal packages.
5. THE Library SHALL accept all external collaborators via constructor parameters or function arguments.

---

### Requirement 2: GoogleDrive Package — Constructor and Configuration

**User Story:** As a developer, I want to instantiate a Google Drive client by passing a config struct, so that I can configure credentials and behavior without modifying library internals.

#### Acceptance Criteria

1. THE GoogleDrive_Package SHALL expose a `New(cfg GoogleDriveConfig) GoogleDriveInterface` constructor function.
2. THE GoogleDriveConfig SHALL contain the following fields: `GDriveClientSecretFileDir string`, `GDriveFolderID string`, `GDriveTokenFileDir string`, `GDriveBackupCycle string`, and `GDriveRetainMonths float64`.
3. WHEN `New` is called with a valid `GoogleDriveConfig`, THE GoogleDrive_Package SHALL return a value that satisfies `GoogleDriveInterface`.
4. THE GoogleDriveInterface SHALL declare the methods `UploadToDrive(ctx context.Context, timeStampDir string, files []string) error`, `DownloadFromDrive(ctx context.Context, outDir string, pageSize int64) (string, error)`, and `PruneOldBackups(ctx context.Context) error`.

---

### Requirement 3: OAuth2 Persistent Token Source

**User Story:** As a developer, I want the library to automatically refresh and persist OAuth2 tokens, so that long-running services do not require manual re-authentication.

#### Acceptance Criteria

1. THE PersistentTokenSource SHALL load the OAuth2 token from the file path specified by `GDriveTokenFileDir` at initialisation.
2. WHEN the OAuth2 access token is refreshed, THE PersistentTokenSource SHALL write the updated token back to the file at `GDriveTokenFileDir`.
3. THE PersistentTokenSource SHALL use the client secret file at `GDriveClientSecretFileDir` to configure the OAuth2 client.
4. IF the token file does not exist at the path specified by `GDriveTokenFileDir`, THEN THE PersistentTokenSource SHALL return an error indicating the token file is missing.

---

### Requirement 4: GoogleDrive Login Function

**User Story:** As a developer, I want a standalone `Login()` function that performs the OAuth2 browser flow, so that I can generate `token.json` on a developer machine before deploying a service.

#### Acceptance Criteria

1. THE GoogleDrive_Package SHALL expose a standalone `Login(cfg GoogleDriveConfig) error` function.
2. WHEN `Login` is called, THE GoogleDrive_Package SHALL initiate the OAuth2 browser-based authorisation flow using the client secret at `GDriveClientSecretFileDir`.
3. WHEN the OAuth2 authorisation flow completes successfully, THE GoogleDrive_Package SHALL write the resulting token to the file at `GDriveTokenFileDir`.
4. IF the OAuth2 authorisation flow fails, THEN THE GoogleDrive_Package SHALL return a descriptive error.

---

### Requirement 5: UploadToDrive

**User Story:** As a developer, I want to upload a set of files to a timestamped Google Drive subfolder, so that each backup is stored in an isolated, identifiable location.

#### Acceptance Criteria

1. WHEN `UploadToDrive` is called with a `timeStampDir` string and a list of file paths, THE GoogleDriveInterface SHALL create a subfolder named `timeStampDir` inside the Google Drive folder identified by `GDriveFolderID`.
2. WHEN the subfolder is created, THE GoogleDriveInterface SHALL upload each file in `files` into that subfolder.
3. IF creating the subfolder fails, THEN THE GoogleDriveInterface SHALL return a descriptive error without uploading any files.
4. IF uploading any file fails, THEN THE GoogleDriveInterface SHALL return a descriptive error identifying the failed file.

---

### Requirement 6: DownloadFromDrive

**User Story:** As a developer, I want to download all CSV files from the most recent backup folder in Google Drive, so that I can restore the latest backup locally.

#### Acceptance Criteria

1. WHEN `DownloadFromDrive` is called, THE GoogleDriveInterface SHALL list folders inside the Google Drive folder identified by `GDriveFolderID`, ordered by `createdTime` descending, up to `pageSize` results.
2. WHEN the folder list is retrieved, THE GoogleDriveInterface SHALL select the most recently created folder.
3. WHEN the most recent folder is identified, THE GoogleDriveInterface SHALL download all CSV files from that folder into the local directory `outDir`.
4. WHEN all files are downloaded successfully, THE GoogleDriveInterface SHALL return the name of the folder from which files were downloaded.
5. IF no folders exist in the configured Drive folder, THEN THE GoogleDriveInterface SHALL return an empty string and a descriptive error.
6. IF downloading any file fails, THEN THE GoogleDriveInterface SHALL return an empty string and a descriptive error.

---

### Requirement 7: PruneOldBackups (GoogleDrive)

**User Story:** As a developer, I want old backup folders in Google Drive to be automatically deleted based on the configured retention policy, so that storage usage stays bounded.

#### Acceptance Criteria

1. WHEN `PruneOldBackups` is called, THE GoogleDriveInterface SHALL list all folders inside the Google Drive folder identified by `GDriveFolderID`, ordered by `createdTime` descending.
2. THE GoogleDriveInterface SHALL compute the keep count using the following rules: if `GDriveBackupCycle` is `"daily"`, keep count equals `30 * GDriveRetainMonths`; if `GDriveBackupCycle` is `"weekly"`, keep count equals `4 * GDriveRetainMonths`; if `GDriveBackupCycle` is `"monthly"`, keep count equals `1 * GDriveRetainMonths`.
3. WHEN the keep count is computed, THE GoogleDriveInterface SHALL retain the most recent N folders (where N equals the keep count) and delete all remaining folders.
4. IF `GDriveBackupCycle` is not one of `"daily"`, `"weekly"`, or `"monthly"`, THEN THE GoogleDriveInterface SHALL return a descriptive error.
5. IF deleting a folder fails, THEN THE GoogleDriveInterface SHALL return a descriptive error identifying the folder.

---

### Requirement 8: DatabaseBackup Package — Constructor and Configuration

**User Story:** As a developer, I want to instantiate a database backup client by injecting a config, a Google Drive client, and a feature flag, so that I can control backup behaviour without modifying library internals.

#### Acceptance Criteria

1. THE DatabaseBackup_Package SHALL expose a `New(cfg DatabaseBackupConfig, gDriveLib GoogleDriveInterface, restoreFeatureFlag bool) DatabaseBackupInterface` constructor function.
2. THE DatabaseBackupConfig SHALL contain the field `DBBackupOutDirPrefix string`.
3. WHEN `New` is called with a valid `DatabaseBackupConfig`, a `GoogleDriveInterface` implementation, and a `restoreFeatureFlag`, THE DatabaseBackup_Package SHALL return a value that satisfies `DatabaseBackupInterface`.
4. THE DatabaseBackupInterface SHALL declare the methods `BackupAll(ctx context.Context, db *gorm.DB, tableOrderMap map[string]string) error`, `RestoreAllBackups(ctx context.Context, db *gorm.DB, tableOrderMap map[string]string) error`, and `PruneOldBackups(ctx context.Context) error`.
5. THE DatabaseBackup_Package SHALL never open or manage its own database connection; the `*gorm.DB` instance SHALL always be provided by the caller.

---

### Requirement 9: BackupAll

**User Story:** As a developer, I want to export all database tables to CSV and upload them to Google Drive in a single call, so that I can automate database backups.

#### Acceptance Criteria

1. WHEN `BackupAll` is called, THE DatabaseBackupInterface SHALL create a temporary local directory using `DBBackupOutDirPrefix` as the path prefix.
2. WHEN the temporary directory is created, THE DatabaseBackupInterface SHALL export each table listed in `tableOrderMap` to a CSV file named `{prefix}_{tableName}.csv`, where `{prefix}` is the numeric string value from `tableOrderMap` for that table.
3. WHEN all CSV files are written, THE DatabaseBackupInterface SHALL upload the CSV files to Google Drive by calling `UploadToDrive` on the injected `GoogleDriveInterface`.
4. WHEN the upload completes successfully, THE DatabaseBackupInterface SHALL delete the temporary local directory.
5. IF exporting any table fails, THEN THE DatabaseBackupInterface SHALL return a descriptive error without uploading.
6. IF the upload fails, THEN THE DatabaseBackupInterface SHALL return a descriptive error and still attempt to delete the temporary directory.

---

### Requirement 10: CSV Export Type Handling

**User Story:** As a developer, I want database values to be serialised to CSV in a consistent, reversible format, so that restores produce the same data that was backed up.

#### Acceptance Criteria

1. WHEN a database column value is `nil`, THE DatabaseBackup_Package SHALL write the string `"NULL"` to the CSV cell.
2. WHEN a database column value is of type `time.Time`, THE DatabaseBackup_Package SHALL format it as RFC3339Nano in UTC and write the result to the CSV cell.
3. WHEN a database column value is of type `[]byte`, THE DatabaseBackup_Package SHALL convert it to a UTF-8 string and write the result to the CSV cell (to handle JSONB and similar binary-encoded text columns).
4. WHEN a database column value is of type `bool`, THE DatabaseBackup_Package SHALL write `"true"` or `"false"` to the CSV cell.
5. FOR ALL supported column types, parsing the exported CSV and re-exporting SHALL produce an equivalent CSV (round-trip property).

---

### Requirement 11: Table Listing by Dialect

**User Story:** As a developer, I want the library to discover tables automatically for Postgres, MySQL, and SQLite, so that I do not need to hard-code table names.

#### Acceptance Criteria

1. WHEN `BackupAll` is called with a Postgres `*gorm.DB`, THE DatabaseBackup_Package SHALL query `information_schema.tables` to list all user tables in the current schema.
2. WHEN `BackupAll` is called with a MySQL `*gorm.DB`, THE DatabaseBackup_Package SHALL query `information_schema.tables` to list all tables in the current database.
3. WHEN `BackupAll` is called with a SQLite `*gorm.DB`, THE DatabaseBackup_Package SHALL query `sqlite_master` to list all tables.
4. IF the database dialect is not one of Postgres, MySQL, or SQLite, THEN THE DatabaseBackup_Package SHALL return a descriptive error.

---

### Requirement 12: RestoreAllBackups

**User Story:** As a developer, I want to restore the latest backup from Google Drive into the database, so that I can recover data after a failure.

#### Acceptance Criteria

1. WHEN `RestoreAllBackups` is called and `restoreFeatureFlag` is `false`, THE DatabaseBackupInterface SHALL return immediately without performing any restore operation.
2. WHEN `RestoreAllBackups` is called and `restoreFeatureFlag` is `true`, THE DatabaseBackupInterface SHALL download the latest backup from Google Drive by calling `DownloadFromDrive` on the injected `GoogleDriveInterface`.
3. WHEN the backup files are downloaded, THE DatabaseBackupInterface SHALL restore each CSV file into the corresponding database table in ascending order of the numeric prefix in the file name.
4. WHEN all tables are restored, THE DatabaseBackupInterface SHALL delete the temporary local directory containing the downloaded CSV files.
5. IF downloading the backup fails, THEN THE DatabaseBackupInterface SHALL return a descriptive error.
6. IF restoring any table fails, THEN THE DatabaseBackupInterface SHALL return a descriptive error identifying the table.

---

### Requirement 13: CSV Restore Insert Strategy

**User Story:** As a developer, I want CSV rows to be inserted using conflict-safe batched inserts, so that restores are idempotent and do not fail on duplicate rows.

#### Acceptance Criteria

1. WHEN restoring a CSV file, THE DatabaseBackup_Package SHALL use `INSERT INTO ... ON CONFLICT DO NOTHING` for each row.
2. THE DatabaseBackup_Package SHALL insert rows in batches of 100 rows per statement.
3. IF a batch insert fails for a reason other than a conflict, THEN THE DatabaseBackup_Package SHALL return a descriptive error identifying the table and batch.
4. FOR ALL valid CSV backup files, restoring the same file twice SHALL result in the same database state as restoring it once (idempotence property).

---

### Requirement 14: PruneOldBackups (DatabaseBackup)

**User Story:** As a developer, I want the database backup package to delegate pruning to the Google Drive package, so that retention logic is not duplicated.

#### Acceptance Criteria

1. WHEN `PruneOldBackups` is called on `DatabaseBackupInterface`, THE DatabaseBackupInterface SHALL delegate the call to `PruneOldBackups` on the injected `GoogleDriveInterface`.
2. IF the delegated `PruneOldBackups` call returns an error, THEN THE DatabaseBackupInterface SHALL propagate that error to the caller.
