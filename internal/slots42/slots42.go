// Package slots42 talks to a campus slots platform (slots.42belgium.be)
// through the JSON endpoints its web page uses. It is unofficial: it
// authenticates with the user's own browser session cookie and may break
// when the site changes.
package slots42

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrNoSession means `ft slots auth` was never run.
var ErrNoSession = errors.New("no slots platform session: run `ft slots auth`")

// ErrExpired means the platform rejected the stored session.
var ErrExpired = errors.New("slots platform session expired: run `ft slots auth` again")

// Session is the stored browser session. Cookie is a Cookie header value,
// such as "sessionid=…".
type Session struct {
	BaseURL string    `json:"base_url"`
	Cookie  string    `json:"cookie"`
	SavedAt time.Time `json:"saved_at"`
}

// SessionPath is where the session lives in the config directory.
func SessionPath(dir string) string {
	return filepath.Join(dir, "slots-session.json")
}

// LoadSession reads the stored session.
func LoadSession(dir string) (*Session, error) {
	b, err := os.ReadFile(SessionPath(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(b, &s); err != nil || s.Cookie == "" {
		return nil, ErrNoSession
	}
	return &s, nil
}

// Save writes the session with 0600 permissions.
func (s *Session) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(SessionPath(dir), b, 0o600)
}

// DeleteSession removes the stored session, if any.
func DeleteSession(dir string) error {
	err := os.Remove(SessionPath(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// CookieHeader turns what the user pasted into a Cookie header: either a
// bare session id or "name=value; …" pairs.
func CookieHeader(pasted string) (string, error) {
	v := strings.TrimSpace(pasted)
	v = strings.TrimPrefix(v, "Cookie:")
	v = strings.TrimSpace(v)
	if v == "" {
		return "", errors.New("empty cookie")
	}
	if strings.ContainsAny(v, "\r\n") {
		return "", errors.New("the cookie must be on one line")
	}
	if !strings.Contains(v, "=") {
		return "sessionid=" + v, nil
	}
	return v, nil
}

// Campus codes used by the platform.
var campusNames = map[string]string{"bx": "Brussels", "anr": "Antwerp"}

// CampusName returns a readable campus name for a platform code.
func CampusName(code string) string {
	if n, ok := campusNames[code]; ok {
		return n
	}
	return code
}

// Slot is a slot on the platform: one of the user's open slots, or a booked
// one (Reserved).
type Slot struct {
	ID       int       `json:"id"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	Campus   string    `json:"campus"`
	Remote   bool      `json:"remote"` // inter-campus
	Login    string    `json:"login,omitempty"`
	Reserved bool      `json:"reserved"`
}

// Client calls the platform with a stored session.
type Client struct {
	Session *Session
	HTTP    *http.Client
}

// New returns a client that does not follow redirects, so that an expired
// session (redirected to the login page) is detected. Cookies the platform
// sets (such as csrftoken) are kept for the client's lifetime.
func New(s *Session) *Client {
	jar, _ := cookiejar.New(nil)
	if u, err := url.Parse(s.BaseURL); err == nil {
		jar.SetCookies(u, parseCookies(s.Cookie))
	}
	return &Client{Session: s, HTTP: &http.Client{
		Jar:     jar,
		Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

func parseCookies(header string) []*http.Cookie {
	var out []*http.Cookie
	for part := range strings.SplitSeq(header, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && name != "" {
			out = append(out, &http.Cookie{Name: name, Value: value})
		}
	}
	return out
}

// Prefs are the user's platform settings, read from the slots page. New
// slots use them unless overridden.
type Prefs struct {
	Campus string // "bx" or "anr"
	Remote bool   // inter-campus
	csrf   string
}

var (
	reCSRF   = regexp.MustCompile(`'X-CSRFToken':\s*'([^']+)'`)
	reCampus = regexp.MustCompile(`id="(\w+)-btn"\s+class="[^"]*\bselected\b`)
	reRemote = regexp.MustCompile(`id="remote-toggle"\s+class="([^"]*)"`)
)

// Prefs loads the slots page for the CSRF token and the user's campus and
// inter-campus settings.
func (c *Client) Prefs(ctx context.Context) (*Prefs, error) {
	body, ctype, err := c.do(ctx, http.MethodGet, "/slots", nil, nil)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(ctype, "html") {
		return nil, ErrExpired
	}
	m := reCSRF.FindSubmatch(body)
	if m == nil {
		return nil, errors.New("could not find the CSRF token on the slots page (the platform may have changed)")
	}
	p := &Prefs{Campus: "bx", csrf: string(m[1])}
	if m := reCampus.FindSubmatch(body); m != nil {
		p.Campus = string(m[1])
	}
	if m := reRemote.FindSubmatch(body); m != nil {
		p.Remote = strings.Contains(" "+string(m[1])+" ", " enabled ")
	}
	return p, nil
}

// Create opens a slot. campus and remote come from Prefs unless the caller
// changed them.
func (c *Client) Create(ctx context.Context, p *Prefs, begin, end time.Time) error {
	form := url.Values{
		"begin_at": {begin.Format(time.RFC3339)},
		"end_at":   {end.Format(time.RFC3339)},
		"campus":   {p.Campus},
		"remote":   {strconv.FormatBool(p.Remote)},
	}
	_, _, err := c.do(ctx, http.MethodPost, "/api/slot", form, p)
	return err
}

// Delete removes one of the user's open slots.
func (c *Client) Delete(ctx context.Context, p *Prefs, s Slot) error {
	if s.Reserved {
		return errors.New("this slot is booked: cancel the evaluation on the platform")
	}
	form := url.Values{
		"start": {s.Start.Local().Format(time.RFC3339)},
		"end":   {s.End.Local().Format(time.RFC3339)},
	}
	_, _, err := c.do(ctx, http.MethodDelete, "/api/slot", form, p)
	return err
}

// statuses are the calendar's event sources: the user's open slots per
// campus, and the booked ones.
var statuses = []struct {
	status, campus string
	reserved       bool
}{
	{"bx", "bx", false},
	{"anr", "anr", false},
	{"reserved-bx", "bx", true},
	{"reserved-anr", "anr", true},
}

// MySlots lists the user's slots between from and to, soonest first.
func (c *Client) MySlots(ctx context.Context, from, to time.Time) ([]Slot, error) {
	var all []Slot
	for _, st := range statuses {
		q := url.Values{
			"status": {st.status},
			"start":  {from.UTC().Format(time.RFC3339)},
			"end":    {to.UTC().Format(time.RFC3339)},
		}
		body, err := c.get(ctx, "/api/slot", q)
		if err != nil {
			return nil, err
		}
		slots, err := decodeSlots(body)
		if err != nil {
			return nil, fmt.Errorf("unexpected slots response (the platform may have changed): %w", err)
		}
		for _, s := range slots {
			if s.Campus == "" {
				s.Campus = st.campus
			}
			s.Reserved = st.reserved
			all = append(all, s)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Start.Before(all[j].Start) })
	return all, nil
}

// Check verifies the session with a cheap request.
func (c *Client) Check(ctx context.Context) error {
	_, err := c.get(ctx, "/api/projects/", nil)
	return err
}

// decodeSlots accepts a list, or an object (the platform answers {} when
// there is nothing, and may key events by id).
func decodeSlots(body []byte) ([]Slot, error) {
	var list []Slot
	if err := json.Unmarshal(body, &list); err == nil {
		return list, nil
	}
	var byKey map[string]Slot
	if err := json.Unmarshal(body, &byKey); err != nil {
		return nil, err
	}
	for _, s := range byKey {
		list = append(list, s)
	}
	return list, nil
}

func (c *Client) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	body, ctype, err := c.do(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(ctype, "json") {
		// A login page served with 200 also means the session is gone.
		return nil, ErrExpired
	}
	return body, nil
}

// do sends a request. Writes (p != nil) carry the CSRF token and the
// same-origin Referer that Django checks, with a form-encoded body like the
// page's jQuery calls.
func (c *Client) do(ctx context.Context, method, path string, form url.Values, p *Prefs) ([]byte, string, error) {
	base := strings.TrimRight(c.Session.BaseURL, "/")
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "application/json, text/html")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	}
	if p != nil {
		req.Header.Set("X-CSRFToken", p.csrf)
		req.Header.Set("Referer", base+"/slots")
		req.Header.Set("Origin", base)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("contacting the slots platform: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, "", err
	}
	ctype := resp.Header.Get("Content-Type")
	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400, resp.StatusCode == http.StatusUnauthorized:
		return nil, "", ErrExpired
	case resp.StatusCode == http.StatusForbidden && !strings.Contains(ctype, "json"):
		// Django's CSRF and login failures are HTML pages.
		return nil, "", ErrExpired
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		if msg := message(b); msg != "" {
			return nil, "", fmt.Errorf("slots platform: %s", msg)
		}
		return nil, "", fmt.Errorf("slots platform: %s %s: HTTP %d", method, path, resp.StatusCode)
	}
	return b, ctype, nil
}

// message extracts the platform's error text: a JSON string, or an object
// with a message, error or detail field.
func message(b []byte) string {
	var s string
	if json.Unmarshal(b, &s) == nil {
		return s
	}
	var o map[string]any
	if json.Unmarshal(b, &o) == nil {
		for _, k := range []string{"message", "error", "detail"} {
			if v, ok := o[k].(string); ok {
				return v
			}
		}
	}
	return ""
}
