# keld-agent: local privacy-preserving enrichment daemon — design

**Date:** 2026-06-30
**Status:** Design approved, pre-implementation
**Supersedes/extends:** [2026-06-27-keld-cli-go-migration-design.md](./2026-06-27-keld-cli-go-migration-design.md)

## 1. Summary

Add a local background daemon, `keld-agent`, that classifies each user prompt
(job type + lightweight entities) using a local ONNX model (GLiNER2) and sends
**only the derived labels** to Keld Atlas, joined to existing telemetry by
`prompt_id`. The raw prompt text is read directly off local disk and **never
leaves the machine** — not even over localhost HTTP.

`keld-agent` becomes the **primary installable product** ("Keld"), delivered via
native GUI installers. The existing lean `keld` CLI remains as a power-user /
CI escape hatch with no daemon and no enrichment.

### Driving constraint: privacy

Enrichment runs locally because the raw prompt **must not leave the machine**.
Today both telemetry paths deliberately suppress prompt text
(`log_user_prompt = false`, `logPrompts: false`). The daemon is therefore the
*sole producer* of a new, privacy-safe derived layer — it does not augment data
Atlas already holds.

## 2. Locked decisions

| Topic | Decision |
|-------|----------|
| Driver | Privacy: raw prompt never leaves the machine; only derived labels reach Atlas |
| Runtime | Go daemon + ONNX Runtime (`yalue/onnxruntime_go` or `hugot`), GLiNER2 exported to ONNX |
| Distribution | GUI installers (macOS `.pkg`, Windows MSI) + shell+binary for Linux |
| Service | Per-user autostart (LaunchAgent / systemd `--user` / per-user logon task) |
| Scheduler | Lean: one low-priority worker + bounded queue, behind a `Dispatcher` interface (adaptive later) |
| Tool scope | Claude Code first, provider-agnostic seam, routed through `keld __hook` |
| Correlation | `prompt_id` (present in both hook stdin and OTEL `prompt.id`, Claude Code ≥ v2.1.196) — no hashing |
| Prompt source | Daemon reads raw prompt from `transcript_path` on local disk — text crosses no boundary |
| Primary artifact | `keld-agent` (superset binary) installed via GUI installer; `keld` CLI also installed on `PATH` |

## 3. Why a daemon (vs. CodeBurn's model)

[CodeBurn](https://github.com/getagentseal/codeburn) is the closest reference. It
deliberately makes the **opposite** choice on three axes, and naming the
divergence justifies our added cost:

| Axis | CodeBurn | keld-agent | Why we differ |
|------|----------|------------|---------------|
| Background process | None (on-demand file reads) | Persistent per-user daemon | Real-time per-prompt enrichment + warm model |
| Classification | Deterministic keywords/patterns | Local ML (GLiNER2), deterministic fallback | Richer, model-driven labels |
| Data destination | Local-only | Derived labels → central Atlas | Cross-machine/org telemetry |

Patterns we **borrow** from CodeBurn:

- **Parser-per-provider** (`src/providers/codex.ts`) → our `TranscriptReader`
  interface with one implementation per tool.
- **Reading standardized disk locations** — confirms our prompt source:
  `~/.claude/projects/<sanitized-path>/<session-id>.jsonl`.
- **Deterministic task taxonomy** (13 categories) → seeds our label vocabulary
  and the Phase-1 / fallback classifier.
- **Dedup by message/prompt id** → our `prompt_id` dedup.
- **Menu-bar app + localhost dashboard** → optional future status UX (out of
  scope for v1).

## 4. Artifacts

One repo (`keld-cli`), shared `internal/*` packages, two binaries:

- **`keld-agent`** — the installable product. Superset binary: does everything
  the CLI does (login, configure Claude/Codex/Gemini hooks + OTEL, hook runner)
  **plus** runs the enrichment daemon and embeds ORT. Standard, likely
  org-enforced path.
- **`keld`** — lean, dependency-free CLI via `curl | sh`. Configures telemetry;
  **no daemon, no enrichment** (no ORT). For power users / CI / anyone refusing
  a background service. Also installed on `PATH` by the GUI installer so standard
  users can `keld login` / `keld status` for re-auth and ad-hoc actions.

### Hook entrypoint stays `keld __hook`

Because `keld` is always installed (standard path via the GUI installer,
power-user path via `curl | sh`), every install configures the same hook command:
`keld __hook --source <tool>`. The localhost-forward to the daemon lives in the
shared `internal/hook` package as a **silent-skip branch**: it POSTs the pointer
to `127.0.0.1:<port>` when the daemon answers and does nothing when it does not
(power-user path). No binary-name parameterization is required.

## 5. Components (inside keld-agent)

1. **HTTP ingress** — `POST /enrich` on `127.0.0.1:<port>`. Body is a *pointer*
   only: `{session_id, prompt_id, transcript_path, cwd, source}`. Bound to
   loopback; requires a per-user shared secret (blocks other local processes).
   Responds `202 Accepted` immediately and enqueues.
   **Port + secret discovery:** the daemon writes `~/.keld/agent.json`
   (`{port, secret}`, mode `0600`) on startup; `keld __hook` reads it to locate
   and authenticate to the daemon. Absent/stale file ⇒ the silent-skip branch
   does nothing (power-user path, or daemon not running).
2. **Dispatcher / queue** — bounded FIFO, single worker at low OS priority,
   dedup by `prompt_id`, drop-oldest-and-log backpressure when full. Hidden
   behind a `Dispatcher` interface so an EWMA-of-idle-CPU governor can replace it
   later without touching producers/consumers.
3. **TranscriptReader** (provider-agnostic interface; one impl per tool) — given
   `transcript_path` + `prompt_id`, returns the raw prompt text for that turn.
   Handles write-timing via a short bounded poll/tail for the line matching
   `prompt_id`. Tolerant JSONL parsing; **clean skip** (never crash) on version
   drift; golden-file tests over real fixtures.
4. **Classifier** (interface) — GLiNER2 via ORT, model loaded once at startup
   (warm). Input: prompt text. Output: job-type label(s) + optional entities
   (languages, frameworks). A **deterministic keyword classifier** is the
   permanent fallback (and the Phase-1 stand-in before ORT lands); taxonomy
   seeded from CodeBurn's 13 categories.
5. **Atlas publisher** — `POST /v1/enrichments` with
   `{prompt_id, session_id, source, labels, schema_version, model_version, ts}`.
   Reuses `hook.json` (`endpoint` + `ingest_token`) — no new credential
   plumbing. Bounded disk-backed retry queue when offline; drop after N attempts.
   **Never sends raw prompt text.**
6. **CPU governor** — sets `nice` / idle priority class at process start now;
   interface stub in place for later adaptive scaling.
7. **Lifecycle CLI** — `keld-agent run | install | uninstall | status`.
   `run` is what the service unit invokes (foreground). `install`/`uninstall`
   write the LaunchAgent plist / systemd `--user` unit / Windows logon task.
   `status` reports health, queue depth, model version.

## 6. Data flow

```
user submits prompt in Claude Code
  └─ UserPromptSubmit hook → `keld __hook --source claude_code`
       stdin: {session_id, prompt_id, transcript_path, cwd}
       ├─ (existing) context POST to Atlas
       └─ (new, silent-skip) POST pointer → 127.0.0.1:<port>/enrich
            [fire-and-forget, <500ms timeout, never blocks the tool]
            └─ keld-agent: enqueue → 202
                 worker (low priority):
                   dedup(prompt_id)
                   → TranscriptReader reads raw prompt from disk (poll for prompt_id)
                   → Classifier → labels
                   → Atlas publisher POST {prompt_id, labels, ...}
Atlas joins enrichment to the turn's telemetry by prompt_id (idempotent upsert)
```

## 7. Correlation

Claude Code exposes a stable `prompt_id` in **both** channels:

- Hook stdin: `prompt_id` (UUID; Claude Code ≥ v2.1.196).
- OTEL: `prompt.id` attribute on every event for that turn
  (`user_prompt`, `api_request`, `tool_decision`, `tool_result`,
  `assistant_response`), alongside `session.id`.

The daemon sends `{prompt_id, labels}`; Atlas joins on `prompt_id`. **No
deterministic-hash scheme is needed** — and a hash of the prompt could not work
anyway, because Atlas never receives the raw prompt to recompute it.

For tools without an equivalent shared id, the fallback (future) is
`session_id` + a daemon-maintained turn ordinal. Not needed for Claude Code.

## 8. Privacy invariants (explicit, testable)

- Raw prompt is read from local disk only and is **never transmitted**,
  including over localhost — only a pointer crosses the hook→daemon boundary.
- Only `{prompt_id, labels, schema_version, model_version, ts}` leave the box.
- Daemon binds `127.0.0.1` only; ingress requires a per-user shared secret.
- No prompt text in logs (ids and lengths only).
- A dedicated leak test scans all outbound payloads + log output and asserts
  **zero** prompt-text content.

## 9. Distribution — trusted, native mechanisms

The GUI installer is the standard, likely-enforced path. Its payload:
`keld-agent` + `keld` (on `PATH`) + `libonnxruntime.{dylib,so,dll}` + model +
service definition.

- **macOS** — signed + **notarized `.pkg`** (`pkgbuild` / `productbuild`).
  Postinstall registers the LaunchAgent via `launchctl bootstrap gui/$UID`.
  Notarization required to clear Gatekeeper.
- **Windows** — **MSI via WiX Toolset** (most trusted/native; per-user install
  + logon task via custom action). *Inno Setup* is the lighter fallback if MSI
  authoring proves too heavy. Authenticode signing to clear SmartScreen.
- **Linux** — `curl | sh` dropping `keld-agent` + `keld` + `.so` + model to
  `~/.local`, then `systemctl --user enable --now keld-agent`. `.deb` / `.rpm`
  via **nfpm** later.
- **Build** — extend the existing **GoReleaser** pipeline; `.pkg` / MSI as CI
  steps. Note: Go + ORT is not a pure-static single file — each platform ships
  the binary + ORT shared lib + model; the installer places all three.

### First-run flow (inside keld-agent, no second install step)

```
login (device flow opens browser)
  → fetch onboarding (endpoint, ingest_token, actor)
  → configure tools (Claude/Codex/Gemini hooks + OTEL, incl. UserPromptSubmit hook)
  → register per-user service
  → start daemon
```

## 10. Error handling

- Hook → localhost POST: silent fire-and-forget, `<500ms` timeout; daemon down
  ⇒ that prompt is simply unenriched. Never blocks or fails the host tool.
- Transcript read failure / format drift ⇒ skip enrichment, increment a metric,
  log a warning (no prompt text).
- Classifier failure ⇒ deterministic fallback ⇒ else skip.
- Atlas publish failure ⇒ bounded disk-backed retry; drop after N attempts.
- Daemon crash ⇒ service manager restarts (`KeepAlive` / `Restart=on-failure`);
  `recover()` in the worker mirrors the existing hook's panic safety.

## 11. Atlas contract (defined here, implemented in keld-atlas)

`POST /v1/enrichments`

```json
{
  "prompt_id": "uuid",
  "session_id": "sess_…",
  "source": "claude_code",
  "labels": {
    "job_type": "codegen",
    "job_types": ["codegen", "testing"],
    "entities": { "languages": ["go"], "frameworks": [] }
  },
  "schema_version": "1",
  "model_version": "gliner2-…",
  "ts": "2026-06-30T…Z"
}
```

- Auth: `x-keld-ingest-token` + `x-keld-actor` (same as existing ingest).
- **Idempotent upsert on `prompt_id`.**
- Joins to existing telemetry rows by `prompt_id`.

(Detailed Atlas-side work is a separate spec in the keld-atlas repo; only the
wire contract is fixed here.)

## 12. Phasing (each phase shippable)

- **P1 — de-risk the pipe (headless, internal dogfooding).** `keld-agent`
  skeleton: HTTP ingress, queue/worker, Claude `TranscriptReader`,
  **deterministic** classifier, Atlas publisher; subsume login + setup into
  `keld-agent`; per-user service install on all 3 OS via `keld-agent install`;
  Linux shell distribution. Proves daemon + service + privacy + correlation
  **without** ML/ORT packaging risk.
- **P2 — ML.** GLiNER2 ONNX integration behind the `Classifier` interface +
  model packaging.
- **P3 — GUI installers.** `.pkg` + MSI + signing/notarization. (The
  launch-blocking deliverable, since the installer is the enforced path; P1–P2
  are validated headless first because that is faster.)
- **P4 — later.** Adaptive CPU governor; tray/menu-bar status UI; Codex/Gemini
  `TranscriptReader`s as those tools expose prompt text to a hook.

## 13. Open risks

1. **Transcript format is internal and changes between Claude Code versions.**
   Mitigation: tolerant parsing, golden fixtures per known shape, clean skip on
   drift, a CI canary that flags format changes.
2. **Write-timing** — the new user turn may not be flushed to the `.jsonl` at the
   instant `UserPromptSubmit` fires. Mitigation: short bounded poll/tail for the
   line matching `prompt_id`; give up after a small budget and skip.
3. **`UserPromptSubmit` raw-prompt availability** — current docs confirm
   `prompt_id` in stdin but do **not** confirm raw `prompt` text; design assumes
   it is unavailable and sources text from the transcript instead. If a future
   version reliably provides prompt text in stdin, the `TranscriptReader` becomes
   an optional optimization.
4. **ORT shared-lib packaging** across 3 OS/arch — size (hundreds of MB with the
   model) and signing surface are larger than today's single binary. Contained
   to P2/P3.
5. **Codex/Gemini prompt access** — may never expose raw prompt to a local hook;
   enrichment may stay Claude-Code-only or degrade to session-level for those.

## 14. Testing

- **Unit** — `TranscriptReader` against golden `.jsonl` fixtures (including
  malformed / version-drift); `Classifier` fixtures (prompt → expected labels,
  with tolerance); `Dispatcher` backpressure + dedup; Atlas publisher retry /
  offline.
- **Integration** — spin the daemon on an ephemeral port, POST a pointer, assert
  the Atlas mock receives `{prompt_id, labels}` and **never** raw prompt.
- **Privacy leak test** — assert no prompt-text content appears in any outbound
  payload or log line.
- **Installer smoke** — CI builds `.pkg` / MSI; verify service registration on
  macOS / Windows / Linux runners.
