// Package config resolves ft's runtime configuration.
//
// Values are resolved with the following precedence (highest first):
//
//	process environment > ./.env > <config dir>/.env > compile-time defaults
//
// Only public values live here (client ID, broker URL, redirect URI). The 42
// client secret is never part of the CLI.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Defaults: the public UID of ft's 42 app and the URL of its token broker
// (the app secret lives only in the broker). Builds for another 42 app can
// override them with
//
//	-ldflags "-X github.com/skyreks00/ft/internal/config.DefaultClientID=... \
//	          -X github.com/skyreks00/ft/internal/config.DefaultBrokerURL=..."
//
// or at run time with FT_CLIENT_ID and FT_BROKER_URL.
var (
	DefaultClientID  = "u-s4t2ud-8a4d99759bfebc7c2a38249e690081ef9b4ad0131eda3116efc22d48f6d1efad"
	DefaultBrokerURL = "https://ft-broker.eggfarm-app.workers.dev"
	Version          = "dev"
)

// DefaultRedirectURIs are registered on the 42 app; login tries them in order
// until one port is free.
var DefaultRedirectURIs = []string{
	"http://localhost:8080/callback",
	"http://localhost:8081/callback",
	"http://localhost:8082/callback",
}

const (
	APIBaseURL = "https://api.intra.42.fr"
	// Scopes enabled on the registered 42 app. profile is needed to join
	// and leave events (POST/DELETE /v2/events_users).
	Scopes = "public projects profile"
)

// BuildVersion is Version, or for a `go install …@vX.Y.Z` build (no
// ldflags) the module version recorded by Go.
func BuildVersion() string {
	if Version != "dev" {
		return Version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return strings.TrimPrefix(bi.Main.Version, "v")
	}
	return Version
}

// Config is the resolved configuration.
type Config struct {
	ClientID     string
	BrokerURL    string
	RedirectURIs []string
	Login        string        // optional, FT_LOGIN
	SlotsURL     string        // optional, FT_SLOTS_URL: external slots platform, or "intra"
	RemindBefore time.Duration // FT_REMIND (default 15m; "off" disables reminders)
	Notify       bool          // FT_NOTIFY ("off" keeps reminders out of the desktop)
	APIBaseURL   string
	Dir          string // config directory, e.g. ~/.config/ft
}

// Load resolves the configuration from the environment, ./.env and the
// config dir .env file.
func Load() (*Config, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	local, err := readEnvFile(".env")
	if err != nil {
		return nil, err
	}
	global, err := readEnvFile(filepath.Join(dir, ".env"))
	if err != nil {
		return nil, err
	}
	return resolve(os.LookupEnv, []map[string]string{local, global}, dir), nil
}

// resolve builds a Config from a lookup function (the process environment)
// and env files in decreasing precedence.
func resolve(lookupEnv func(string) (string, bool), files []map[string]string, dir string) *Config {
	get := func(key, def string) string {
		if v, ok := lookupEnv(key); ok && v != "" {
			return v
		}
		for _, f := range files {
			if v := f[key]; v != "" {
				return v
			}
		}
		return def
	}

	c := &Config{
		ClientID:     get("FT_CLIENT_ID", DefaultClientID),
		BrokerURL:    strings.TrimRight(get("FT_BROKER_URL", DefaultBrokerURL), "/"),
		RedirectURIs: DefaultRedirectURIs,
		Login:        get("FT_LOGIN", ""),
		SlotsURL:     get("FT_SLOTS_URL", ""),
		RemindBefore: parseRemind(get("FT_REMIND", "")),
		Notify:       get("FT_NOTIFY", "") != "off",
		APIBaseURL:   strings.TrimRight(get("FT_API_URL", APIBaseURL), "/"),
		Dir:          dir,
	}
	if uri := get("FT_REDIRECT_URI", ""); uri != "" {
		// Try the override first, keeping the registered ones as fallbacks.
		c.RedirectURIs = []string{uri}
		for _, d := range DefaultRedirectURIs {
			if d != uri {
				c.RedirectURIs = append(c.RedirectURIs, d)
			}
		}
	}
	return c
}

// defaultRemind is how long before an evaluation or event the TUI reminds.
const defaultRemind = 15 * time.Minute

func parseRemind(v string) time.Duration {
	switch v {
	case "":
		return defaultRemind
	case "off", "0":
		return 0
	}
	if d, err := time.ParseDuration(v); err == nil && d > 0 {
		return d
	}
	return defaultRemind
}

// Validate reports missing values required to talk to the API.
func (c *Config) Validate() error {
	var missing []string
	if c.ClientID == "" {
		missing = append(missing, "FT_CLIENT_ID")
	}
	if c.BrokerURL == "" {
		missing = append(missing, "FT_BROKER_URL")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing configuration: %s (set it in the environment or a .env file, or build with -ldflags)", strings.Join(missing, ", "))
	}
	return nil
}

// Dir returns the ft config directory: $FT_CONFIG_DIR, else
// $XDG_CONFIG_HOME/ft, else ~/.config/ft (on every OS, macOS included).
func Dir() (string, error) {
	if d := os.Getenv("FT_CONFIG_DIR"); d != "" {
		return d, nil
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "ft"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locating home directory: %w", err)
	}
	return filepath.Join(home, ".config", "ft"), nil
}

// EnsureDir creates the config directory with 0700 permissions.
func (c *Config) EnsureDir() error {
	return os.MkdirAll(c.Dir, 0o700)
}

func readEnvFile(path string) (map[string]string, error) {
	m, err := godotenv.Read(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return m, nil
}
