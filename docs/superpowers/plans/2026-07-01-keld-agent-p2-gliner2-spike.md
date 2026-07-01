# keld-agent P2 (2a) — GLiNER2 Spike + Eval Gate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** De-risk the GLiNER2 in-process-vs-sidecar fork by porting a reusable Go eval harness and building a Go+ONNX prototype of `enrich.Model`, measured against the reference Python sidecar, ending in a go/no-go decision doc.

**Architecture:** Two durable, TDD'd deliverables (a Go eval harness + gold set that scores any `enrich.Model`) plus a spike (ONNX export tool → fixture capture from the sidecar → Go+ONNX `enrich.Model` prototype → parity measurement → decision doc). The prototype lives behind a build tag and is exercised only by tests/harness — it is **never** wired into `daemon.Run`; the deterministic backend stays the shipped default.

**Tech Stack:** Go 1.x (module `github.com/ncx-ai/keld-cli`), `go:embed`, `github.com/yalue/onnxruntime_go` (CGO ORT bindings), a Go HF tokenizer (`github.com/daulet/tokenizers`, candidate — confirmed in Task 5), Python `gliner2`/`optimum`/`onnx` for the one-off export, the reference sidecar (`~/keld/inference-enrichment`).

## Global Constraints

- Module path is `github.com/ncx-ai/keld-cli`; the enrich package is `internal/agent/enrich`.
- The `enrich.Model` interface is FROZEN (contract with P1): `Classify(text string, tasks map[string][]string) map[string][]Ranked`; `Entities(text string, labels map[string]string) []Entity`; `Extract(text string, labels map[string]string, tasks map[string][]string) ExtractResult`.
- The prototype MUST NOT be imported by `internal/agent/daemon` or flip any default. Deterministic backend stays default.
- `go test ./...` and `go vet ./...` MUST stay green **without** the ONNX model file or the ORT native library present — ONNX-dependent code is behind `//go:build onnx` and its tests skip when the model/lib is absent.
- No secrets or private prompt text committed. Parity/fixture prompt samples are synthetic or scrubbed.
- The `.onnx` weight artifact is NOT committed (produced by the export tool / downloaded locally).
- Sensitivity labels vocab (verbatim): `none, pii, secrets, phi, pci, proprietary`. A missed sensitive item (`sensitive_recall`) is the costly error and the gate that matters most.
- Privacy invariant carried from P1: sensitivity spans always masked (`Entity.Text` cleared, `Entity.Masked` set) — the prototype's `Entities`/`Extract` must preserve this; parity fixtures must not contain raw sensitive text.

---

### Task 1: Eval scorer + gold set (durable, reusable)

**Files:**
- Create: `internal/agent/enrich/eval/gold.jsonl`
- Create: `internal/agent/enrich/eval/eval.go`
- Test: `internal/agent/enrich/eval/eval_test.go`

**Interfaces:**
- Consumes: nothing (leaf package `eval`, imports only stdlib + `encoding/json`).
- Produces:
  - `type GoldRow struct { Text string; TaskType string; Domain string; Sensitivity string }` (JSON tags `text`,`task_type`,`domain`,`sensitivity`)
  - `type Pred struct { TaskType, Domain, Sensitivity string }`
  - `func LoadGold() ([]GoldRow, error)` — reads the embedded gold.jsonl
  - `func Score(gold []GoldRow, pred []Pred, fields []string) map[string]map[string]float64` — per-field `accuracy`; for `"sensitivity"` also `sensitive_recall` (recall over rows whose gold sensitivity ≠ `"none"`; 1.0 when there are none). Mirrors `inference-enrichment/services/api/app/eval/run_eval.py::score`.

- [ ] **Step 1: Create the gold data file** (ported verbatim from the reference)

`internal/agent/enrich/eval/gold.jsonl`:
```json
{"text": "Write a Python function that reverses a linked list.", "task_type": "codegen", "domain": "software", "sensitivity": "none"}
{"text": "Summarize the attached quarterly earnings report in three bullets.", "task_type": "summarization", "domain": "finance", "sensitivity": "none"}
{"text": "Translate this paragraph from English to German.", "task_type": "translation", "domain": "general", "sensitivity": "none"}
{"text": "My SSN is 123-45-6789, can you fill out the medical intake form?", "task_type": "extraction", "domain": "medical", "sensitivity": "phi"}
{"text": "Here is my API key sk-live-9f8a7b6c, configure the deployment.", "task_type": "agentic_tool_use", "domain": "software", "sensitivity": "secrets"}
{"text": "Classify these support tickets as bug, feature, or question.", "task_type": "classification", "domain": "software", "sensitivity": "none"}
{"text": "Given these docs, answer: what is the refund policy?", "task_type": "rag_qa", "domain": "business", "sensitivity": "none"}
{"text": "Prove that the square root of 2 is irrational.", "task_type": "reasoning", "domain": "science", "sensitivity": "none"}
```

- [ ] **Step 2: Write the failing tests** for `Score` and `LoadGold`

`internal/agent/enrich/eval/eval_test.go`:
```go
package eval

import "testing"

func TestScoreAccuracy(t *testing.T) {
	gold := []GoldRow{{TaskType: "codegen"}, {TaskType: "summarization"}}
	pred := []Pred{{TaskType: "codegen"}, {TaskType: "codegen"}}
	m := Score(gold, pred, []string{"task_type"})
	if m["task_type"]["accuracy"] != 0.5 {
		t.Fatalf("accuracy = %v, want 0.5", m["task_type"]["accuracy"])
	}
}

func TestScoreSensitiveRecall(t *testing.T) {
	gold := []GoldRow{{Sensitivity: "secrets"}, {Sensitivity: "none"}}
	pred := []Pred{{Sensitivity: "none"}, {Sensitivity: "none"}} // missed the secret
	m := Score(gold, pred, []string{"sensitivity"})
	if m["sensitivity"]["sensitive_recall"] != 0.0 {
		t.Fatalf("sensitive_recall = %v, want 0.0", m["sensitivity"]["sensitive_recall"])
	}
}

func TestScoreSensitiveRecallAllNoneIsOne(t *testing.T) {
	gold := []GoldRow{{Sensitivity: "none"}}
	pred := []Pred{{Sensitivity: "none"}}
	m := Score(gold, pred, []string{"sensitivity"})
	if m["sensitivity"]["sensitive_recall"] != 1.0 {
		t.Fatalf("sensitive_recall = %v, want 1.0", m["sensitivity"]["sensitive_recall"])
	}
}

func TestLoadGoldReadsEightRows(t *testing.T) {
	g, err := LoadGold()
	if err != nil {
		t.Fatal(err)
	}
	if len(g) != 8 {
		t.Fatalf("gold rows = %d, want 8", len(g))
	}
	if g[3].Sensitivity != "phi" || g[4].Sensitivity != "secrets" {
		t.Fatalf("unexpected gold sensitivity: %q %q", g[3].Sensitivity, g[4].Sensitivity)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd ~/keld/keld-cli && go test ./internal/agent/enrich/eval/ -run 'Score|LoadGold' -v`
Expected: FAIL — `eval.go` does not exist (`undefined: Score`, `undefined: LoadGold`).

- [ ] **Step 4: Implement `eval.go`**

`internal/agent/enrich/eval/eval.go`:
```go
// Package eval scores an enrich.Model's pipeline output against a gold set.
// Ported from inference-enrichment/services/api/app/eval.
package eval

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"strings"
)

//go:embed gold.jsonl
var goldJSONL string

// GoldRow is one labeled evaluation example.
type GoldRow struct {
	Text        string `json:"text"`
	TaskType    string `json:"task_type"`
	Domain      string `json:"domain"`
	Sensitivity string `json:"sensitivity"`
}

// Pred is one model prediction for the scored fields.
type Pred struct {
	TaskType    string
	Domain      string
	Sensitivity string
}

// LoadGold parses the embedded gold set.
func LoadGold() ([]GoldRow, error) {
	var out []GoldRow
	sc := bufio.NewScanner(strings.NewReader(goldJSONL))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r GoldRow
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, sc.Err()
}

func fieldOf(x any, f string) string {
	switch v := x.(type) {
	case GoldRow:
		switch f {
		case "task_type":
			return v.TaskType
		case "domain":
			return v.Domain
		case "sensitivity":
			return v.Sensitivity
		}
	case Pred:
		switch f {
		case "task_type":
			return v.TaskType
		case "domain":
			return v.Domain
		case "sensitivity":
			return v.Sensitivity
		}
	}
	return ""
}

// Score computes per-field accuracy and, for "sensitivity", sensitive_recall
// (recall over rows whose gold sensitivity != "none"; 1.0 when there are none).
func Score(gold []GoldRow, pred []Pred, fields []string) map[string]map[string]float64 {
	metrics := map[string]map[string]float64{}
	n := len(gold)
	if len(pred) < n {
		n = len(pred)
	}
	total := len(gold)
	if total == 0 {
		total = 1
	}
	for _, f := range fields {
		correct := 0
		for i := 0; i < n; i++ {
			if fieldOf(gold[i], f) == fieldOf(pred[i], f) {
				correct++
			}
		}
		entry := map[string]float64{"accuracy": float64(correct) / float64(total)}
		if f == "sensitivity" {
			sens, hit := 0, 0
			for i := 0; i < n; i++ {
				if fieldOf(gold[i], f) != "none" {
					sens++
					if fieldOf(gold[i], f) == fieldOf(pred[i], f) {
						hit++
					}
				}
			}
			if sens > 0 {
				entry["sensitive_recall"] = float64(hit) / float64(sens)
			} else {
				entry["sensitive_recall"] = 1.0
			}
		}
		metrics[f] = entry
	}
	return metrics
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd ~/keld/keld-cli && go test ./internal/agent/enrich/eval/ -v`
Expected: PASS (4 tests).

- [ ] **Step 6: Commit**

```bash
git add internal/agent/enrich/eval/
git commit -m "feat(eval): Go eval scorer + gold set ported from inference-enrichment"
```

---

### Task 2: Eval runner over any `enrich.Model` + deterministic baseline

**Files:**
- Modify: `internal/agent/enrich/eval/eval.go` (add `RunModel`)
- Test: `internal/agent/enrich/eval/runner_test.go`

**Interfaces:**
- Consumes: `enrich.Run(text, source string, m enrich.Model) enrich.Profile` (from `internal/agent/enrich`); `GoldRow`, `Pred`, `Score` (Task 1).
- Produces: `func RunModel(m enrich.Model, gold []GoldRow) []Pred` — runs the wave-1 pipeline per gold row and maps `Profile.TaskType.Value`/`Domain.Value`/`Sensitivity.Value` into `Pred`.

Note on import direction: `eval` imports `enrich` (the parent package). This is fine — `eval` is a child package and `enrich` does not import `eval`. If a future import cycle appears, keep `RunModel` in a separate file `runner.go` in a sub-package; not needed now.

- [ ] **Step 1: Write the failing test** (baseline against the deterministic backend)

`internal/agent/enrich/eval/runner_test.go`:
```go
package eval

import (
	"testing"

	"github.com/ncx-ai/keld-cli/internal/agent/enrich"
)

func TestRunModelOnDeterministicBaseline(t *testing.T) {
	gold, err := LoadGold()
	if err != nil {
		t.Fatal(err)
	}
	pred := RunModel(enrich.NewDeterministic(), gold)
	if len(pred) != len(gold) {
		t.Fatalf("pred len = %d, want %d", len(pred), len(gold))
	}
	m := Score(gold, pred, []string{"task_type", "domain", "sensitivity"})

	// The deterministic backend has strong regex priors for SSN + API keys, so
	// it must catch BOTH sensitive gold rows (missing a secret is the costly error).
	if got := m["sensitivity"]["sensitive_recall"]; got < 1.0 {
		t.Fatalf("deterministic sensitive_recall = %v, want 1.0 (SSN->phi, api key->secrets)", got)
	}
	// task_type accuracy is a baseline signal, not a hard gate here; just require
	// the runner actually produced predictions the scorer can read.
	if _, ok := m["task_type"]["accuracy"]; !ok {
		t.Fatal("task_type accuracy missing")
	}
	t.Logf("deterministic baseline: %+v", m)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd ~/keld/keld-cli && go test ./internal/agent/enrich/eval/ -run TestRunModel -v`
Expected: FAIL — `undefined: RunModel`.

- [ ] **Step 3: Implement `RunModel`** (append to `eval.go`)

```go
import (
	// ...existing...
	"github.com/ncx-ai/keld-cli/internal/agent/enrich"
)

// RunModel scores a backend by running the enrichment pipeline over each gold
// row and extracting the classified fields.
func RunModel(m enrich.Model, gold []GoldRow) []Pred {
	pred := make([]Pred, 0, len(gold))
	for _, g := range gold {
		p := enrich.Run(g.Text, "eval", m)
		pred = append(pred, Pred{
			TaskType:    p.TaskType.Value,
			Domain:      p.Domain.Value,
			Sensitivity: p.Sensitivity.Value,
		})
	}
	return pred
}
```

(If the `_ "embed"` import and the new `enrich` import need reconciling, keep imports grouped: stdlib block, then the `enrich` module import.)

- [ ] **Step 4: Run to verify it passes**

Run: `cd ~/keld/keld-cli && go test ./internal/agent/enrich/eval/ -v`
Expected: PASS. If `sensitive_recall < 1.0`, that is a real finding about the deterministic backend, not a test bug — STOP and report it (the baseline is diagnostic).

- [ ] **Step 5: Commit**

```bash
git add internal/agent/enrich/eval/
git commit -m "feat(eval): RunModel scores any enrich.Model; deterministic baseline test"
```

---

### Task 3: ONNX export tool (one-off, documented)

**Files:**
- Create: `tools/gliner2-export/export.py`
- Create: `tools/gliner2-export/requirements.txt`
- Create: `tools/gliner2-export/README.md`

**Interfaces:**
- Produces (on disk, not committed): `gliner2-large-v1.onnx` + `tokenizer.json` + a printed SHA256 of the `.onnx`. The README documents the exact command and where the artifact must be placed for Task 5 (`~/.keld/models/gliner2-large-v1/`).

This task is a scripted deliverable, not TDD (a one-off export). Verification = the script runs and emits the artifact + SHA.

- [ ] **Step 1: Write `requirements.txt`**

`tools/gliner2-export/requirements.txt`:
```
gliner2[local]
optimum[exporters]
onnx
onnxruntime
transformers
torch
```

- [ ] **Step 2: Write `export.py`**

`tools/gliner2-export/export.py`:
```python
"""One-off: export fastino/gliner2-large-v1 to ONNX for the keld-agent Go spike.

Usage:
    python -m venv .venv && . .venv/bin/activate
    pip install -r requirements.txt
    python export.py --out ~/.keld/models/gliner2-large-v1

Emits <out>/gliner2-large-v1.onnx and <out>/tokenizer.json, prints the .onnx SHA256.
The .onnx is NOT committed to the repo.
"""
import argparse
import hashlib
import pathlib
import sys


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--model", default="fastino/gliner2-large-v1")
    ap.add_argument("--out", required=True)
    args = ap.parse_args()

    out = pathlib.Path(args.out).expanduser()
    out.mkdir(parents=True, exist_ok=True)

    # GLiNER2 wraps a HF encoder (DeBERTa-family). Export the underlying encoder
    # graph + tokenizer. The exact export entrypoint depends on the installed
    # gliner2 version; this documents the intended path and must be confirmed
    # against the library at run time (spike step).
    from gliner2 import GLiNER2  # noqa: F401

    model = GLiNER2.from_pretrained(args.model)
    # Save tokenizer.json (fast tokenizer) for the Go side.
    tok = getattr(model, "tokenizer", None) or getattr(getattr(model, "model", None), "tokenizer", None)
    if tok is None:
        print("ERROR: could not locate tokenizer on the GLiNER2 model", file=sys.stderr)
        return 2
    tok.save_pretrained(str(out))

    # Export the encoder to ONNX via optimum or torch.onnx.export. The concrete
    # call is confirmed during the spike; the deliverable is <out>/gliner2-large-v1.onnx.
    onnx_path = out / "gliner2-large-v1.onnx"
    _export_onnx(model, onnx_path)  # implemented against the confirmed library API

    sha = hashlib.sha256(onnx_path.read_bytes()).hexdigest()
    print(f"exported: {onnx_path}\nsha256:   {sha}")
    return 0


def _export_onnx(model, onnx_path: pathlib.Path) -> None:
    """Export the GLiNER2 encoder graph to ONNX.

    Confirmed during the spike against the installed gliner2/optimum version:
    prefer `optimum.exporters.onnx` for the encoder; fall back to
    torch.onnx.export on model.model with (input_ids, attention_mask) and dynamic
    axes {0: batch, 1: seq}. Writes onnx_path.
    """
    raise NotImplementedError("confirm export path against installed gliner2/optimum during the spike")


if __name__ == "__main__":
    raise SystemExit(main())
```

- [ ] **Step 3: Write `README.md`** documenting the command, the expected output paths, the SHA, and that the `.onnx` is not committed.

`tools/gliner2-export/README.md`:
```markdown
# gliner2-export

One-off export of `fastino/gliner2-large-v1` to ONNX for the keld-agent Go+ONNX spike.

    python -m venv .venv && . .venv/bin/activate
    pip install -r requirements.txt
    python export.py --out ~/.keld/models/gliner2-large-v1

Produces `~/.keld/models/gliner2-large-v1/{gliner2-large-v1.onnx,tokenizer.json}`
and prints the `.onnx` SHA256. The `.onnx` weight file is **not** committed.

`_export_onnx` is confirmed against the installed `gliner2`/`optimum` version as
the first spike step; if in-process export proves impractical, that is itself a
go/no-go input (record it in the decision doc).
```

- [ ] **Step 4: Run the export** (spike; confirm `_export_onnx` against the installed library, then run)

Run: `cd tools/gliner2-export && python export.py --out ~/.keld/models/gliner2-large-v1`
Expected: prints `exported: …/gliner2-large-v1.onnx` + `sha256: …`. If the library makes export impractical, record that as a finding for the decision doc and proceed to Task 4 (parity can still capture sidecar fixtures; the go/no-go may resolve to sidecar).

- [ ] **Step 5: Commit** (script + docs only; no `.onnx`)

```bash
git add tools/gliner2-export/
git commit -m "tools: gliner2 ONNX export script + docs (spike)"
```

---

### Task 4: Capture sidecar fixtures + build a synthetic parity prompt set

**Files:**
- Create: `internal/agent/enrich/onnxmodel/testdata/prompts.jsonl` (synthetic, no secrets/private text)
- Create: `tools/sidecar-fixtures/capture.py`
- Create: `internal/agent/enrich/onnxmodel/testdata/sidecar_golden.json` (captured; committed — it is masked/synthetic)

**Interfaces:**
- Produces: `sidecar_golden.json` = for each prompt, the sidecar's `/extract` output (entities with `start`/`end`/`label`/`masked` and `results` label→ranked). These are the golden expectations the Go decoder targets, and the parity reference. Masking is applied so no raw sensitive strings are stored.

- [ ] **Step 1: Create the synthetic prompt set** (~40 rows to start; expandable)

`internal/agent/enrich/onnxmodel/testdata/prompts.jsonl` (first lines shown; include the 8 gold texts plus ~32 synthetic prompts spanning task types/domains and a few synthetic sensitive strings that are obviously fake):
```json
{"text": "Write a Python function that reverses a linked list."}
{"text": "Refactor this Go handler to use context cancellation."}
{"text": "Summarize the attached quarterly earnings report in three bullets."}
{"text": "Translate this paragraph from English to German."}
{"text": "Classify these support tickets as bug, feature, or question."}
{"text": "My test SSN is 000-00-0000, fill out the intake form."}
{"text": "Here is a fake API key sk-test-0000000000, configure the deploy."}
```

- [ ] **Step 2: Write the sidecar capture script**

`tools/sidecar-fixtures/capture.py`:
```python
"""Capture GLiNER2 sidecar /extract output as golden fixtures for the Go spike.

Requires the inference-enrichment sidecar running (docker compose up sidecar) on
http://localhost:8300. Reads prompts.jsonl, calls /extract with the keld label +
task vocab, masks sensitive entity text, writes sidecar_golden.json.
"""
import json
import pathlib
import sys
import httpx

SIDE = "http://localhost:8300"
DOMAIN_LABELS = {"language": "Programming languages such as Python, Rust, TypeScript",
                 "framework": "Software frameworks such as Django, React, FastAPI",
                 "library": "Software libraries or packages such as numpy, pandas, requests",
                 "org": "Organizations, companies, or institutions",
                 "product": "Named products, tools, or services"}
SENS_LABELS = {"email": "Email addresses", "phone": "Phone numbers",
               "ssn": "Social security or national identity numbers",
               "credit_card": "Credit card or payment card numbers",
               "api_key": "API keys, access tokens, or secret keys"}
TASKS = {"task_type": ["codegen", "summarization", "extraction", "translation",
                       "rag_qa", "classification", "reasoning", "agentic_tool_use", "other"],
         "domain": ["software", "legal", "medical", "finance", "science",
                    "business", "education", "creative", "general"]}
SENSITIVE = set(SENS_LABELS)


def mask(label: str, text: str) -> str:
    return f"<{label}>" if label in SENSITIVE else text


def main() -> int:
    here = pathlib.Path(__file__).resolve().parents[2] / "internal/agent/enrich/onnxmodel/testdata"
    prompts = [json.loads(l) for l in (here / "prompts.jsonl").read_text().splitlines() if l.strip()]
    labels = {**DOMAIN_LABELS, **SENS_LABELS}
    out = []
    with httpx.Client(timeout=30) as c:
        for row in prompts:
            r = c.post(f"{SIDE}/extract", json={"text": row["text"], "labels": labels, "tasks": TASKS})
            r.raise_for_status()
            data = r.json()
            for e in data.get("entities", []):
                if e.get("label") in SENSITIVE:
                    e["text"] = ""              # never store raw sensitive text
                    e["masked"] = mask(e["label"], "")
            out.append({"text": row["text"], "extract": data})
    (here / "sidecar_golden.json").write_text(json.dumps(out, indent=2))
    print(f"wrote {len(out)} fixtures")
    return 0


if __name__ == "__main__":
    sys.exit(main())
```

- [ ] **Step 3: Run the sidecar and capture**

Run:
```bash
cd ~/keld/inference-enrichment && docker compose up -d sidecar   # wait for :8300/health ok
cd ~/keld/keld-cli && python tools/sidecar-fixtures/capture.py
```
Expected: `wrote 40 fixtures` and `sidecar_golden.json` created. Verify no raw sensitive strings: `grep -iE '000-00-0000|sk-test-0000000000' internal/agent/enrich/onnxmodel/testdata/sidecar_golden.json` returns nothing.

- [ ] **Step 4: Commit** (synthetic prompts + masked golden + capture script)

```bash
git add internal/agent/enrich/onnxmodel/testdata/ tools/sidecar-fixtures/
git commit -m "test(onnx): synthetic parity prompts + masked sidecar golden fixtures"
```

---

### Task 5: Go+ONNX prototype implementing `enrich.Model` (spike core)

**Files:**
- Create: `internal/agent/enrich/onnxmodel/onnxmodel.go` (build tag `onnx`)
- Create: `internal/agent/enrich/onnxmodel/tokenizer.go` (build tag `onnx`)
- Create: `internal/agent/enrich/onnxmodel/decode.go` (build tag `onnx`)
- Test: `internal/agent/enrich/onnxmodel/onnxmodel_test.go` (build tag `onnx`)
- Modify: `go.mod` / `go.sum` (add `github.com/yalue/onnxruntime_go`, tokenizer lib)

**Interfaces:**
- Consumes: `enrich.Model`, `enrich.Ranked`, `enrich.Entity`, `enrich.ExtractResult` (implements the interface); `sidecar_golden.json` (Task 4) as test expectations.
- Produces: `func New(modelDir string) (enrich.Model, error)` — loads `gliner2-large-v1.onnx` + `tokenizer.json` from `modelDir`, returns a Model. `Classify`/`Entities`/`Extract` per the frozen interface.

**Build-tag rationale:** all files carry `//go:build onnx`. Default `go test ./...` (no tag) never compiles this package's CGO/ORT code, so CI without the ORT lib/model stays green. The spike runs `go test -tags onnx ./internal/agent/enrich/onnxmodel/` locally with the model present.

- [ ] **Step 1: Add dependencies**

Run:
```bash
cd ~/keld/keld-cli
go get github.com/yalue/onnxruntime_go@latest
go get github.com/daulet/tokenizers@latest   # candidate HF tokenizer; confirm loads tokenizer.json
```
Expected: `go.mod` updated. If `daulet/tokenizers` (CGO to HF tokenizers) proves unsuitable, record the tokenizer choice as a spike finding and try `github.com/sugarme/tokenizer` (pure-Go); the decision doc notes which worked.

- [ ] **Step 2: Write the fixture-driven failing test**

`internal/agent/enrich/onnxmodel/onnxmodel_test.go`:
```go
//go:build onnx

package onnxmodel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type goldenExtract struct {
	Text    string `json:"text"`
	Extract struct {
		Entities []struct {
			Label string `json:"label"`
			Start int    `json:"start"`
			End   int    `json:"end"`
		} `json:"entities"`
		Results map[string][]struct {
			Label string `json:"label"`
		} `json:"results"`
	} `json:"extract"`
}

func modelDir(t *testing.T) string {
	d := os.Getenv("KELD_GLINER2_DIR")
	if d == "" {
		d = filepath.Join(os.Getenv("HOME"), ".keld/models/gliner2-large-v1")
	}
	if _, err := os.Stat(filepath.Join(d, "gliner2-large-v1.onnx")); err != nil {
		t.Skipf("model not present at %s (run tools/gliner2-export); skipping", d)
	}
	return d
}

func TestClassifyTopLabelMatchesSidecar(t *testing.T) {
	dir := modelDir(t)
	m, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/sidecar_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden []goldenExtract
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	tasks := map[string][]string{
		"task_type": {"codegen", "summarization", "extraction", "translation", "rag_qa", "classification", "reasoning", "agentic_tool_use", "other"},
	}
	agree, total := 0, 0
	for _, g := range golden {
		want := g.Extract.Results["task_type"]
		if len(want) == 0 {
			continue
		}
		total++
		got := m.Classify(g.Text, tasks)
		if len(got["task_type"]) > 0 && got["task_type"][0].Label == want[0].Label {
			agree++
		}
	}
	// Spike gate placeholder: require majority agreement so the test is meaningful
	// while iterating. The LOCKED threshold is set from the parity run (Task 6).
	if total == 0 || float64(agree)/float64(total) < 0.5 {
		t.Fatalf("task_type top-1 agreement %d/%d below 0.5", agree, total)
	}
	t.Logf("task_type top-1 agreement: %d/%d", agree, total)
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `cd ~/keld/keld-cli && go test -tags onnx ./internal/agent/enrich/onnxmodel/ -run TestClassify -v`
Expected: FAIL — `undefined: New` (package not implemented). If the model is absent it SKIPS; to exercise the spike the model must be exported first (Task 3).

- [ ] **Step 4: Implement the prototype** (tokenizer → ORT session → GLiNER2 decode)

`internal/agent/enrich/onnxmodel/onnxmodel.go`:
```go
//go:build onnx

// Package onnxmodel is the P2 spike: an in-process GLiNER2 backend implementing
// enrich.Model via onnxruntime. Build-tagged `onnx`; never imported by daemon.
package onnxmodel

import (
	"fmt"
	"path/filepath"
	"sync"

	ort "github.com/yalue/onnxruntime_go"

	"github.com/ncx-ai/keld-cli/internal/agent/enrich"
)

type model struct {
	mu   sync.Mutex // ORT sessions are not guaranteed concurrency-safe; serialize
	sess *ort.DynamicAdvancedSession
	tok  *tokenizer
}

// New loads the exported GLiNER2 ONNX graph + tokenizer from modelDir.
func New(modelDir string) (enrich.Model, error) {
	if err := ort.InitializeEnvironment(); err != nil {
		return nil, fmt.Errorf("ort init: %w", err)
	}
	tok, err := newTokenizer(filepath.Join(modelDir, "tokenizer.json"))
	if err != nil {
		return nil, err
	}
	sess, err := ort.NewDynamicAdvancedSession(
		filepath.Join(modelDir, "gliner2-large-v1.onnx"),
		[]string{"input_ids", "attention_mask"}, // confirmed against exported graph
		[]string{"logits"},                      // confirmed against exported graph
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("ort session: %w", err)
	}
	return &model{sess: sess, tok: tok}, nil
}

func (m *model) Classify(text string, tasks map[string][]string) map[string][]enrich.Ranked {
	m.mu.Lock()
	defer m.mu.Unlock()
	return classify(m, text, tasks) // in decode.go
}

func (m *model) Entities(text string, labels map[string]string) []enrich.Entity {
	m.mu.Lock()
	defer m.mu.Unlock()
	return entities(m, text, labels) // in decode.go — MUST mask sensitive spans
}

func (m *model) Extract(text string, labels map[string]string, tasks map[string][]string) enrich.ExtractResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	return enrich.ExtractResult{
		Entities: entities(m, text, labels),
		Results:  classify(m, text, tasks),
	}
}
```

`internal/agent/enrich/onnxmodel/tokenizer.go`: wraps the chosen tokenizer lib — `newTokenizer(path string) (*tokenizer, error)` and `(*tokenizer).encode(text string) (ids []int64, mask []int64, offsets [][2]int)`; offsets are byte spans used to map decoded entity token spans back to `Start`/`End` in the original text.

`internal/agent/enrich/onnxmodel/decode.go`: `classify(m, text, tasks)` and `entities(m, text, labels)` implement the GLiNER2 prompt construction (label/task options as schema tokens), run the ORT session, and decode logits into `[]Ranked` / `[]Entity`. **`entities` must clear `Text` and set `Masked` for any label in the sensitive set** (email/phone/ssn/credit_card/api_key), preserving the P1 privacy invariant — reuse `enrich`'s masking if exported, else mirror it. The precise decode math is confirmed against the sidecar fixtures during the spike.

- [ ] **Step 5: Iterate until the fixture test passes**

Run: `cd ~/keld/keld-cli && go test -tags onnx ./internal/agent/enrich/onnxmodel/ -v`
Expected: PASS (majority agreement) with the model present, or SKIP without it. Iterate decode against `sidecar_golden.json`. If in-process decode cannot reach majority agreement after the time-box, STOP — that is a go/no-go=sidecar outcome; record it in Task 7.

- [ ] **Step 6: Confirm default build stays clean** (no tag)

Run: `cd ~/keld/keld-cli && go build ./... && go vet ./... && go test ./...`
Expected: PASS — the onnx package is excluded without the tag; nothing else references it.

- [ ] **Step 7: Commit**

```bash
git add internal/agent/enrich/onnxmodel/ go.mod go.sum
git commit -m "spike(onnx): in-process GLiNER2 enrich.Model prototype (build tag onnx)"
```

---

### Task 6: Parity + footprint measurement harness

**Files:**
- Create: `internal/agent/enrich/onnxmodel/parity_test.go` (build tag `onnx`)
- Create: `tools/spike-measure/measure.sh`

**Interfaces:**
- Consumes: `New` (Task 5), `sidecar_golden.json` (Task 4), `eval.Score`/`eval.LoadGold` (Tasks 1-2).
- Produces: printed metrics — per-field top-1 agreement vs sidecar, sensitivity `sensitive_recall` parity, entity span agreement (label + start/end match rate), the 8-row gold smoke, cold/warm latency; plus a script capturing binary-size delta and cross-compile results.

- [ ] **Step 1: Write the parity test** (reports numbers; asserts only the safety-critical floor)

`internal/agent/enrich/onnxmodel/parity_test.go`:
```go
//go:build onnx

package onnxmodel

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestParityAgainstSidecar(t *testing.T) {
	dir := modelDir(t)
	m, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile("testdata/sidecar_golden.json")
	var golden []goldenExtract
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	tasks := map[string][]string{
		"task_type": {"codegen", "summarization", "extraction", "translation", "rag_qa", "classification", "reasoning", "agentic_tool_use", "other"},
		"domain":    {"software", "legal", "medical", "finance", "science", "business", "education", "creative", "general"},
	}
	var ttAgree, ttTotal, spanAgree, spanTotal int
	start := time.Now()
	for _, g := range golden {
		got := m.Classify(g.Text, tasks)
		if w := g.Extract.Results["task_type"]; len(w) > 0 {
			ttTotal++
			if len(got["task_type"]) > 0 && got["task_type"][0].Label == w[0].Label {
				ttAgree++
			}
		}
		// span agreement: fraction of sidecar entity (label,start,end) triples the
		// prototype also reports.
		// ... compute against m.Entities(g.Text, labels) ...
		_ = spanAgree
		_ = spanTotal
	}
	dur := time.Since(start)
	t.Logf("PARITY task_type top-1: %d/%d; latency total=%s avg=%s over %d prompts",
		ttAgree, ttTotal, dur, dur/time.Duration(max(1, len(golden))), len(golden))
	// Safety floor: sensitivity must not regress vs sidecar. Compute sensitive
	// span recall here and REQUIRE >= 1.0 parity on the synthetic sensitive rows.
	// (Full numbers are recorded in the decision doc.)
}

func max(a, b int) int { if a > b { return a }; return b }
```

- [ ] **Step 2: Write the footprint/cross-compile script**

`tools/spike-measure/measure.sh`:
```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
echo "== binary size: default vs onnx =="
go build -o /tmp/keld-agent-default ./cmd/keld-agent 2>/dev/null || go build -o /tmp/keld-agent-default ./...
ls -la /tmp/keld-agent-default | awk '{print "default:", $5, "bytes"}'
echo "== cross-compile feasibility (onnx tag) =="
for osarch in darwin/arm64 linux/amd64 windows/amd64; do
  GOOS=${osarch%/*} GOARCH=${osarch#*/} go build -tags onnx ./internal/agent/enrich/onnxmodel/ \
    && echo "$osarch: OK" || echo "$osarch: FAIL (CGO/ORT lib) — record for decision doc"
done
```

- [ ] **Step 3: Run the measurements**

Run:
```bash
cd ~/keld/keld-cli
go test -tags onnx ./internal/agent/enrich/onnxmodel/ -run TestParity -v   # with model present
bash tools/spike-measure/measure.sh
```
Expected: parity numbers logged; per-OS cross-compile OK/FAIL lines. Capture all output for Task 7.

- [ ] **Step 4: Commit**

```bash
git add internal/agent/enrich/onnxmodel/parity_test.go tools/spike-measure/
git commit -m "spike(onnx): parity + footprint/cross-compile measurement harness"
```

---

### Task 7: Go/no-go decision doc (the deliverable)

**Files:**
- Create: `docs/keld-agent-p2-onnx-decision.md`

- [ ] **Step 1: Write the decision doc** with the measured evidence and a recommendation

`docs/keld-agent-p2-onnx-decision.md` — required sections, each filled with **actual measured numbers** from Tasks 5-6 (no placeholders in the committed version):
```markdown
# keld-agent P2 — in-process Go+ONNX vs sidecar: decision

## Method
- Model: fastino/gliner2-large-v1, exported to ONNX (sha256: <value>).
- Parity set: <N> synthetic prompts; reference = Python gliner2 sidecar /extract.
- Tokenizer lib: <daulet/tokenizers | sugarme/tokenizer | other>.

## Results
- task_type top-1 agreement: <a>/<n> (<pct>%)
- domain top-1 agreement: <a>/<n> (<pct>%)
- sensitivity sensitive_recall parity: <value> (MUST be >= sidecar)
- entity span agreement (label+start+end): <pct>%
- 8-row gold smoke: task_type <acc>, domain <acc>, sensitivity sensitive_recall <val>
- latency: cold <ms>, warm avg <ms>/prompt
- binary size: default <MB> -> onnx <MB> (delta <MB>) + ORT lib <MB>
- cross-compile: darwin/arm64 <OK/FAIL>, linux/amd64 <OK/FAIL>, windows/amd64 <OK/FAIL>

## Decision
GO (in-process) | NO-GO (bundled sidecar) — <one-paragraph rationale tying to the
numbers above and the single-binary value + P3 installer impact>.

## Implications for 2b
<what the chosen path means for packaging, the governor, and P3.>
```

- [ ] **Step 2: Commit**

```bash
git add docs/keld-agent-p2-onnx-decision.md
git commit -m "docs(keld-agent): P2 spike go/no-go decision (in-process vs sidecar)"
```

---

## Notes for the executor

- Tasks 1-2 are durable production code (the eval gate) and fully TDD'd — do these first and they stand on their own even if the spike resolves to sidecar.
- Tasks 3-6 are a spike: exact ONNX export entrypoint, tokenizer library, ORT input/output tensor names, and GLiNER2 decode math are **discovered against the installed library and the sidecar fixtures**. Where this plan shows `confirmed during the spike`, that is a real investigation step, not a placeholder to skip — capture what you find in the decision doc.
- If any spike step hits a hard wall (export impractical, decode can't reach parity, CGO cross-compile infeasible), that is a legitimate **NO-GO** result — stop spiking, write Task 7 with the evidence, and the fork resolves to the bundled sidecar for 2b.
- Never wire the onnx package into `daemon.Run`; never flip the default backend in 2a.
