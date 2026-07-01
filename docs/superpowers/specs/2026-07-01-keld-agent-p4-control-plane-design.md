# keld-agent P4 (control-plane) — org remote settings — design

**Date:** 2026-07-01
**Status:** Design approved (brainstorm), pre-spec-review
**Branch:** `feat/keld-agent-p4-control-plane` (off `main`, keld-cli)
**Parent design:** `docs/superpowers/specs/2026-06-30-keld-agent-enrichment-daemon-design.md` (§12 P4)
**Spans:** keld-cli (daemon client) + keld-atlas (server). Client-first.

## 1. Summary

An admin sets org-level daemon settings in keld-atlas; every running keld-agent
polls a per-org settings document and applies it on the next fetch — the "org
control plane" the local `~/.keld/agent-config.json` was designed to become.
Starts with `include_entity_text`; the schema is extensible to future keys.

## 2. Governance model (locked)

**Remote org settings override local**, per key: for any key the org sets, the
org value wins; the local file supplies only keys the org leaves unset. Org
policy is authoritative (an admin can force `include_entity_text` off org-wide).
No per-key "enforced" flag yet — that is a future evolution; today the whole
remote doc is authoritative for the keys it contains.

## 3. Client — keld-agent (build first; self-contained + stub-testable)

- **Live settings holder.** Today `include_entity_text` is read once at startup
  and passed as a bool to `Worker`. Replace that with a concurrency-safe live
  value (mutex or atomic) the worker reads per job, so a poll update takes effect
  without a restart. `Worker`/`process`/`publish.Build` read the *current* value.
- **Poller.** On startup and every **5 minutes** (constant; `KELD_SETTINGS_POLL`
  env may override for tests), GET the org settings and merge over local
  (remote-wins per key present). Non-fatal on any error (network, 404 on an older
  Atlas, decode) — keep the last-known effective settings; retry next tick.
- **Effective settings = local file as the base, remote doc overlaid.** Load the
  local file once at startup for the base; each poll overlays the remote keys.
- **Endpoint.** Derive from the configured ingest endpoint (as `enrichEndpoint`
  does) → `<base>/v1/agent-settings`; authenticate with the existing ingest token
  (the daemon already holds it). Read-only GET.
- **Client shape.** A small `settings` client (mirrors the sidecar client
  pattern) tested against an `httptest` stub: returns the parsed remote doc;
  errors surface so the poller can keep last-known. Unknown keys in the doc are
  ignored (client applies only keys it knows).

## 4. Server — keld-atlas (build second; coordinate with the shared tree)

- **Model + migration.** A per-org settings row (`org_agent_settings`:
  `org_id` FK, `include_entity_text bool`, `updated_at`), org-scoped.
- **Daemon endpoint.** `GET /v1/agent-settings` — token→org via the existing
  ingest-token resolver (mirror `enrichments`/`otel`); returns the org's settings
  JSON, or documented defaults when unset. Org-scoped (tenant isolation).
- **Admin surface.** An admin-authed API to read/set the org's settings
  (`current_org`, `require_admin`), plus a **minimal toggle** on the existing
  admin **Settings** page. UI kept small; API-first is acceptable if the shared
  atlas tree makes the page risky to touch concurrently.
- **Coordination.** keld-atlas `main` carries another session's unpushed
  onboarding-wizard work; branch cleanly off atlas `main` and avoid its files.
  Hold the server side if that session is still editing the areas we touch
  (models, routers, admin settings page).

## 5. Data shape

```json
{ "include_entity_text": false }
```
Extensible: future keys (e.g. `ml_backend`) ride the same doc + live holder. The
client applies only keys it recognizes; unknown keys are ignored (forward-compat).

## 6. Sequencing

1. **keld-agent client** — live settings holder + poller + settings client +
   tests (stub). No Atlas dependency; ships independently (until the endpoint
   exists it simply keeps local settings — the 404 path is non-fatal).
2. **keld-atlas server** — model + migration + `GET /v1/agent-settings` +
   admin set (API + minimal UI). Then end-to-end wiring.

## 7. Scope / non-goals

- Only `include_entity_text` initially (the same mechanism carries more keys
  later). No push/websockets — **poll only** (firewall-friendly, simple). No
  per-key enforced flag (remote-wins is the whole doc). No device/user targeting
  — **org-wide** only. No change to the enrichment runtime beyond making the
  setting live.

## 8. Open risks

1. **Live-apply concurrency** — the poller writes the setting while the worker
   reads it. Mitigation: a mutex/atomic-guarded holder; a `-race` test.
2. **Older Atlas without the endpoint** — client must treat 404/errors as "no
   remote settings" and keep local (non-fatal), so the daemon never breaks when
   the server side lags the client rollout.
3. **Eventual consistency** — a change takes up to one poll interval (5 min) to
   apply. Acceptable for settings governance; documented.
4. **Shared keld-atlas tree** — concurrent edits with the other session; mitigate
   by branching off atlas `main` and scoping to new files (model/migration/router)
   plus a minimal, additive settings-page change.
