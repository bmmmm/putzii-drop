// SPDX-License-Identifier: GPL-3.0-or-later

// Package gh shells out to the GitHub CLI. Two identities:
//   - the OWNER's gh keyring auth for admin ops (secrets, variables, repo)
//   - the fine-grained PAT (Actions:write only) for dispatches — passed via
//     GH_TOKEN env, NEVER argv. Secrets always travel via stdin.
package gh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Runner abstracts exec for tests.
type Runner func(env []string, stdin []byte, args ...string) (stdout []byte, stderr []byte, err error)

func execRunner(env []string, stdin []byte, args ...string) ([]byte, []byte, error) {
	cmd := exec.Command("gh", args...)
	cmd.Env = append(os.Environ(), env...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	return out.Bytes(), errb.Bytes(), err
}

type Client struct {
	Repo string // owner/name
	Run  Runner
}

func New(repo string) *Client {
	return &Client{Repo: repo, Run: execRunner}
}

func (c *Client) run(env []string, stdin []byte, args ...string) ([]byte, error) {
	out, errb, err := c.Run(env, stdin, args...)
	if err != nil {
		msg := strings.TrimSpace(string(errb))
		if msg == "" {
			msg = err.Error()
		}
		return out, fmt.Errorf("gh %s: %s", args[0], msg)
	}
	return out, nil
}

// AuthLogin returns the authenticated login of the keyring identity.
func (c *Client) AuthLogin() (string, error) {
	out, err := c.run(nil, nil, "api", "user", "--jq", ".login")
	return strings.TrimSpace(string(out)), err
}

// Dispatch triggers a workflow_dispatch. token != "" switches to PAT
// identity (env only). Inputs go through --raw-field via stdin-safe args —
// gh sends them as JSON body; values never hit a shell.
func (c *Client) Dispatch(workflow string, token string, inputs map[string]string) error {
	args := []string{"api", "-X", "POST",
		fmt.Sprintf("repos/%s/actions/workflows/%s/dispatches", c.Repo, workflow)}
	args = append(args, "-f", "ref=main")
	for k, v := range inputs {
		args = append(args, "-f", fmt.Sprintf("inputs[%s]=%s", k, v))
	}
	var env []string
	if token != "" {
		env = append(env, "GH_TOKEN="+token)
	}
	_, err := c.run(env, nil, args...)
	return err
}

// DispatchProbe returns the HTTP status of a dispatch attempt under the
// given token — used by the setup grant probes (expect 204 / 403).
func (c *Client) DispatchProbe(workflow string, token string, inputs map[string]string) (int, error) {
	body, _ := json.Marshal(map[string]any{"ref": "main", "inputs": inputs})
	args := []string{"api", "-X", "POST", "--silent", "--include",
		fmt.Sprintf("repos/%s/actions/workflows/%s/dispatches", c.Repo, workflow),
		"--input", "-"}
	var env []string
	if token != "" {
		env = append(env, "GH_TOKEN="+token)
	}
	out, errb, _ := c.Run(env, body, args...)
	return parseStatus(out, errb), nil
}

// ProbeStatus performs METHOD path under token and reports the HTTP status
// (negative grant tests: PUT contents → 403, GET secrets → 403).
func (c *Client) ProbeStatus(method, path string, token string, body []byte) int {
	args := []string{"api", "-X", method, "--silent", "--include", path}
	if body != nil {
		args = append(args, "--input", "-")
	}
	var env []string
	if token != "" {
		env = append(env, "GH_TOKEN="+token)
	}
	out, errb, _ := c.Run(env, body, args...)
	return parseStatus(out, errb)
}

func parseStatus(out, errb []byte) int {
	// gh --include prints "HTTP/2.0 204 No Content" as first line of stdout;
	// on errors gh prints "gh: HTTP 403: ..." style to stderr. Check both.
	for _, b := range [][]byte{out, errb} {
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			var code int
			if n, _ := fmt.Sscanf(line, "HTTP/2.0 %d", &code); n == 1 {
				return code
			}
			if n, _ := fmt.Sscanf(line, "HTTP/1.1 %d", &code); n == 1 {
				return code
			}
			if idx := strings.Index(line, "HTTP "); idx >= 0 {
				if n, _ := fmt.Sscanf(line[idx:], "HTTP %d", &code); n == 1 {
					return code
				}
			}
		}
	}
	return 0
}

// SetSecret writes an Actions secret from stdin (never argv).
func (c *Client) SetSecret(name string, value []byte) error {
	_, err := c.run(nil, value, "secret", "set", name, "--repo", c.Repo)
	return err
}

// SetVariable writes an Actions variable.
func (c *Client) SetVariable(name, value string) error {
	_, err := c.run(nil, []byte(value), "variable", "set", name, "--repo", c.Repo)
	return err
}

// SecretNames lists which secrets exist (values are write-only).
func (c *Client) SecretNames() ([]string, error) {
	out, err := c.run(nil, nil, "secret", "list", "--repo", c.Repo, "--json", "name", "--jq", ".[].name")
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

// Variable reads an Actions variable value ("" if unset).
func (c *Client) Variable(name string) string {
	out, err := c.run(nil, nil, "variable", "get", name, "--repo", c.Repo, "--json", "value", "--jq", ".value")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// API is the generic escape hatch (owner identity).
func (c *Client) API(method, path string, body []byte) ([]byte, error) {
	args := []string{"api", "-X", method, path}
	if body != nil {
		args = append(args, "--input", "-")
	}
	return c.run(nil, body, args...)
}

// LatestRun returns status/conclusion/id of the newest run of a workflow.
func (c *Client) LatestRun(workflow string) (status, conclusion string, id int64, err error) {
	out, err := c.run(nil, nil, "run", "list", "--repo", c.Repo, "--workflow", workflow,
		"--limit", "1", "--json", "status,conclusion,databaseId")
	if err != nil {
		return "", "", 0, err
	}
	var rows []struct {
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		DatabaseID int64  `json:"databaseId"`
	}
	if err := json.Unmarshal(out, &rows); err != nil || len(rows) == 0 {
		return "", "", 0, fmt.Errorf("no runs for %s", workflow)
	}
	return rows[0].Status, rows[0].Conclusion, rows[0].DatabaseID, nil
}

// ResolveRef resolves a ref of a repo to a commit SHA.
func (c *Client) ResolveRef(repo, ref string) (string, error) {
	out, err := c.run(nil, nil, "api", fmt.Sprintf("repos/%s/commits/%s", repo, ref), "--jq", ".sha")
	return strings.TrimSpace(string(out)), err
}
