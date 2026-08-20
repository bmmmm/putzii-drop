// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"flag"
	"fmt"

	"github.com/bmmmm/putzii-drop/internal/config"
)

// cmdButton renders check-in button snippets — a dumb HTTP POST is
// semantically correct because the WORKFLOW mints the event (mode=checkin).
func cmdButton(args []string) error {
	if len(args) < 1 || args[0] != "new" {
		return fmt.Errorf("usage: dropii button new --kind curl|ha|shortcut|http --area <areaId> [--user <id>]")
	}
	fs := flag.NewFlagSet("button new", flag.ExitOnError)
	kind := fs.String("kind", "curl", "curl | ha | shortcut | http")
	area := fs.String("area", "", "areaId to check in")
	userID := fs.String("user", "", "acting user (default: first configured)")
	confPath := fs.String("config", config.DefaultPath(), "config file")
	fs.Parse(args[1:])
	if *area == "" {
		return fmt.Errorf("--area <areaId> required")
	}
	c, err := loadCtx(*confPath)
	if err != nil {
		return err
	}
	u, err := pickUser(c.cfg, *userID)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/actions/workflows/apply.yml/dispatches", c.cfg.Repo)
	bodyTmpl := fmt.Sprintf(`{"ref":"main","inputs":{"mode":"checkin","planId":"%s","personId":"%s","token":"%s","nonce":"NONCE","payload":"%s"}}`,
		c.cfg.PlanID, u.ID, u.Token, *area)

	switch *kind {
	case "curl":
		fmt.Printf(`# check-in button for %s / area %s — replace NONCE with 8 random [a-z2-9]
# chars per press (reusing one skips as replay; that also makes retries safe)
curl -sS -X POST \
  -H "Authorization: Bearer %s" \
  -H "Accept: application/vnd.github+json" \
  %s \
  -d '%s'
`, u.Name, *area, c.cfg.PAT, url, bodyTmpl)
	case "ha":
		fmt.Printf(`# Home Assistant rest_command (configuration.yaml) — check-in %s / %s
rest_command:
  putzii_checkin_%s:
    url: "%s"
    method: POST
    headers:
      Authorization: "Bearer %s"
      Accept: "application/vnd.github+json"
    payload: >-
      {"ref":"main","inputs":{"mode":"checkin","planId":"%s","personId":"%s",
      "token":"%s","nonce":"{{ range(100000, 999999) | random }}z{{ range(2, 9) | random }}",
      "payload":"%s"}}
# note: HA's random nonce uses digits — the drop accepts [a-z2-9]; digits 0/1
# never appear in the ranges above.
`, u.Name, *area, *area, url, c.cfg.PAT, c.cfg.PlanID, u.ID, u.Token, *area)
	case "shortcut":
		fmt.Printf(`# Apple Shortcut — check-in %s / area %s
1. Action "Text": generate nonce — use "Zufallszahl 22222222–99999999" then
   Text = that number with the digits 0/1 never occurring (range trick).
2. Action "Inhalte von URL abrufen":
   URL:    %s
   Methode: POST
   Header:  Authorization = Bearer %s
            Accept = application/vnd.github+json
   Text (JSON): %s
   (NONCE durch die Zufallszahl aus Schritt 1 ersetzen)
`, u.Name, *area, url, c.cfg.PAT, bodyTmpl)
	case "http":
		fmt.Printf(`POST %s
Authorization: Bearer %s
Accept: application/vnd.github+json
Content-Type: application/json

%s
`, url, c.cfg.PAT, bodyTmpl)
	default:
		return fmt.Errorf("unknown --kind %q", *kind)
	}
	return nil
}
