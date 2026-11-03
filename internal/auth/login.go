package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/skyreks00/ft/internal/browser"
)

// LoginOptions configures the browser login flow.
type LoginOptions struct {
	ClientID     string
	APIBaseURL   string
	Scopes       string
	RedirectURIs []string // tried in order until one port is free
	Broker       *Broker
	Out          io.Writer
	NoBrowser    bool
	Timeout      time.Duration

	// ProbePKCE first tries a secret-less code exchange directly against 42,
	// to check whether the app accepts PKCE without a client secret.
	ProbePKCE bool
}

// Login runs the authorization code flow: it listens on a local redirect URI,
// opens the browser on the 42 authorize page, waits for the callback, checks
// the state, and exchanges the code through the broker.
func Login(ctx context.Context, o LoginOptions) (*oauth2.Token, error) {
	ln, redirectURI, err := listen(o.RedirectURIs)
	if err != nil {
		return nil, err
	}
	defer ln.Close()

	state, err := randomString(32)
	if err != nil {
		return nil, err
	}
	verifier := oauth2.GenerateVerifier()
	oc := &oauth2.Config{
		ClientID:    o.ClientID,
		Endpoint:    oauth2.Endpoint{AuthURL: o.APIBaseURL + "/oauth/authorize", TokenURL: o.APIBaseURL + "/oauth/token"},
		RedirectURL: redirectURI,
		Scopes:      strings.Fields(o.Scopes),
	}
	authURL := oc.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	srv := &http.Server{
		Handler:           callbackHandler(callbackPath(redirectURI), state, codeCh, errCh),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	defer srv.Close()

	fmt.Fprintf(o.Out, "Opening the 42 login page in your browser.\nIf it does not open, visit:\n\n  %s\n\n", authURL)
	if !o.NoBrowser {
		_ = browser.Open(authURL)
	}

	timeout := o.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return nil, err
	case <-time.After(timeout):
		return nil, errors.New("timed out waiting for the browser login")
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	if o.ProbePKCE {
		tok, err := exchangeWithoutSecret(ctx, oc, code, verifier)
		if err == nil {
			fmt.Fprintln(o.Out, "PKCE probe: 42 accepted a secret-less exchange. The broker can be dropped.")
			return tok, nil
		}
		fmt.Fprintf(o.Out, "PKCE probe: secret-less exchange rejected (%v). Falling back to the broker.\n", err)
	}
	return o.Broker.Exchange(ctx, code, redirectURI, verifier)
}

// exchangeWithoutSecret posts directly to 42 /oauth/token with only the
// client ID and the PKCE verifier.
func exchangeWithoutSecret(ctx context.Context, oc *oauth2.Config, code, verifier string) (*oauth2.Token, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {oc.ClientID},
		"code":          {code},
		"redirect_uri":  {oc.RedirectURL},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oc.Endpoint.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, err
	}
	return parseTokenResponse(resp.StatusCode, raw)
}

func callbackHandler(path, state string, codeCh chan<- string, errCh chan<- error) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			writePage(w, http.StatusBadRequest, "Login failed: "+e)
			sendErr(errCh, fmt.Errorf("authorization denied: %s %s", e, q.Get("error_description")))
			return
		}
		if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 {
			// Do not abort the flow: this may be a stray or forged request.
			writePage(w, http.StatusBadRequest, "Invalid state parameter.")
			return
		}
		code := q.Get("code")
		if code == "" {
			writePage(w, http.StatusBadRequest, "Missing authorization code.")
			return
		}
		writePage(w, http.StatusOK, "Logged in to ft. You can close this tab.")
		select {
		case codeCh <- code:
		default:
		}
	})
	return mux
}

func sendErr(ch chan<- error, err error) {
	select {
	case ch <- err:
	default:
	}
}

func writePage(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintf(w, "<!doctype html><title>ft</title><body style=\"font-family:sans-serif;padding:2em\"><p>%s</p></body>", html.EscapeString(msg))
}

// listen binds the first redirect URI whose port is free.
func listen(uris []string) (net.Listener, string, error) {
	var errs []error
	for _, raw := range uris {
		u, err := url.Parse(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", raw, err))
			continue
		}
		host := u.Hostname()
		if host == "localhost" {
			host = "127.0.0.1"
		}
		ln, err := net.Listen("tcp", net.JoinHostPort(host, u.Port()))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		return ln, raw, nil
	}
	return nil, "", fmt.Errorf("no free port for the login callback: %w", errors.Join(errs...))
}

func callbackPath(redirectURI string) string {
	u, err := url.Parse(redirectURI)
	if err != nil || u.Path == "" {
		return "/"
	}
	return u.Path
}

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
