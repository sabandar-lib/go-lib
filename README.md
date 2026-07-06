# go-lib

A collection of reusable Go libraries published at `github.com/sabandar-lib/go-lib`.

This repository contains small, focused packages that can be consumed by any Go project. Each package is designed to be self-contained, tested, and versioned independently through Go module semantic versioning.

## Available packages

Each package README is a self-contained implementation guide. It includes the exact files to create, environment variables to set, OAuth2 setup steps, and copy-pasteable code snippets for a fresh repository.

| Package | Path | Purpose |
|---|---|---|
| `googledrive` | `github.com/sabandar-lib/go-lib/googledrive` | OAuth2 Google Drive client for uploading, downloading, and pruning backup folders. |
| `databasebackup` | `github.com/sabandar-lib/go-lib/databasebackup` | Export/restore a whole database to/from CSV through a pluggable storage backend. |

If you plan to use both packages together (Google Drive as the storage backend for database backups), read the `googledrive` README first, then the `databasebackup` README.

## Installation

Add the package you need to your module:

```bash
go get github.com/sabandar-lib/go-lib/googledrive
go get github.com/sabandar-lib/go-lib/databasebackup
```

Then import it in your Go code:

```go
import "github.com/sabandar-lib/go-lib/googledrive"
import "github.com/sabandar-lib/go-lib/databasebackup"
```

## Versioning

This module follows [semantic versioning](https://semver.org/). Pin to a released tag in your `go.mod`:

```text
require github.com/sabandar-lib/go-lib v0.1.1-beta
```

Breaking changes will bump the major version. New features bump the minor version. Bug fixes bump the patch version.

## Recommended integration pattern

Each package in this repo is intentionally decoupled from application concerns such as configuration loading, feature flags, and logging. We recommend creating a thin local facade in your own repository (for example, under `your-project/lib/googledrive/` and `your-project/lib/databasebackup/`). This gives you:

- **Single point of change** when the remote library is updated.
- **Application-specific defaults** (env var names, file paths, table ordering).
- **Easier testing** because you can swap the facade in one place.

Direct usage is also perfectly fine if you prefer a simpler setup. See each package's README for both options.

## Sample consumer repository layout

After following the package READMEs, a typical consumer project looks like this:

```text
your-project/
├── .env
├── cmd/
│   ├── login-google/
│   │   └── main.go          # calls googledrivelib.Login and saves the token
│   └── backup-db/
│       └── main.go          # calls databasebackuplib.BackupAll on demand
├── config/
│   └── ...                  # loads env vars used by your app
├── internal/
│   └── backup/
│       └── table_order.go   # application-specific table restore order
├── lib/
│   ├── googledrive/         # local facade over github.com/sabandar-lib/go-lib/googledrive
│   │   ├── remote.go
│   │   ├── google_drive.go
│   │   ├── token_store.go
│   │   └── login.go
│   └── databasebackup/      # local facade over github.com/sabandar-lib/go-lib/databasebackup
│       ├── remote.go
│       ├── storage_adapter.go
│       └── database_backup.go
├── transport/
│   └── container/
│       └── client.go        # constructs the facades and injects them
├── go.mod
└── Makefile
```

This layout keeps all remote-library imports inside `lib/`, making upgrades and testing straightforward.

## Development

Clone the repository and run tests:

```bash
git clone https://github.com/sabandar-lib/go-lib.git
cd go-lib
go test ./...
```

## Testing policy

Packages include unit tests that do not require real external services. Google Drive integration is tested with fakes; database backup round-trips use an in-memory SQLite database.

## License

[Add your license here]
