// Package api is the 42 API v2 client shared by the CLI and the TUI. It
// handles rate limiting, retries on 429 and transient errors, response
// caching and pagination.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// The 42 API allows ~2 requests/second per application.
const (
	defaultRate       = 2
	defaultMaxRetries = 3
	maxBodySize       = 8 << 20
)

// Client is a 42 API client. HTTP must add the Authorization header (for
// example an oauth2 client).
type Client struct {
	BaseURL    string
	HTTP       *http.Client
	Limiter    *rate.Limiter
	Cache      Cache // nil disables caching
	NoCache    bool  // skip cache reads (responses are still stored)
	MaxRetries int

	// sleep waits for d or until ctx is done; replaced in tests.
	sleep func(ctx context.Context, d time.Duration) error

	mu    sync.Mutex
	quota Quota
}

// Quota is the rate limit state reported by the last response.
type Quota struct {
	SecondlyRemaining int
	HourlyRemaining   int
	Known             bool
}

// New returns a client with the default rate limit and retry policy.
func New(baseURL string, hc *http.Client, cache Cache) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTP:       hc,
		Limiter:    rate.NewLimiter(defaultRate, defaultRate),
		Cache:      cache,
		MaxRetries: defaultMaxRetries,
	}
}

// Error is a non-2xx API response.
type Error struct {
	StatusCode int
	Method     string
	Path       string
	Message    string
}

func (e *Error) Error() string {
	msg := fmt.Sprintf("%s %s: HTTP %d", e.Method, e.Path, e.StatusCode)
	if e.Message != "" {
		msg += ": " + e.Message
	}
	switch {
	case e.StatusCode == http.StatusUnauthorized:
		msg += " (run `ft login`)"
	case e.StatusCode == http.StatusForbidden && strings.Contains(e.Message, "Insufficient scope"):
		msg += " (run `ft login` again to grant the missing permission)"
	case e.StatusCode == http.StatusForbidden:
		msg += " (your account or the app's scopes may not allow this)"
	}
	return msg
}

// IsNotFound reports whether err is an API 404.
func IsNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.StatusCode == http.StatusNotFound
}

// IsForbidden reports whether err is an API 403.
func IsForbidden(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.StatusCode == http.StatusForbidden
}

// Response is a raw API response.
type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// Quota returns the rate limit state reported by the last response.
func (c *Client) Quota() Quota {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.quota
}

// Get fetches path and decodes the JSON response into out. Responses are
// cached for ttl (0 disables caching for this call).
func (c *Client) Get(ctx context.Context, path string, query url.Values, ttl time.Duration, out any) error {
	resp, err := c.get(ctx, path, query, ttl)
	if err != nil {
		return err
	}
	return decode(resp.Body, out)
}

type noCacheKey struct{}

// WithNoCache returns a context whose requests skip cache reads, e.g. for
// an explicit refresh.
func WithNoCache(ctx context.Context) context.Context {
	return context.WithValue(ctx, noCacheKey{}, true)
}

func (c *Client) get(ctx context.Context, path string, query url.Values, ttl time.Duration) (*Response, error) {
	key := http.MethodGet + " " + c.url(path, query)
	skip := c.NoCache || ctx.Value(noCacheKey{}) != nil
	if c.Cache != nil && ttl > 0 && !skip {
		if body, ok := c.Cache.Get(key); ok {
			return &Response{StatusCode: http.StatusOK, Body: body}, nil
		}
	}
	resp, err := c.Do(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return nil, err
	}
	if c.Cache != nil && ttl > 0 {
		_ = c.Cache.Set(key, resp.Body, ttl)
	}
	return resp, nil
}

// Forget drops the cached GET response for path and query, so the next Get
// sees the effect of a write.
func (c *Client) Forget(path string, query url.Values) {
	if c.Cache != nil {
		_ = c.Cache.Delete(http.MethodGet + " " + c.url(path, query))
	}
}

// ForgetList drops every cached page of a GetAll listing.
func (c *Client) ForgetList(path string, query url.Values) {
	if c.Cache == nil {
		return
	}
	for page := 1; page <= maxPages; page++ {
		c.Forget(path, pageQuery(query, page))
	}
}

// Send performs a write request with an optional JSON body and decodes the
// response into out (if non-nil).
func (c *Client) Send(ctx context.Context, method, path string, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	resp, err := c.Do(ctx, method, path, nil, payload)
	if err != nil {
		return err
	}
	if out == nil || len(resp.Body) == 0 {
		return nil
	}
	return decode(resp.Body, out)
}

// Do performs a request with rate limiting and retries, returning the raw
// response. Non-2xx responses are returned as *Error.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body []byte) (*Response, error) {
	u := c.url(path, query)
	for attempt := 0; ; attempt++ {
		if c.Limiter != nil {
			if err := c.Limiter.Wait(ctx); err != nil {
				return nil, err
			}
		}
		resp, err := c.once(ctx, method, u, body)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}
		if attempt < c.MaxRetries && retryable(method, resp.StatusCode) {
			if err := c.wait(ctx, retryDelay(resp.Header, attempt)); err != nil {
				return nil, err
			}
			continue
		}
		return resp, &Error{StatusCode: resp.StatusCode, Method: method, Path: path, Message: errorMessage(resp.Body)}
	}
}

func (c *Client) once(ctx context.Context, method, u string, body []byte) (*Response, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, transportError(ctx, req.URL.Host, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return nil, err
	}
	c.recordQuota(resp.Header)
	return &Response{StatusCode: resp.StatusCode, Header: resp.Header, Body: b}, nil
}

// transportError turns HTTP client failures into readable errors: token
// refresh failures are returned as is (they already say what to do),
// network failures name the host.
func transportError(ctx context.Context, host string, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var ue *url.Error
	if !errors.As(err, &ue) {
		return err
	}
	// Only a direct network failure is about this host; a wrapped one comes
	// from the token source (the broker) and already explains itself.
	if _, ok := ue.Err.(net.Error); ok {
		return fmt.Errorf("cannot reach %s (offline?): %w", host, ue.Err)
	}
	return ue.Err
}

func (c *Client) url(path string, query url.Values) string {
	u := c.BaseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}

func (c *Client) wait(ctx context.Context, d time.Duration) error {
	if c.sleep != nil {
		return c.sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) recordQuota(h http.Header) {
	sec, errS := strconv.Atoi(h.Get("X-Secondly-RateLimit-Remaining"))
	hour, errH := strconv.Atoi(h.Get("X-Hourly-RateLimit-Remaining"))
	if errS != nil && errH != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.quota = Quota{SecondlyRemaining: sec, HourlyRemaining: hour, Known: true}
}

func retryable(method string, status int) bool {
	if status == http.StatusTooManyRequests {
		return true
	}
	// Only retry server errors for idempotent reads.
	return method == http.MethodGet && (status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout)
}

// retryDelay honours Retry-After, else backs off exponentially from 1s.
func retryDelay(h http.Header, attempt int) time.Duration {
	if s, err := strconv.Atoi(h.Get("Retry-After")); err == nil && s >= 0 {
		return time.Duration(s) * time.Second
	}
	return time.Second << attempt
}

func decode(body []byte, out any) error {
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decoding API response: %w", err)
	}
	return nil
}

// errorMessage extracts a human readable message from an error body.
func errorMessage(body []byte) string {
	var e struct {
		Error   any    `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &e) == nil {
		var parts []string
		if s, ok := e.Error.(string); ok && s != "" {
			parts = append(parts, s)
		}
		if e.Message != "" {
			parts = append(parts, e.Message)
		}
		if len(parts) > 0 {
			return strings.Join(parts, ": ")
		}
	}
	s := strings.TrimSpace(string(body))
	if strings.HasPrefix(s, "<") {
		return ""
	}
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
