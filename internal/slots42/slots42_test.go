package slots42

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCookieHeader(t *testing.T) {
	cases := map[string]string{
		"abc123":                     "sessionid=abc123",
		"  abc123\n":                 "sessionid=abc123",
		"sessionid=abc; csrftoken=x": "sessionid=abc; csrftoken=x",
		"Cookie: sessionid=abc":      "sessionid=abc",
	}
	for in, want := range cases {
		got, err := CookieHeader(in)
		if err != nil || got != want {
			t.Errorf("CookieHeader(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "   ", "a=b\nc=d"} {
		if _, err := CookieHeader(bad); err == nil {
			t.Errorf("CookieHeader(%q): expected error", bad)
		}
	}
}

func TestSessionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadSession(dir); err != ErrNoSession {
		t.Fatalf("err = %v", err)
	}
	s := &Session{BaseURL: "https://slots.example", Cookie: "sessionid=x", SavedAt: time.Now()}
	if err := s.Save(dir); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(SessionPath(dir))
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", fi.Mode().Perm())
	}
	got, err := LoadSession(dir)
	if err != nil || got.Cookie != "sessionid=x" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if err := DeleteSession(dir); err != nil {
		t.Fatal(err)
	}
	if err := DeleteSession(dir); err != nil {
		t.Fatal(err)
	}
}

func TestMySlots(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "sessionid=good" {
			http.Redirect(w, r, "https://accounts.example/login/", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("status") {
		case "bx":
			fmt.Fprint(w, `[{"id":2,"start":"2026-11-04T09:00:00Z","end":"2026-11-04T10:00:00Z","remote":true,"campus":"bx","login":"swilmus"}]`)
		case "reserved-bx":
			fmt.Fprint(w, `{"7":{"id":7,"start":"2026-11-03T09:00:00Z","end":"2026-11-03T09:30:00Z"}}`)
		default:
			fmt.Fprint(w, `{}`)
		}
	}))
	defer srv.Close()

	c := New(&Session{BaseURL: srv.URL, Cookie: "sessionid=good"})
	slots, err := c.MySlots(context.Background(), time.Now(), time.Now().Add(14*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 2 {
		t.Fatalf("got %+v", slots)
	}
	if slots[0].ID != 7 || !slots[0].Reserved || slots[0].Campus != "bx" {
		t.Errorf("first = %+v (sorted by start, reserved, campus filled)", slots[0])
	}
	if slots[1].ID != 2 || slots[1].Reserved || !slots[1].Remote {
		t.Errorf("second = %+v", slots[1])
	}

	stale := New(&Session{BaseURL: srv.URL, Cookie: "sessionid=stale"})
	if _, err := stale.MySlots(context.Background(), time.Now(), time.Now()); err != ErrExpired {
		t.Fatalf("expired session: err = %v", err)
	}
}

func TestLoginPageMeansExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html>login</html>")
	}))
	defer srv.Close()
	err := New(&Session{BaseURL: srv.URL, Cookie: "sessionid=x"}).Check(context.Background())
	if err != ErrExpired {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(fmt.Sprint(err), "sessionid") {
		t.Fatal("error leaks the cookie")
	}
}

// fakePlatform mimics the Django app: a session cookie, a csrftoken cookie
// set by the slots page, and CSRF + Referer checks on writes.
func fakePlatform(t *testing.T, got *url.Values, method *string) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("sessionid"); err != nil || c.Value != "good" {
			http.Redirect(w, r, "/login/", http.StatusFound)
			return
		}
		if r.URL.Path == "/slots" {
			http.SetCookie(w, &http.Cookie{Name: "csrftoken", Value: "secret"})
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<div id="campuses"><div id="anr-btn" class="" value="anr"></div><div id="bx-btn" class="selected" value="bx"></div></div>
<div id="remote-toggle" class="remote-toggle enabled" onclick="toggleRemote()"></div>
<script>headers: { 'X-CSRFToken': 'masked-token', },</script>`)
			return
		}
		csrf, err := r.Cookie("csrftoken")
		if err != nil || csrf.Value != "secret" || r.Header.Get("X-CSRFToken") != "masked-token" || r.Header.Get("Referer") != srv.URL+"/slots" {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, "<h1>CSRF verification failed</h1>")
			return
		}
		raw, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(raw))
		*got, *method = form, r.Method
		if form.Get("begin_at") == "2026-11-04T09:00:00+01:00" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `"Slots must be at least 1 hour long"`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	}))
	return srv
}

func TestCreateAndDelete(t *testing.T) {
	var form url.Values
	var method string
	srv := fakePlatform(t, &form, &method)
	defer srv.Close()

	c := New(&Session{BaseURL: srv.URL, Cookie: "sessionid=good"})
	p, err := c.Prefs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if p.Campus != "bx" || !p.Remote {
		t.Fatalf("prefs = %+v", p)
	}

	loc := time.FixedZone("CET", 3600)
	begin := time.Date(2026, 11, 4, 10, 0, 0, 0, loc)
	if err := c.Create(context.Background(), p, begin, begin.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || form.Get("begin_at") != "2026-11-04T10:00:00+01:00" ||
		form.Get("end_at") != "2026-11-04T12:00:00+01:00" || form.Get("campus") != "bx" || form.Get("remote") != "true" {
		t.Fatalf("POST %v", form)
	}

	err = c.Create(context.Background(), p, begin.Add(-time.Hour), begin)
	if err == nil || !strings.Contains(err.Error(), "at least 1 hour") {
		t.Fatalf("platform error not relayed: %v", err)
	}

	s := Slot{ID: 1, Start: begin.UTC(), End: begin.Add(time.Hour).UTC()}
	if err := c.Delete(context.Background(), p, s); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodDelete || form.Get("start") == "" || form.Get("end") == "" {
		t.Fatalf("DELETE %s %v", method, form)
	}
	if err := c.Delete(context.Background(), p, Slot{Reserved: true}); err == nil {
		t.Fatal("deleting a booked slot must fail")
	}

	// Without the CSRF token, Django answers an HTML 403: reported as expired.
	if err := c.Create(context.Background(), &Prefs{Campus: "bx"}, begin, begin.Add(time.Hour)); err != ErrExpired {
		t.Fatalf("missing CSRF: err = %v", err)
	}
}
