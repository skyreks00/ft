package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/oauth2"
)

// Broker talks to the ft token broker, which holds the 42 client secret and
// performs the code exchange and refresh on our behalf.
type Broker struct {
	URL  string
	HTTP *http.Client
}

// tokenResponse is the 42 /oauth/token response, as relayed by the broker.
type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int64  `json:"expires_in"`
	RefreshToken     string `json:"refresh_token"`
	Scope            string `json:"scope"`
	CreatedAt        int64  `json:"created_at"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// Exchange trades an authorization code for a token.
func (b *Broker) Exchange(ctx context.Context, code, redirectURI, verifier string) (*oauth2.Token, error) {
	return b.post(ctx, "/token", map[string]string{
		"code":          code,
		"redirect_uri":  redirectURI,
		"code_verifier": verifier,
	})
}

// Refresh trades a refresh token for a new token.
func (b *Broker) Refresh(ctx context.Context, refreshToken string) (*oauth2.Token, error) {
	return b.post(ctx, "/refresh", map[string]string{"refresh_token": refreshToken})
}

func (b *Broker) post(ctx context.Context, path string, body map[string]string) (*oauth2.Token, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.URL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	hc := b.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach the ft token broker at %s (offline? developers: is `make broker-dev` running?): %w", b.URL, netCause(err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, fmt.Errorf("reading broker response: %w", err)
	}
	return parseTokenResponse(resp.StatusCode, raw)
}

// parseTokenResponse converts a /oauth/token response body to a token. Error
// messages never include the response body, which may contain tokens.
func parseTokenResponse(status int, raw []byte) (*oauth2.Token, error) {
	var tr tokenResponse
	if err := json.Unmarshal(raw, &tr); err != nil {
		return nil, fmt.Errorf("token endpoint returned HTTP %d with a non-JSON body", status)
	}
	if status != http.StatusOK || tr.Error != "" || tr.AccessToken == "" {
		msg := tr.Error
		if tr.ErrorDescription != "" {
			msg += ": " + tr.ErrorDescription
		}
		if msg == "" {
			msg = "no access token in response"
		}
		return nil, fmt.Errorf("token request failed (HTTP %d): %s", status, msg)
	}
	tok := &oauth2.Token{
		AccessToken:  tr.AccessToken,
		TokenType:    tr.TokenType,
		RefreshToken: tr.RefreshToken,
	}
	if tr.ExpiresIn > 0 {
		issued := time.Now()
		if tr.CreatedAt > 0 {
			issued = time.Unix(tr.CreatedAt, 0)
		}
		tok.Expiry = issued.Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	return tok, nil
}

// netCause strips the "Post <url>:" prefix of HTTP client errors.
func netCause(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
