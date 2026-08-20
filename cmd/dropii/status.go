// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"flag"
	"fmt"
	"time"

	"github.com/bmmmm/putzii-drop/internal/config"
	"github.com/bmmmm/putzii-drop/internal/dropclient"
)

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	watch := fs.Bool("watch", false, "refresh every 10s")
	confPath := fs.String("config", config.DefaultPath(), "config file")
	fs.Parse(args)
	c, err := loadCtx(*confPath)
	if err != nil {
		return err
	}
	for {
		if err := printStatus(c); err != nil {
			return err
		}
		if !*watch {
			return nil
		}
		time.Sleep(10 * time.Second)
		fmt.Println()
	}
}

func printStatus(c *ctx) error {
	dc := dropclient.New(c.cfg)
	repoHealth, err := dc.Health()
	if err != nil {
		return err
	}
	fmt.Printf("plan %s — rev %d (repo head), last apply %s\n", c.cfg.PlanID, repoHealth.Rev, repoHealth.At)
	if ph, err := fetchPagesHealth(c.cfg.DropBase); err == nil {
		lagNote := "in sync"
		if ph.Rev < repoHealth.Rev {
			lagNote = fmt.Sprintf("deploy lagging %d rev(s)", repoHealth.Rev-ph.Rev)
		}
		fmt.Printf("pages  — rev %d (%s)\n", ph.Rev, lagNote)
	} else {
		fmt.Printf("pages  — unreachable: %v\n", err)
	}
	if exp := patExpiration(c.cfg.PAT); exp != "" {
		fmt.Printf("PAT    — expires %s\n", exp)
	} else if c.cfg.PAT != "" {
		fmt.Println("PAT    — no expiration")
	}
	pin := c.admin.Variable(varPutziiRef)
	if mainSHA, err := c.admin.ResolveRef(defaultPutzii, "main"); err == nil {
		if mainSHA == pin {
			fmt.Printf("pin    — %s (current with %s main)\n", short(pin), defaultPutzii)
		} else {
			fmt.Printf("pin    — %s (%s main is at %s)\n", short(pin), defaultPutzii, short(mainSHA))
		}
	}
	fmt.Printf("users  — %d configured\n", len(c.cfg.Users))
	n := len(repoHealth.Tail)
	if n > 5 {
		n = 5
	}
	if n > 0 {
		fmt.Println("tail:")
		for _, t := range repoHealth.Tail[:n] {
			fmt.Printf("  %s  %-8s rev %-4d %v\n", t.At, t.By, t.Rev, t.Counts)
		}
	}
	return nil
}
