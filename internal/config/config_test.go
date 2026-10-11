package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	data := `{"projects": [
		{"name": "shop", "tenant_id": "abc", "api_key": "k1"},
		{"name": "self", "admin_url": "https://auth.example.com/admin/", "api_key_env": "HANKOPOD_TEST_KEY"}
	]}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HANKOPOD_TEST_KEY", "k2")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Projects) != 2 {
		t.Fatalf("projects = %d", len(cfg.Projects))
	}
	if p := cfg.Projects[0]; p.AdminURL != "https://abc.hanko.io/admin" || p.StatusURL != "https://abc.hanko.io/" || p.APIKey != "k1" {
		t.Errorf("cloud project = %+v", p)
	}
	if p := cfg.Projects[1]; p.AdminURL != "https://auth.example.com/admin/" || p.StatusURL != "" || p.APIKey != "k2" {
		t.Errorf("self-hosted project = %+v", p)
	}
	if cfg.SessionScanLimit != DefaultSessionScanLimit {
		t.Errorf("session scan limit = %d", cfg.SessionScanLimit)
	}
	if !strings.Contains(cfg.Warning, "chmod 600") {
		t.Errorf("no warning for a world-readable file: %q", cfg.Warning)
	}
}

func TestResolveErrors(t *testing.T) {
	env := func(string) string { return "" }
	for name, f := range map[string]File{
		"empty":     {},
		"no url":    {Projects: []ProjectEntry{{Name: "a"}}},
		"duplicate": {Projects: []ProjectEntry{{Name: "a", TenantID: "x"}, {Name: "a", TenantID: "y"}}},
		"empty env": {Projects: []ProjectEntry{{Name: "a", TenantID: "x", APIKeyEnv: "NOPE"}}},
		"nameless":  {Projects: []ProjectEntry{{AdminURL: "https://x"}}},
	} {
		if _, err := f.resolve(env); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
