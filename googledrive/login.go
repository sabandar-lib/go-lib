package googledrive

import (
	"context"
	"fmt"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
)

// Login runs the OAuth2 browser-based authorisation flow and writes the
// resulting token to cfg.GDriveTokenFileDir.
//
// The caller must open the printed URL in a browser, grant access, and paste
// the authorisation code back into stdin.
//
// Returns a wrapped error if the OAuth2 exchange fails.
func Login(cfg GoogleDriveConfig) error {
	// 1. Read the client secret file.
	secretData, err := os.ReadFile(cfg.GDriveClientSecretFileDir)
	if err != nil {
		return fmt.Errorf("failed to read client secret: %w", err)
	}

	// 2. Parse the OAuth2 config from the secret file.
	oauthCfg, err := google.ConfigFromJSON(secretData, drive.DriveScope)
	if err != nil {
		return fmt.Errorf("failed to parse client secret: %w", err)
	}

	// 3. Generate the authorisation URL.
	authURL := oauthCfg.AuthCodeURL("state-token", oauth2.AccessTypeOffline)

	// 4. Print the URL for the user to visit.
	fmt.Printf("Go to the following link in your browser then type the authorization code: \n%v\n", authURL)

	// 5. Read the authorisation code from stdin.
	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		return fmt.Errorf("failed to read authorization code: %w", err)
	}

	// 6. Exchange the code for a token.
	token, err := oauthCfg.Exchange(context.Background(), authCode)
	if err != nil {
		return fmt.Errorf("oauth2 exchange failed: %w", err)
	}

	// 7. Write the token to disk.
	return writeTokenFile(cfg.GDriveTokenFileDir, token)
}
