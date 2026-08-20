// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bmmmm/putzii-drop/internal/config"
	"github.com/bmmmm/putzii-drop/internal/dropclient"
	"github.com/bmmmm/putzii-drop/internal/wire"
)

func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	selftest := fs.Bool("selftest", false, "run an end-to-end no-op apply and measure latency")
	confPath := fs.String("config", config.DefaultPath(), "config file")
	fs.Parse(args)

	c, err := loadCtx(*confPath)
	if err != nil {
		return err
	}
	fails := 0
	ok := func(cond bool, format string, a ...any) {
		mark := "✓"
		if !cond {
			mark = "✗"
			fails++
		}
		fmt.Printf("%s %s\n", mark, fmt.Sprintf(format, a...))
	}

	login, err := c.admin.AuthLogin()
	ok(err == nil, "gh auth (%s)", login)

	_, err = c.admin.API("GET", "repos/"+c.cfg.Repo, nil)
	ok(err == nil, "repo %s reachable", c.cfg.Repo)

	_, err = c.admin.API("GET", "repos/"+c.cfg.Repo+"/contents/.github/workflows/apply.yml", nil)
	ok(err == nil, "apply.yml present")

	names, err := c.admin.SecretNames()
	hasKey, hasTokens := false, false
	for _, n := range names {
		if n == secretKeyName {
			hasKey = true
		}
		if n == secretTokensName {
			hasTokens = true
		}
	}
	ok(err == nil && hasKey && hasTokens, "secrets %s + %s set", secretKeyName, secretTokensName)

	pin := c.admin.Variable(varPutziiRef)
	ok(pin != "" && pin == c.cfg.PutziiRef, "PUTZII_REF %s (matches config)", short(pin))
	if mainSHA, err := c.admin.ResolveRef(defaultPutzii, "main"); err == nil && pin != "" {
		if mainSHA == pin {
			fmt.Printf("  pin is current with %s main\n", defaultPutzii)
		} else {
			fmt.Printf("  note: %s main moved to %s — bump via `dropii pin --ref %s` after parity\n", defaultPutzii, short(mainSHA), short(mainSHA))
		}
	}

	// pages freshness: health via CDN vs repo head
	dc := dropclient.New(c.cfg)
	repoHealth, repoErr := dc.Health()
	ok(repoErr == nil, "health.json in repo")
	if pagesHealth, err := fetchPagesHealth(c.cfg.DropBase); err != nil {
		ok(false, "pages serves health.json (%v)", err)
	} else if repoErr == nil {
		lag := repoHealth.Rev - pagesHealth.Rev
		ok(lag >= 0 && lag <= 2, "pages freshness: deployed rev %d, repo rev %d (lag %d)", pagesHealth.Rev, repoHealth.Rev, lag)
	}

	// PAT grant probes (negative tests are the point)
	if c.cfg.PAT == "" {
		ok(false, "PAT configured")
	} else {
		st, _ := c.admin.DispatchProbe("apply.yml", c.cfg.PAT, map[string]string{})
		ok(st == 422 || st == 204, "PAT dispatch probe (HTTP %d)", st)
		st = c.admin.ProbeStatus("GET", "repos/"+c.cfg.Repo+"/actions/secrets", c.cfg.PAT, nil)
		ok(st == 403 || st == 404, "PAT cannot read secrets (HTTP %d)", st)
		if exp := patExpiration(c.cfg.PAT); exp != "" {
			t, err := time.Parse("2006-01-02 15:04:05 MST", exp)
			if err == nil {
				days := int(time.Until(t).Hours() / 24)
				ok(days > 30, "PAT expires %s (%d days)", exp, days)
			} else {
				fmt.Printf("  PAT expiration header: %s\n", exp)
			}
		} else {
			fmt.Println("  PAT has no expiration (good — no rotation ritual)")
		}
	}

	// selfcheck workflow state
	if status, conclusion, _, err := c.admin.LatestRun("selfcheck.yml"); err == nil {
		ok(status != "completed" || conclusion == "success", "last selfcheck: %s/%s", status, conclusion)
	}

	if *selftest {
		if err := runSelftest(c); err != nil {
			ok(false, "selftest: %v", err)
		}
	}

	if fails > 0 {
		return fmt.Errorf("%d check(s) failed", fails)
	}
	fmt.Println("doctor: all green")
	return nil
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func fetchPagesHealth(dropBase string) (*dropclient.Health, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", dropBase+"/health.json", nil)
	req.Header.Set("Cache-Control", "no-store")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var h dropclient.Health
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return nil, err
	}
	return &h, nil
}

// patExpiration reads the expiration header GitHub sends for fine-grained
// PATs on any authenticated call (metadata:read is implicit).
func patExpiration(pat string) string {
	req, _ := http.NewRequest("GET", "https://api.github.com/rate_limit", nil)
	req.Header.Set("Authorization", "Bearer "+pat)
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	return strings.TrimSpace(resp.Header.Get("github-authentication-token-expiration"))
}

// runSelftest dispatches a NO-OP envelope (empty plan, merges nothing) as
// the first user and measures dispatch → tail-confirm latency.
func runSelftest(c *ctx) error {
	u, err := pickUser(c.cfg, "")
	if err != nil {
		return err
	}
	if c.cfg.PlanID == "" {
		return fmt.Errorf("no plan yet — run `dropii plan init` first")
	}
	dc := dropclient.New(c.cfg)
	empty := &wire.Plan{PlanID: c.cfg.PlanID}
	payload, err := dropclient.EnvelopePayload(empty)
	if err != nil {
		return err
	}
	nonce := randomID(8)
	t0 := time.Now()
	if err := dc.Dispatch("envelope", u.ID, u.Token, nonce, payload); err != nil {
		return err
	}
	entry, err := dc.AwaitNonce(nonce, 3*time.Minute)
	if err != nil {
		return err
	}
	applyLatency := time.Since(t0).Round(time.Second)
	// wait for pages to serve the new rev
	var deployLatency time.Duration
	for time.Now().Before(t0.Add(5 * time.Minute)) {
		if ph, err := fetchPagesHealth(c.cfg.DropBase); err == nil && ph.Rev >= entry.Rev {
			deployLatency = time.Since(t0).Round(time.Second)
			break
		}
		time.Sleep(5 * time.Second)
	}
	fmt.Printf("✓ selftest: apply confirmed in %s (rev %d), visible on pages after %s\n", applyLatency, entry.Rev, deployLatency)
	return nil
}
