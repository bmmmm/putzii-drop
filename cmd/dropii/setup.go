// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	putziidrop "github.com/bmmmm/putzii-drop"
	"github.com/bmmmm/putzii-drop/internal/config"
	"github.com/bmmmm/putzii-drop/internal/gh"
)

const (
	secretKeyName    = "DROP_KEY_B64"
	secretTokensName = "DROP_TOKENS_SHA256"
	varPutziiRef     = "PUTZII_REF"
	defaultPutzii    = "bmmmm/putzii"
)

// idAlphabet matches the app's ID_ALPHABET (no l/o/0/1).
const idAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

func randomID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	out := make([]byte, n)
	for i, v := range b {
		out[i] = idAlphabet[v&31]
	}
	return string(out)
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func tokensSecret(cfg *config.Config) []byte {
	m := map[string]string{}
	for id, u := range cfg.Users {
		if u.Token != "" {
			m[id] = sha256Hex(u.Token)
		}
	}
	raw, _ := json.Marshal(m)
	return raw
}

func cmdSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	repo := fs.String("repo", "", "drop repo (owner/name)")
	putzii := fs.String("putzii", defaultPutzii, "putzii app repo to pin")
	appBase := fs.String("app-base", "", "app base URL for credential links (default: pages URL of --putzii)")
	confPath := fs.String("config", config.DefaultPath(), "config file")
	fs.Parse(args)

	// load or start a config
	cfg, err := config.Load(*confPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		cfg = config.New()
		cfg.Path = *confPath
	}
	if *repo != "" {
		cfg.Repo = *repo
	}
	if cfg.Repo == "" {
		return fmt.Errorf("no repo configured — pass --repo owner/name")
	}
	client := gh.New(cfg.Repo)

	login, err := client.AuthLogin()
	if err != nil {
		return fmt.Errorf("gh auth: %w (run `gh auth login`)", err)
	}
	step("gh auth ok (%s)", login)

	// 1. repo exists? create + push skeleton
	if _, err := client.API("GET", "repos/"+cfg.Repo, nil); err != nil {
		step("repo %s missing — creating (public)", cfg.Repo)
		if out, eerr := exec.Command("gh", "repo", "create", cfg.Repo, "--public",
			"--description", "putzii sync drop — encrypted state relay (GitHub Actions merge server)").CombinedOutput(); eerr != nil {
			return fmt.Errorf("repo create: %s", strings.TrimSpace(string(out)))
		}
	}
	if err := pushSkeleton(client); err != nil {
		return err
	}

	// 2. pages
	if _, err := client.API("POST", "repos/"+cfg.Repo+"/pages", []byte(`{"build_type":"workflow"}`)); err != nil {
		// 409 = already enabled — fine (idempotent)
		if !strings.Contains(err.Error(), "409") && !strings.Contains(err.Error(), "already") {
			return fmt.Errorf("enable pages: %w", err)
		}
	}
	if cfg.DropBase == "" {
		owner, name, _ := strings.Cut(cfg.Repo, "/")
		cfg.DropBase = fmt.Sprintf("https://%s.github.io/%s", owner, name)
	}
	step("pages ready: %s", cfg.DropBase)

	// 3. pinned putzii ref
	if cfg.PutziiRef == "" {
		sha, err := client.ResolveRef(*putzii, "main")
		if err != nil {
			return fmt.Errorf("resolve %s main: %w", *putzii, err)
		}
		cfg.PutziiRef = sha
	}
	if err := client.SetVariable(varPutziiRef, cfg.PutziiRef); err != nil {
		return err
	}
	step("PUTZII_REF pinned: %s", cfg.PutziiRef[:12])
	if cfg.AppBase == "" {
		if *appBase != "" {
			cfg.AppBase = *appBase
		} else {
			owner, name, _ := strings.Cut(*putzii, "/")
			cfg.AppBase = fmt.Sprintf("https://%s.github.io/%s", owner, name)
		}
	}

	// 4. state key
	if cfg.EncKey == "" {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return err
		}
		cfg.EncKey = base64.RawURLEncoding.EncodeToString(key)
		step("generated fresh AES-256 state key")
	}
	if err := client.SetSecret(secretKeyName, []byte(cfg.EncKey)); err != nil {
		return err
	}

	// 5. token map (empty map is fine — users come via `dropii user add`)
	if err := client.SetSecret(secretTokensName, tokensSecret(cfg)); err != nil {
		return err
	}
	step("secrets set: %s, %s (%d users)", secretKeyName, secretTokensName, len(cfg.Users))

	// 6. THE one manual step: fine-grained PAT (UI-only, by design)
	if cfg.PAT == "" {
		fmt.Printf(`
── manual step: create the fine-grained PAT ────────────────────────────
Open:  https://github.com/settings/personal-access-tokens/new
  Token name:         putzii-drop dispatch
  Expiration:         "No expiration" if offered (personal accounts allow
                      it) — otherwise the longest offered; dropii warns
                      30 days before expiry.
  Repository access:  Only select repositories → %s
  Permissions →  Repository permissions → Actions: Read and write
                 EVERYTHING else: No access
Generate, copy the token.
─────────────────────────────────────────────────────────────────────────
`, cfg.Repo)
		pat, err := readSecretPrompt("Paste PAT (input hidden): ")
		if err != nil {
			return err
		}
		if pat == "" {
			return fmt.Errorf("no PAT provided")
		}
		cfg.PAT = pat
	}

	// save BEFORE the probes — a probe failure must not lose the config
	if err := cfg.Save(*confPath); err != nil {
		return err
	}
	step("config saved: %s (0600)", *confPath)

	// 7. empirical grant probes — turn assumptions into measurements
	if err := grantProbes(client, cfg); err != nil {
		return err
	}

	if cfg.PlanID == "" {
		fmt.Println("\nnext: dropii plan init --file <putzii export> ; then dropii user add --name <name>")
	} else {
		fmt.Println("\nnext: dropii doctor --selftest")
	}
	return nil
}

func step(format string, a ...any) {
	fmt.Printf("✓ "+format+"\n", a...)
}

// pushSkeleton writes any missing skeleton file via the contents API.
func pushSkeleton(client *gh.Client) error {
	files, err := putziidrop.SkeletonFiles()
	if err != nil {
		return err
	}
	pushed := 0
	for _, f := range files {
		if _, err := client.API("GET", "repos/"+client.Repo+"/contents/"+f, nil); err == nil {
			continue // exists
		}
		data, err := putziidrop.Skeleton.ReadFile(f)
		if err != nil {
			return err
		}
		body, _ := json.Marshal(map[string]string{
			"message": "setup: skeleton " + f,
			"content": base64.StdEncoding.EncodeToString(data),
		})
		if _, err := client.API("PUT", "repos/"+client.Repo+"/contents/"+f, body); err != nil {
			return fmt.Errorf("push %s: %w", f, err)
		}
		pushed++
	}
	if pushed > 0 {
		step("skeleton pushed (%d files)", pushed)
	} else {
		step("skeleton complete (%d files present)", len(files))
	}
	return nil
}

// grantProbes measures what the PAT can actually do. Negative tests turn
// assumption into measurement.
func grantProbes(client *gh.Client, cfg *config.Config) error {
	// (a) Actions:write — dispatch with MISSING required inputs: 422 proves
	// the grant without starting a run.
	st, _ := client.DispatchProbe("apply.yml", cfg.PAT, map[string]string{})
	if st == 422 {
		step("probe dispatch: 422 (Actions:write confirmed, no run started)")
	} else if st == 204 {
		step("probe dispatch: 204 (Actions:write confirmed)")
	} else {
		return fmt.Errorf("probe dispatch: HTTP %d — PAT lacks Actions:write on %s (or wrong repo selected)", st, cfg.Repo)
	}
	// (b) contents must be DENIED
	body, _ := json.Marshal(map[string]string{"message": "grant probe", "content": "eA=="})
	st = client.ProbeStatus("PUT", "repos/"+cfg.Repo+"/contents/.grant-probe", cfg.PAT, body)
	if st == 403 || st == 404 {
		step("probe contents write: %d (denied — good)", st)
	} else {
		if st == 201 {
			// the probe landed — clean it up (owner identity) before failing
			if out, err := client.API("GET", "repos/"+cfg.Repo+"/contents/.grant-probe", nil); err == nil {
				var obj struct {
					SHA string `json:"sha"`
				}
				if json.Unmarshal(out, &obj) == nil && obj.SHA != "" {
					del, _ := json.Marshal(map[string]string{"message": "remove grant probe", "sha": obj.SHA})
					_, _ = client.API("DELETE", "repos/"+cfg.Repo+"/contents/.grant-probe", del)
				}
			}
		}
		return fmt.Errorf("probe contents write: HTTP %d — PAT can write repo contents! Recreate it with Actions ONLY", st)
	}
	// (c) secrets must be DENIED
	st = client.ProbeStatus("GET", "repos/"+cfg.Repo+"/actions/secrets", cfg.PAT, nil)
	if st == 403 || st == 404 {
		step("probe secrets read: %d (denied — good)", st)
	} else {
		return fmt.Errorf("probe secrets read: HTTP %d — PAT can read secrets! Recreate it with Actions ONLY", st)
	}
	return nil
}

// readSecretPrompt reads a line without echo (stty shellout — stdlib only).
func readSecretPrompt(prompt string) (string, error) {
	fmt.Print(prompt)
	if err := exec.Command("stty", "-F", "/dev/tty", "-echo").Run(); err != nil {
		// macOS stty uses -f; try both, fall back to visible input from stdin
		_ = exec.Command("stty", "-f", "/dev/tty", "-echo").Run()
	}
	defer func() {
		_ = exec.Command("stty", "-F", "/dev/tty", "echo").Run()
		_ = exec.Command("stty", "-f", "/dev/tty", "echo").Run()
		fmt.Println()
	}()
	var line string
	if _, err := fmt.Fscanln(os.Stdin, &line); err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
