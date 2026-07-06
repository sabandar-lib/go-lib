package googledrive

import (
	"golang.org/x/oauth2"
)

// persistentTokenSource wraps an oauth2.TokenSource and persists refreshed
// tokens through the configured TokenStore.
type persistentTokenSource struct {
	source  oauth2.TokenSource
	store   TokenStore
	current *oauth2.Token
}

func (p *persistentTokenSource) Token() (*oauth2.Token, error) {
	token, err := p.source.Token()
	if err != nil {
		return nil, err
	}

	// Only persist when the access token changes.
	if token.AccessToken != p.current.AccessToken {
		p.current = token
		if err := p.store.Save(token); err != nil {
			return nil, err
		}
	}

	return token, nil
}
