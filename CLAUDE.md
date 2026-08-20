# putzii-drop — Agent Guide

GitHub-Actions-based merge server for putzii (the serverless cleaning-schedule
PWA). Validating relay, not a source of truth: its merge IS the app's
`mergePlans`, loaded UNMODIFIED from `bmmmm/putzii` — another peer that happens
to be always online. `#p1.` links remain the full offline/fallback path.

## Identity — deliberate exception

This repo is **GitHub-primary and public** (`github.com/bmmmm/putzii-drop`),
NOT Forgejo-first like every other repo: GitHub Actions must commit here
(encrypted state + health tail), and Actions never commit on a Forgejo mirror.
Single remote `origin` = GitHub. Decided with the user 2026-08-20.

## Verified facts (lab-measured 2026-08-20, repo putzii-drop-lab)

- **V1 Concurrency**: a concurrency group holds exactly ONE pending slot —
  4 rapid dispatches → runs 2+3 CANCELLED, only first+last survived. Never
  use concurrency groups for mutations; apply serializes via in-job retry
  loop. For deploys `cancel-in-progress: true` is correct (superseding).
- **V3 Input size**: dispatch inputs of 65,006 total chars accepted, 65,535
  → HTTP 422 "inputs are too large" (cliff slightly BELOW the documented
  65,535). All payloads 8–65 kB arrived byte-lossless (sha256-verified in
  run logs). Envelope (3–11 kB) fits comfortably — no chunking.
- **V4 workflow_run**: 10/10 followers fired, apply-complete→follower-start
  ~1–2 s, dispatch→follower-done end-to-end 21–28 s. Two-workflow design
  (apply commits, pages deploys via workflow_run) confirmed.
- **V7 Node↔Browser parity**: helpers/model/share.js run UNMODIFIED in a
  node:vm context (bare `window` + Node web globals). Wire round-trip,
  merge idempotence/commutativity, gzip byte-parity, DST day numbers: all
  green under TZ=Europe/Berlin AND TZ=UTC.
  - **TZ divergence is real**: Sun 22:30 UTC = Mon 00:30 CEST → isoWeekKey
    says 2026-W34 (UTC) vs 2026-W35 (Berlin). apply.yml MUST pin
    `env: TZ: Europe/Berlin` at job level.
  - **Cross-realm instanceof trap**: `isoWeekKey` guards `d instanceof
    Date`; a Date built in the Node realm fails that check inside the vm
    realm. loadapp.mjs therefore passes Node's `Date` constructor INTO the
    sandbox — do not remove it.
  - 2026 has 53 ISO weeks (starts on a Thursday) — "2026-W53" is valid.
- **V5 Pages caching**: fastly edge, `cache-control: max-age=600` on the
  headers — but a deploy PURGES the edge (plain GET fresh seconds after
  deploy despite 9 min residual TTL), entries live only ~seconds–minutes
  per cache node, and **query-param cache-busting is INEFFECTIVE** (unique
  `?b=` still answered `x-cache: HIT` from the same object — the query
  string is not part of the cache key). Consequences: no immutable
  rev-paths needed, the app's read path uses `cache:"no-store"` (browser
  cache only) and NO bust param; worst-case staleness is minutes, the
  "3 stale pulls in a row → hint" logic covers it.
- V2/V6 PAT grant probes: MEASURED 2026-08-20 on the prod repo — dispatch
  probe with missing required inputs → 422 (proves Actions:write without
  starting a run), contents write → 403, secrets read → 403. PAT creation
  stays UI-only (the one manual setup step). The created PAT carries a
  1-year expiry (2027-08-17); doctor warns below 30 days. Prod selftest:
  apply confirmed 13 s, visible on pages 33 s.

## Invariants — do not break

1. AUTH FIRST in apply.mjs: sha256(token) timing-safe against the token-map
   secret BEFORE parsing a single attacker-controlled payload byte. Auth or
   validation failure = exit 2 = fatal, NEVER retried.
2. The merge is `mergePlans` from putzii, loaded verbatim via loadapp.mjs
   from a second checkout pinned to a COMMIT SHA (`vars.PUTZII_REF`). Bump
   only via `dropii pin` after a green parity test. Divergence is always
   fixed in the app, never in the runner. A stale pin FAILS LOUD: envelopes
   (or stored state) with more wire slots than the pinned app emits →
   fatal `wire-unknown-slots`/`state-unknown-slots`, never a lossy strip.
   Drift alarm = daily `driftcheck.yml` against putzii@main — kept as a
   SEPARATE workflow because `dropii pin` gates on selfcheck's conclusion.
3. State file `site/plans/<planId>.json`: AES-256-GCM, fresh 12-byte IV per
   write, AAD = planId+"|1". `rev`/`at` stay plaintext (freshness check
   without decrypt). Plaintext = gzip(UNCAPPED wire envelope) → always runs
   through `planFromWire`, the same sanitizer as hostile links.
4. Logs carry COUNTS ONLY — no names, no payloads, no plaintext. The
   self-check has a no-plaintext-in-log gate that can go red.
5. Caps (violation discards the WHOLE dispatch): payload ≤ 64 kB,
   gunzip ≤ 512 kB, events ≤ 500, areas/people ≤ 200, weeks ≤ 400.
6. Replay guard: nonce already in health tail → green no-op, no commit.
   Rate guard: >60 pushes/h → fatal.
7. checkin mode mints via mint.mjs: deviceKey `"g"+personId`, seq numeric
   (cmpEventId semantics), ts minute-quantized, 10-min idempotency window,
   unknown areaId rejected.
8. Attribution is auditable, not enforced: events are accepted regardless
   of the pusher's personId ("Timo scans, picks Sina" is supported); the
   health tail stamps the AUTHENTICATED pusher.
9. TZ=Europe/Berlin pinned in apply.yml (see V7 above).

## Layout

```
site/                 Pages publish root (keep small — on the deploy path)
  health.json         PLAINTEXT {rev, at, lastRunId, tail:[…50]}
  plans/<planId>.json encrypted state
runner/               apply.mjs loadapp.mjs crypto.mjs mint.mjs test.mjs
.github/workflows/    apply.yml pages.yml selfcheck.yml driftcheck.yml ci.yml
cmd/dropii/ internal/ Go CLI (phase 2)
```

## Dev loop

- `node runner/test.mjs` — full suite, runs WITHOUT network (path to a
  putzii checkout via PUTZII_DIR, defaults to ../putzii).
- Secrets: `DROP_KEY_B64` (32-byte key, base64url), `DROP_TOKENS_SHA256`
  (JSON map personId → sha256hex of token). Set via `gh secret set` from
  stdin only.
- Repo variable `PUTZII_REF` = commit SHA of bmmmm/putzii used by loadapp.
