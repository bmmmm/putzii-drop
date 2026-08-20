// SPDX-License-Identifier: GPL-3.0-or-later

// dropii — admin CLI for a putzii GitHub drop.
package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/bmmmm/putzii-drop/internal/config"
	"github.com/bmmmm/putzii-drop/internal/gh"
)

var version = "dev"

func main() {
	if version == "dev" {
		if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "(devel)" && bi.Main.Version != "" {
			version = bi.Main.Version
		}
	}
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "setup":
		err = cmdSetup(args)
	case "doctor":
		err = cmdDoctor(args)
	case "plan":
		err = cmdPlan(args)
	case "user":
		err = cmdUser(args)
	case "status":
		err = cmdStatus(args)
	case "pin":
		err = cmdPin(args)
	case "rotate":
		err = cmdRotate(args)
	case "compact":
		err = cmdCompact(args)
	case "qr":
		err = cmdQr(args)
	case "button":
		err = cmdButton(args)
	case "config":
		err = cmdConfig(args)
	case "version", "--version", "-v":
		fmt.Println("dropii", version)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "dropii:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `dropii — putzii GitHub-drop admin

usage: dropii <command> [flags]

  setup     create/complete the drop repo (idempotent; ONE manual PAT step)
  doctor    health checks incl. PAT grant probes; --selftest for E2E latency
  plan      init | pull | push — move plan state (init/rotate-key are the
            ONLY direct commits; everything else goes through apply.yml)
  user      add | list | link | revoke — manage write access
  status    drop freshness, audit tail, PAT expiry, pin parity [--watch]
  pin       --ref <sha|branch> — bump the pinned putzii commit (gated on selfcheck)
  rotate    pat | key | token — credential rotation
  compact   --keep-days N — drop old events + squash ciphertext history
  qr        print/export QR codes (--areas | --user | --sheet)
  button    new --kind curl|ha|shortcut|http — check-in button snippets
  config    path | edit | template
`)
}

// ctx bundles what most commands need.
type ctx struct {
	cfg   *config.Config
	admin *gh.Client
}

func loadCtx(confPath string) (*ctx, error) {
	if confPath == "" {
		confPath = config.DefaultPath()
	}
	cfg, err := config.Load(confPath)
	if err != nil {
		return nil, fmt.Errorf("config %s: %w (run `dropii setup` or `dropii config template`)", confPath, err)
	}
	if cfg.Repo == "" {
		return nil, fmt.Errorf("config %s has no repo", confPath)
	}
	return &ctx{cfg: cfg, admin: gh.New(cfg.Repo)}, nil
}
