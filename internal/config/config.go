// Package config loads the list of Hanko projects to watch from
// ~/.config/hankopod/config.json (or $HANKOPOD_CONFIG).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pvanek/hankopod/internal/core"
	"github.com/pvanek/hankopod/internal/hanko"
)

// DefaultSessionScanLimit caps how many users are asked for their sessions
// per project and refresh; Hanko has no project-wide session count.
const DefaultSessionScanLimit = 500

// File is the on-disk configuration.
type File struct {
	Projects []ProjectEntry `json:"projects"`
	// SessionScanLimit overrides DefaultSessionScanLimit; 0 keeps the default.
	SessionScanLimit int `json:"session_scan_limit,omitempty"`
}

// ProjectEntry is one Hanko project. Give either TenantID (Hanko Cloud) or
// AdminURL (self-hosted, or to override), and the API key either inline
// or through an environment variable.
type ProjectEntry struct {
	Name     string `json:"name"`
	TenantID string `json:"tenant_id,omitempty"`
	AdminURL string `json:"admin_url,omitempty"`
	// StatusURL is the public status page; it defaults to the public API
	// root for Hanko Cloud and to the Admin API root otherwise.
	StatusURL string `json:"status_url,omitempty"`
	APIKey    string `json:"api_key,omitempty"`
	APIKeyEnv string `json:"api_key_env,omitempty"`
}

// Config is the validated configuration.
type Config struct {
	Path             string
	Projects         []core.Project
	SessionScanLimit int
	// Warning is set when the file is readable by other users.
	Warning string
}

// DefaultPath is $HANKOPOD_CONFIG or ~/.config/hankopod/config.json.
func DefaultPath() (string, error) {
	if p := os.Getenv("HANKOPOD_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "hankopod", "config.json"), nil
}

// Load reads and validates the configuration at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("no configuration at %s: see the README for the format", path)
	}
	if err != nil {
		return nil, err
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	cfg, err := f.resolve(os.Getenv)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	cfg.Path = path
	if info, err := os.Stat(path); err == nil && info.Mode().Perm()&0o077 != 0 {
		cfg.Warning = fmt.Sprintf("%s holds API keys but is readable by other users; run chmod 600 on it", path)
	}
	return cfg, nil
}

func (f File) resolve(getenv func(string) string) (*Config, error) {
	if len(f.Projects) == 0 {
		return nil, errors.New("no projects configured")
	}
	cfg := &Config{SessionScanLimit: f.SessionScanLimit}
	if cfg.SessionScanLimit <= 0 {
		cfg.SessionScanLimit = DefaultSessionScanLimit
	}
	seen := map[string]bool{}
	for i, p := range f.Projects {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			name = p.TenantID
		}
		if name == "" {
			return nil, fmt.Errorf("project %d: name or tenant_id is required", i+1)
		}
		if seen[name] {
			return nil, fmt.Errorf("project %q is listed twice", name)
		}
		seen[name] = true

		adminURL, statusURL := p.AdminURL, p.StatusURL
		if p.TenantID != "" {
			if adminURL == "" {
				adminURL = hanko.CloudAdminURL(p.TenantID)
			}
			if statusURL == "" {
				statusURL = hanko.CloudPublicURL(p.TenantID) + "/"
			}
		}
		if adminURL == "" {
			return nil, fmt.Errorf("project %q: tenant_id or admin_url is required", name)
		}
		key := p.APIKey
		if p.APIKeyEnv != "" {
			key = getenv(p.APIKeyEnv)
			if key == "" {
				return nil, fmt.Errorf("project %q: environment variable %s is empty", name, p.APIKeyEnv)
			}
		}
		cfg.Projects = append(cfg.Projects, core.Project{Name: name, AdminURL: adminURL, StatusURL: statusURL, APIKey: key})
	}
	return cfg, nil
}
