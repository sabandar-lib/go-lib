package googledrive

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/oauth2"
)

type fakeTokenStore struct {
	saved []*oauth2.Token
	load  *oauth2.Token
	err   error
}

func (f *fakeTokenStore) Save(token *oauth2.Token) error {
	if f.err != nil {
		return f.err
	}
	f.saved = append(f.saved, token)
	return nil
}

func (f *fakeTokenStore) Load() (*oauth2.Token, error) {
	return f.load, nil
}

type staticTokenSource struct {
	token *oauth2.Token
	err   error
}

func (s *staticTokenSource) Token() (*oauth2.Token, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.token, nil
}

func TestNewNop(t *testing.T) {
	client := NewNop()

	tests := []struct {
		name string
		fn   func() error
	}{
		{
			name: "upload returns nil",
			fn: func() error {
				return client.Upload(context.Background(), "folder", []string{"a.csv"})
			},
		},
		{
			name: "download latest returns empty string and nil error",
			fn: func() error {
				out, err := client.DownloadLatest(context.Background(), "/tmp")
				if err != nil {
					return err
				}
				if out != "" {
					return errors.New("expected empty output dir")
				}
				return nil
			},
		},
		{
			name: "prune returns nil",
			fn: func() error {
				return client.PruneOldFolders(context.Background())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.fn(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestPersistentTokenSource_SavesOnChange(t *testing.T) {
	store := &fakeTokenStore{}

	first := &oauth2.Token{AccessToken: "first", RefreshToken: "refresh"}
	second := &oauth2.Token{AccessToken: "second", RefreshToken: "refresh"}

	source := &staticTokenSource{token: first}
	pts := &persistentTokenSource{
		source:  source,
		store:   store,
		current: first,
	}

	// First call with the same token should not trigger a save.
	tok, err := pts.Token()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok.AccessToken != first.AccessToken {
		t.Fatalf("expected first token, got %s", tok.AccessToken)
	}
	if len(store.saved) != 0 {
		t.Fatalf("expected no save for unchanged token, got %d", len(store.saved))
	}

	// Change the token returned by the underlying source.
	source.token = second
	tok, err = pts.Token()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok.AccessToken != second.AccessToken {
		t.Fatalf("expected second token, got %s", tok.AccessToken)
	}
	if len(store.saved) != 1 {
		t.Fatalf("expected one save, got %d", len(store.saved))
	}
	if store.saved[0].AccessToken != second.AccessToken {
		t.Fatalf("expected saved token to be second, got %s", store.saved[0].AccessToken)
	}
}

func TestPersistentTokenSource_ReturnsSaveError(t *testing.T) {
	first := &oauth2.Token{AccessToken: "first"}
	second := &oauth2.Token{AccessToken: "second"}

	store := &fakeTokenStore{err: errors.New("disk full")}
	source := &staticTokenSource{token: second}
	pts := &persistentTokenSource{
		source:  source,
		store:   store,
		current: first,
	}

	_, err := pts.Token()
	if err == nil {
		t.Fatal("expected error when store.Save fails")
	}
	if !errors.Is(err, store.err) {
		t.Fatalf("expected disk full error, got %v", err)
	}
}

func TestNew_RequiresConfig(t *testing.T) {
	ctx := context.Background()
	store := &fakeTokenStore{load: &oauth2.Token{AccessToken: "x"}}

	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "missing client secret",
			cfg:  Config{FolderID: "folder", RetainCount: 2},
		},
		{
			name: "missing folder id",
			cfg:  Config{ClientSecretFileDir: "/secret.json", RetainCount: 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(ctx, tt.cfg, store)
			if err == nil {
				t.Fatal("expected error for invalid config")
			}
		})
	}
}
