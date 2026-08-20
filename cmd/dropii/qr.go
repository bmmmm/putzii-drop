// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmmmm/putzii-drop/internal/config"
	"github.com/bmmmm/putzii-drop/internal/dropclient"
	"github.com/bmmmm/putzii-drop/internal/link"
	pngpkg "github.com/bmmmm/putzii-drop/internal/png"
	"github.com/bmmmm/putzii-drop/internal/qr"
	"github.com/bmmmm/putzii-drop/internal/sheet"
)

func cmdQr(args []string) error {
	fs := flag.NewFlagSet("qr", flag.ExitOnError)
	areas := fs.Bool("areas", false, "one check-in QR (c1.) per live area")
	userID := fs.String("user", "", "credential-link QR (d1.) for this user")
	doSheet := fs.Bool("sheet", false, "printable HTML sheet of all area QRs")
	out := fs.String("out", ".", "output directory (or file for --sheet)")
	confPath := fs.String("config", config.DefaultPath(), "config file")
	fs.Parse(args)
	c, err := loadCtx(*confPath)
	if err != nil {
		return err
	}
	switch {
	case *userID != "":
		return qrUser(c, *userID, *out)
	case *areas || *doSheet:
		return qrAreas(c, *doSheet, *out)
	default:
		return fmt.Errorf("usage: dropii qr --areas | --user <id> | --sheet [--out path]")
	}
}

// qrUser: the d1 link is dense — LOW ECC keeps the module count printable
// (same choice the app makes for big payloads).
func qrUser(c *ctx, userID, out string) error {
	u := c.cfg.Users[userID]
	if u == nil || u.Token == "" {
		return fmt.Errorf("unknown or revoked user %q", userID)
	}
	url, err := link.URL(c.cfg.AppBase, &link.Credentials{
		PlanID: c.cfg.PlanID, PersonID: u.ID, PersonName: u.Name, Token: u.Token,
		EncKey: c.cfg.EncKey, PAT: c.cfg.PAT, Repo: c.cfg.Repo, DropBase: c.cfg.DropBase,
	})
	if err != nil {
		return err
	}
	code, err := qr.EncodeText(url, qr.Low)
	if err != nil {
		return err
	}
	path := filepath.Join(out, "putzii-zugang-"+u.ID+".png")
	if err := pngpkg.WriteFile(path, pngpkg.Render(code, 8, 4)); err != nil {
		return err
	}
	fmt.Printf("✓ %s — PERSONAL SECRET QR for %s (full access, treat like a key)\n", path, u.Name)
	return nil
}

// qrAreas: check-in QRs use QUARTILE ECC — printed stickers get splashed.
func qrAreas(c *ctx, asSheet bool, out string) error {
	dc := dropclient.New(c.cfg)
	plan, _, err := dc.PullState()
	if err != nil {
		return err
	}
	var items []sheet.Item
	for _, a := range plan.Areas {
		if a.DeletedAt != 0 {
			continue
		}
		url := fmt.Sprintf("%s/c.html#c1.%s.%s", c.cfg.AppBase, c.cfg.PlanID, a.ID)
		code, err := qr.EncodeText(url, qr.Quartile)
		if err != nil {
			return err
		}
		img := pngpkg.Render(code, 8, 4)
		if asSheet {
			data, err := pngpkg.Encode(img)
			if err != nil {
				return err
			}
			items = append(items, sheet.Item{Label: a.Name, Sublabel: fmt.Sprintf("alle %d Tage", int(a.IntervalDays)), PNG: data})
			continue
		}
		path := filepath.Join(out, "putzii-checkin-"+a.ID+".png")
		if err := pngpkg.WriteFile(path, img); err != nil {
			return err
		}
		fmt.Printf("✓ %s (%s)\n", path, a.Name)
	}
	if asSheet {
		html := sheet.Render("putzii Check-in QRs", items)
		path := out
		if !strings.HasSuffix(path, ".html") {
			path = filepath.Join(out, "putzii-qr-sheet.html")
		}
		if err := os.WriteFile(path, []byte(html), 0o644); err != nil {
			return err
		}
		fmt.Printf("✓ %s (%d areas — print via browser)\n", path, len(items))
	}
	return nil
}
