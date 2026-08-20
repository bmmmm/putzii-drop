// SPDX-License-Identifier: GPL-3.0-or-later

// Package dropclient bundles the state operations the CLI commands share:
// fetch/decrypt the current state (via the contents API — fresher than
// Pages), encrypt a plan into a state file, dispatch envelopes, and wait
// for the write confirmation (nonce in the health tail).
package dropclient

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bmmmm/putzii-drop/internal/config"
	"github.com/bmmmm/putzii-drop/internal/dropcrypto"
	"github.com/bmmmm/putzii-drop/internal/gh"
	"github.com/bmmmm/putzii-drop/internal/wire"
)

type Health struct {
	Rev       int64       `json:"rev"`
	At        string      `json:"at"`
	LastRunID string      `json:"lastRunId"`
	Tail      []TailEntry `json:"tail"`
}

type TailEntry struct {
	At     string           `json:"at"`
	By     string           `json:"by"`
	Nonce  string           `json:"nonce"`
	Run    string           `json:"run"`
	Rev    int64            `json:"rev"`
	Counts map[string]int64 `json:"counts"`
}

type Client struct {
	Cfg *config.Config
	GH  *gh.Client
}

func New(cfg *config.Config) *Client {
	return &Client{Cfg: cfg, GH: gh.New(cfg.Repo)}
}

func (c *Client) key() ([]byte, error) {
	key, err := dropcrypto.B64urlDecode(c.Cfg.EncKey)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("enc_key is not a 32-byte base64url key")
	}
	return key, nil
}

// contentsGet fetches a repo file's raw bytes at HEAD (owner identity).
func (c *Client) contentsGet(path string) ([]byte, error) {
	out, err := c.GH.API("GET", "repos/"+c.Cfg.Repo+"/contents/"+path, nil)
	if err != nil {
		return nil, err
	}
	var obj struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.Unmarshal(out, &obj); err != nil {
		return nil, err
	}
	if obj.Encoding != "base64" {
		return nil, fmt.Errorf("unexpected contents encoding %q", obj.Encoding)
	}
	return base64.StdEncoding.DecodeString(strings.ReplaceAll(obj.Content, "\n", ""))
}

// contentsPut creates/updates a repo file (owner identity, direct commit —
// only plan init / rotate key / compact use this).
func (c *Client) contentsPut(path, message string, data []byte) error {
	body := map[string]string{
		"message": message,
		"content": base64.StdEncoding.EncodeToString(data),
	}
	// need the current sha when the file exists
	if out, err := c.GH.API("GET", "repos/"+c.Cfg.Repo+"/contents/"+path, nil); err == nil {
		var obj struct {
			SHA string `json:"sha"`
		}
		if json.Unmarshal(out, &obj) == nil && obj.SHA != "" {
			body["sha"] = obj.SHA
		}
	}
	raw, _ := json.Marshal(body)
	_, err := c.GH.API("PUT", "repos/"+c.Cfg.Repo+"/contents/"+path, raw)
	return err
}

func (c *Client) statePath() string {
	return "site/plans/" + c.Cfg.PlanID + ".json"
}

// StateExists reports whether the plan's state file is present.
func (c *Client) StateExists() bool {
	_, err := c.GH.API("GET", "repos/"+c.Cfg.Repo+"/contents/"+c.statePath(), nil)
	return err == nil
}

// PullState fetches + decrypts the current plan state.
func (c *Client) PullState() (*wire.Plan, int64, error) {
	key, err := c.key()
	if err != nil {
		return nil, 0, err
	}
	raw, err := c.contentsGet(c.statePath())
	if err != nil {
		return nil, 0, fmt.Errorf("no state for plan %s: %w", c.Cfg.PlanID, err)
	}
	rev, _, iv, ct, err := dropcrypto.ParseStateFile(raw)
	if err != nil {
		return nil, 0, err
	}
	plain, err := dropcrypto.Decrypt(key, c.Cfg.PlanID, iv, ct)
	if err != nil {
		return nil, 0, fmt.Errorf("decrypt failed — key mismatch? (%w)", err)
	}
	wireJSON, err := dropcrypto.Gunzip(plain)
	if err != nil {
		return nil, 0, err
	}
	plan, _, err := wire.FromWire(wireJSON)
	if err != nil {
		return nil, 0, err
	}
	return plan, rev, nil
}

// EncodeState encrypts a plan into state-file bytes (fresh IV).
func (c *Client) EncodeState(plan *wire.Plan, rev int64, at time.Time) ([]byte, error) {
	key, err := c.key()
	if err != nil {
		return nil, err
	}
	wireJSON, err := wire.ToWire(plan)
	if err != nil {
		return nil, err
	}
	gz, err := dropcrypto.Gzip(wireJSON)
	if err != nil {
		return nil, err
	}
	iv, ct, err := dropcrypto.Encrypt(key, plan.PlanID, gz)
	if err != nil {
		return nil, err
	}
	return dropcrypto.SerializeStateFile(rev, at.UTC().Format("2006-01-02T15:04:05.000Z"), iv, ct)
}

// WriteStateDirect commits state + a reset health file (init / rotate-key).
func (c *Client) WriteStateDirect(plan *wire.Plan, rev int64, message string) error {
	state, err := c.EncodeState(plan, rev, time.Now())
	if err != nil {
		return err
	}
	if err := c.contentsPut(c.statePath(), message, state); err != nil {
		return err
	}
	health := Health{Rev: rev, At: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), LastRunID: "direct", Tail: []TailEntry{}}
	hraw, _ := json.MarshalIndent(health, "", " ")
	return c.contentsPut("site/health.json", message+" (health reset)", hraw)
}

// Health fetches the audit head from the repo (freshest) …
func (c *Client) Health() (*Health, error) {
	raw, err := c.contentsGet("site/health.json")
	if err != nil {
		return nil, err
	}
	var h Health
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// EnvelopePayload builds the b64url(gzip(wire)) dispatch payload.
func EnvelopePayload(plan *wire.Plan) (string, error) {
	wireJSON, err := wire.ToWire(plan)
	if err != nil {
		return "", err
	}
	gz, err := dropcrypto.Gzip(wireJSON)
	if err != nil {
		return "", err
	}
	payload := dropcrypto.B64urlEncode(gz)
	if len(payload) > 64*1024 {
		return "", fmt.Errorf("envelope %d chars exceeds the 64 kB dispatch cap — run `dropii compact` or use `plan init --force`", len(payload))
	}
	return payload, nil
}

// Dispatch sends an apply dispatch as the given user (PAT identity).
func (c *Client) Dispatch(mode, personID, token, nonce, payload string) error {
	return c.GH.Dispatch("apply.yml", c.Cfg.PAT, map[string]string{
		"mode":     mode,
		"planId":   c.Cfg.PlanID,
		"personId": personID,
		"token":    token,
		"nonce":    nonce,
		"payload":  payload,
	})
}

// AwaitNonce polls the health tail until the nonce shows up (write
// confirmation) or the timeout passes. Returns the confirming entry.
func (c *Client) AwaitNonce(nonce string, timeout time.Duration) (*TailEntry, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		h, err := c.Health()
		if err == nil {
			for i := range h.Tail {
				if h.Tail[i].Nonce == nonce {
					return &h.Tail[i], nil
				}
			}
		}
		time.Sleep(5 * time.Second)
	}
	return nil, fmt.Errorf("nonce %s not confirmed within %s — check `gh run list --repo %s --workflow apply.yml`", nonce, timeout, c.Cfg.Repo)
}
