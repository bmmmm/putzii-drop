// SPDX-License-Identifier: GPL-3.0-or-later

// Package config reads/writes dropii.conf — a commented flat key=value file.
// The real file holds the encryption key, the PAT and user tokens: it is
// written 0600 and lives in .gitignore; only the blank template is tracked.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const FileName = "dropii.conf"

type User struct {
	ID    string
	Name  string
	Token string
}

type Config struct {
	Repo      string // owner/name of the drop repo
	DropBase  string // Pages base URL, e.g. https://bmmmm.github.io/putzii-drop
	AppBase   string // putzii app base URL (credential links point here)
	PlanID    string
	EncKey    string // base64url, 32 bytes
	PAT       string // fine-grained, Actions:write only
	PutziiRef string // pinned commit SHA of bmmmm/putzii (mirror of the repo variable)
	Users     map[string]*User
	Path      string // where this config was loaded from
}

func New() *Config {
	return &Config{Users: map[string]*User{}}
}

// DefaultPath: ./dropii.conf (convention: config lives in the project folder).
func DefaultPath() string {
	return FileName
}

func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s is group/world readable (%o) — chmod 600 it", path, fi.Mode().Perm())
	}
	cfg := New()
	cfg.Path = path
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("bad config line: %q", line)
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		switch {
		case key == "repo":
			cfg.Repo = val
		case key == "drop_base":
			cfg.DropBase = strings.TrimRight(val, "/")
		case key == "app_base":
			cfg.AppBase = strings.TrimRight(val, "/")
		case key == "plan_id":
			cfg.PlanID = val
		case key == "enc_key":
			cfg.EncKey = val
		case key == "pat":
			cfg.PAT = val
		case key == "putzii_ref":
			cfg.PutziiRef = val
		case strings.HasPrefix(key, "user."):
			parts := strings.SplitN(key, ".", 3)
			if len(parts) != 3 {
				return nil, fmt.Errorf("bad user key: %q", key)
			}
			id, field := parts[1], parts[2]
			u := cfg.Users[id]
			if u == nil {
				u = &User{ID: id}
				cfg.Users[id] = u
			}
			switch field {
			case "name":
				u.Name = val
			case "token":
				u.Token = val
			default:
				return nil, fmt.Errorf("unknown user field: %q", key)
			}
		default:
			return nil, fmt.Errorf("unknown config key: %q", key)
		}
	}
	return cfg, sc.Err()
}

// Save writes the config 0600, atomically (temp file + rename).
func (c *Config) Save(path string) error {
	if path == "" {
		return errors.New("no config path")
	}
	var b strings.Builder
	b.WriteString("# dropii.conf — HOLDS SECRETS (encryption key, PAT, user tokens).\n")
	b.WriteString("# Keep chmod 600. Never commit; dropii.conf is gitignored.\n\n")
	fmt.Fprintf(&b, "repo = %s\n", c.Repo)
	fmt.Fprintf(&b, "drop_base = %s\n", c.DropBase)
	fmt.Fprintf(&b, "app_base = %s\n", c.AppBase)
	fmt.Fprintf(&b, "plan_id = %s\n", c.PlanID)
	fmt.Fprintf(&b, "enc_key = %s\n", c.EncKey)
	fmt.Fprintf(&b, "pat = %s\n", c.PAT)
	fmt.Fprintf(&b, "putzii_ref = %s\n", c.PutziiRef)
	ids := make([]string, 0, len(c.Users))
	for id := range c.Users {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		u := c.Users[id]
		b.WriteString("\n")
		fmt.Fprintf(&b, "user.%s.name = %s\n", id, u.Name)
		fmt.Fprintf(&b, "user.%s.token = %s\n", id, u.Token)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".dropii-conf-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(b.String()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// Template is the tracked blank example (dropii.conf.example).
func Template() string {
	return `# dropii.conf — configuration for the dropii CLI.
# COPY to dropii.conf, chmod 600. The real file HOLDS SECRETS
# (encryption key, PAT, user tokens) and must never be committed.

# Drop repo (GitHub owner/name)
repo =

# GitHub Pages base URL of the drop repo (no trailing slash)
drop_base =

# putzii app base URL — credential links point here (no trailing slash)
app_base =

# putzii plan id (8 base64url chars, from the app's share link / export)
plan_id =

# AES-256 state key, base64url (43 chars). 'dropii setup' generates this.
enc_key =

# Fine-grained PAT, permissions: Actions Read+write ONLY (see dropii setup)
pat =

# Pinned bmmmm/putzii commit SHA (mirrors the repo variable PUTZII_REF)
putzii_ref =

# Users ('dropii user add' manages these):
# user.<personId>.name = <display name>
# user.<personId>.token = <write token>
`
}
