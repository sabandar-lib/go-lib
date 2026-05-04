package googledrive

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
)

// persistentTokenSource is an oauth2.TokenSource that persists refreshed tokens
// to disk so that long-running services survive token expiry without manual
// intervention.
type persistentTokenSource struct {
	mu        sync.Mutex
	tokenFile string
	source    oauth2.TokenSource // wraps oauth2.ReuseTokenSource
	lastToken *oauth2.Token      // used to detect refreshes by comparing Expiry
}

// Token implements oauth2.TokenSource. It delegates to the inner source and,
// if the returned token has a different Expiry than the last known token
// (indicating a refresh occurred), writes the new token to disk under a mutex.
func (p *persistentTokenSource) Token() (*oauth2.Token, error) {
	tok, err := p.source.Token()
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Detect a refresh by comparing the token's Expiry to the last known value.
	if p.lastToken == nil || !tok.Expiry.Equal(p.lastToken.Expiry) {
		if writeErr := writeTokenFile(p.tokenFile, tok); writeErr != nil {
			// Non-fatal: return the token even if we couldn't persist it.
			_ = writeErr
		}
		p.lastToken = tok
	}

	return tok, nil
}

// writeTokenFile serialises tok to JSON and writes it to path.
func writeTokenFile(path string, tok *oauth2.Token) error {
	data, err := json.Marshal(tok)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// newPersistentTokenSource constructs an oauth2.TokenSource that reads the
// initial token from cfg.GDriveTokenFileDir, refreshes it automatically via
// the OAuth2 library, and persists any refreshed token back to disk.
func newPersistentTokenSource(cfg GoogleDriveConfig) (oauth2.TokenSource, error) {
	// 1. Read and unmarshal the stored token.
	tokenData, err := os.ReadFile(cfg.GDriveTokenFileDir)
	if err != nil {
		return nil, fmt.Errorf("token file not found: %w", err)
	}

	var tok oauth2.Token
	if err := json.Unmarshal(tokenData, &tok); err != nil {
		return nil, fmt.Errorf("failed to parse token file: %w", err)
	}

	// 2. Read and parse the client secret file.
	secretData, err := os.ReadFile(cfg.GDriveClientSecretFileDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read client secret: %w", err)
	}

	oauthCfg, err := google.ConfigFromJSON(secretData, drive.DriveScope)
	if err != nil {
		return nil, fmt.Errorf("failed to read client secret: %w", err)
	}

	// 3. Build a ReuseTokenSource so the underlying HTTP transport only
	//    refreshes when the token is actually expired.
	ctx := context.Background()
	reuseSource := oauth2.ReuseTokenSource(&tok, oauthCfg.TokenSource(ctx, &tok))

	// 4. Wrap in persistentTokenSource to persist refreshes to disk.
	return &persistentTokenSource{
		tokenFile: cfg.GDriveTokenFileDir,
		source:    reuseSource,
		lastToken: &tok,
	}, nil
}
