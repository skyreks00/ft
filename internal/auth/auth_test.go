package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestStoreRoundTrip(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, err := s.Load(); err != ErrNotLoggedIn {
		t.Fatalf("Load on empty store = %v, want ErrNotLoggedIn", err)
	}
	tok := &oauth2.Token{AccessToken: "a", RefreshToken: "r", Expiry: time.Now().Add(time.Hour).Round(time.Second)}
	if err := s.Save(tok); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("token file mode = %o, want 600", perm)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "a" || got.RefreshToken != "r" || !got.Expiry.Equal(tok.Expiry) {
		t.Fatalf("got %+v", got)
	}
	if err := s.Delete(); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(); err != nil {
		t.Fatalf("second Delete = %v", err)
	}
}

func TestBrokerExchange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" || r.Method != http.MethodPost {
			http.Error(w, "bad route", http.StatusNotFound)
			return
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["code"] != "c0de" || body["redirect_uri"] != "http://localhost:8080/callback" || body["code_verifier"] != "v" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		w.Write([]byte(`{"access_token":"at","token_type":"bearer","expires_in":7200,"refresh_token":"rt","created_at":` +
			jsonInt(time.Now().Unix()) + `}`))
	}))
	defer srv.Close()

	b := &Broker{URL: srv.URL}
	tok, err := b.Exchange(context.Background(), "c0de", "http://localhost:8080/callback", "v")
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "at" || tok.RefreshToken != "rt" {
		t.Fatalf("got %+v", tok)
	}
	if d := time.Until(tok.Expiry); d < 7100*time.Second || d > 7200*time.Second {
		t.Fatalf("expiry in %v, want ~2h", d)
	}

	_, err = b.Exchange(context.Background(), "wrong", "http://localhost:8080/callback", "v")
	if err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("err = %v, want invalid_grant", err)
	}
}

func TestParseTokenResponseDoesNotLeakBody(t *testing.T) {
	_, err := parseTokenResponse(500, []byte(`<html>access_token=secret</html>`))
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("err = %v", err)
	}
}

func TestTokenSourceRefreshes(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path != "/refresh" || body["refresh_token"] != "old-rt" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		w.Write([]byte(`{"access_token":"new-at","expires_in":7200}`))
	}))
	defer srv.Close()

	store := NewStore(t.TempDir())
	store.Save(&oauth2.Token{AccessToken: "old-at", RefreshToken: "old-rt", Expiry: time.Now().Add(-time.Minute)})

	ts, err := NewTokenSource(context.Background(), store, &Broker{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		tok, err := ts.Token()
		if err != nil {
			t.Fatal(err)
		}
		if tok.AccessToken != "new-at" || tok.RefreshToken != "old-rt" {
			t.Fatalf("got %+v", tok)
		}
	}
	if calls != 1 {
		t.Fatalf("broker called %d times, want 1", calls)
	}
	saved, _ := store.Load()
	if saved.AccessToken != "new-at" {
		t.Fatalf("refreshed token not persisted: %+v", saved)
	}
}

func TestLoginFlow(t *testing.T) {
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["code"] != "the-code" || body["code_verifier"] == "" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		w.Write([]byte(`{"access_token":"at","expires_in":7200,"refresh_token":"rt"}`))
	}))
	defer broker.Close()

	// Grab a free port for the callback.
	probe := httptest.NewServer(http.NotFoundHandler())
	addr := probe.Listener.Addr().String()
	probe.Close()
	redirect := "http://" + addr + "/callback"

	pr, pw := io.Pipe()
	done := make(chan struct{})
	var tok *oauth2.Token
	var loginErr error
	go func() {
		defer close(done)
		tok, loginErr = Login(context.Background(), LoginOptions{
			ClientID:     "cid",
			APIBaseURL:   "https://api.intra.42.fr",
			Scopes:       "public projects",
			RedirectURIs: []string{redirect},
			Broker:       &Broker{URL: broker.URL},
			Out:          pw,
			NoBrowser:    true,
			Timeout:      5 * time.Second,
		})
		pw.Close()
	}()

	// Read the printed authorize URL to learn the state.
	buf := make([]byte, 4096)
	n, _ := pr.Read(buf)
	go io.Copy(io.Discard, pr)
	var authURL *url.URL
	for _, f := range strings.Fields(string(buf[:n])) {
		if strings.HasPrefix(f, "https://") {
			authURL, _ = url.Parse(f)
		}
	}
	if authURL == nil {
		t.Fatalf("no authorize URL printed: %q", buf[:n])
	}
	q := authURL.Query()
	if q.Get("client_id") != "cid" || q.Get("scope") != "public projects" || q.Get("code_challenge_method") != "S256" || q.Get("redirect_uri") != redirect {
		t.Fatalf("bad authorize URL: %s", authURL)
	}

	// A forged state is rejected without ending the flow.
	resp, err := http.Get(redirect + "?code=evil&state=wrong")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("forged state status = %d", resp.StatusCode)
	}

	resp, err = http.Get(redirect + "?code=the-code&state=" + url.QueryEscape(q.Get("state")))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	<-done
	if loginErr != nil {
		t.Fatal(loginErr)
	}
	if tok.AccessToken != "at" {
		t.Fatalf("got %+v", tok)
	}
}

func jsonInt(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
