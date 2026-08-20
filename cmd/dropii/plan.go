// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/bmmmm/putzii-drop/internal/config"
	"github.com/bmmmm/putzii-drop/internal/dropclient"
	"github.com/bmmmm/putzii-drop/internal/wire"
)

func cmdPlan(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: dropii plan init|pull|push [flags]")
	}
	switch args[0] {
	case "init":
		return planInit(args[1:])
	case "pull":
		return planPull(args[1:])
	case "push":
		return planPush(args[1:])
	default:
		return fmt.Errorf("unknown plan subcommand %q", args[0])
	}
}

// planInit: the ONLY way a plan enters the drop outside apply.yml (direct
// commit). planId is taken from the export — that is what makes the
// migration lossless.
func planInit(args []string) error {
	fs := flag.NewFlagSet("plan init", flag.ExitOnError)
	file := fs.String("file", "", "putzii export file (putzii-plan JSON)")
	force := fs.Bool("force", false, "overwrite an existing state file")
	confPath := fs.String("config", config.DefaultPath(), "config file")
	fs.Parse(args)
	if *file == "" {
		return fmt.Errorf("--file <export.json> required")
	}
	c, err := loadCtx(*confPath)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	plan, err := wire.ParseFile(data)
	if err != nil {
		return fmt.Errorf("parse export: %w", err)
	}
	c.cfg.PlanID = plan.PlanID
	dc := dropclient.New(c.cfg)
	if dc.StateExists() && !*force {
		return fmt.Errorf("state for plan %s already exists — merge via `plan push`, overwrite via --force", plan.PlanID)
	}
	t0 := time.Now()
	if err := dc.WriteStateDirect(plan, 1, "plan init"); err != nil {
		return err
	}
	if err := c.cfg.Save(c.cfg.Path); err != nil {
		return err
	}
	fmt.Printf("✓ plan %s initialized (rev 1, %d events, %d areas, %d people, %d weeks) in %s\n",
		plan.PlanID, len(plan.Events), len(plan.Areas), len(plan.People), len(plan.Weeks), time.Since(t0).Round(time.Millisecond))
	fmt.Println("  pages deploy follows automatically (site/** push trigger)")
	return nil
}

func planPull(args []string) error {
	fs := flag.NewFlagSet("plan pull", flag.ExitOnError)
	out := fs.String("out", "", "write export file (default: stdout summary only)")
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
	fmt.Printf("✓ plan %s rev %d: %d events, %d areas, %d people, %d weeks\n",
		plan.PlanID, rev, len(plan.Events), len(plan.Areas), len(plan.People), len(plan.Weeks))
	if *out != "" {
		data, err := wire.SerializeFile(plan)
		if err != nil {
			return err
		}
		if err := os.WriteFile(*out, data, 0o644); err != nil {
			return err
		}
		fmt.Printf("✓ export written: %s\n", *out)
	}
	return nil
}

// planPush merges a local export INTO the drop — through apply.yml like
// every other mutation (one write path, one merge, one audit).
func planPush(args []string) error {
	fs := flag.NewFlagSet("plan push", flag.ExitOnError)
	file := fs.String("file", "", "putzii export file to merge in")
	userID := fs.String("user", "", "dispatch as this user (default: first configured)")
	confPath := fs.String("config", config.DefaultPath(), "config file")
	fs.Parse(args)
	if *file == "" {
		return fmt.Errorf("--file <export.json> required")
	}
	c, err := loadCtx(*confPath)
	if err != nil {
		return err
	}
	u, err := pickUser(c.cfg, *userID)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	plan, err := wire.ParseFile(data)
	if err != nil {
		return fmt.Errorf("parse export: %w", err)
	}
	if plan.PlanID != c.cfg.PlanID {
		return fmt.Errorf("export is for plan %s, drop holds %s", plan.PlanID, c.cfg.PlanID)
	}
	payload, err := dropclient.EnvelopePayload(plan)
	if err != nil {
		return err
	}
	dc := dropclient.New(c.cfg)
	nonce := randomID(8)
	t0 := time.Now()
	if err := dc.Dispatch("envelope", u.ID, u.Token, nonce, payload); err != nil {
		return err
	}
	fmt.Printf("dispatched as %s (nonce %s, %d chars) — waiting for confirmation…\n", u.ID, nonce, len(payload))
	entry, err := dc.AwaitNonce(nonce, 3*time.Minute)
	if err != nil {
		return err
	}
	fmt.Printf("✓ applied: rev %d, counts %v (end-to-end %s)\n", entry.Rev, entry.Counts, time.Since(t0).Round(time.Second))
	return nil
}

func pickUser(cfg *config.Config, id string) (*config.User, error) {
	if id != "" {
		u := cfg.Users[id]
		if u == nil {
			return nil, fmt.Errorf("unknown user %q", id)
		}
		if u.Token == "" {
			return nil, fmt.Errorf("user %q has no token", id)
		}
		return u, nil
	}
	for _, u := range cfg.Users {
		if u.Token != "" {
			return u, nil
		}
	}
	return nil, fmt.Errorf("no users configured — run `dropii user add --name <name>`")
}
