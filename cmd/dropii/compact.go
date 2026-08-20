// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bmmmm/putzii-drop/internal/config"
	"github.com/bmmmm/putzii-drop/internal/dropclient"
	"github.com/bmmmm/putzii-drop/internal/wire"
)

// cmdCompact drops old events from the state AND squashes the git history
// (the public ciphertext history is the privacy leak — a squash is the only
// way to delete the past). Quarterly ritual, always interactive.
func cmdCompact(args []string) error {
	fs := flag.NewFlagSet("compact", flag.ExitOnError)
	keepDays := fs.Int("keep-days", 90, "keep events younger than this")
	yes := fs.Bool("yes", false, "skip the confirmation prompt (still prints the plan)")
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

	cutoffMs := float64(time.Now().AddDate(0, 0, -*keepDays).UnixMilli())
	kept, dropped := compactEvents(plan, cutoffMs)
	fmt.Printf("compact plan %s: keep %d events, drop %d (older than %d days; per-area due anchors kept)\n",
		plan.PlanID, len(kept), dropped, *keepDays)
	fmt.Println("then: squash the ENTIRE git history of the drop repo into one commit (force-update main)")
	if !*yes {
		fmt.Print("proceed? [y/N] ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.TrimSpace(strings.ToLower(line)) != "y" {
			return fmt.Errorf("aborted")
		}
	}
	plan.Events = kept
	if err := dc.WriteStateDirect(plan, rev+1, fmt.Sprintf("compact: keep %d days", *keepDays)); err != nil {
		return err
	}
	fmt.Printf("✓ state compacted at rev %d\n", rev+1)

	// squash: new parentless commit with the CURRENT tree, force-set main
	repo := c.cfg.Repo
	out, err := c.admin.API("GET", "repos/"+repo+"/git/ref/heads/main", nil)
	if err != nil {
		return err
	}
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := json.Unmarshal(out, &ref); err != nil {
		return err
	}
	out, err = c.admin.API("GET", "repos/"+repo+"/git/commits/"+ref.Object.SHA, nil)
	if err != nil {
		return err
	}
	var commit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := json.Unmarshal(out, &commit); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{
		"message": fmt.Sprintf("compact: squashed history (keep-days %d)", *keepDays),
		"tree":    commit.Tree.SHA,
		"parents": []string{},
	})
	out, err = c.admin.API("POST", "repos/"+repo+"/git/commits", body)
	if err != nil {
		return err
	}
	var newCommit struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(out, &newCommit); err != nil {
		return err
	}
	body, _ = json.Marshal(map[string]any{"sha": newCommit.SHA, "force": true})
	if _, err := c.admin.API("PATCH", "repos/"+repo+"/git/refs/heads/main", body); err != nil {
		return err
	}
	fmt.Printf("✓ history squashed — main is now single commit %s\n", short(newCommit.SHA))
	fmt.Println("  note: GitHub may retain old objects for a while; for a hard cut contact GitHub support or rotate the key too")
	return nil
}

// compactEvents keeps events younger than cutoff plus the NEWEST event of
// every live area (its due-date anchor must survive, like trimEvents).
func compactEvents(plan *wire.Plan, cutoffMs float64) (kept []wire.Event, dropped int) {
	liveAreas := map[string]bool{}
	for _, a := range plan.Areas {
		if a.DeletedAt == 0 {
			liveAreas[a.ID] = true
		}
	}
	newestPerArea := map[string]int{}
	for i, e := range plan.Events {
		if !liveAreas[e.AreaID] {
			continue
		}
		if j, ok := newestPerArea[e.AreaID]; !ok || e.TsMs > plan.Events[j].TsMs {
			newestPerArea[e.AreaID] = i
		}
	}
	anchor := map[int]bool{}
	for _, i := range newestPerArea {
		anchor[i] = true
	}
	for i, e := range plan.Events {
		if e.TsMs >= cutoffMs || anchor[i] {
			kept = append(kept, e)
		} else {
			dropped++
		}
	}
	return kept, dropped
}
