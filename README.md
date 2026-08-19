# putzii-drop

GitHub-Actions-based sync drop for [putzii](https://github.com/bmmmm/putzii)
— the serverless cleaning-schedule PWA. GitHub Actions acts as a validating
merge relay; the merged plan lives AES-256-GCM-encrypted on the GitHub Pages
site of this repo. No server of our own, no new semantics: the drop's merge
is literally the app's `mergePlans`, loaded unmodified from a pinned putzii
commit — a peer that happens to be always online. Share links (`#p1.`)
remain the full offline/fallback path.

Status: phase 1 (runner + workflows, driven via curl). The `dropii` CLI and
the app integration follow.

## How a write works

1. A client dispatches `apply.yml` (`workflow_dispatch`, fine-grained PAT
   with Actions:write only) with either a gzipped wire envelope or a bare
   `checkin` (the workflow mints the event — a dumb curl/HomeAssistant/
   Shortcut button is semantically correct without any plan knowledge).
2. `runner/apply.mjs` authenticates the token (sha256, timing-safe) BEFORE
   parsing anything, merges via the app's own `mergePlans`, re-encrypts with
   a fresh IV, bumps `rev`, appends to the plaintext audit tail in
   `site/health.json`, commits.
3. `pages.yml` deploys `site/` (workflow_run + 15-min floor). Clients pull
   `site/plans/<planId>.json`, decrypt locally, merge — the same code path
   as opening a share link.

## Threat model (short version)

Wer den persönlichen Link (`#d1.`) hat, kann ALLES lesen — auch die
Vergangenheit — und unbegrenzt schreiben (auditierbar im health-Tail).
Nicht möglich: Repo-Inhalte ändern, Secrets lesen, Workflows/Pin ändern.
Widerruf: Token revoken (Sekunden), Key rotieren (Minuten, neue Links),
PAT revoken (alle Writes sofort). Ehrliche Restrisiken: GitHub sieht den
Klartext im Actions-RAM; die verschlüsselte Historie ist permanent public
(nur `compact` löscht Vergangenheit); der PAT klebt am Kühlschrank-QR.
Household-Trust-Tool, kein Security-Produkt.

## License

GPL-3.0-or-later.
