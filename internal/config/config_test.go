package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func lookup(env map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	}
}

func TestResolvePrecedence(t *testing.T) {
	oldID, oldBroker := DefaultClientID, DefaultBrokerURL
	DefaultClientID, DefaultBrokerURL = "default-id", "https://default.example"
	t.Cleanup(func() { DefaultClientID, DefaultBrokerURL = oldID, oldBroker })

	tests := []struct {
		name       string
		env        map[string]string
		local      map[string]string
		global     map[string]string
		wantID     string
		wantBroker string
	}{
		{"defaults", nil, nil, nil, "default-id", "https://default.example"},
		{"global over default", nil, nil, map[string]string{"FT_CLIENT_ID": "global"}, "global", "https://default.example"},
		{"local over global", nil, map[string]string{"FT_CLIENT_ID": "local"}, map[string]string{"FT_CLIENT_ID": "global"}, "local", "https://default.example"},
		{"env over local", map[string]string{"FT_CLIENT_ID": "env"}, map[string]string{"FT_CLIENT_ID": "local"}, nil, "env", "https://default.example"},
		{"empty env ignored", map[string]string{"FT_CLIENT_ID": ""}, map[string]string{"FT_CLIENT_ID": "local"}, nil, "local", "https://default.example"},
		{"trailing slash trimmed", map[string]string{"FT_BROKER_URL": "http://localhost:8787/"}, nil, nil, "default-id", "http://localhost:8787"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := resolve(lookup(tt.env), []map[string]string{tt.local, tt.global}, "/cfg")
			if c.ClientID != tt.wantID {
				t.Errorf("ClientID = %q, want %q", c.ClientID, tt.wantID)
			}
			if c.BrokerURL != tt.wantBroker {
				t.Errorf("BrokerURL = %q, want %q", c.BrokerURL, tt.wantBroker)
			}
		})
	}
}

func TestResolveRedirectURI(t *testing.T) {
	c := resolve(lookup(nil), nil, "/cfg")
	if len(c.RedirectURIs) != 3 {
		t.Fatalf("default redirect URIs = %v, want 3", c.RedirectURIs)
	}
	c = resolve(lookup(map[string]string{"FT_REDIRECT_URI": "http://localhost:9000/callback"}), nil, "/cfg")
	if len(c.RedirectURIs) != 4 || c.RedirectURIs[0] != "http://localhost:9000/callback" {
		t.Fatalf("override redirect URIs = %v", c.RedirectURIs)
	}
	c = resolve(lookup(map[string]string{"FT_REDIRECT_URI": DefaultRedirectURIs[1]}), nil, "/cfg")
	if len(c.RedirectURIs) != 3 || c.RedirectURIs[0] != DefaultRedirectURIs[1] {
		t.Fatalf("registered override not deduplicated: %v", c.RedirectURIs)
	}
}

func TestValidate(t *testing.T) {
	if err := (&Config{}).Validate(); err == nil {
		t.Fatal("expected error for empty config")
	}
	if err := (&Config{ClientID: "x", BrokerURL: "y"}).Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadReadsEnvFiles(t *testing.T) {
	cfgDir := t.TempDir()
	workDir := t.TempDir()
	t.Setenv("FT_CONFIG_DIR", cfgDir)
	t.Setenv("FT_CLIENT_ID", "")
	t.Setenv("FT_BROKER_URL", "")
	t.Setenv("FT_LOGIN", "")
	t.Chdir(workDir)

	write := func(path, content string) {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(workDir, ".env"), "FT_CLIENT_ID=from-local\n# comment\n")
	write(filepath.Join(cfgDir, ".env"), "FT_CLIENT_ID=from-global\nFT_BROKER_URL=https://broker.example\nFT_LOGIN=swilmus\n")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.ClientID != "from-local" || c.BrokerURL != "https://broker.example" || c.Login != "swilmus" {
		t.Fatalf("got %+v", c)
	}
	if c.Dir != cfgDir {
		t.Fatalf("Dir = %q, want %q", c.Dir, cfgDir)
	}
}

func TestLoadMissingFilesOK(t *testing.T) {
	t.Setenv("FT_CONFIG_DIR", t.TempDir())
	t.Chdir(t.TempDir())
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}

func TestParseRemind(t *testing.T) {
	for in, want := range map[string]time.Duration{"": 15 * time.Minute, "off": 0, "0": 0, "30m": 30 * time.Minute, "nonsense": 15 * time.Minute} {
		if got := parseRemind(in); got != want {
			t.Errorf("parseRemind(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestShippedDefaults(t *testing.T) {
	if !strings.HasPrefix(DefaultClientID, "u-s4t2ud-") {
		t.Errorf("DefaultClientID = %q, want ft's public app UID", DefaultClientID)
	}
	if !strings.HasPrefix(DefaultBrokerURL, "https://") {
		t.Errorf("DefaultBrokerURL = %q, want an https URL", DefaultBrokerURL)
	}
}
