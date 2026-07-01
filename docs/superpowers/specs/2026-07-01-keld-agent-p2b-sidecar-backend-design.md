# keld-agent P2 (2b) — GLiNER2 sidecar backend + governor — design

**Date:** 2026-07-01
**Status:** Design approved (brainstorm), pre-spec-review
**Branch:** `feat/keld-agent-p2b-sidecar` (off `main`)
**Parent design:** `docs/superpowers/specs/2026-06-30-keld-agent-enrichment-daemon-design.md` (§12 P2)
**Spike decision:** `docs/keld-agent-p2-onnx-decision.md` (NO-GO in-process ONNX → sidecar)
**Builds on:** P1 (`enrich.Model`, deterministic backend, daemon, queue/worker, settings, publisher) + 2a (the `eval` harness).

## 1. Summary

Add a GLiNER2 ML backend behind the existing `enrich.Model` interface via a
**bundled localhost sidecar**, plus an **adaptive host-load governor** that paces
the (expensive) sidecar calls. The **pure-Go deterministic backend stays the
zero-dependency default and permanent fallback**; the sidecar is an **opt-in ML
upgrade**. The daemon is useful the instant it installs (accepting + backlogging
prompts) while the sidecar + model provision in the background.

## 2. Locked decisions (from brainstorming)

- **Packaging:** a **frozen, self-contained sidecar binary** per OS (Python +
  torch + gliner2 bundled), shipped as a keld-agent release artifact. No user
  Python. (The per-OS freeze build + installer wiring is the closing packaging
  concern — see §9 scope; the runtime integration is testable against a dev-run
  sidecar without it.)
- **Provisioning-window lifecycle:** the daemon **always accepts + enqueues**
  prompts immediately; during sidecar/model provisioning the backlog is **held
  for ML** and drained once the sidecar is healthy; **deterministic is the
  fallback only on sidecar failure/timeout** (not mere slowness). The P1 bounded
  queue + drop-sampling floor remains the safety net.
- **Model source:** `fastino/gliner2-large-v1` fetched from **Hugging Face Hub at
  a pinned commit revision, SHA256-verified**, into `~/.keld/models/`.
- **Backend selection:** **auto** (sidecar when healthy, else deterministic) via
  a `ml_backend: auto|off` setting (default `auto`) in `~/.keld/agent-config.json`
  (the P1 settings file). No re-enrichment of already-published results.

## 3. Sidecar (owned by keld-agent)

- A minimal FastAPI app vendored in `keld-cli` under `sidecar/`: endpoints
  `/health`, `/classify`, `/entities`, `/extract` with request/response shapes
  matching the `enrich.Model` contract (reuse the proven normalize/adapter logic
  from `inference-enrichment`, copied — not a runtime dependency on that repo).
- Loads `gliner2-large-v1` from a model dir passed by env; warms up at startup;
  `/health` reports ok only once warm.
- Binds `127.0.0.1` on a port chosen by the daemon (passed via env/arg).

## 4. Go side — `sidecarClient` implementing `enrich.Model`

- New package (e.g. `internal/agent/enrich/sidecar/`) with
  `func New(baseURL string, timeout time.Duration) enrich.Model` — `Classify`,
  `Entities`, `Extract` call the sidecar over `localhost` HTTP and map JSON →
  `Ranked`/`Entity`/`ExtractResult`.
- **Privacy invariant (from P1):** any sensitive-labeled entity is masked
  (`Text` cleared, `Masked` set) before it leaves the client — never trust the
  sidecar to have done it. Sensitivity label set matches `enrich`'s.
- Per-call timeout; errors surface so the worker can fall back to deterministic.

## 5. Daemon lifecycle — supervision + health-gating

- On start (when `ml_backend != off` and a sidecar binary is present), the daemon
  **spawns the sidecar** as a child process on a `127.0.0.1` ephemeral port,
  passing the model dir; **polls `/health`** to a ready state.
- **Backend routing:** worker uses the sidecar `enrich.Model` when the sidecar is
  healthy; otherwise the deterministic `enrich.Model`. Selection is re-evaluated
  as health changes (a health-gated model provider, not a one-time choice).
- **Provisioning window:** until the sidecar is healthy, the worker **holds** the
  backlog (bounded) rather than deterministic-publishing it; once healthy, drain
  through ML. If the sidecar fails to become healthy within a timeout, or
  crash-loops, **release the backlog to deterministic**.
- **Supervision:** restart the sidecar with backoff on crash; cap restarts →
  then deterministic-only. Sidecar is terminated on daemon shutdown (no orphan).
- Reuses P1's graceful-shutdown ordering (stop intake → drain/close queue →
  stop sidecar).

## 6. Model provisioning

- First run (ML enabled, model absent): download `fastino/gliner2-large-v1` at
  the **pinned revision** from HF Hub into `~/.keld/models/gliner2-large-v1/`,
  **verify SHA256** against a pinned manifest before use; atomic move into place
  (download to temp, verify, rename) so a partial download is never loaded.
- Emit progress (for the P3 GUI) and log via the P1 debug log; failures are
  non-fatal — the daemon keeps running on deterministic and retries provisioning
  with backoff.
- The pinned revision + SHA256 manifest live in the repo (constants), so a moved
  or altered upstream artifact is detected, not silently loaded.

## 7. Adaptive governor

- Sample **host CPU utilization** via `gopsutil` (cross-platform); maintain an
  **EWMA**.
- Two knobs, applied to the **ML path only**:
  1. **Sidecar concurrency** — a small cap (default 2), scaled toward 1 under
     sustained high load.
  2. **Admission / sample rate** — under sustained high load, shed or sample new
     enrichment work (graduating the P1 fixed drop-sampling floor into an
     adaptive rate).
- Deterministic enrichment stays cheap and ungoverned. The governor is a pure,
  unit-testable policy (EWMA + thresholds → concurrency + admission decision)
  separate from the sampling/host-reading plumbing.

## 8. Eval gate (reuse 2a)

- The 2a `eval` harness scores the sidecar backend exactly as it scores
  deterministic (`RunModel(sidecarClient, gold)`), behind a build tag / skip when
  the sidecar isn't running, so CI without the sidecar stays green.
- Expand the gold set (8 → ~50–100 labeled prompts) for a meaningful gate;
  the sidecar must beat the deterministic baseline on task_type/domain accuracy
  and not regress `sensitive_recall`.

## 9. Scope / non-goals

- **In scope:** sidecar app, `sidecarClient`, daemon supervision + health-gated
  routing + backlog lifecycle, model provisioning (HF pinned + SHA256), governor,
  `ml_backend` setting, eval-gate-on-sidecar, gold-set expansion.
- **Testable without freezing:** all of the above runs against a **dev-run
  sidecar** (local `python:3.12` / venv). The **per-OS frozen-binary build**
  (PyInstaller/Nuitka + CI matrix) and **install.sh/goreleaser + GUI installer
  wiring** are the closing packaging step — included as the final 2b task but
  **deferrable to P3** if the freeze matrix proves heavy; the runtime is complete
  and shippable-in-principle without it.
- **Non-goals:** GUI installer chrome + signing/notarization (P3); remote
  settings control plane (P4); re-enrichment of past results; new sources (P4).

## 10. Open risks

1. **Freezing torch is large/fragile per-OS.** Mitigation: the runtime is
   decoupled from the freeze (dev sidecar for build/test); the freeze is a
   discrete, deferrable task; artifact size is a known cost (§ decision doc).
2. **Held backlog vs long model download.** Mitigation: single-user prompt rates
   make the provisioning-window backlog small; the P1 bounded queue + drop-sample
   floor bounds pathological cases; deterministic-release on timeout prevents
   indefinite hold.
3. **Sidecar port/process management across OSes.** Mitigation: 127.0.0.1
   ephemeral port, child-process supervision with backoff, terminate-on-shutdown;
   covered by daemon integration tests against a stub sidecar.
4. **Pinned HF revision drift / availability.** Mitigation: pinned revision +
   SHA256 verify (fail closed to deterministic); Keld mirror is the P3/P4
   hardening path.
