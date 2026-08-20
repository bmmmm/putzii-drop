// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"

	"github.com/bmmmm/putzii-drop/internal/config"
	"github.com/bmmmm/putzii-drop/internal/dropclient"
)

func cmdRotate(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: dropii rotate pat|key|token [flags]")
	}
	switch args[0] {
	case "pat":
		return rotatePAT(args[1:])
	case "key":
		return rotateKey(args[1:])
	case "token":
		return rotateToken(args[1:])
	default:
		return fmt.Errorf("unknown rotate subcommand %q", args[0])
	}
}

// rotatePAT: revoke the old one in the GitHub UI, mint a new one, feed it in.
func rotatePAT(args []string) error {
	fs := flag.NewFlagSet("rotate pat", flag.ExitOnError)
	confPath := fs.String("config", config.DefaultPath(), "config file")
	fs.Parse(args)
	c, err := loadCtx(*confPath)
	if err != nil {
		return err
	}
	fmt.Printf(`Revoke the old PAT and create a new one (same recipe):
  https://github.com/settings/personal-access-tokens
  → new token: Repository access %s, Actions Read+write ONLY
`, c.cfg.Repo)
	pat, err := readSecretPrompt("Paste new PAT (input hidden): ")
	if err != nil || pat == "" {
		return fmt.Errorf("no PAT provided")
	}
	c.cfg.PAT = pat
	if err := grantProbes(c.admin, c.cfg); err != nil {
		return err
	}
	if err := c.cfg.Save(c.cfg.Path); err != nil {
		return err
	}
	fmt.Println("✓ PAT rotated — ALL personal links carry the old PAT and must be re-issued:")
	for id := range c.cfg.Users {
		printUserLink(c.cfg, id)
	}
	return nil
}

// rotateKey cuts READ access for everyone holding an old link: fresh key,
// state re-encrypted (direct commit #2 of 2), all links re-issued.
func rotateKey(args []string) error {
	fs := flag.NewFlagSet("rotate key", flag.ExitOnError)
	confPath := fs.String("config", config.DefaultPath(), "config file")
	fs.Parse(args)
	c, err := loadCtx(*confPath)
	if err != nil {
		return err
	}
	dc := dropclient.New(c.cfg)
	plan, rev, err := dc.PullState()
	if err != nil {
		return err
	}
	newKey := make([]byte, 32)
	if _, err := rand.Read(newKey); err != nil {
		return err
	}
	c.cfg.EncKey = base64.RawURLEncoding.EncodeToString(newKey)
	// secret first (short fatal window heals with the state commit below)
	if err := c.admin.SetSecret(secretKeyName, []byte(c.cfg.EncKey)); err != nil {
		return err
	}
	if err := dc.WriteStateDirect(plan, rev+1, "rotate key"); err != nil {
		return err
	}
	if err := c.cfg.Save(c.cfg.Path); err != nil {
		return err
	}
	fmt.Printf("✓ key rotated, state re-encrypted at rev %d\n", rev+1)
	fmt.Println("  old links can no longer READ. Everyone needs a fresh link:")
	for id := range c.cfg.Users {
		printUserLink(c.cfg, id)
	}
	fmt.Println("  note: the OLD ciphertext history in git still opens with the old key —\n  run `dropii compact` to squash it away")
	return nil
}

func rotateToken(args []string) error {
	fs := flag.NewFlagSet("rotate token", flag.ExitOnError)
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
	u.Token = randomID(22)
	if err := c.admin.SetSecret(secretTokensName, tokensSecret(c.cfg)); err != nil {
		return err
	}
	if err := c.cfg.Save(c.cfg.Path); err != nil {
		return err
	}
	fmt.Printf("✓ token rotated for %s (%s) — old link can no longer write\n", u.ID, u.Name)
	printUserLink(c.cfg, u.ID)
	return nil
}
