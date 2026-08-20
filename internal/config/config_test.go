// SPDX-License-Identifier: GPL-3.0-or-later
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	c := New()
	c.Repo = "bmmmm/putzii-drop"
	c.DropBase = "https://bmmmm.github.io/putzii-drop"
	c.PlanID = "AbC123xy"
	c.EncKey = "k123"
	c.PAT = "github_pat_test" // gitleaks:allow
	c.PutziiRef = "68f95ff8b023ead99124ca4a21557a3975e19344"
	c.Users["sina7"] = &User{ID: "sina7", Name: "Sina M.", Token: "tok1"}
	c.Users["timo3"] = &User{ID: "timo3", Name: "Timo", Token: "tok2"}
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o, want 600", fi.Mode().Perm())
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.Repo != c.Repo || back.DropBase != c.DropBase || back.PlanID != c.PlanID ||
		back.EncKey != c.EncKey || back.PAT != c.PAT || back.PutziiRef != c.PutziiRef {
		t.Fatalf("scalar mismatch: %+v", back)
	}
	if len(back.Users) != 2 || back.Users["sina7"].Name != "Sina M." || back.Users["timo3"].Token != "tok2" {
		t.Fatalf("users mismatch: %+v", back.Users)
	}
}

func TestRejectsLoosePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte("repo = x/y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("0644 config accepted")
	}
}

func TestRejectsUnknownKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte("nonsense = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("unknown key accepted")
	}
}

func TestTemplateParses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte(Template()), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("template does not parse: %v", err)
	}
	if c.Repo != "" || len(c.Users) != 0 {
		t.Fatalf("template not blank: %+v", c)
	}
}
