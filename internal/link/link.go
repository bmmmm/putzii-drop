// SPDX-License-Identifier: GPL-3.0-or-later

// Package link builds/parses the #d1. credential link: a positional
// b64url-encoded JSON array — NO gzip, the content is high-entropy anyway.
// [1, planId, personId, personName, token, encKey, pat, repo, dropBase]
// dropBase travels IN the link (not hardcoded) so a local dev loop or a
// foreign household can run its own drop with the same app.
package link

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const Version = 1

type Credentials struct {
	PlanID     string
	PersonID   string
	PersonName string
	Token      string
	EncKey     string // base64url state key
	PAT        string
	Repo       string // owner/name
	DropBase   string // Pages base URL
}

// Fragment renders the "#d1.<payload>" fragment (with leading '#').
func Fragment(c *Credentials) (string, error) {
	for name, v := range map[string]string{
		"planId": c.PlanID, "personId": c.PersonID, "token": c.Token,
		"encKey": c.EncKey, "pat": c.PAT, "repo": c.Repo, "dropBase": c.DropBase,
	} {
		if v == "" {
			return "", fmt.Errorf("credential link: %s missing", name)
		}
	}
	arr := []any{Version, c.PlanID, c.PersonID, c.PersonName, c.Token, c.EncKey, c.PAT, c.Repo, c.DropBase}
	raw, err := json.Marshal(arr)
	if err != nil {
		return "", err
	}
	return "#d1." + base64.RawURLEncoding.EncodeToString(raw), nil
}

// URL renders the full app URL for a credential link.
func URL(appBase string, c *Credentials) (string, error) {
	frag, err := Fragment(c)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(appBase, "/") + "/" + frag, nil
}

// ── #k1. confirm links ──────────────────────────────────────────────────
//
// A k1 link is the pre-scoped Signal variant of d1: same positional b64url
// JSON array but WITHOUT the encKey — the holder can trigger server-minted
// check-ins (mode=checkin), never read state — plus the fixed activity list:
//   [1, planId, personId, personName, token, pat, repo, dropBase, [[areaId, label], …]]
// It targets c.html (the confirm page), not the app root.
//
// Capability note: the embedded token is the person's full write token, so a
// k1 holder could technically hand-craft envelope dispatches as that person
// (they still cannot READ anything). Fine as long as k1 links only go to the
// person themselves. If links should ever go to semi-trusted outsiders
// (guest, cleaner), the token map needs a per-token scope ("checkin-only")
// that apply.mjs enforces in its AUTH step — a runner change near
// invariant 1, to be planned as its own unit of work.

const CheckinVersion = 1

// MaxCheckinAreas mirrors the app's K1_MAX_AREAS in drop.js.
const MaxCheckinAreas = 12

// CheckinArea is one pre-scoped activity in a #k1. confirm link.
type CheckinArea struct {
	ID    string
	Label string
}

// CheckinCredentials is Credentials minus the encKey, plus the area scope.
type CheckinCredentials struct {
	PlanID     string
	PersonID   string
	PersonName string
	Token      string
	PAT        string
	Repo       string
	DropBase   string
	Areas      []CheckinArea
}

// CheckinFragment renders the "#k1.<payload>" fragment (with leading '#').
func CheckinFragment(c *CheckinCredentials) (string, error) {
	for name, v := range map[string]string{
		"planId": c.PlanID, "personId": c.PersonID, "token": c.Token,
		"pat": c.PAT, "repo": c.Repo, "dropBase": c.DropBase,
	} {
		if v == "" {
			return "", fmt.Errorf("confirm link: %s missing", name)
		}
	}
	if len(c.Areas) == 0 || len(c.Areas) > MaxCheckinAreas {
		return "", fmt.Errorf("confirm link: need 1–%d areas, got %d", MaxCheckinAreas, len(c.Areas))
	}
	areas := make([][2]string, 0, len(c.Areas))
	for _, a := range c.Areas {
		if a.ID == "" {
			return "", errors.New("confirm link: empty areaId")
		}
		areas = append(areas, [2]string{a.ID, a.Label})
	}
	arr := []any{CheckinVersion, c.PlanID, c.PersonID, c.PersonName, c.Token, c.PAT, c.Repo, c.DropBase, areas}
	raw, err := json.Marshal(arr)
	if err != nil {
		return "", err
	}
	return "#k1." + base64.RawURLEncoding.EncodeToString(raw), nil
}

// CheckinURL renders the full confirm-page URL (c.html owns the k1 flow).
func CheckinURL(appBase string, c *CheckinCredentials) (string, error) {
	frag, err := CheckinFragment(c)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(appBase, "/") + "/c.html" + frag, nil
}

// ParseCheckin accepts a k1 fragment with or without leading '#'/URL prefix.
func ParseCheckin(s string) (*CheckinCredentials, error) {
	if i := strings.Index(s, "#"); i >= 0 {
		s = s[i+1:]
	}
	if !strings.HasPrefix(s, "k1.") {
		return nil, errors.New("not a k1 link")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSuffix(s[3:], "="))
	if err != nil {
		return nil, err
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, err
	}
	if len(arr) < 9 {
		return nil, errors.New("short k1 payload")
	}
	var v int
	if json.Unmarshal(arr[0], &v) != nil || v != CheckinVersion {
		return nil, errors.New("unknown k1 version")
	}
	fields := make([]string, 7)
	for i := 1; i <= 7; i++ {
		if json.Unmarshal(arr[i], &fields[i-1]) != nil {
			return nil, errors.New("bad k1 field")
		}
	}
	var rawAreas [][2]string
	if json.Unmarshal(arr[8], &rawAreas) != nil || len(rawAreas) == 0 {
		return nil, errors.New("bad k1 areas")
	}
	areas := make([]CheckinArea, 0, len(rawAreas))
	for _, a := range rawAreas {
		areas = append(areas, CheckinArea{ID: a[0], Label: a[1]})
	}
	return &CheckinCredentials{
		PlanID: fields[0], PersonID: fields[1], PersonName: fields[2], Token: fields[3],
		PAT: fields[4], Repo: fields[5], DropBase: strings.TrimRight(fields[6], "/"),
		Areas: areas,
	}, nil
}

// Parse accepts a fragment with or without leading '#'/URL prefix.
func Parse(s string) (*Credentials, error) {
	if i := strings.Index(s, "#"); i >= 0 {
		s = s[i+1:]
	}
	if !strings.HasPrefix(s, "d1.") {
		return nil, errors.New("not a d1 link")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSuffix(s[3:], "="))
	if err != nil {
		return nil, err
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, err
	}
	if len(arr) < 9 {
		return nil, errors.New("short d1 payload")
	}
	var v int
	if json.Unmarshal(arr[0], &v) != nil || v != Version {
		return nil, errors.New("unknown d1 version")
	}
	fields := make([]string, 8)
	for i := 1; i <= 8; i++ {
		if json.Unmarshal(arr[i], &fields[i-1]) != nil {
			return nil, errors.New("bad d1 field")
		}
	}
	return &Credentials{
		PlanID: fields[0], PersonID: fields[1], PersonName: fields[2], Token: fields[3],
		EncKey: fields[4], PAT: fields[5], Repo: fields[6], DropBase: strings.TrimRight(fields[7], "/"),
	}, nil
}
