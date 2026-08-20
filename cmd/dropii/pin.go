// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"flag"
	"fmt"
	"time"

	"github.com/bmmmm/putzii-drop/internal/config"
)

// cmdPin bumps PUTZII_REF — gated on a green selfcheck run against the new
// ref; on red the old pin is restored.
func cmdPin(args []string) error {
	fs := flag.NewFlagSet("pin", flag.ExitOnError)
	ref := fs.String("ref", "", "putzii ref (sha or branch) to pin")
	confPath := fs.String("config", config.DefaultPath(), "config file")
	fs.Parse(args)
	if *ref == "" {
		return fmt.Errorf("--ref required")
	}
	c, err := loadCtx(*confPath)
	if err != nil {
		return err
	}
	sha, err := c.admin.ResolveRef(defaultPutzii, *ref)
	if err != nil {
		return err
	}
	old := c.admin.Variable(varPutziiRef)
	if old == sha {
		fmt.Printf("already pinned to %s\n", short(sha))
		return nil
	}
	fmt.Printf("pin %s → %s — running parity selfcheck…\n", short(old), short(sha))
	if err := c.admin.SetVariable(varPutziiRef, sha); err != nil {
		return err
	}
	if err := c.admin.Dispatch("selfcheck.yml", "", nil); err != nil {
		restore(c, old)
		return err
	}
	deadline := time.Now().Add(5 * time.Minute)
	time.Sleep(10 * time.Second)
	for time.Now().Before(deadline) {
		status, conclusion, id, err := c.admin.LatestRun("selfcheck.yml")
		if err == nil && status == "completed" {
			if conclusion == "success" {
				c.cfg.PutziiRef = sha
				if err := c.cfg.Save(c.cfg.Path); err != nil {
					return err
				}
				fmt.Printf("✓ pinned %s (selfcheck run %d green)\n", short(sha), id)
				return nil
			}
			restore(c, old)
			return fmt.Errorf("selfcheck %s on new ref (run %d) — pin restored to %s", conclusion, id, short(old))
		}
		time.Sleep(10 * time.Second)
	}
	restore(c, old)
	return fmt.Errorf("selfcheck did not finish in time — pin restored to %s", short(old))
}

func restore(c *ctx, old string) {
	if old != "" {
		_ = c.admin.SetVariable(varPutziiRef, old)
	}
}
