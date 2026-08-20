// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"flag"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bmmmm/putzii-drop/internal/config"
	"github.com/bmmmm/putzii-drop/internal/dropclient"
	"github.com/bmmmm/putzii-drop/internal/link"
	"github.com/bmmmm/putzii-drop/internal/wire"
)

func cmdUser(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: dropii user add|list|link|revoke [flags]")
	}
	switch args[0] {
	case "add":
		return userAdd(args[1:])
	case "list":
		return userList(args[1:])
	case "link":
		return userLink(args[1:])
	case "revoke":
		return userRevoke(args[1:])
	default:
		return fmt.Errorf("unknown user subcommand %q", args[0])
	}
}

// userAdd: name-match against the live plan REUSES existing personIds so
// history stays attributed. The token secret is updated FIRST, then the
// person record (if new) is created through apply.yml — dispatched AS the
// new user, so the audit tail attributes the creation correctly.
func userAdd(args []string) error {
	fs := flag.NewFlagSet("user add", flag.ExitOnError)
	name := fs.String("name", "", "display name (matched against plan people)")
	confPath := fs.String("config", config.DefaultPath(), "config file")
	fs.Parse(args)
	if *name == "" {
		return fmt.Errorf("--name required")
	}
	c, err := loadCtx(*confPath)
	if err != nil {
		return err
	}
	if c.cfg.PlanID == "" {
		return fmt.Errorf("no plan yet — run `dropii plan init` first")
	}
	dc := dropclient.New(c.cfg)
	plan, _, err := dc.PullState()
	if err != nil {
		return err
	}

	norm := wire.NormalizeName(*name)
	var personID string
	var isNew bool
	for _, p := range plan.People {
		if p.DeletedAt == 0 && wire.NormalizeName(p.Name) == norm {
			personID = p.ID
			break
		}
	}
	if personID == "" {
		isNew = true
		for {
			personID = randomID(5)
			taken := false
			for _, p := range plan.People {
				if p.ID == personID {
					taken = true
					break
				}
			}
			if !taken && c.cfg.Users[personID] == nil {
				break
			}
		}
	}
	if u := c.cfg.Users[personID]; u != nil && u.Token != "" {
		return fmt.Errorf("user %s (%s) already has a token — `dropii rotate token --user %s` to replace it", personID, u.Name, personID)
	}

	token := randomID(22)
	c.cfg.Users[personID] = &config.User{ID: personID, Name: norm, Token: token}
	if err := c.admin.SetSecret(secretTokensName, tokensSecret(c.cfg)); err != nil {
		return err
	}
	if err := c.cfg.Save(c.cfg.Path); err != nil {
		return err
	}
	if isNew {
		// create the person record through the ONE write path, as themselves
		now := float64(time.Now().Unix())
		mini := &wire.Plan{
			PlanID: c.cfg.PlanID,
			Name:   "",
			People: []wire.Person{{ID: personID, Name: norm, CreatedAt: now, UpdatedAt: now}},
		}
		payload, err := dropclient.EnvelopePayload(mini)
		if err != nil {
			return err
		}
		nonce := randomID(8)
		if err := dc.Dispatch("envelope", personID, token, nonce, payload); err != nil {
			return err
		}
		fmt.Printf("creating person %s via apply (nonce %s)…\n", personID, nonce)
		if _, err := dc.AwaitNonce(nonce, 3*time.Minute); err != nil {
			return err
		}
		fmt.Printf("✓ new person %s (%s) created and authorized\n", personID, norm)
	} else {
		fmt.Printf("✓ matched existing person %s (%s) — history stays attributed\n", personID, norm)
	}
	printUserLink(c.cfg, personID)
	return nil
}

func userList(args []string) error {
	fs := flag.NewFlagSet("user list", flag.ExitOnError)
	confPath := fs.String("config", config.DefaultPath(), "config file")
	fs.Parse(args)
	c, err := loadCtx(*confPath)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(c.cfg.Users))
	for id := range c.cfg.Users {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		fmt.Println("no users — `dropii user add --name <name>`")
		return nil
	}
	for _, id := range ids {
		u := c.cfg.Users[id]
		state := "write ok"
		if u.Token == "" {
			state = "NO TOKEN (revoked?)"
		}
		fmt.Printf("%-8s %-20s %s\n", id, u.Name, state)
	}
	return nil
}

func userLink(args []string) error {
	fs := flag.NewFlagSet("user link", flag.ExitOnError)
	userID := fs.String("user", "", "person id")
	confPath := fs.String("config", config.DefaultPath(), "config file")
	fs.Parse(args)
	c, err := loadCtx(*confPath)
	if err != nil {
		return err
	}
	u, err := pickUser(c.cfg, *userID)
	if err != nil {
		return err
	}
	printUserLink(c.cfg, u.ID)
	return nil
}

func printUserLink(cfg *config.Config, personID string) {
	u := cfg.Users[personID]
	if u == nil || u.Token == "" {
		return
	}
	// A credential link embeds the PAT. Refuse to render one around anything
	// that is not a fine-grained PAT (github_pat_…) — a classic OAuth/keyring
	// token has FULL repo scope and must never travel in a link.
	if !strings.HasPrefix(cfg.PAT, "github_pat_") {
		fmt.Println("\nlink NOT rendered: configured pat is not a fine-grained PAT (github_pat_…) —")
		fmt.Println("a broader token must never be embedded in a share link. Fix via `dropii rotate pat`.")
		return
	}
	url, err := link.URL(cfg.AppBase, &link.Credentials{
		PlanID: cfg.PlanID, PersonID: u.ID, PersonName: u.Name, Token: u.Token,
		EncKey: cfg.EncKey, PAT: cfg.PAT, Repo: cfg.Repo, DropBase: cfg.DropBase,
	})
	if err != nil {
		fmt.Println("link:", err)
		return
	}
	fmt.Printf("\npersonal link for %s (SECRET — share only with them, e.g. via Signal):\n%s\n", u.Name, url)
}

// userRevoke kills WRITES within seconds (token out of the secret map).
// Reads survive until `dropii rotate key`.
func userRevoke(args []string) error {
	fs := flag.NewFlagSet("user revoke", flag.ExitOnError)
	userID := fs.String("user", "", "person id")
	confPath := fs.String("config", config.DefaultPath(), "config file")
	fs.Parse(args)
	if *userID == "" {
		return fmt.Errorf("--user <personId> required")
	}
	c, err := loadCtx(*confPath)
	if err != nil {
		return err
	}
	u := c.cfg.Users[*userID]
	if u == nil {
		return fmt.Errorf("unknown user %q", *userID)
	}
	u.Token = ""
	if err := c.admin.SetSecret(secretTokensName, tokensSecret(c.cfg)); err != nil {
		return err
	}
	if err := c.cfg.Save(c.cfg.Path); err != nil {
		return err
	}
	fmt.Printf("✓ %s (%s) can no longer write (effective now)\n", u.ID, u.Name)
	fmt.Println("  they can still READ with their old link — run `dropii rotate key` to cut reads")
	return nil
}
