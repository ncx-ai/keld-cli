# keld-agent P2 (2b) — GLiNER2 Sidecar Backend + Governor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a GLiNER2 ML backend behind `enrich.Model` via a bundled localhost sidecar, health-gated with a deterministic fallback and an adaptive host-load governor, keeping the pure-Go deterministic backend the zero-dependency default.

**Architecture:** A Go `sidecarClient` implements the existing `enrich.Model` (raw entities — the pipeline still masks). A health-gated `router` picks sidecar-when-healthy else deterministic. A supervisor spawns/health-checks/restarts the sidecar child process and holds the backlog for ML during provisioning (deterministic-fallback on failure/timeout). A pure governor policy (EWMA host CPU) paces the ML path. Model is fetched from HF Hub (pinned revision, SHA256-verified). Tasks 1–7 are pure Go, unit-testable against fakes; Tasks 8–11 are environment-dependent (real sidecar app, live fetch, freeze) and separated.

**Tech Stack:** Go (module `github.com/ncx-ai/keld-cli`), stdlib `net/http`/`net/http/httptest`, `github.com/shirou/gopsutil/v4/cpu` (host load), Python 3.12 + FastAPI + `gliner2[local]` (sidecar), PyInstaller (freeze, P3-adjacent).

## Global Constraints

- Module path `github.com/ncx-ai/keld-cli`; enrich interface FROZEN: `Classify(text string, tasks map[string][]string) map[string][]enrich.Ranked`, `Entities(text string, labels map[string]string) []enrich.Entity`, `Extract(text string, labels map[string]string, tasks map[string][]string) enrich.ExtractResult`.
- **Masking is the pipeline's job, not the backend's.** `sidecarClient` returns RAW entities (`Text`,`Start`,`End`,`Label`,`Confidence`), exactly like `deterministic`. It MUST NOT clear `Text`/set `Masked` — `SensitivityExtractor.Run` does that (`Masked = enrich.Mask(label, text)`). Pre-masking would degrade the hint.
- Deterministic (`enrich.NewDeterministic()`) stays the default and the fallback. ML is opt-in via `ml_backend: auto|off` (default `auto`) in `~/.keld/agent-config.json`.
- Lifecycle: daemon accepts+enqueues immediately; hold backlog for ML during provisioning; deterministic-fallback only on sidecar failure/timeout; sidecar terminated on shutdown.
- `go test ./...` + `go vet ./...` green WITHOUT a running sidecar, model, or Python — env-dependent tests are build-tagged (`//go:build sidecar`) or skip when their dependency is absent.
- Raw prompt text may cross only the loopback daemon→sidecar hop; only masked spans are published (unchanged from P1).
- Model: `fastino/gliner2-large-v1`, pinned revision + SHA256 manifest as repo constants.

## File Structure

- `internal/agent/settings/settings.go` (MOD) — add `MLBackend`.
- `internal/agent/enrich/sidecar/client.go` (NEW) — `sidecarClient` implements `enrich.Model` + `Healthy`.
- `internal/agent/enrich/router.go` (NEW) — health-gated `enrich.Model` selector.
- `internal/agent/provision/provision.go` (NEW) — `EnsureModel` + `Fetcher` iface (fake in tests).
- `internal/agent/govern/govern.go` (NEW) — pure EWMA governor policy + `Sampler` iface.
- `internal/agent/daemon/supervisor.go` (NEW) — sidecar process supervision + readiness gate.
- `internal/agent/daemon/daemon.go` (MOD) — wire router + supervisor + governor + backlog lifecycle.
- `sidecar/` (NEW) — the FastAPI sidecar app (env task).
- `internal/agent/enrich/sidecar/hf.go` (NEW) — real HF fetcher (env task).
- `internal/agent/enrich/eval/sidecar_eval_test.go` (NEW, tagged) — eval gate vs live sidecar.

---

### Task 1: `ml_backend` setting

**Files:** Modify `internal/agent/settings/settings.go`; Test `internal/agent/settings/settings_test.go`.

**Interfaces:** Produces `Settings.MLBackend string` (json `ml_backend`) + `func (s Settings) MLEnabled() bool` (true unless value is `"off"`; empty/absent → auto → true).

- [ ] **Step 1: Write the failing test**

```go
package settings

import "testing"

func TestMLEnabledDefaultsAuto(t *testing.T) {
	if !(Settings{}).MLEnabled() {
		t.Fatal("empty MLBackend should default to enabled (auto)")
	}
	if !(Settings{MLBackend: "auto"}).MLEnabled() {
		t.Fatal("auto should be enabled")
	}
	if (Settings{MLBackend: "off"}).MLEnabled() {
		t.Fatal("off should be disabled")
	}
}
```

- [ ] **Step 2: Run to verify fail** — `cd ~/keld/keld-cli && go test ./internal/agent/settings/ -run MLEnabled -v` → FAIL (`MLEnabled` undefined).

- [ ] **Step 3: Implement** — add to `Settings` struct and file:

```go
	// MLBackend selects the ML backend: "auto" (use the GLiNER2 sidecar when
	// healthy, else deterministic) or "off" (deterministic only). Default auto.
	MLBackend string `json:"ml_backend"`
```
```go
// MLEnabled reports whether the ML sidecar backend may be used.
func (s Settings) MLEnabled() bool { return s.MLBackend != "off" }
```

- [ ] **Step 4: Run to verify pass** — same command → PASS.

- [ ] **Step 5: Commit** — `git add internal/agent/settings/ && git commit -m "feat(settings): ml_backend auto|off (default auto)"`

---

### Task 2: `sidecarClient` implements `enrich.Model`

**Files:** Create `internal/agent/enrich/sidecar/client.go`; Test `internal/agent/enrich/sidecar/client_test.go`.

**Interfaces:**
- Consumes: `enrich.Ranked`, `enrich.Entity`, `enrich.ExtractResult`.
- Produces: `func New(baseURL string, timeout time.Duration) *Client`; `Client` implements `enrich.Model`; `func (c *Client) Healthy(ctx context.Context) bool`. On any HTTP/decode error the methods return empty results (so the pipeline degrades, not panics) — but `Healthy` is the routing signal.

- [ ] **Step 1: Write the failing test** (httptest stub returns canned JSON; assert RAW entities, not masked)

```go
package sidecar

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func stub(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/extract", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"entities":[{"text":"a@b.com","label":"email","start":5,"end":12,"confidence":1.0}],"results":{"task_type":[{"label":"codegen","confidence":0.9}]}}`))
	})
	return httptest.NewServer(mux)
}

func TestExtractReturnsRawEntities(t *testing.T) {
	s := stub(t)
	defer s.Close()
	c := New(s.URL, 5*time.Second)
	res := c.Extract("email a@b.com", map[string]string{"email": "Email addresses"}, map[string][]string{"task_type": {"codegen"}})
	if len(res.Entities) != 1 {
		t.Fatalf("entities = %d, want 1", len(res.Entities))
	}
	e := res.Entities[0]
	if e.Text != "a@b.com" || e.Masked != "" { // RAW; masking is the pipeline's job
		t.Fatalf("want raw text unmasked, got Text=%q Masked=%q", e.Text, e.Masked)
	}
	if e.Start != 5 || e.End != 12 || e.Label != "email" {
		t.Fatalf("bad span: %+v", e)
	}
	if r := res.Results["task_type"]; len(r) != 1 || r[0].Label != "codegen" {
		t.Fatalf("bad results: %+v", res.Results)
	}
}

func TestHealthy(t *testing.T) {
	s := stub(t)
	defer s.Close()
	c := New(s.URL, time.Second)
	if !c.Healthy(t.Context()) {
		t.Fatal("stub should be healthy")
	}
	c2 := New("http://127.0.0.1:1", 200*time.Millisecond) // nothing listening
	if c2.Healthy(t.Context()) {
		t.Fatal("unreachable sidecar must be unhealthy")
	}
}
```

- [ ] **Step 2: Run to verify fail** — `go test ./internal/agent/enrich/sidecar/ -v` → FAIL (package missing).

- [ ] **Step 3: Implement `client.go`**

```go
// Package sidecar is the HTTP client for the bundled GLiNER2 sidecar; it
// implements enrich.Model. It returns RAW entities — masking is enforced by the
// enrichment pipeline (SensitivityExtractor), not here.
package sidecar

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/ncx-ai/keld-cli/internal/agent/enrich"
)

type Client struct {
	base string
	hc   *http.Client
}

func New(baseURL string, timeout time.Duration) *Client {
	return &Client{base: baseURL, hc: &http.Client{Timeout: timeout}}
}

func (c *Client) post(path string, body any, out any) bool {
	b, err := json.Marshal(body)
	if err != nil {
		return false
	}
	resp, err := c.hc.Post(c.base+path, "application/json", bytes.NewReader(b))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	return json.NewDecoder(resp.Body).Decode(out) == nil
}

type extractReq struct {
	Text   string              `json:"text"`
	Labels map[string]string   `json:"labels"`
	Tasks  map[string][]string `json:"tasks"`
}
type extractResp struct {
	Entities []enrich.Entity            `json:"entities"`
	Results  map[string][]enrich.Ranked `json:"results"`
}

func (c *Client) Extract(text string, labels map[string]string, tasks map[string][]string) enrich.ExtractResult {
	var r extractResp
	if !c.post("/extract", extractReq{text, labels, tasks}, &r) {
		return enrich.ExtractResult{}
	}
	return enrich.ExtractResult{Entities: r.Entities, Results: r.Results}
}

func (c *Client) Entities(text string, labels map[string]string) []enrich.Entity {
	var r extractResp
	if !c.post("/entities", struct {
		Text   string            `json:"text"`
		Labels map[string]string `json:"labels"`
	}{text, labels}, &r) {
		return nil
	}
	return r.Entities
}

func (c *Client) Classify(text string, tasks map[string][]string) map[string][]enrich.Ranked {
	var r extractResp
	if !c.post("/classify", struct {
		Text  string              `json:"text"`
		Tasks map[string][]string `json:"tasks"`
	}{text, tasks}, &r) {
		return nil
	}
	return r.Results
}

func (c *Client) Healthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var h struct{ Ok bool `json:"ok"` }
	return resp.StatusCode == http.StatusOK && json.NewDecoder(resp.Body).Decode(&h) == nil && h.Ok
}
```

Note: the `/entities` and `/classify` sidecar responses reuse the `extractResp` shape (`entities`/`results`); the sidecar app (Task 8) returns those keys.

- [ ] **Step 4: Run to verify pass** — `go test ./internal/agent/enrich/sidecar/ -v` → PASS. Confirm `go build ./... && go vet ./...` clean.

- [ ] **Step 5: Commit** — `git commit -m "feat(sidecar): enrich.Model HTTP client (raw entities) + Healthy"`

---

### Task 3: Health-gated backend router

**Files:** Create `internal/agent/enrich/router.go`; Test `internal/agent/enrich/router_test.go`.

**Interfaces:**
- Produces: `type HealthFunc func() bool`; `func NewRouter(primary, fallback Model, healthy HealthFunc) Model` — a `Model` whose each call delegates to `primary` when `healthy()` is true, else `fallback`. Re-checks per call (health can change).

- [ ] **Step 1: Write the failing test** (fakes implementing `enrich.Model`)

```go
package enrich

import "testing"

type tagModel struct{ tag string }

func (m tagModel) Classify(string, map[string][]string) map[string][]Ranked {
	return map[string][]Ranked{"task_type": {{Label: m.tag, Confidence: 1}}}
}
func (m tagModel) Entities(string, map[string]string) []Entity { return nil }
func (m tagModel) Extract(string, map[string]string, map[string][]string) ExtractResult {
	return ExtractResult{Results: m.Classify("", nil)}
}

func TestRouterPicksByHealth(t *testing.T) {
	healthy := true
	r := NewRouter(tagModel{"sidecar"}, tagModel{"det"}, func() bool { return healthy })
	if got := r.Classify("x", nil)["task_type"][0].Label; got != "sidecar" {
		t.Fatalf("healthy -> want sidecar, got %s", got)
	}
	healthy = false
	if got := r.Classify("x", nil)["task_type"][0].Label; got != "det" {
		t.Fatalf("unhealthy -> want det, got %s", got)
	}
}
```

- [ ] **Step 2: Run to verify fail** — `go test ./internal/agent/enrich/ -run Router -v` → FAIL.

- [ ] **Step 3: Implement `router.go`**

```go
package enrich

// HealthFunc reports whether the primary (ML) backend is currently usable.
type HealthFunc func() bool

type router struct {
	primary, fallback Model
	healthy           HealthFunc
}

// NewRouter returns a Model that delegates to primary when healthy() is true,
// else to fallback. Health is re-checked on every call.
func NewRouter(primary, fallback Model, healthy HealthFunc) Model {
	return &router{primary: primary, fallback: fallback, healthy: healthy}
}

func (r *router) pick() Model {
	if r.healthy != nil && r.healthy() {
		return r.primary
	}
	return r.fallback
}

func (r *router) Classify(t string, tasks map[string][]string) map[string][]Ranked {
	return r.pick().Classify(t, tasks)
}
func (r *router) Entities(t string, labels map[string]string) []Entity {
	return r.pick().Entities(t, labels)
}
func (r *router) Extract(t string, labels map[string]string, tasks map[string][]string) ExtractResult {
	return r.pick().Extract(t, labels, tasks)
}
```

- [ ] **Step 4: Run to verify pass** — → PASS.

- [ ] **Step 5: Commit** — `git commit -m "feat(enrich): health-gated backend router"`

---

### Task 4: Model provisioning with an injected fetcher

**Files:** Create `internal/agent/provision/provision.go`; Test `internal/agent/provision/provision_test.go`.

**Interfaces:**
- Produces: `type Fetcher interface { Fetch(ctx context.Context, destDir string) error }`; `func EnsureModel(ctx context.Context, dir, sha256Manifest string, f Fetcher) error` — if `dir` already contains a valid model (per a sentinel file whose sha matches `sha256Manifest`), return nil; else fetch into a temp dir, verify the sentinel sha, atomically rename temp→dir. Sha mismatch → error, nothing installed.

Note: keep verification simple + testable — hash a single sentinel file (e.g. the `.onnx`/`model.safetensors`) named by the caller. For the plan, verify a file named `model.safetensors` in the dir.

- [ ] **Step 1: Write the failing test** (fake fetcher writes a known file; assert install/skip/mismatch)

```go
package provision

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

type fakeFetcher struct{ content []byte }

func (f fakeFetcher) Fetch(_ context.Context, dest string) error {
	return os.WriteFile(filepath.Join(dest, "model.safetensors"), f.content, 0o644)
}

func sha(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func TestEnsureModelFetchesVerifiesInstalls(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gliner2")
	content := []byte("weights")
	if err := EnsureModel(t.Context(), dir, sha(content), fakeFetcher{content}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "model.safetensors")); err != nil {
		t.Fatal("model not installed")
	}
}

func TestEnsureModelShaMismatchDoesNotInstall(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gliner2")
	err := EnsureModel(t.Context(), dir, sha([]byte("expected")), fakeFetcher{[]byte("actual")})
	if err == nil {
		t.Fatal("want sha-mismatch error")
	}
	if _, statErr := os.Stat(dir); statErr == nil {
		t.Fatal("nothing should be installed on mismatch")
	}
}

func TestEnsureModelSkipsWhenPresentValid(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gliner2")
	content := []byte("weights")
	if err := EnsureModel(t.Context(), dir, sha(content), fakeFetcher{content}); err != nil {
		t.Fatal(err)
	}
	// second call with a fetcher that would error must NOT be invoked
	if err := EnsureModel(t.Context(), dir, sha(content), errFetcher{}); err != nil {
		t.Fatalf("present+valid should skip fetch: %v", err)
	}
}

type errFetcher struct{}

func (errFetcher) Fetch(context.Context, string) error { return os.ErrPermission }
```

- [ ] **Step 2: Run to verify fail** — `go test ./internal/agent/provision/ -v` → FAIL.

- [ ] **Step 3: Implement `provision.go`**

```go
// Package provision fetches + verifies the GLiNER2 model into a local dir.
package provision

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

const sentinel = "model.safetensors"

type Fetcher interface {
	Fetch(ctx context.Context, destDir string) error
}

func fileSHA(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// EnsureModel makes dir contain a verified model. If already present and its
// sentinel matches wantSHA, it's a no-op. Otherwise it fetches into a temp dir,
// verifies, and atomically renames into place. On mismatch nothing is installed.
func EnsureModel(ctx context.Context, dir, wantSHA string, f Fetcher) error {
	if got, err := fileSHA(filepath.Join(dir, sentinel)); err == nil && got == wantSHA {
		return nil
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dir), ".gliner2-dl-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := f.Fetch(ctx, tmp); err != nil {
		return err
	}
	got, err := fileSHA(filepath.Join(tmp, sentinel))
	if err != nil {
		return fmt.Errorf("fetched model missing %s: %w", sentinel, err)
	}
	if got != wantSHA {
		return fmt.Errorf("model sha mismatch: got %s want %s", got, wantSHA)
	}
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dir)
}
```

- [ ] **Step 4: Run to verify pass** — → PASS. (Note: `t.Context()` requires Go 1.24+; the repo is on go1.26 per `go version`.)

- [ ] **Step 5: Commit** — `git commit -m "feat(provision): EnsureModel fetch+sha256-verify+atomic install"`

---

### Task 5: Governor policy (pure) + sampler seam

**Files:** Create `internal/agent/govern/govern.go`; Test `internal/agent/govern/govern_test.go`.

**Interfaces:**
- Produces: `type Sampler interface { CPUPercent() float64 }`; `type Governor struct{...}`; `func New(sampler Sampler, maxConc int) *Governor`; `func (g *Governor) Observe(sample float64)` (updates EWMA); `func (g *Governor) Concurrency() int` (maxConc when calm, 1 under high load); `func (g *Governor) Admit() bool` (false-sample under sustained high load). Pure/deterministic given samples.

- [ ] **Step 1: Write the failing test**

```go
package govern

import "testing"

func TestConcurrencyDropsUnderLoad(t *testing.T) {
	g := New(nil, 4)
	for i := 0; i < 20; i++ {
		g.Observe(95) // sustained high CPU
	}
	if g.Concurrency() != 1 {
		t.Fatalf("high load -> concurrency 1, got %d", g.Concurrency())
	}
}

func TestConcurrencyFullWhenCalm(t *testing.T) {
	g := New(nil, 4)
	for i := 0; i < 20; i++ {
		g.Observe(5)
	}
	if g.Concurrency() != 4 {
		t.Fatalf("calm -> maxConc, got %d", g.Concurrency())
	}
}

func TestAdmitShedsUnderSustainedHighLoad(t *testing.T) {
	g := New(nil, 4)
	for i := 0; i < 20; i++ {
		g.Observe(99)
	}
	shed := 0
	for i := 0; i < 100; i++ {
		if !g.Admit() {
			shed++
		}
	}
	if shed == 0 {
		t.Fatal("sustained high load should shed some admissions")
	}
}
```

- [ ] **Step 2: Run to verify fail** — `go test ./internal/agent/govern/ -v` → FAIL.

- [ ] **Step 3: Implement `govern.go`** (EWMA + thresholds; `Admit` sheds proportionally to how far EWMA exceeds a high-water mark; deterministic pseudo-shedding via a counter to keep tests stable)

```go
// Package govern is the adaptive host-load governor for the ML path.
package govern

const (
	alpha    = 0.3 // EWMA smoothing
	highMark = 85.0
	lowMark  = 60.0
)

type Sampler interface{ CPUPercent() float64 }

type Governor struct {
	sampler Sampler
	maxConc int
	ewma    float64
	seen    bool
	tick    uint64
}

func New(sampler Sampler, maxConc int) *Governor {
	if maxConc < 1 {
		maxConc = 1
	}
	return &Governor{sampler: sampler, maxConc: maxConc}
}

func (g *Governor) Observe(sample float64) {
	if !g.seen {
		g.ewma, g.seen = sample, true
		return
	}
	g.ewma = alpha*sample + (1-alpha)*g.ewma
}

// Concurrency scales from maxConc (<= lowMark) down to 1 (>= highMark).
func (g *Governor) Concurrency() int {
	switch {
	case g.ewma >= highMark:
		return 1
	case g.ewma <= lowMark:
		return g.maxConc
	default:
		// linear interp between lowMark..highMark
		frac := (highMark - g.ewma) / (highMark - lowMark) // 1 at low, 0 at high
		c := 1 + int(frac*float64(g.maxConc-1))
		if c < 1 {
			c = 1
		}
		return c
	}
}

// Admit sheds a fraction of work proportional to overload above highMark.
func (g *Governor) Admit() bool {
	if g.ewma < highMark {
		return true
	}
	// keep 1 of every N; N grows with overload (85->keep most, 100->shed ~half)
	keep := uint64(2)
	if g.ewma >= 95 {
		keep = 2
	} else {
		keep = 4
	}
	g.tick++
	return g.tick%keep == 0
}

// Sample reads the host sampler (when configured) and updates the EWMA.
func (g *Governor) Sample() {
	if g.sampler != nil {
		g.Observe(g.sampler.CPUPercent())
	}
}
```

- [ ] **Step 4: Run to verify pass** — → PASS. Tune constants only if a test's intent (drop under load / full when calm / shed some) fails; keep the asserted behaviors.

- [ ] **Step 5: Commit** — `git commit -m "feat(govern): EWMA host-load governor policy (concurrency + admission)"`

---

### Task 6: Sidecar supervisor + readiness gate

**Files:** Create `internal/agent/daemon/supervisor.go`; Test `internal/agent/daemon/supervisor_test.go`.

**Interfaces:**
- Produces: `type Supervisor struct{...}`; `func NewSupervisor(spawn func(port int) (*exec.Cmd, error), port int, health enrich.HealthFunc, readyTimeout time.Duration) *Supervisor`; `func (s *Supervisor) Start(ctx context.Context)` (spawns, polls health, sets ready, restarts w/ backoff on exit, gives up after N restarts); `func (s *Supervisor) Ready() bool`; `func (s *Supervisor) FellBack() bool` (true once readyTimeout/restart-cap passed without health). `Ready`/`FellBack` are the worker's gate.

Design keeps process spawning behind the injected `spawn` func so tests use a fake (a cmd that sleeps) and a fake health func — no real sidecar needed.

- [ ] **Step 1: Write the failing test** (fake spawn + toggled health → Ready flips; never-healthy → FellBack)

```go
package daemon

import (
	"context"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"
)

func sleepCmd() (*exec.Cmd, error) { return exec.Command("sleep", "30"), nil }

func TestSupervisorBecomesReadyWhenHealthy(t *testing.T) {
	var healthy atomic.Bool
	s := NewSupervisor(func(int) (*exec.Cmd, error) { return sleepCmd() }, 0,
		func() bool { return healthy.Load() }, 2*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Start(ctx)
	healthy.Store(true)
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) && !s.Ready() {
		time.Sleep(10 * time.Millisecond)
	}
	if !s.Ready() {
		t.Fatal("supervisor should be ready once health is true")
	}
}

func TestSupervisorFallsBackWhenNeverHealthy(t *testing.T) {
	s := NewSupervisor(func(int) (*exec.Cmd, error) { return sleepCmd() }, 0,
		func() bool { return false }, 150*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Start(ctx)
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) && !s.FellBack() {
		time.Sleep(10 * time.Millisecond)
	}
	if !s.FellBack() {
		t.Fatal("never-healthy sidecar must fall back")
	}
}
```

- [ ] **Step 2: Run to verify fail** — `go test ./internal/agent/daemon/ -run Supervisor -v` → FAIL.

- [ ] **Step 3: Implement `supervisor.go`** (spawn → health-poll loop with readyTimeout + restart backoff; set atomic ready/fellBack; kill child on ctx.Done). Full implementation with `sync/atomic`, a poll ticker, restart counter (cap e.g. 3), and `cmd.Process.Kill()` on shutdown. Health polling uses the injected `health` func.

(Complete code omitted here for brevity in this planning excerpt — the implementer writes the struct with: `Start` spawns via `spawn(port)`, launches a goroutine that polls `health()` every 200ms until `readyTimeout`; on success sets `ready=true`; on child exit before ctx cancel, restart with exponential backoff up to 3 times; if readyTimeout elapses without health OR restarts exhausted, set `fellBack=true`; on `ctx.Done()` kill the process. Use `atomic.Bool` for `ready`/`fellBack`.)

> IMPLEMENTER NOTE: this is the one task with real concurrency. Write it against the two tests above, run with `-race`, and add a shutdown test asserting the child is killed on ctx cancel. Keep the state on `atomic.Bool`; guard the `*exec.Cmd` with a mutex.

- [ ] **Step 4: Run to verify pass** — `go test ./internal/agent/daemon/ -run Supervisor -race -v` → PASS.

- [ ] **Step 5: Commit** — `git commit -m "feat(daemon): sidecar supervisor with readiness gate + fallback"`

---

### Task 7: Wire router + supervisor + governor + backlog lifecycle into `daemon.Run`

**Files:** Modify `internal/agent/daemon/daemon.go`; Test `internal/agent/daemon/daemon_test.go`.

**Interfaces:** Consumes Tasks 1–6. Modifies `Worker` to accept the readiness gate so it **holds** (doesn't pull-and-deterministic) until `Ready() || FellBack()`, and routes via the `enrich.NewRouter(sidecar, deterministic, supervisor.Ready)` model. Governor gates ML admission/concurrency.

- [ ] **Step 1: Write the failing test** — a daemon-level test with a fake sidecar (httptest) + fake queue feeding one job, asserting: while not ready, the job is not published; once the stub is healthy, it's published with the sidecar result; and with `ml_backend:off` the deterministic path publishes immediately. (Use the existing `Sender` fake pattern.)

```go
// Sketch — implementer completes against real signatures:
// 1. start httptest sidecar stub (healthy) -> New(url) client
// 2. supervisor with spawn=noop cmd, health=client.Healthy
// 3. Worker(q, router, fakeSender, actor, false, gate)
// 4. Offer one job; assert fakeSender receives it with sidecar-derived profile
```

- [ ] **Step 2: Run to verify fail.**

- [ ] **Step 3: Implement** — change `Worker` signature to `Worker(q *queue.Queue, m enrich.Model, pub Sender, actor string, includeEntityText bool, ready func() bool)` and, before processing each job, block until `ready()` (poll with a short sleep or a readiness channel). In `Run`: load settings; if `set.MLEnabled()` and the sidecar binary exists (`sidecarBinPath()` present), build the sidecar client, supervisor, governor, and `router`; pass `router` + `supervisor gate` to `Worker`; else pass `enrich.NewDeterministic()` with an always-ready gate. Kick provisioning (`provision.EnsureModel`) in a goroutine before/at spawn. Governor sampling loop updates admission/concurrency. Ensure shutdown stops the sidecar (supervisor honors ctx).

> IMPLEMENTER NOTE: keep the deterministic path 100% unchanged when ML is off/absent (an always-ready gate + `NewDeterministic()`), so existing behavior + tests are preserved. The gate's "hold" must still exit promptly on `q.Close()` (check closed between polls) so shutdown isn't blocked.

- [ ] **Step 4: Run to verify pass** — `go test ./internal/agent/... -race` green; full `go test ./...` green.

- [ ] **Step 5: Commit** — `git commit -m "feat(daemon): wire sidecar backend, governor, backlog-hold lifecycle"`

---

### Task 8 (env): The FastAPI sidecar app

**Files:** Create `sidecar/app/main.py`, `sidecar/app/adapter.py`, `sidecar/requirements.txt`, `sidecar/README.md`.

Port the proven `inference-enrichment` sidecar (`/health`,`/classify`,`/entities`,`/extract` + `normalize_*` adapter) into keld-cli's `sidecar/`, model dir from env (`KELD_GLINER2_DIR`), `SIDECAR_MODEL=fastino/gliner2-large-v1`. Verification: run via docker/venv, `curl /health` → ok, `curl /extract` returns the contract (as validated in the spike). Build-tag/skip any Go test that needs it.

- [ ] Steps: copy+adapt main.py/adapter.py (reuse the spike-validated shapes); requirements (`fastapi`, `uvicorn[standard]`, `gliner2[local]`); README with dev run (`python:3.12` container or venv); manual smoke `curl` asserted; commit.

---

### Task 9 (env): Real HF fetcher + pinned manifest

**Files:** Create `internal/agent/enrich/sidecar/hf.go` (implements `provision.Fetcher` via HF Hub download at a pinned revision) + a `provision` manifest constant file (pinned revision + `model.safetensors` SHA256).

- [ ] Steps: implement `Fetch` (download the pinned-revision model files into destDir using the HF resolve URLs, e.g. `https://huggingface.co/fastino/gliner2-large-v1/resolve/<rev>/...`); record the real SHA256 (obtain by fetching once); wire `EnsureModel(dir, MANIFEST_SHA, hf.New(rev))` in `Run`. Live-download test build-tagged `//go:build hf_live` (skipped in normal CI). Commit.

---

### Task 10 (env): Eval gate vs live sidecar + gold-set expansion

**Files:** Create `internal/agent/enrich/eval/sidecar_eval_test.go` (`//go:build sidecar`); expand `internal/agent/enrich/eval/gold.jsonl` (8 → ~50–100 labeled prompts).

- [ ] Steps: `RunModel(sidecar.New(url), gold)` scored via the Task-1 `Score`; assert sidecar beats deterministic on task_type/domain accuracy and `sensitive_recall` ≥ deterministic (1.0); build-tagged so CI without the sidecar stays green; expand + hand-label the gold set; commit.

---

### Task 11 (env, DEFERRABLE to P3): Per-OS freeze + release wiring

**Files:** `sidecar/build/` (PyInstaller spec), `.goreleaser`/`install.sh` updates.

- [ ] Steps: PyInstaller spec freezing the sidecar per OS (bundling torch+gliner2); CI matrix (darwin/linux/windows); goreleaser adds the frozen `keld-agent-sidecar` artifact; `install.sh` places it beside `keld-agent`; daemon `sidecarBinPath()` resolves it. **This is deferrable to P3** (the runtime is complete + testable via the dev sidecar without it); document the deferral in the decision/handoff.

---

## Notes for the executor

- **Tasks 1–7 are pure Go, autonomous-executable** with no sidecar/model/Python — do these via subagent-driven development; they deliver the full runtime against fakes.
- **Tasks 8–11 are environment-dependent** (Python sidecar, live HF fetch, freeze) — run in a dev-sidecar session; Task 11 is deferrable to P3.
- Never change the deterministic default/behavior when ML is off or absent (always-ready gate + `NewDeterministic()`).
- Keep raw prompt text on the loopback daemon↔sidecar hop only; publishing still emits masked spans (unchanged).
- Run concurrency-touching tasks (6, 7) with `-race`.
