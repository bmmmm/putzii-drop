// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/bmmmm/putzii-drop/internal/config"
	"github.com/bmmmm/putzii-drop/internal/dropclient"
	"github.com/bmmmm/putzii-drop/internal/link"
	"github.com/bmmmm/putzii-drop/internal/wire"
)

// cmdLink renders share links beyond the personal d1 (which lives under
// `user link`). "link checkin" builds the pre-scoped #k1. confirm link for
// the Signal flow: person + fixed activities, confirm-only page, no encKey
// in the link (can trigger check-ins, never read state).
func cmdLink(args []string) error {
	if len(args) < 1 || args[0] != "checkin" {
		return fmt.Errorf("usage: dropii link checkin --user <id> --areas <id|name,...>")
	}
	fs := flag.NewFlagSet("link checkin", flag.ExitOnError)
	userID := fs.String("user", "", "person id (default: first configured)")
	areasFlag := fs.String("areas", "", "comma-separated area ids or names")
	confPath := fs.String("config", config.DefaultPath(), "config file")
	fs.Parse(args[1:])
	if *areasFlag == "" {
		return fmt.Errorf("--areas <id-or-name,...> required")
	}
	c, err := loadCtx(*confPath)
	if err != nil {
		return err
	}
	u, err := pickUser(c.cfg, *userID)
	if err != nil {
		return err
	}
	// Same guard as printUserLink: only a fine-grained PAT may travel in a link.
	if !strings.HasPrefix(c.cfg.PAT, "github_pat_") {
		return fmt.Errorf("configured pat is not a fine-grained PAT (github_pat_…) — a broader token must never travel in a link; fix via `dropii rotate pat`")
	}
	dc := dropclient.New(c.cfg)
	plan, _, err := dc.PullState()
	if err != nil {
		return err
	}
	areas, err := resolveAreas(plan, strings.Split(*areasFlag, ","))
	if err != nil {
		return err
	}
	url, err := link.CheckinURL(c.cfg.AppBase, &link.CheckinCredentials{
		PlanID: c.cfg.PlanID, PersonID: u.ID, PersonName: u.Name, Token: u.Token,
		PAT: c.cfg.PAT, Repo: c.cfg.Repo, DropBase: c.cfg.DropBase, Areas: areas,
	})
	if err != nil {
		return err
	}
	labels := make([]string, len(areas))
	for i, a := range areas {
		labels[i] = a.Label
	}
	fmt.Printf("\nconfirm link for %s — %s (SECRET — embeds write access as them, share only with them, e.g. via Signal):\n%s\n",
		u.Name, strings.Join(labels, ", "), url)
	return nil
}

// resolveAreas matches each token against live areas by id first, then by
// normalized name — the caller thinks in names, links carry ids.
func resolveAreas(plan *wire.Plan, tokens []string) ([]link.CheckinArea, error) {
	var out []link.CheckinArea
	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		var found *wire.Area
		for i := range plan.Areas {
			a := &plan.Areas[i]
			if a.DeletedAt != 0 {
				continue
			}
			if a.ID == tok || strings.EqualFold(wire.NormalizeName(a.Name), wire.NormalizeName(tok)) {
				found = a
				break
			}
		}
		if found == nil {
			var live []string
			for _, a := range plan.Areas {
				if a.DeletedAt == 0 {
					live = append(live, fmt.Sprintf("%s (%s)", a.Name, a.ID))
				}
			}
			return nil, fmt.Errorf("area %q not found — live areas: %s", tok, strings.Join(live, ", "))
		}
		out = append(out, link.CheckinArea{ID: found.ID, Label: found.Name})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no areas resolved")
	}
	return out, nil
}
