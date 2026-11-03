// Package auth implements the 42 OAuth2 login flow through the ft token
// broker, and local token storage.
package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/oauth2"
)

// ErrNotLoggedIn is returned when no token is stored.
var ErrNotLoggedIn = errors.New("not logged in: run `ft login`")

// Store persists the user's token as JSON in a 0600 file.
type Store struct {
	Path string
}

// NewStore returns a Store for <dir>/token.json.
func NewStore(dir string) *Store {
	return &Store{Path: filepath.Join(dir, "token.json")}
}

// Load reads the stored token.
func (s *Store) Load() (*oauth2.Token, error) {
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotLoggedIn
	}
	if err != nil {
		return nil, fmt.Errorf("reading token: %w", err)
	}
	var tok oauth2.Token
	if err := json.Unmarshal(b, &tok); err != nil {
		return nil, fmt.Errorf("corrupt token file %s (run `ft login`): %w", s.Path, err)
	}
	if tok.AccessToken == "" {
		return nil, ErrNotLoggedIn
	}
	return &tok, nil
}

// Save atomically writes the token with 0600 permissions.
func (s *Store) Save(tok *oauth2.Token) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(tok)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".token-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.Path)
}

// Delete removes the stored token. Deleting a missing token is not an error.
func (s *Store) Delete() error {
	err := os.Remove(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
