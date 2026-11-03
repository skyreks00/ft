package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"golang.org/x/oauth2"
)

// TokenSource returns the stored token, refreshing it through the broker
// when it is expired and persisting the refreshed token.
type TokenSource struct {
	ctx    context.Context
	store  *Store
	broker *Broker

	mu  sync.Mutex
	tok *oauth2.Token
}

// NewTokenSource loads the stored token. It returns ErrNotLoggedIn when there
// is none.
func NewTokenSource(ctx context.Context, store *Store, broker *Broker) (*TokenSource, error) {
	tok, err := store.Load()
	if err != nil {
		return nil, err
	}
	return &TokenSource{ctx: ctx, store: store, broker: broker, tok: tok}, nil
}

// Token implements oauth2.TokenSource.
func (s *TokenSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tok.Valid() {
		return s.tok, nil
	}
	if s.tok.RefreshToken == "" {
		return nil, errors.New("session expired: run `ft login`")
	}
	tok, err := s.broker.Refresh(s.ctx, s.tok.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("refreshing session (run `ft login` if this persists): %w", err)
	}
	if tok.RefreshToken == "" {
		tok.RefreshToken = s.tok.RefreshToken
	}
	if err := s.store.Save(tok); err != nil {
		return nil, fmt.Errorf("saving refreshed token: %w", err)
	}
	s.tok = tok
	return tok, nil
}
