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
