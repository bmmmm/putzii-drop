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

func checkinCreds() *CheckinCredentials {
	return &CheckinCredentials{
		PlanID: "AbC123xy", PersonID: "sina7", PersonName: "Sina",
		Token: "tttttttttttttttttttttt",
		PAT:   "github_pat_" + strings.Repeat("x", 71), Repo: "bmmmm/putzii-drop",
		DropBase: "https://bmmmm.github.io/putzii-drop",
		Areas:    []CheckinArea{{ID: "jwtec", Label: "Bad"}, {ID: "5gu5d", Label: "Müll rausbringen"}},
	}
}

func TestCheckinRoundtrip(t *testing.T) {
	c := checkinCreds()
	frag, err := CheckinFragment(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(frag, "#k1.") {
		t.Fatalf("prefix: %q", frag[:8])
	}
	if strings.Contains(frag, "encKey") {
		t.Fatal("k1 fragment mentions encKey")
	}
	back, err := ParseCheckin(frag)
	if err != nil {
		t.Fatal(err)
	}
	if back.PlanID != c.PlanID || back.Token != c.Token || len(back.Areas) != 2 ||
		back.Areas[1] != c.Areas[1] {
		t.Fatalf("roundtrip mismatch:\n%+v\n%+v", back, c)
	}
	u, err := CheckinURL("https://bmmmm.github.io/putzii/", c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(u, "/c.html#k1.") {
		t.Fatalf("URL must target c.html: %q", u)
	}
	if back2, err := ParseCheckin(u); err != nil || back2.PersonID != c.PersonID {
		t.Fatalf("URL parse: %v", err)
	}
}

func TestCheckinSize(t *testing.T) {
	frag, err := CheckinFragment(checkinCreds())
	if err != nil {
		t.Fatal(err)
	}
	// must stay comfortably inside the app's 1800-char Signal budget
	if len(frag) > 800 {
		t.Fatalf("k1 fragment grew to %d chars", len(frag))
	}
}

func TestCheckinValidation(t *testing.T) {
	c := checkinCreds()
	c.Areas = nil
	if _, err := CheckinFragment(c); err == nil {
		t.Fatal("zero areas accepted")
	}
	c = checkinCreds()
	c.Areas = make([]CheckinArea, MaxCheckinAreas+1)
	for i := range c.Areas {
		c.Areas[i] = CheckinArea{ID: "jwtec", Label: "x"}
	}
	if _, err := CheckinFragment(c); err == nil {
		t.Fatal("13 areas accepted")
	}
	c = checkinCreds()
	c.PAT = ""
	if _, err := CheckinFragment(c); err == nil {
		t.Fatal("missing PAT accepted")
	}
	for _, s := range []string{"", "#d1.abc", "k1.!!!", "k1.aGk"} {
		if _, err := ParseCheckin(s); err == nil {
			t.Fatalf("junk accepted: %q", s)
		}
	}
}
