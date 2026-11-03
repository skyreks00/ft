package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient returns a client against srv with no rate limiting and
// instant retries; slept collects the requested retry delays.
func newTestClient(srv *httptest.Server, cache Cache) (*Client, *[]time.Duration) {
	c := New(srv.URL, srv.Client(), cache)
	c.Limiter = nil
	var slept []time.Duration
	c.sleep = func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}
	return c, &slept
}

func TestMe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/me" || r.Header.Get("Accept") != "application/json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("X-Secondly-RateLimit-Remaining", "1")
		w.Header().Set("X-Hourly-RateLimit-Remaining", "1100")
		fmt.Fprint(w, `{"id":1,"login":"swilmus","wallet":42,"correction_point":5,"location":"c1r2s3",
			"cursus_users":[{"level":3.5,"cursus":{"id":9,"slug":"c-piscine"}},{"level":4.21,"blackholed_at":"2027-01-01T00:00:00.000Z","cursus":{"id":21,"slug":"42cursus","name":"42cursus"}}],
			"campus":[{"id":1,"name":"Paris"},{"id":12,"name":"Brussels"}],
			"campus_users":[{"campus_id":1,"is_primary":false},{"campus_id":12,"is_primary":true}]}`)
	}))
	defer srv.Close()

	c, _ := newTestClient(srv, nil)
	u, err := c.Me(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if u.Login != "swilmus" || u.Wallet != 42 || u.CorrectionPoint != 5 || *u.Location != "c1r2s3" {
		t.Fatalf("got %+v", u)
	}
	if cu := u.MainCursus(); cu == nil || cu.Level != 4.21 || cu.BlackholedAt == nil {
		t.Fatalf("MainCursus = %+v", cu)
	}
	if camp := u.PrimaryCampus(); camp == nil || camp.Name != "Brussels" {
		t.Fatalf("PrimaryCampus = %+v", camp)
	}
	if q := c.Quota(); !q.Known || q.HourlyRemaining != 1100 {
		t.Fatalf("Quota = %+v", q)
	}
}

func TestRetryOn429(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			fmt.Fprint(w, `{"ok":true}`)
		}
	}))
	defer srv.Close()

	c, slept := newTestClient(srv, nil)
	var out struct{ OK bool }
	if err := c.Get(context.Background(), "/v2/x", nil, 0, &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK || calls.Load() != 3 {
		t.Fatalf("out=%+v calls=%d", out, calls.Load())
	}
	want := []time.Duration{3 * time.Second, 2 * time.Second}
	if fmt.Sprint(*slept) != fmt.Sprint(want) {
		t.Fatalf("slept %v, want %v", *slept, want)
	}
}

func TestRetryGivesUp(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c, _ := newTestClient(srv, nil)
	err := c.Get(context.Background(), "/v2/x", nil, 0, nil)
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != int32(c.MaxRetries+1) {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestNoRetryOnServerErrorForWrites(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c, _ := newTestClient(srv, nil)
	if err := c.Send(context.Background(), http.MethodPost, "/v2/slots", map[string]string{"a": "b"}, nil); err == nil {
		t.Fatal("expected error")
	}
	if calls.Load() != 1 {
		t.Fatalf("POST retried: calls = %d", calls.Load())
	}
}

func TestErrorMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":"Forbidden","message":"You are not authorized"}`)
	}))
	defer srv.Close()

	c, _ := newTestClient(srv, nil)
	err := c.Get(context.Background(), "/v2/slots", nil, 0, nil)
	want := "GET /v2/slots: HTTP 403: Forbidden: You are not authorized (your account or the app's scopes may not allow this)"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v", err)
	}
}

func TestCache(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"n":%d}`, calls.Add(1))
	}))
	defer srv.Close()

	cache := NewFileCache(t.TempDir())
	now := time.Now()
	cache.now = func() time.Time { return now }
	c, _ := newTestClient(srv, cache)

	get := func() int {
		var out struct{ N int }
		if err := c.Get(context.Background(), "/v2/me", nil, time.Minute, &out); err != nil {
			t.Fatal(err)
		}
		return out.N
	}
	if get() != 1 || get() != 1 {
		t.Fatal("second call not served from cache")
	}
	c.NoCache = true
	if get() != 2 {
		t.Fatal("NoCache did not bypass the cache")
	}
	c.NoCache = false
	if get() != 2 {
		t.Fatal("NoCache response was not stored")
	}
	now = now.Add(2 * time.Minute)
	if get() != 3 {
		t.Fatal("expired entry served")
	}
}

func TestGetAll(t *testing.T) {
	const total = 230
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page[number]"))
		size, _ := strconv.Atoi(r.URL.Query().Get("page[size]"))
		if r.URL.Query().Get("filter[active]") != "true" {
			http.Error(w, "filter lost", http.StatusBadRequest)
			return
		}
		w.Write([]byte("["))
		for i := (page - 1) * size; i < min(page*size, total); i++ {
			if i > (page-1)*size {
				w.Write([]byte(","))
			}
			fmt.Fprintf(w, `{"id":%d}`, i)
		}
		w.Write([]byte("]"))
	}))
	defer srv.Close()

	c, _ := newTestClient(srv, nil)
	items, err := GetAll[struct{ ID int }](context.Background(), c, "/v2/locations", map[string][]string{"filter[active]": {"true"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != total || items[total-1].ID != total-1 {
		t.Fatalf("got %d items", len(items))
	}
}

func TestInsufficientScopeHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":"Forbidden","message":"Insufficient scope. The action need the following scopes: [profile]"}`)
	}))
	defer srv.Close()

	c, _ := newTestClient(srv, nil)
	err := c.Send(context.Background(), http.MethodPost, "/v2/events_users", map[string]int{}, nil)
	if err == nil || !strings.Contains(err.Error(), "run `ft login` again") {
		t.Fatalf("err = %v", err)
	}
}

func TestTransportErrors(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := srv.URL
	srv.Close() // nothing listens there now

	c := New(addr, http.DefaultClient, nil)
	c.Limiter = nil
	err := c.Get(context.Background(), "/v2/me", nil, 0, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot reach") || strings.Contains(err.Error(), `Get "`) {
		t.Fatalf("err = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Get(ctx, "/v2/me", nil, 0, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
}
