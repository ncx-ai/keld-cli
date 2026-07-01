# keld-agent P2 (2a) — GLiNER2 backend spike + eval gate — design

**Date:** 2026-07-01
**Status:** Design approved, pre-implementation
**Branch:** `feat/keld-agent-p2-spike` (off `main`)
**Parent design:** `docs/superpowers/specs/2026-06-30-keld-agent-enrichment-daemon-design.md` (§12 P2)
**Builds on:** P1 (merged `68ac562`) — the `enrich.Model` interface + deterministic backend.

## 1. Summary

P2 replaces the P1 deterministic backend with a real GLiNER2 classifier behind
the existing `enrich.Model` interface, paired with an adaptive host-load
governor. The architecture has one unresolved fork — **in-process Go+ONNX** vs a
**bundled sidecar** — so P2 is split:

- **2a (this spec):** de-risk the fork. Port the reference eval harness, build a
  Go+ONNX prototype of `enrich.Model`, measure it against the proven Python
  GLiNER2 sidecar, and produce a **go/no-go decision**.
- **2b (planned after 2a):** productionize the chosen backend + model packaging
  + the adaptive governor + daemon wiring. Out of scope here.

2a ships no user-facing behavior change; its deliverable is a **reusable eval
gate** plus a **decision** with measured evidence.

## 2. Background (reference implementation)

`~/keld/inference-enrichment` runs GLiNER2 as a Python **sidecar**
(`fastino/gliner2-large-v1` via the `gliner2` package, `/classify` + `/entities`
over HTTP) behind a staged pipeline, with an eval scorer
(`app/eval/run_eval.py::score`) computing per-field **accuracy** and
**sensitive_recall** against `app/eval/gold.jsonl`. The gold set is currently
**8 rows** — a smoke check, not a real gate.

The keld-cli seam already exists: `internal/agent/enrich/types.go` defines
`Model` (`Classify`, `Entities`, `Extract`) with `deterministic.go` behind it.

## 3. The seam (unchanged contract)

The spike implements the existing interface; nothing downstream changes:

```go
type Model interface {
    Classify(text string, tasks map[string][]string) map[string][]Ranked
    Entities(text string, labels map[string]string) []Entity
    Extract(text string, labels map[string]string, tasks map[string][]string) ExtractResult
}
```

The prototype backend is selected only by the spike harness/tests — it is **not**
wired into `daemon.Run` in 2a (no packaging, no default flip). The deterministic
backend remains the shipped default until 2b.

## 4. Eval harness (ported to Go, reusable)

- Port `gold.jsonl` into keld-cli under `internal/agent/enrich/eval/` (data +
  loader).
- A Go `score(gold, pred, fields)` mirroring the reference: per-field
  `accuracy`, and `sensitive_recall` over rows whose gold value ≠ `"none"` (a
  missed secret is the costly error). Table-tested against the reference's known
  cases (e.g. the 0.5-accuracy and 0.0-sensitive-recall examples).
- A runner that scores **any** `enrich.Model` — so it also becomes the standing
  quality gate for the deterministic backend, not just the spike.

The 8-row gold set is retained as an **absolute smoke check**. It is not the
go/no-go gate (see §5).

## 5. Spike metric — parity against the reference sidecar

The go/no-go question is *viability of in-process*, i.e. "does the Go+ONNX path
match the proven backend?" — which needs no hand-labels. Primary metric is
**agreement with the Python `gliner2-large` sidecar** over a sampled prompt set:

- Run both the sidecar and the Go+ONNX prototype over **N sampled prompts**
  (a few hundred; source: dev transcripts / synthetic prompts — no secrets
  committed).
- Measure agreement: task_type / domain / sensitivity label match rate, and
  entity/span overlap (IoU or start/end+label match).
- **sensitive_recall parity is the gate that matters most** — the in-process
  backend must not miss sensitivity the sidecar catches.
- The 8-row gold smoke check must also pass for both.

Thresholds are set from the sidecar's **measured** numbers (parity within a few
points; sensitive_recall no worse than the sidecar), not guessed absolutes — the
spike reports actuals and we lock the bar from them.

## 6. Go+ONNX prototype

- **Export:** one-off Python script under `tools/` exports
  `fastino/gliner2-large-v1` to ONNX (documented, reproducible; the `.onnx`
  artifact itself is not committed — it is produced/downloaded for the spike).
- **Runtime:** `onnxruntime-go` (CGO bindings to ORT). The prototype loads the
  model from a local path (`~/.keld/models/…` or an env-pointed path) — **no
  bundling/download decision in 2a**; that is a 2b concern.
- **Decode:** implement GLiNER2 tokenization + span/classify decode in Go to
  produce `[]Entity` and `map[string][]Ranked`, satisfying `enrich.Model`.
- Cross-compile / CGO feasibility for macOS, Linux, Windows is **explicitly
  measured** (it gates P3 installers), even if the spike itself only runs on the
  dev OS.

## 7. Decision doc (the deliverable)

A committed markdown decision under `docs/` recording: parity numbers (per
field + sensitive_recall + span overlap), the 8-row smoke result, binary-size
delta, cold+warm p95 latency on a laptop, CGO cross-compile findings, and a
clear **go (in-process) / no-go (sidecar fallback)** recommendation with
rationale. This is what 2b is planned from.

## 8. Testing

- **Eval harness:** `score` table tests (accuracy, sensitive_recall) matching the
  reference cases; loader test on the ported gold set; runner scores the
  deterministic backend as a regression baseline.
- **Prototype:** unit tests for the Go tokenizer/decoder on fixed inputs with
  known expected spans/labels (fixtures captured from the sidecar); a parity
  test asserting agreement ≥ the locked threshold over the sample set (may be
  build-tagged / skipped when the ONNX model or ORT lib is absent, so CI without
  the model still passes).
- `go test ./...` and `go vet ./...` stay green; the deterministic default path
  is untouched.

## 9. Scope / non-goals

- **No governor in 2a** (paired with real inference in 2b).
- **No daemon wiring / default flip / model packaging** in 2a — the prototype is
  exercised only by tests/harness.
- **No gold-set expansion** in 2a (2b's real gate); 2a relies on sidecar-parity.
- No change to the deterministic backend's behavior (only add the shared eval
  runner around it).

## 10. Open risks

1. **GLiNER2 structured decode in Go is non-trivial** (tokenizer + span scoring).
   Mitigation: this is exactly what the spike de-risks; fixtures captured from
   the sidecar make the decoder testable in isolation; sidecar fallback is the
   defined escape hatch.
2. **CGO/ORT packaging undermines the single-binary value.** Mitigation:
   cross-compile feasibility is a measured gate item, feeding the go/no-go and
   P3 planning.
3. **Model footprint/latency too heavy for a laptop daemon.** Mitigation:
   measured in the decision doc; a smaller GLiNER2 variant is a 2b lever if
   large fails the bar.
