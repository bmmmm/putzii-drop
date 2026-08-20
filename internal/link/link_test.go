// SPDX-License-Identifier: GPL-3.0-or-later
package link

import (
	"strings"
	"testing"
)

func creds() *Credentials {
	return &Credentials{
		PlanID: "AbC123xy", PersonID: "sina7", PersonName: "Sina",
		Token: "tttttttttttttttttttttt", EncKey: strings.Repeat("k", 43),
		PAT: "github_pat_" + strings.Repeat("x", 71), Repo: "bmmmm/putzii-drop",
		DropBase: "https://bmmmm.github.io/putzii-drop",
	}
}

func TestRoundtrip(t *testing.T) {
	c := creds()
	frag, err := Fragment(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(frag, "#d1.") {
		t.Fatalf("prefix: %q", frag[:8])
	}
	back, err := Parse(frag)
	if err != nil {
		t.Fatal(err)
	}
	if *back != *c {
		t.Fatalf("roundtrip mismatch:\n%+v\n%+v", back, c)
	}
	// also from a full URL
	u, err := URL("https://bmmmm.github.io/putzii/", c)
	if err != nil {
		t.Fatal(err)
	}
	back2, err := Parse(u)
	if err != nil || *back2 != *c {
		t.Fatalf("URL parse: %v", err)
	}
}

func TestSize(t *testing.T) {
	frag, err := Fragment(creds())
	if err != nil {
		t.Fatal(err)
	}
	// plan says ≈440 chars, QR-able; guard against silent bloat
	if len(frag) > 600 {
		t.Fatalf("d1 fragment grew to %d chars", len(frag))
	}
}

func TestMissingField(t *testing.T) {
	c := creds()
	c.PAT = ""
	if _, err := Fragment(c); err == nil {
		t.Fatal("missing PAT accepted")
	}
}

func TestParseJunk(t *testing.T) {
	for _, s := range []string{"", "#p1.abc", "d1.!!!", "d1.aGk"} {
		if _, err := Parse(s); err == nil {
			t.Fatalf("junk accepted: %q", s)
		}
	}
}
