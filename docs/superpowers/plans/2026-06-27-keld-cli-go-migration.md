# Keld CLI → Go Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Python `keld` CLI (and its Python telemetry hook) with a single self-contained Go binary that needs nothing installed on the user's machine at install or runtime.

**Architecture:** Port the existing package layout 1:1 into Go `internal/` packages, preserving exact command/flag surface, config-file edits, and error messages. The telemetry hook becomes a hidden `keld __hook` subcommand of the same binary; `keld signal setup` writes `~/.keld/hook.json` (endpoint + ingest token) and wires tools to call `keld __hook --source <tool>` instead of `python3`. Ship per-platform binaries via GoReleaser.

**Tech Stack:** Go 1.22+, cobra (CLI), fatih/color (styled output), iancoleman/orderedmap (order-preserving JSON), pelletier/go-toml/v2 (TOML validate/read), pmezard/go-difflib (unified diff), pkg/browser (open browser), stdlib `net/http`/`crypto/sha256`/`os`. GoReleaser + GitHub Actions for release.

## How to use this plan

The existing Python source in `src/keld/` is the **behavioral reference** and stays in-tree until the final task. Each task names the Python file it ports and the exact Go signatures it must produce. Where logic is subtle (JSON key ordering, TOML block markers, device-flow polling, hook dedup) the Go code is given in full. Where the port is a mechanical 1:1 translation, exact signatures + full test code are given and you port the body from the cited Python source (which encodes the required behavior, including comments).

**Golden-parity rule:** for any config-file output, the Go binary must produce **byte-identical** text to the current Python CLI for the same input. Several tasks assert this with golden files captured from the Python CLI.

## Global Constraints

- Module path: `github.com/ncx-ai/keld-cli` (matches the GitHub repo). All internal imports use this prefix.
- Go version floor: `go 1.22` in `go.mod`.
- JSON output format: 2-space indent, single trailing `\n` (matches Python `json.dumps(obj, indent=2) + "\n"`). Existing keys keep their original order; new keys append. Use `orderedmap`, never `map[string]any`, for any JSON object that round-trips a user's file.
- Files written into user configs are written atomically (temp file in same dir + `os.Rename`).
- `~/.keld` is resolved via `KELD_HOME` env if set, else `$HOME/.keld`. Credential/secret files (`auth.json`, `hook.json`) are mode `0600`.
- API base URL precedence: `--api-url` flag override → `KELD_API_URL` env → `https://atlas.keld.co`. Trailing slash stripped.
- The hook must **never** block the host tool: `keld __hook` always exits 0.
- User-facing error type `KeldError`: the top-level runner prints `Error: <msg>` (bold red) to stderr and exits 1.
- Command/flag names, help strings, and printed messages must match the Python CLI exactly (verified against `README.md` and source). Do not "improve" copy.
- TDD: every task writes a failing test first, then minimal code, then commits. Conventional-commit messages.
- Co-author trailer on every commit: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.

## File structure (target)

```
keld-cli/
  go.mod
  go.sum
  cmd/keld/main.go                 # entrypoint → cli.Execute()
  internal/
    errs/errs.go                   # KeldError
    paths/paths.go                 # ~/.keld paths + api-base override
    console/console.go             # styled stdout/stderr + Fail
    config/
      merge_json.go                # order-preserving JSON helpers
      merge_toml.go                # keld-block markers, strip/upsert, validate
      writer.go                    # atomic write, delete-if-empty, backup
      manifest.go                  # Manifest / ToolManifest / HookRecord
    telemetry/telemetry.go         # otel env, gemini block, codex block, hook cmd
    tools/
      adapter.go                   # Adapter interface + Plan/SetupParams/ToolStatus
      claude.go  codex.go  gemini.go
      registry.go
    api/client.go                  # AtlasClient
    auth/
      store.go                     # AuthData load/save/clear
      device.go                    # device-flow login + require-auth
    diffview/diffview.go           # unified diff render
    hook/hook.go                   # ported telemetry hook (keld __hook)
    cli/
      root.go                      # cobra root, Execute(), KeldError handling
      login.go status.go setup.go uninstall.go hook.go   # command wiring
  testdata/golden/                 # golden config outputs captured from Python CLI
  .goreleaser.yaml
  .github/workflows/release.yml
  scripts/install.sh  scripts/install.ps1
```

Phases:
- **Phase 1 (Tasks 1–17):** core CLI port — `keld login/logout/whoami`, `keld signal setup/status/doctor/uninstall` working with full parity. Hook still references the Python path string in this phase only where unavoidable; replaced in Phase 2.
- **Phase 2 (Tasks 18–20):** self-contained `keld __hook`, `hook.json`, tool wiring switched off `python3`.
- **Phase 3 (Tasks 21–23):** GoReleaser pipeline, install scripts, retire Python package.

---

## Phase 1 — Core CLI port

### Task 1: Project scaffolding + cobra root

**Files:**
- Create: `go.mod`, `cmd/keld/main.go`, `internal/cli/root.go`, `internal/errs/errs.go`
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Produces: `errs.KeldError` (type `errs.Error struct{ Msg string }` implementing `error`; constructor `errs.New(format string, args ...any) error`); `cli.NewRootCmd() *cobra.Command`; `cli.Execute() int`.

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/root_test.go
package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootHelpListsSignalGroup(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("help failed: %v", err)
	}
	s := out.String()
	for _, want := range []string{"login", "logout", "whoami", "signal"} {
		if !strings.Contains(s, want) {
			t.Errorf("help missing %q\n%s", want, s)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestRootHelpListsSignalGroup`
Expected: FAIL — package/`NewRootCmd` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
// go.mod
module github.com/ncx-ai/keld-cli

go 1.22

require (
	github.com/fatih/color v1.17.0
	github.com/iancoleman/orderedmap v0.3.0
	github.com/pelletier/go-toml/v2 v2.2.2
	github.com/pkg/browser v0.0.0-20240102092130-5ac0b6a4141c
	github.com/pmezard/go-difflib v1.0.0
	github.com/spf13/cobra v1.8.1
)
```

```go
// internal/errs/errs.go
package errs

import "fmt"

// Error is a user-facing error; the CLI prints its message and exits non-zero.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

func New(format string, args ...any) error {
	return &Error{Msg: fmt.Sprintf(format, args...)}
}
```

```go
// internal/cli/root.go
package cli

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/ncx-ai/keld-cli/internal/errs"
)

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "keld",
		Short:         "Keld CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	// Auth commands are added in Task 14; signal group in later tasks.
	signal := &cobra.Command{
		Use:   "signal",
		Short: "Set up Keld Signal telemetry for your local AI coding tools.",
	}
	root.AddCommand(signal)
	return root
}

// Execute runs the CLI and returns a process exit code.
func Execute() int {
	root := NewRootCmd()
	if err := root.Execute(); err != nil {
		var ke *errs.Error
		if as(err, &ke) {
			color.New(color.FgRed, color.Bold).Fprint(os.Stderr, "Error: ")
			fmt.Fprintln(os.Stderr, ke.Msg)
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		return 1
	}
	return 0
}

func as(err error, target **errs.Error) bool {
	for err != nil {
		if e, ok := err.(*errs.Error); ok {
			*target = e
			return true
		}
		type unwrap interface{ Unwrap() error }
		u, ok := err.(unwrap)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
```

```go
// cmd/keld/main.go
package main

import (
	"os"

	"github.com/ncx-ai/keld-cli/internal/cli"
)

func main() { os.Exit(cli.Execute()) }
```

- [ ] **Step 4: Run test + build to verify pass**

Run: `go mod tidy && go test ./internal/cli/ -run TestRootHelpListsSignalGroup && go build ./...`
Expected: PASS; binary builds.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum cmd internal/errs internal/cli
git commit -m "feat(go): scaffold cobra root + KeldError runner

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: `paths` package

**Ports:** `src/keld/paths.py`.

**Files:**
- Create: `internal/paths/paths.go`
- Test: `internal/paths/paths_test.go`

**Interfaces:**
- Produces: `paths.SetAPIBaseOverride(url string)`, `paths.APIBaseOverride() string`, `paths.KeldHome() string`, `paths.AuthPath() string`, `paths.ManifestPath() string`, `paths.HookConfigPath() string` (→ `~/.keld/hook.json`), `paths.StateDir() string` (→ `~/.keld/state`), `paths.BackupsDir() string`, `paths.APIBase() string`. Const `paths.DefaultAPIURL = "https://atlas.keld.co"`.

Note: the Python `hook_path()` (`~/.keld/keld-context.py`) is intentionally **not** ported; the Go hook is the binary itself. `HookConfigPath()` and `StateDir()` are new.

- [ ] **Step 1: Write the failing test**

```go
// internal/paths/paths_test.go
package paths

import (
	"path/filepath"
	"testing"
)

func TestKeldHomeRespectsEnv(t *testing.T) {
	t.Setenv("KELD_HOME", "/tmp/kh")
	if KeldHome() != "/tmp/kh" {
		t.Fatalf("got %q", KeldHome())
	}
	if AuthPath() != filepath.Join("/tmp/kh", "auth.json") {
		t.Fatalf("auth path %q", AuthPath())
	}
}

func TestAPIBasePrecedence(t *testing.T) {
	t.Setenv("KELD_API_URL", "https://env.example/")
	SetAPIBaseOverride("")
	if APIBase() != "https://env.example" {
		t.Fatalf("env precedence wrong: %q", APIBase())
	}
	SetAPIBaseOverride("http://localhost:8000/")
	if APIBase() != "http://localhost:8000" {
		t.Fatalf("override precedence wrong: %q", APIBase())
	}
	if DefaultAPIURL != "https://atlas.keld.co" {
		t.Fatalf("default url wrong")
	}
	SetAPIBaseOverride("") // reset for other tests
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/paths/`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Write minimal implementation**

Port `paths.py`. Key Go translation (note: `SetAPIBaseOverride("")` clears the override, matching Python's `None`; track set-vs-clear with a bool):

```go
// internal/paths/paths.go
package paths

import (
	"os"
	"path/filepath"
	"strings"
)

const DefaultAPIURL = "https://atlas.keld.co"

var (
	apiOverride    string
	apiOverrideSet bool
)

func SetAPIBaseOverride(url string) {
	if url == "" {
		apiOverride, apiOverrideSet = "", false
		return
	}
	apiOverride, apiOverrideSet = strings.TrimRight(url, "/"), true
}

func APIBaseOverride() string { return apiOverride }

func KeldHome() string {
	if v := os.Getenv("KELD_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".keld")
}

func AuthPath() string       { return filepath.Join(KeldHome(), "auth.json") }
func ManifestPath() string   { return filepath.Join(KeldHome(), "manifest.json") }
func HookConfigPath() string { return filepath.Join(KeldHome(), "hook.json") }
func StateDir() string       { return filepath.Join(KeldHome(), "state") }
func BackupsDir() string     { return filepath.Join(KeldHome(), "backups") }

func APIBase() string {
	if apiOverrideSet {
		return apiOverride
	}
	if v := os.Getenv("KELD_API_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return DefaultAPIURL
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/paths/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/paths
git commit -m "feat(go): port paths package (~/.keld + api-base override)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: `console` package

**Ports:** `src/keld/console.py`.

**Files:**
- Create: `internal/console/console.go`
- Test: `internal/console/console_test.go`

**Interfaces:**
- Produces: `console.Out` / `console.Err` (writers, default `os.Stdout`/`os.Stderr`, overridable in tests); `console.Print(a ...any)`, `console.Printf(format string, a ...any)`, `console.Rule(title string)`, `console.Fail(msg string)` (prints `Error: <msg>` to Err and returns `*errs.Error` for the caller to return — Go has no `SystemExit`; callers `return console.Fail(...)`).

Note: `rich` markup (`[bold red]…[/]`) is replaced by `fatih/color`. Styling helpers live here so commands stay declarative. Color must auto-disable when output is not a TTY (color.NoColor handles this).

- [ ] **Step 1: Write the failing test**

```go
// internal/console/console_test.go
package console

import (
	"bytes"
	"strings"
	"testing"
)

func TestRuleContainsTitle(t *testing.T) {
	var buf bytes.Buffer
	Out = &buf
	color.NoColor = true
	Rule("Claude Code · /x")
	if !strings.Contains(buf.String(), "Claude Code · /x") {
		t.Fatalf("rule missing title: %q", buf.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/console/`
Expected: FAIL — undefined.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/console/console.go
package console

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/fatih/color"

	"github.com/ncx-ai/keld-cli/internal/errs"
)

var (
	Out io.Writer = os.Stdout
	Err io.Writer = os.Stderr
)

func Print(a ...any)                  { fmt.Fprintln(Out, a...) }
func Printf(format string, a ...any)  { fmt.Fprintf(Out, format, a...) }

// Rule renders a titled horizontal divider (parity with rich console.rule).
func Rule(title string) {
	const width = 80
	dashes := width - len(title) - 2
	if dashes < 4 {
		dashes = 4
	}
	left := dashes / 2
	right := dashes - left
	line := strings.Repeat("─", left) + " " + title + " " + strings.Repeat("─", right)
	color.New(color.Faint).Fprintln(Out, line)
}

func Fail(msg string) error {
	color.New(color.FgRed, color.Bold).Fprint(Err, "Error: ")
	fmt.Fprintln(Err, msg)
	return errs.New("%s", msg)
}
```

Add `import "github.com/fatih/color"` to the test file.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/console/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/console
git commit -m "feat(go): port console helpers (styled output + rule)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: JSON merge helpers (order-preserving)

**Ports:** the JSON half of `src/keld/config/merge.py` (`load_json`, `dump_json`, `merge_env`, `remove_section_keys`, `add_claude_hook`, `has_hook_with_command`, `remove_hooks_by_command`).

**Files:**
- Create: `internal/config/merge_json.go`
- Test: `internal/config/merge_json_test.go`

**Interfaces:**
- Produces:
  - `config.LoadJSON(text string) (*orderedmap.OrderedMap, error)` — empty/blank → empty map; invalid → `errs.New("existing config is not valid JSON: %v", err)`.
  - `config.DumpJSON(obj *orderedmap.OrderedMap) string` — 2-space indent + trailing `\n`.
  - `config.MergeEnv(obj *orderedmap.OrderedMap, env *orderedmap.OrderedMap) []string` — upserts into `"env"` sub-object, returns the env keys in order.
  - `config.RemoveSectionKeys(obj *orderedmap.OrderedMap, section string, keys []string)` — removes keys from sub-object; deletes the section if it becomes empty.
  - `config.AddClaudeHook(obj *orderedmap.OrderedMap, event string, matcher *string, command string)` — appends `{matcher?, hooks:[{type:command,command}]}` to `hooks[event]` if not already present (deep-equal check).
  - `config.HasHookWithCommand(obj *orderedmap.OrderedMap, substr string) bool` — substring search over JSON-serialized `hooks`.
  - `config.RemoveHooksByCommand(obj *orderedmap.OrderedMap, substr string)` — drops hook entries whose JSON contains substr; prunes empty events and empty `hooks`.

**Critical:** `orderedmap.OrderedMap` marshals in insertion order. `DumpJSON` must emit exactly Python's format. Python uses `json.dumps(indent=2)`: `", "`/`": "` separators collapse to `",\n"`/`": "` under indent, and **non-ASCII is escaped** (`ensure_ascii=True` default) — Go's `json.Marshal` also escapes `<`, `>`, `&` by default (`SetEscapeHTML(true)`), which Python does **not**. Disable HTML escaping to match Python. Use `json.MarshalIndent` via a buffer with `enc.SetEscapeHTML(false)`.

- [ ] **Step 1: Write the failing test**

```go
// internal/config/merge_json_test.go
package config

import (
	"testing"

	"github.com/iancoleman/orderedmap"
)

func TestDumpJSONFormatAndOrder(t *testing.T) {
	o := orderedmap.New()
	o.Set("b", "1")
	o.Set("a", "2")
	got := DumpJSON(o)
	want := "{\n  \"b\": \"1\",\n  \"a\": \"2\"\n}\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLoadJSONInvalid(t *testing.T) {
	if _, err := LoadJSON("{not json"); err == nil {
		t.Fatal("expected error")
	}
	o, err := LoadJSON("   ")
	if err != nil || len(o.Keys()) != 0 {
		t.Fatalf("blank should be empty map, got %v %v", o, err)
	}
}

func TestMergeEnvPreservesExistingOrder(t *testing.T) {
	o, _ := LoadJSON(`{"env":{"EXISTING":"x"}}`)
	env := orderedmap.New()
	env.Set("NEW", "y")
	keys := MergeEnv(o, env)
	if len(keys) != 1 || keys[0] != "NEW" {
		t.Fatalf("keys %v", keys)
	}
	if DumpJSON(o) != "{\n  \"env\": {\n    \"EXISTING\": \"x\",\n    \"NEW\": \"y\"\n  }\n}\n" {
		t.Fatalf("merge output:\n%s", DumpJSON(o))
	}
}

func TestRemoveSectionKeysDeletesEmptySection(t *testing.T) {
	o, _ := LoadJSON(`{"env":{"A":"1"}}`)
	RemoveSectionKeys(o, "env", []string{"A"})
	if DumpJSON(o) != "{}\n" {
		t.Fatalf("expected empty obj, got %s", DumpJSON(o))
	}
}

func TestClaudeHookAddIdempotentAndRemove(t *testing.T) {
	o := orderedmap.New()
	m := "startup"
	AddClaudeHook(o, "SessionStart", &m, "keld __hook --source claude_code")
	AddClaudeHook(o, "SessionStart", &m, "keld __hook --source claude_code") // dup → no-op
	AddClaudeHook(o, "CwdChanged", nil, "keld __hook --source claude_code")
	if !HasHookWithCommand(o, "keld __hook") {
		t.Fatal("expected hook present")
	}
	RemoveHooksByCommand(o, "keld __hook")
	if HasHookWithCommand(o, "keld __hook") {
		t.Fatal("expected hooks removed")
	}
	if len(o.Keys()) != 0 {
		t.Fatalf("hooks key should be pruned, keys=%v", o.Keys())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run JSON`
Expected: FAIL — undefined.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/config/merge_json.go
package config

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/iancoleman/orderedmap"

	"github.com/ncx-ai/keld-cli/internal/errs"
)

func LoadJSON(text string) (*orderedmap.OrderedMap, error) {
	o := orderedmap.New()
	if strings.TrimSpace(text) == "" {
		return o, nil
	}
	if err := json.Unmarshal([]byte(text), o); err != nil {
		return nil, errs.New("existing config is not valid JSON: %v", err)
	}
	return o, nil
}

func DumpJSON(obj *orderedmap.OrderedMap) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // match Python json.dumps (no &,<,> escaping)
	enc.SetIndent("", "  ")
	_ = enc.Encode(obj) // Encode appends a trailing newline, matching Python "+ \n"
	return buf.String()
}

func subMap(obj *orderedmap.OrderedMap, key string) *orderedmap.OrderedMap {
	if v, ok := obj.Get(key); ok {
		if sm, ok := v.(orderedmap.OrderedMap); ok {
			return &sm
		}
		if sm, ok := v.(*orderedmap.OrderedMap); ok {
			return sm
		}
	}
	return orderedmap.New()
}

func MergeEnv(obj *orderedmap.OrderedMap, env *orderedmap.OrderedMap) []string {
	sec := subMap(obj, "env")
	var keys []string
	for _, k := range env.Keys() {
		v, _ := env.Get(k)
		sec.Set(k, v)
		keys = append(keys, k)
	}
	obj.Set("env", sec)
	return keys
}

func RemoveSectionKeys(obj *orderedmap.OrderedMap, section string, keys []string) {
	v, ok := obj.Get(section)
	if !ok {
		return
	}
	sec, ok := asMap(v)
	if !ok {
		return
	}
	for _, k := range keys {
		sec.Delete(k)
	}
	if len(sec.Keys()) == 0 {
		obj.Delete(section)
	} else {
		obj.Set(section, sec)
	}
}
```

Implement the remaining helpers (`AddClaudeHook`, `HasHookWithCommand`, `RemoveHooksByCommand`, and the `asMap`/marshal-substring helpers) porting `merge.py`. The "entry not in arr" idempotency check in Python compares dict equality; in Go marshal each existing entry to canonical JSON and compare strings. `HasHookWithCommand`/`RemoveHooksByCommand` mirror Python's `substr in json.dumps(...)`.

```go
func asMap(v any) (*orderedmap.OrderedMap, bool) {
	switch m := v.(type) {
	case orderedmap.OrderedMap:
		return &m, true
	case *orderedmap.OrderedMap:
		return m, true
	}
	return nil, false
}

func marshalCompact(v any) string {
	b, _ := json.Marshal(v) // ordering: orderedmap preserves; plain maps sorted — fine for substring tests
	return string(b)
}
```

> Implementation note for `AddClaudeHook`: build the entry as an `*orderedmap.OrderedMap` with keys in the Python order — `matcher` (if non-nil) first, then `hooks` — so golden output matches. `hooks` value is `[]any{ map-with type,command }`; build those inner objects as ordered maps too (`type` then `command`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run JSON`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/merge_json.go internal/config/merge_json_test.go
git commit -m "feat(go): order-preserving JSON merge helpers

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: TOML block helpers

**Ports:** the TOML half of `src/keld/config/merge.py` (`KELD_TOML_START`/`END`, `has_keld_block`, `strip_keld_block`, `upsert_keld_block`, `validate_toml`, `strip_toml_table`).

**Files:**
- Create: `internal/config/merge_toml.go`
- Test: `internal/config/merge_toml_test.go`

**Interfaces:**
- Produces: const `config.KeldTOMLStart`, `config.KeldTOMLEnd`; `config.HasKeldBlock(text string) bool`; `config.StripKeldBlock(text string) string`; `config.UpsertKeldBlock(text, body string) string`; `config.ValidateTOML(text string) error`; `config.StripTOMLTable(text, table string) string`.

These are pure string/line operations except `ValidateTOML`. Port verbatim — the marker strings and line semantics must match exactly so already-installed blocks remain detectable.

```go
const (
	KeldTOMLStart = "# >>> keld (managed by keld CLI — do not edit between markers)"
	KeldTOMLEnd   = "# <<< keld"
)
```

`ValidateTOML` uses go-toml/v2:

```go
func ValidateTOML(text string) error {
	var v any
	if err := toml.Unmarshal([]byte(text), &v); err != nil {
		return errs.New("resulting TOML is invalid: %v", err)
	}
	return nil
}
```

- [ ] **Step 1: Write the failing test**

```go
// internal/config/merge_toml_test.go
package config

import "testing"

func TestUpsertAndStripRoundtrip(t *testing.T) {
	body := "[otel]\nenvironment = \"prod\"\n"
	out := UpsertKeldBlock("", body)
	if !HasKeldBlock(out) {
		t.Fatal("expected block present")
	}
	if err := ValidateTOML(out); err != nil {
		t.Fatalf("invalid toml: %v", err)
	}
	stripped := StripKeldBlock(out)
	if stripped != "" {
		t.Fatalf("expected empty after strip, got %q", stripped)
	}
}

func TestUpsertPreservesExistingContent() (*testing.T) { return nil } // placeholder removed below
```

Replace the placeholder with a real second test:

```go
func TestUpsertKeepsUserContent(t *testing.T) {
	existing := "[model]\nname = \"x\"\n"
	out := UpsertKeldBlock(existing, "[otel]\na = 1\n")
	if !HasKeldBlock(out) || !contains(out, "name = \"x\"") {
		t.Fatalf("must keep user content + block:\n%s", out)
	}
	// re-upsert replaces the prior block, not duplicates it
	out2 := UpsertKeldBlock(out, "[otel]\nb = 2\n")
	if count(out2, KeldTOMLStart) != 1 {
		t.Fatalf("expected single block after re-upsert:\n%s", out2)
	}
}

func TestStripTOMLTableDropsOnlyTable(t *testing.T) {
	in := "[keep]\nx=1\n[otel]\ny=2\n[otel.sub]\nz=3\n[after]\nw=4\n"
	out := StripTOMLTable(in, "otel")
	if contains(out, "[otel]") || contains(out, "[otel.sub]") {
		t.Fatalf("otel not fully stripped:\n%s", out)
	}
	if !contains(out, "[keep]") || !contains(out, "[after]") {
		t.Fatalf("dropped unrelated tables:\n%s", out)
	}
}
```

Add tiny `contains`/`count` helpers (or use `strings.Contains`/`strings.Count` directly).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TOML`
Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Port `strip_keld_block`/`upsert_keld_block`/`strip_toml_table` from `merge.py` line-for-line (they are `splitlines()`-based; in Go use `strings.Split(text, "\n")` and rejoin with `"\n"`, replicating the `rstrip("\n")` + conditional trailing newline logic exactly).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TOML`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/merge_toml.go internal/config/merge_toml_test.go
git commit -m "feat(go): TOML keld-block + table-strip helpers

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: `writer` (atomic write + backups)

**Ports:** `src/keld/config/writer.py`.

**Files:**
- Create: `internal/config/writer.go`
- Test: `internal/config/writer_test.go`

**Interfaces:**
- Produces: `config.WriteAtomic(path, text string, backup bool) error`; `config.DeleteIfEmpty(path, text string) (bool, error)` (true when text trims to `""`/`{}`, deleting the file); `config.BackupConfig(path, toolName string) (string, error)` (copies into `~/.keld/backups/<tool>/<name>`, one-time, returns "" if source missing or backup exists).

- [ ] **Step 1: Write the failing test**

```go
// internal/config/writer_test.go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomicAndDeleteIfEmpty(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "f.json")
	if err := WriteAtomic(p, "{\n  \"a\": 1\n}\n", false); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "{\n  \"a\": 1\n}\n" {
		t.Fatalf("content %q", b)
	}
	deleted, err := DeleteIfEmpty(p, "{}")
	if err != nil || !deleted {
		t.Fatalf("expected delete, got %v %v", deleted, err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("file should be gone")
	}
}

func TestBackupConfigOneTime(t *testing.T) {
	t.Setenv("KELD_HOME", t.TempDir())
	dir := t.TempDir()
	src := filepath.Join(dir, "settings.json")
	os.WriteFile(src, []byte("orig"), 0o644)
	first, err := BackupConfig(src, "claude_code")
	if err != nil || first == "" {
		t.Fatalf("first backup failed: %v %v", first, err)
	}
	second, _ := BackupConfig(src, "claude_code")
	if second != "" {
		t.Fatal("second backup should be no-op")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run "Atomic|Backup"`
Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Port `writer.py`: `WriteAtomic` = mkdir parent, optional one-time `.keld.bak` copy, write temp file in same dir, `os.Rename`. `DeleteIfEmpty` = trim in {"","{}"} → remove. `BackupConfig` = copy to `BackupsDir()/tool/name` if absent. Use `os.CreateTemp(dir, ".keld-*.tmp")`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run "Atomic|Backup"`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/writer.go internal/config/writer_test.go
git commit -m "feat(go): atomic config writer + one-time backups

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: `manifest`

**Ports:** `src/keld/config/manifest.py`. **Note the hook-record change** (Phase 2): `HookRecord` keeps `Path`/`Version`/`SHA256` fields for JSON compatibility with already-written manifests, but new writes set `Version` to the CLI version and `SHA256`/`Path` may be empty. Keep the struct fields so old manifests still load.

**Files:**
- Create: `internal/config/manifest.go`
- Test: `internal/config/manifest_test.go`

**Interfaces:**
- Produces: structs `HookRecord{Path,Version,SHA256 string}`, `ToolManifest{Name,ConfigPath string; Managed map[string]any; BackupPath *string}`, `Manifest{Endpoint,Actor *string; Tools map[string]ToolManifest; Hook *HookRecord}`; `config.LoadManifest() (*Manifest, error)`; `(*Manifest).Save() error`. JSON keys: `endpoint, actor, tools, hook`; per-tool `name, config_path, managed, backup_path`.

JSON field tags must match Python output exactly (snake_case, `indent=2` + trailing newline). `Managed` is free-form (`map[string]any`); preserve as-is. Tools map serialization: Python emits `tools` as an object keyed by tool name — use `map[string]ToolManifest` with `json:"tools"`.

- [ ] **Step 1: Write the failing test**

```go
// internal/config/manifest_test.go
package config

import "testing"

func TestManifestRoundTrip(t *testing.T) {
	t.Setenv("KELD_HOME", t.TempDir())
	ep := "https://atlas.keld.co"
	m := &Manifest{Endpoint: &ep, Tools: map[string]ToolManifest{}}
	m.Tools["claude_code"] = ToolManifest{
		Name: "claude_code", ConfigPath: "/x/settings.json",
		Managed: map[string]any{"created": true},
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if got.Endpoint == nil || *got.Endpoint != ep {
		t.Fatalf("endpoint lost: %v", got.Endpoint)
	}
	if _, ok := got.Tools["claude_code"]; !ok {
		t.Fatal("tool lost")
	}
}

func TestLoadManifestMissingReturnsEmpty(t *testing.T) {
	t.Setenv("KELD_HOME", t.TempDir())
	m, err := LoadManifest()
	if err != nil || len(m.Tools) != 0 {
		t.Fatalf("expected empty manifest, got %v %v", m, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run Manifest`
Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Port `manifest.py`. `LoadManifest`: missing file → `&Manifest{Tools: map[string]ToolManifest{}}`. `Save`: mkdir `KeldHome()`, `MarshalIndent` (escape-HTML off), trailing newline, write. Initialize `Tools` to non-nil empty map on load when absent.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run Manifest`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/manifest.go internal/config/manifest_test.go
git commit -m "feat(go): manifest load/save

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: `telemetry` snippets

**Ports:** `src/keld/telemetry.py`. **One change:** `HookCommand` no longer emits `python3 {path}; true`. It emits the binary-hook invocation. Add `HookCommand(source string) string` returning `keld __hook --source <source>`. Keep the OTEL env / gemini / codex body builders identical, but their embedded hook command now comes from `HookCommand`.

**Files:**
- Create: `internal/telemetry/telemetry.go`
- Test: `internal/telemetry/telemetry_test.go`

**Interfaces:**
- Produces: const `telemetry.HookCommandSubstr = "keld __hook"`; vars `telemetry.ClaudeHookEvents []struct{Event string; Matcher *string}` (`SessionStart/startup`, `SessionStart/resume`, `CwdChanged/nil`); `telemetry.CodexHookEvents = []string{"SessionStart","PreToolUse"}`; `telemetry.HookCommand(source string) string`; `telemetry.ClaudeEnv(p SetupParams) *orderedmap.OrderedMap`; `telemetry.GeminiTelemetry(p SetupParams) *orderedmap.OrderedMap`; `telemetry.CodexBlockBody(p SetupParams, source string) string`.

`SetupParams` (defined in Task 9's `tools` package) is imported here — to avoid an import cycle, define `SetupParams` in a small leaf package `internal/telemetry/params` OR define it in `telemetry` and have `tools` import it. **Decision:** define `SetupParams{Endpoint, IngestToken, Actor string}` in `telemetry` and have `tools` alias/use it. Update Task 9 interface accordingly.

**Important parity detail:** `ClaudeEnv` key order must be exactly: `CLAUDE_CODE_ENABLE_TELEMETRY, OTEL_LOGS_EXPORTER, OTEL_METRICS_EXPORTER, OTEL_EXPORTER_OTLP_PROTOCOL, OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_EXPORTER_OTLP_HEADERS`. The headers value is `x-keld-ingest-token={token},x-keld-actor={actor}`. Build with `orderedmap` to lock order.

`CodexBlockBody` must reproduce the exact TOML text from `telemetry.py:codex_block_body` (including the per-event `[[hooks.<event>]]` blocks built from `CodexHookEvents` and `HookCommand(source)`).

- [ ] **Step 1: Write the failing test**

```go
// internal/telemetry/telemetry_test.go
package telemetry

import (
	"strings"
	"testing"
)

func TestHookCommand(t *testing.T) {
	if HookCommand("claude_code") != "keld __hook --source claude_code" {
		t.Fatalf("got %q", HookCommand("claude_code"))
	}
}

func TestClaudeEnvOrderAndHeaders(t *testing.T) {
	p := SetupParams{Endpoint: "https://e", IngestToken: "tok", Actor: "me"}
	env := ClaudeEnv(p)
	keys := env.Keys()
	if keys[0] != "CLAUDE_CODE_ENABLE_TELEMETRY" || keys[len(keys)-1] != "OTEL_EXPORTER_OTLP_HEADERS" {
		t.Fatalf("env order wrong: %v", keys)
	}
	v, _ := env.Get("OTEL_EXPORTER_OTLP_HEADERS")
	if v.(string) != "x-keld-ingest-token=tok,x-keld-actor=me" {
		t.Fatalf("headers %q", v)
	}
}

func TestCodexBlockBodyHasHooksAndOtel(t *testing.T) {
	p := SetupParams{Endpoint: "https://e", IngestToken: "tok", Actor: "me"}
	body := CodexBlockBody(p, "codex")
	for _, want := range []string{"[otel]", "[[hooks.SessionStart]]", "[[hooks.PreToolUse]]", "keld __hook --source codex"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\n%s", want, body)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/telemetry/`
Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Port `telemetry.py`. Define `SetupParams` here. Build `ClaudeEnv`/`GeminiTelemetry` as `orderedmap`. `CodexBlockBody` mirrors the Python f-string exactly (use the same single/double-quote layout).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/telemetry/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/telemetry
git commit -m "feat(go): telemetry snippets (otel env, gemini, codex block) with binary-hook command

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: `tools` adapter interface + types

**Ports:** `src/keld/tools/base.py`.

**Files:**
- Create: `internal/tools/adapter.go`
- Test: covered via concrete adapters (Tasks 10–12); add a compile-time interface assertion test here.

**Interfaces:**
- Produces: type alias `tools.SetupParams = telemetry.SetupParams`; structs `Plan{Name string; ConfigPath string; AfterText string; Managed map[string]any; Summary []string; Changed bool; Conflict string}` (empty `Conflict` == no conflict; Python's `None`), `ToolStatus{Name string; Installed, Configured bool; Detail string}`; interface:

```go
type Adapter interface {
	Name() string
	DisplayName() string
	Detect() bool
	ConfigPath() string
	Apply(currentText *string, p SetupParams, replace bool) Plan
	Remove(currentText *string, managed map[string]any) Plan
	Status(currentText *string, managed map[string]any) ToolStatus
}
```

`currentText *string`: nil == file absent (Python `current_text: str | None`). `Apply`'s `replace` defaults to false at call sites.

- [ ] **Step 1: Write the failing test**

```go
// internal/tools/adapter_test.go
package tools

func _assertImplementations() {
	var _ Adapter = (*ClaudeAdapter)(nil)
	var _ Adapter = (*CodexAdapter)(nil)
	var _ Adapter = (*GeminiAdapter)(nil)
}
```

(This won't compile until Tasks 10–12 exist; that's expected — it's the cross-task contract. Run it after Task 12.)

- [ ] **Step 2: Write minimal implementation**

Define the types/interface above in `adapter.go`.

- [ ] **Step 3: Commit**

```bash
git add internal/tools/adapter.go internal/tools/adapter_test.go
git commit -m "feat(go): tool adapter interface + Plan/ToolStatus types

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: Claude adapter

**Ports:** `src/keld/tools/claude.py`. The hook command is now `telemetry.HookCommand("claude_code")`; the managed `hook_substr` is `telemetry.HookCommandSubstr`.

**Files:**
- Create: `internal/tools/claude.go`
- Test: `internal/tools/claude_test.go` + golden `testdata/golden/claude_apply.json`

**Interfaces:**
- Produces: `*ClaudeAdapter` with `Name()=="claude_code"`, `DisplayName()=="Claude Code"`, `ConfigPath()==~/.claude/settings.json`, `Detect()==parent dir exists`.

- [ ] **Step 1: Write the failing test (golden parity)**

```go
// internal/tools/claude_test.go
package tools

import (
	"os"
	"testing"
)

func TestClaudeApplyGolden(t *testing.T) {
	a := &ClaudeAdapter{}
	p := SetupParams{Endpoint: "https://atlas.keld.co", IngestToken: "tok", Actor: "me"}
	cur := "{\n  \"model\": \"x\"\n}\n"
	plan := a.Apply(&cur, p, false)
	if !plan.Changed || plan.Conflict != "" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	want, _ := os.ReadFile("testdata/golden/claude_apply.json")
	if plan.AfterText != string(want) {
		t.Fatalf("AFTER mismatch:\n--got--\n%s\n--want--\n%s", plan.AfterText, want)
	}
}

func TestClaudeStatusConfigured(t *testing.T) {
	a := &ClaudeAdapter{}
	cur, _ := os.ReadFile("testdata/golden/claude_apply.json")
	s := cur2 := string(cur)
	st := a.Status(&cur2, nil)
	_ = s
	if !st.Configured {
		t.Fatal("expected configured")
	}
}
```

Generate the golden file once from the **current Python CLI** so parity is anchored to real output:

```bash
# from repo root, with the Python venv active
python - <<'PY' > internal/tools/testdata/golden/claude_apply.json
from keld.tools.claude import ClaudeAdapter
from keld.tools.base import SetupParams
import sys
p = SetupParams(endpoint="https://atlas.keld.co", ingest_token="tok", actor="me")
plan = ClaudeAdapter().apply('{\n  "model": "x"\n}\n', p)
sys.stdout.write(plan.after_text)
PY
```

> Because Phase 1 still uses the Python `hook_command` (`python3 …`) while Go uses `keld __hook`, the hook **command string differs** between Python and Go output. To keep this golden meaningful in Phase 1, generate the golden by temporarily monkeypatching the Python `hook_command` to return `keld __hook --source claude_code`, OR (simpler) defer the byte-exact golden assertion to Phase 2 and in Phase 1 assert structural parity: env keys present in order, `hooks.SessionStart`/`CwdChanged` present, command contains `keld __hook`. **Choose structural assertions for Phase 1; add the byte-golden in Task 19.**

Revise the test to structural checks for Phase 1 (env order via parsing, hooks present, command substring). Keep the golden file generation script in the plan for Phase 2.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/ -run Claude`
Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Port `claude.py` using Task 4 helpers and `telemetry.ClaudeEnv` / `ClaudeHookEvents` / `HookCommand("claude_code")`. `managed` map keys: `env_keys` ([]string), `hook_substr` (`telemetry.HookCommandSubstr`), `created` (bool, `currentText==nil`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/ -run Claude`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/claude.go internal/tools/claude_test.go internal/tools/testdata
git commit -m "feat(go): Claude Code adapter

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 11: Codex adapter

**Ports:** `src/keld/tools/codex.py` — including the replace-safety logic (strip `[otel]`, re-validate, verify the strip altered *only* `[otel]` via a real TOML parse, fall back to a conflict otherwise).

**Files:**
- Create: `internal/tools/codex.go`
- Test: `internal/tools/codex_test.go`

**Interfaces:**
- Produces: `*CodexAdapter` (`name=="codex"`, `~/.codex/config.toml`).

Port detail for the replace-safety check (Python uses `tomllib.loads` twice and compares dicts): in Go, `toml.Unmarshal` both `current` (minus `otel`) and `stripped` into `map[string]any` and compare with `reflect.DeepEqual`; on mismatch or parse error, return the manual-resolution conflict Plan. Match the exact conflict message strings from `codex.py`.

- [ ] **Step 1: Write the failing test**

```go
// internal/tools/codex_test.go
package tools

import "testing"

func TestCodexApplyFreshAddsBlock(t *testing.T) {
	a := &CodexAdapter{}
	p := SetupParams{Endpoint: "https://e", IngestToken: "tok", Actor: "me"}
	plan := a.Apply(nil, p, false)
	if plan.Conflict != "" || !plan.Changed {
		t.Fatalf("fresh apply should succeed: %+v", plan)
	}
}

func TestCodexConflictOnExistingOtel(t *testing.T) {
	a := &CodexAdapter{}
	p := SetupParams{Endpoint: "https://e", IngestToken: "tok", Actor: "me"}
	cur := "[otel]\nexporter = \"otherthing\"\n"
	plan := a.Apply(&cur, p, false)
	if plan.Conflict == "" {
		t.Fatalf("expected conflict, got %+v", plan)
	}
	// replace=true should resolve by swapping just the [otel] table
	rep := a.Apply(&cur, p, true)
	if rep.Conflict != "" || !rep.Changed {
		t.Fatalf("replace should succeed: %+v", rep)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/ -run Codex`
Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Port `codex.py`. `managed` = `{"block": true, "created": currentText==nil}`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/ -run Codex`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/codex.go internal/tools/codex_test.go
git commit -m "feat(go): Codex adapter with replace-safety check

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 12: Gemini adapter + interface assertion

**Ports:** `src/keld/tools/gemini.py`.

**Files:**
- Create: `internal/tools/gemini.go`
- Test: `internal/tools/gemini_test.go`; enable `internal/tools/adapter_test.go` assertions.

**Interfaces:**
- Produces: `*GeminiAdapter` (`name=="gemini"`, `~/.gemini/settings.json`). `Apply` sets `obj["telemetry"] = telemetry.GeminiTelemetry(p)`; managed `{"keys":["telemetry"],"created":…}`. `Status` configured when `telemetry.otlpEndpoint` present.

- [ ] **Step 1: Write the failing test**

```go
// internal/tools/gemini_test.go
package tools

import (
	"strings"
	"testing"
)

func TestGeminiApplySetsTelemetry(t *testing.T) {
	a := &GeminiAdapter{}
	p := SetupParams{Endpoint: "https://e", IngestToken: "tok", Actor: "me"}
	cur := "{\n  \"theme\": \"dark\"\n}\n"
	plan := a.Apply(&cur, p, false)
	if !plan.Changed || !strings.Contains(plan.AfterText, "otlpEndpoint") || !strings.Contains(plan.AfterText, "\"theme\"") {
		t.Fatalf("telemetry not merged into existing config:\n%s", plan.AfterText)
	}
	st := a.Status(&plan.AfterText, nil)
	if !st.Configured {
		t.Fatal("expected configured")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/`
Expected: FAIL (gemini undefined; also the `adapter_test.go` assertions now compile).

- [ ] **Step 3: Write minimal implementation**

Port `gemini.py`.

- [ ] **Step 4: Run all tools tests**

Run: `go test ./internal/tools/`
Expected: PASS (all three adapters + interface assertion).

- [ ] **Step 5: Commit**

```bash
git add internal/tools/gemini.go internal/tools/gemini_test.go internal/tools/adapter_test.go
git commit -m "feat(go): Gemini adapter + adapter interface assertions

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 13: `registry`

**Ports:** `src/keld/tools/registry.py`.

**Files:**
- Create: `internal/tools/registry.go`
- Test: `internal/tools/registry_test.go`

**Interfaces:**
- Produces: `tools.All() []Adapter` (Claude, Codex, Gemini in that order); `tools.Get(name string) (Adapter, error)` (unknown → `errs.New("unknown tool '%s'. Known tools: %s", name, known)`); `tools.Select(names []string) ([]Adapter, error)` (names given → `Get` each; nil/empty → all that `Detect()`).

- [ ] **Step 1: Write the failing test**

```go
// internal/tools/registry_test.go
package tools

import "testing"

func TestGetUnknown(t *testing.T) {
	if _, err := Get("nope"); err == nil {
		t.Fatal("expected error")
	}
}

func TestSelectExplicit(t *testing.T) {
	got, err := Select([]string{"codex"})
	if err != nil || len(got) != 1 || got[0].Name() != "codex" {
		t.Fatalf("got %v %v", got, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/ -run "Get|Select"`
Expected: FAIL.

- [ ] **Step 3–5: Implement, test, commit**

Port `registry.py`.

```bash
git add internal/tools/registry.go internal/tools/registry_test.go
git commit -m "feat(go): tool registry (get/select)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 14: `api` client + auth wiring of `login/logout/whoami`

**Ports:** `src/keld/api/client.py` (minus `fetch_hook`, which is dropped — the hook is no longer downloaded) and `src/keld/auth/store.py`, plus `src/keld/commands/login.py`.

**Files:**
- Create: `internal/api/client.go`, `internal/auth/store.go`, `internal/cli/login.go`
- Test: `internal/api/client_test.go`, `internal/auth/store_test.go`

**Interfaces:**
- Produces:
  - `api.NewClient(baseURL string, token string) *api.Client` (token "" == none); `client.BaseURL`.
  - structs `api.DeviceStart{DeviceCode,UserCode,VerificationURL string; Interval,ExpiresIn int}` (json: `device_code,user_code,verification_url,interval,expires_in`), `api.Onboarding{Endpoint,IngestToken,Actor string}` (json: `endpoint,ingest_token,actor`).
  - `client.DeviceStart() (*DeviceStart, error)` (POST `/v1/cli/device/start`); `client.DevicePoll(deviceCode string) (map[string]any, error)` (POST `/v1/cli/device/poll`, 202 → nil,nil); `client.Onboarding() (*Onboarding, error)` (GET `/v1/cli/onboarding`, Bearer; needs token else `errs.New("onboarding requires authentication")`).
  - Network errors → `errs.New("network error contacting Atlas: %v", err)`; status ≥400 → `errs.New("Atlas returned %d: %s", code, body[:200])`.
  - `auth.AuthData{AccessToken,Principal,Org,APIURL string}` (json: `access_token,principal,org,api_url`); `auth.Save(AuthData) error` (0600), `auth.Load() (*AuthData, error)` (missing → nil,nil), `auth.Clear() (bool, error)`.
  - cobra commands: `cli.newLoginCmd()`, `cli.newLogoutCmd()`, `cli.newWhoamiCmd()`, registered on root in `root.go`.

`fetch_hook` is intentionally **not** ported.

- [ ] **Step 1: Write failing tests (httptest)**

```go
// internal/api/client_test.go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeviceStartAndPoll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/cli/device/start":
			w.Write([]byte(`{"device_code":"dc","user_code":"UC","verification_url":"https://v","interval":1,"expires_in":2}`))
		case "/v1/cli/device/poll":
			w.WriteHeader(202)
		}
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "")
	ds, err := c.DeviceStart()
	if err != nil || ds.UserCode != "UC" {
		t.Fatalf("device start %v %v", ds, err)
	}
	res, err := c.DevicePoll("dc")
	if err != nil || res != nil {
		t.Fatalf("202 should give nil,nil; got %v %v", res, err)
	}
}

func TestOnboardingRequiresToken(t *testing.T) {
	c := NewClient("https://x", "")
	if _, err := c.Onboarding(); err == nil {
		t.Fatal("expected auth error")
	}
}
```

```go
// internal/auth/store_test.go
package auth

import (
	"os"
	"testing"
)

func TestSaveLoadClear(t *testing.T) {
	t.Setenv("KELD_HOME", t.TempDir())
	if err := Save(AuthData{AccessToken: "t", Principal: "p", Org: "o", APIURL: "u"}); err != nil {
		t.Fatal(err)
	}
	got, _ := Load()
	if got == nil || got.Principal != "p" {
		t.Fatalf("load %v", got)
	}
	fi, _ := os.Stat(mustPath(t))
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("perm %v", fi.Mode())
	}
	ok, _ := Clear()
	if !ok {
		t.Fatal("clear should report removed")
	}
}
```

(Add a tiny `mustPath` test helper that returns `paths.AuthPath()`.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ ./internal/auth/`
Expected: FAIL.

- [ ] **Step 3: Implement client, store, and login/logout/whoami commands**

Port the three sources. Register the commands on the root in `root.go`. `whoami`: `auth.Load()`; nil → `console.Fail("not logged in (run `+"`keld login`"+`)")`; else print `<principal> · org <org> · <api_url>[ · endpoint <ep>]` where endpoint comes from the manifest.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/api/ ./internal/auth/ && go build ./...`
Expected: PASS, builds.

- [ ] **Step 5: Commit**

```bash
git add internal/api internal/auth internal/cli/login.go internal/cli/root.go
git commit -m "feat(go): Atlas API client, auth store, login/logout/whoami

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 15: `device` flow + `require-auth`

**Ports:** `src/keld/auth/device_flow.py`.

**Files:**
- Create: `internal/auth/device.go`
- Test: `internal/auth/device_test.go`

**Interfaces:**
- Produces: `auth.Login(c *api.Client, openBrowser bool, sleep func(time.Duration), opener func(string) error) (*AuthData, error)`; `auth.RequireAuth(noLogin bool, openBrowser bool) (*AuthData, error)`.

Inject `sleep`/`opener` for tests (mirrors Python's `sleep=`/`opener=` params). Poll loop: while `waited <= expiresIn`, `DevicePoll`; non-nil → build+save AuthData (`api_url = c.BaseURL`), print, return; else `sleep(interval)`, `waited += max(interval,1)`. Timeout → `errs.New("login timed out; please run `+"`keld login`"+` again")`. `RequireAuth`: existing → return; `noLogin` → error; else `Login(api.NewClient(paths.APIBase(), ""), …)`.

**Import cycle note:** `auth` importing `api` is fine; `api` must not import `auth`.

- [ ] **Step 1: Write the failing test**

```go
// internal/auth/device_test.go
package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ncx-ai/keld-cli/internal/api"
)

func TestLoginPollsThenSucceeds(t *testing.T) {
	t.Setenv("KELD_HOME", t.TempDir())
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/cli/device/start":
			w.Write([]byte(`{"device_code":"dc","user_code":"UC","verification_url":"https://v","interval":1,"expires_in":10}`))
		case "/v1/cli/device/poll":
			calls++
			if calls < 2 {
				w.WriteHeader(202)
				return
			}
			w.Write([]byte(`{"access_token":"AT","principal":"p","org":"o"}`))
		}
	}))
	defer srv.Close()
	got, err := Login(api.NewClient(srv.URL, ""), false, func(time.Duration) {}, func(string) error { return nil })
	if err != nil || got.AccessToken != "AT" {
		t.Fatalf("login %v %v", got, err)
	}
}
```

- [ ] **Step 2–4: Fail, implement, pass**

Run: `go test ./internal/auth/ -run Login`

- [ ] **Step 5: Commit**

```bash
git add internal/auth/device.go internal/auth/device_test.go
git commit -m "feat(go): device-flow login + require-auth

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 16: `diffview`

**Ports:** `src/keld/diffview.py`.

**Files:**
- Create: `internal/diffview/diffview.go`
- Test: `internal/diffview/diffview_test.go`

**Interfaces:**
- Produces: `diffview.Render(before *string, after, label string)` — unified diff via go-difflib, colored per line (`+`→green, `-`→red, `@@`→cyan, else dim), `+++`/`---` excluded from add/remove coloring. Writes to `console.Out`.

Use `difflib.GetUnifiedDiffString(difflib.UnifiedDiff{A: split(before), B: split(after), FromFile:"a/"+label, ToFile:"b/"+label, Context: 3})` then colorize line by line. Match Python's keepends behavior (difflib handles this).

- [ ] **Step 1: Write the failing test**

```go
// internal/diffview/diffview_test.go
package diffview

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/ncx-ai/keld-cli/internal/console"
)

func TestRenderShowsAddedLine(t *testing.T) {
	var buf bytes.Buffer
	console.Out = &buf
	color.NoColor = true
	before := "a\n"
	Render(&before, "a\nb\n", "f")
	if !strings.Contains(buf.String(), "+b") {
		t.Fatalf("diff missing added line:\n%s", buf.String())
	}
}
```

- [ ] **Step 2–4: Fail, implement, pass**

Run: `go test ./internal/diffview/`

- [ ] **Step 5: Commit**

```bash
git add internal/diffview
git commit -m "feat(go): unified diff renderer

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 17: `setup`, `status`, `doctor`, `uninstall` commands

**Ports:** `src/keld/commands/setup.py`, `status.py`, `uninstall.py`. This is the largest task — the interactive setup flow, conflict resolution, status/doctor, and uninstall. **In Phase 1, `setup` still references the Python-era hook installation only via the manifest `HookRecord`; the actual hook writing (`hook.json`) and `install_hook` replacement land in Task 18.** For Phase 1, implement setup writing tool configs + manifest, and stub the hook record as `&HookRecord{Version: version.CLI}` with a `// TODO(Task18): write hook.json` marker — *but the task is not "done" until Task 18 fills it*. To keep Phase 1 independently testable, gate the hook write behind Task 18 and assert tool-config behavior here.

**Files:**
- Create: `internal/cli/setup.go`, `internal/cli/status.go`, `internal/cli/uninstall.go`, `internal/version/version.go`
- Test: `internal/cli/setup_test.go`, `internal/cli/status_test.go`, `internal/cli/uninstall_test.go`

**Interfaces:**
- Produces: cobra commands `newSetupCmd`, `newStatusCmd`, `newDoctorCmd`, `newUninstallCmd`, registered under the `signal` group; a testable core `runSetup(adapters []tools.Adapter, p tools.SetupParams, client *api.Client, ob *api.Onboarding, opts SetupOpts) (*config.Manifest, error)` mirroring Python `_run_setup`, with injectable `Confirm func(string) bool` and `ResolveConflict func(a tools.Adapter, plan tools.Plan) string` ("skip"/"replace"/"abort"). `SetupOpts{DryRun, Yes, ShowDiff bool}`.
- `runUninstall(m *config.Manifest, names []string, yes bool, confirm func(string) bool) error` mirroring Python `_run_uninstall`.
- `version.CLI` constant (e.g. `"0.2.0"`).

This task ports a lot of branching logic; port `_run_setup`/`_run_uninstall`/`status`/`doctor` faithfully (conflict prompt strings, "already configured", "Nothing to apply", "Aborted.", the per-tool `✓`, backup messages, the final "Setup complete…" line). Use Task 16 `diffview` for `--diff` and replace.

- [ ] **Step 1: Write the failing tests**

```go
// internal/cli/setup_test.go
package cli

import (
	"testing"

	"github.com/ncx-ai/keld-cli/internal/config"
	"github.com/ncx-ai/keld-cli/internal/tools"
)

// fakeAdapter lets us test runSetup without touching real config paths.
type fakeAdapter struct {
	name string
	plan tools.Plan
}

func (f *fakeAdapter) Name() string        { return f.name }
func (f *fakeAdapter) DisplayName() string  { return f.name }
func (f *fakeAdapter) Detect() bool         { return true }
func (f *fakeAdapter) ConfigPath() string   { return f.plan.ConfigPath }
func (f *fakeAdapter) Apply(_ *string, _ tools.SetupParams, _ bool) tools.Plan { return f.plan }
func (f *fakeAdapter) Remove(_ *string, _ map[string]any) tools.Plan          { return f.plan }
func (f *fakeAdapter) Status(_ *string, _ map[string]any) tools.ToolStatus {
	return tools.ToolStatus{Name: f.name, Installed: true, Configured: true}
}

func TestRunSetupDryRunWritesNothing(t *testing.T) {
	t.Setenv("KELD_HOME", t.TempDir())
	dir := t.TempDir()
	fa := &fakeAdapter{name: "x", plan: tools.Plan{
		Name: "x", ConfigPath: dir + "/c.json", AfterText: "{}\n", Changed: true,
		Managed: map[string]any{"created": true}, Summary: []string{"do thing"},
	}}
	opts := SetupOpts{DryRun: true}
	m, err := runSetup([]tools.Adapter{fa}, tools.SetupParams{}, nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	_ = m
	if _, err := config.LoadManifest(); err != nil {
		t.Fatalf("manifest load: %v", err)
	}
	// dry-run must not create the config file
	if fileExists(dir + "/c.json") {
		t.Fatal("dry-run wrote a file")
	}
}
```

```go
// internal/cli/uninstall_test.go — assert it removes tool entries and clears manifest when empty
```

(Write the uninstall and status tests analogously, using `fakeAdapter` and a temp `KELD_HOME`. Add a `fileExists` helper.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/`
Expected: FAIL.

- [ ] **Step 3: Implement commands + cores**

Port `_run_setup`, `_run_uninstall`, `status`, `doctor`. Wire `setup`/`status`/`doctor`/`uninstall` under the `signal` group in `root.go`. For the real `setup` command's `ResolveConflict`, prompt via stdin (port `_default_resolve_conflict`); for `Confirm`, port `typer.confirm` (read y/N from stdin).

- [ ] **Step 4: Run tests**

Run: `go test ./... && go build ./...`
Expected: PASS, builds.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/setup.go internal/cli/status.go internal/cli/uninstall.go internal/version internal/cli/*_test.go internal/cli/root.go
git commit -m "feat(go): signal setup/status/doctor/uninstall commands

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

**End of Phase 1 checkpoint:** `go build -o keld ./cmd/keld` then manually run `./keld --help`, `./keld signal --help`, and against a local Atlas (`./keld login --api-url http://localhost:8000`) verify login + setup edit tool configs identically to the Python CLI.

---

## Phase 2 — Self-contained hook

### Task 18: `keld __hook` subcommand + `hook.json` wiring

**Ports:** `keld-atlas/services/api/app/telemetry_hook.py` (logic) into Go; replaces `src/keld/hook.py` + the `python3` wiring.

**Files:**
- Create: `internal/hook/hook.go`, `internal/cli/hook.go`
- Modify: `internal/cli/setup.go` (write `~/.keld/hook.json`, set `HookRecord`), `internal/cli/uninstall.go` (remove `hook.json` + `StateDir()`)
- Test: `internal/hook/hook_test.go`

**Interfaces:**
- Produces:
  - `hook.Config{Endpoint, IngestToken string}` with `hook.LoadConfig() (*Config, error)` (reads `~/.keld/hook.json`; env `KELD_CTX_ENDPOINT`/`KELD_CTX_TOKEN` override).
  - `hook.Run(source string, stdin io.Reader, stderr io.Writer, now time.Time) int` — the full hook; always returns 0.
  - helpers (exported for tests): `hook.NormalizeRemote(url string) string`, `hook.DeriveRepo(cwd string) string`, `hook.ReadAttributes(cwd string) map[string]string`, `hook.ChangedSinceLast(sessionID, repo string, attrs map[string]string) bool`, `hook.IsDev(endpoint string) bool`.
  - cobra hidden command `__hook` with `--source` flag calling `hook.Run`.
- `setup` writes `hook.json` (0600) via a new `config.SaveHookConfig(endpoint, token string) error`; sets `manifest.Hook = &HookRecord{Version: version.CLI}`.

Port behavior exactly (see spec §"What the hook does"): session-id resolution, `cwd`, repo derivation with 2s git timeouts (`exec.CommandContext` + `context.WithTimeout`), `.keld.toml` `[keld]` scalar extraction (go-toml/v2), sha256 dedup signature file under `StateDir()`, 3s POST, never block, dev/prod stderr behavior, error summary (`HTTP <code>` / reason / type name). Timestamp: RFC3339 UTC from injected `now`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/hook/hook_test.go
package hook

import "testing"

func TestNormalizeRemoteVariants(t *testing.T) {
	cases := map[string]string{
		"git@github.com:acme/api.git":        "github.com/acme/api",
		"https://github.com/acme/api.git":    "github.com/acme/api",
		"https://user:tok@github.com/a/b":    "github.com/a/b",
		"ssh://git@github.com/a/b.git":       "github.com/a/b",
	}
	for in, want := range cases {
		if got := NormalizeRemote(in); got != want {
			t.Errorf("NormalizeRemote(%q)=%q want %q", in, got, want)
		}
	}
}

func TestReadAttributesScalarsOnly(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/.keld.toml", []byte("[keld]\nteam = \"core\"\ncost = 5\n[keld.nested]\nx=1\n"), 0o644)
	a := ReadAttributes(dir)
	if a["team"] != "core" || a["cost"] != "5" {
		t.Fatalf("attrs %v", a)
	}
	if _, ok := a["nested"]; ok {
		t.Fatal("non-scalar leaked")
	}
}

func TestChangedSinceLastDedup(t *testing.T) {
	t.Setenv("KELD_HOME", t.TempDir())
	if !ChangedSinceLast("s1", "repo", map[string]string{"a": "1"}) {
		t.Fatal("first call should be changed")
	}
	if ChangedSinceLast("s1", "repo", map[string]string{"a": "1"}) {
		t.Fatal("identical second call should be unchanged")
	}
}

func TestRunNeverBlocksWithoutConfig(t *testing.T) {
	t.Setenv("KELD_HOME", t.TempDir())
	t.Setenv("KELD_CTX_ENDPOINT", "")
	t.Setenv("KELD_CTX_TOKEN", "")
	if code := Run("claude_code", strings.NewReader("{}"), io.Discard, time.Unix(0, 0).UTC()); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
}
```

(Add necessary imports: `os`, `io`, `strings`, `time`.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/hook/`
Expected: FAIL.

- [ ] **Step 3: Implement the hook + wire setup/uninstall**

Implement `hook.go` per the spec. Add `config.SaveHookConfig`. In `setup.go`, after approval, write `hook.json` and set the manifest hook record; in `uninstall.go`, when all tools removed, delete `hook.json` and `StateDir()`. Register hidden `__hook` command.

- [ ] **Step 4: Run tests**

Run: `go test ./... && go build ./...`
Expected: PASS, builds.

- [ ] **Step 5: Commit**

```bash
git add internal/hook internal/cli/hook.go internal/cli/setup.go internal/cli/uninstall.go internal/config
git commit -m "feat(go): self-contained keld __hook + hook.json wiring

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 19: Byte-golden config parity tests

**Files:**
- Create: `internal/tools/testdata/golden/{claude_apply.json,codex_apply.toml,gemini_apply.json}`, `internal/tools/golden_test.go`
- Modify: tool tests to assert byte-equality where deferred from Phase 1.

**Interfaces:** none (test-only). Capture goldens from the **Python CLI with its `hook_command` patched to `keld __hook --source <tool>`** so Go and Python agree on the only line that changed by design:

```bash
python - <<'PY' > internal/tools/testdata/golden/codex_apply.toml
import keld.telemetry as tmod
tmod.hook_command = lambda _p: "keld __hook --source codex"   # match Go binary-hook
from keld.tools.codex import CodexAdapter
from keld.tools.base import SetupParams
import sys
p = SetupParams(endpoint="https://atlas.keld.co", ingest_token="tok", actor="me")
sys.stdout.write(CodexAdapter().apply(None, p).after_text)
PY
```

(Generate claude/gemini goldens analogously; gemini has no hook command so it's a straight capture.)

- [ ] **Step 1: Write the failing golden test**

```go
// internal/tools/golden_test.go
package tools

import (
	"os"
	"testing"
)

func TestGoldenParity(t *testing.T) {
	p := SetupParams{Endpoint: "https://atlas.keld.co", IngestToken: "tok", Actor: "me"}
	t.Run("codex", func(t *testing.T) {
		want, _ := os.ReadFile("testdata/golden/codex_apply.toml")
		got := (&CodexAdapter{}).Apply(nil, p, false).AfterText
		if got != string(want) {
			t.Fatalf("codex mismatch:\n--got--\n%s\n--want--\n%s", got, want)
		}
	})
	// claude + gemini subtests analogously
}
```

- [ ] **Step 2: Run to verify it fails (or reveals real drift)**

Run: `go test ./internal/tools/ -run Golden`
Expected: FAIL until output matches byte-for-byte. Fix any formatting drift in the Go adapters/helpers until green. **Any diff here is a real parity bug to fix, not a golden to loosen.**

- [ ] **Step 3: Commit**

```bash
git add internal/tools/testdata/golden internal/tools/golden_test.go
git commit -m "test(go): byte-golden config parity vs python CLI

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 20: Remove Python source

**Files:**
- Delete: `src/`, `pyproject.toml`, `tests/` (Python), `.venv/` references, `dist/` Python artifacts.
- Modify: `README.md` (install via binary), `.gitignore`.

**Interfaces:** none.

Do this only after Task 19 is green and the manual checkpoint passed. Keep `docs/`.

- [ ] **Step 1: Verify full Go suite green**

Run: `go test ./... && go vet ./...`
Expected: PASS.

- [ ] **Step 2: Remove Python, update README**

Replace the Install section with binary install (`curl … | sh`, Homebrew, direct download); update Usage if any command text changed (`keld __hook` is hidden, so user-facing usage is unchanged).

- [ ] **Step 3: Commit**

```bash
git rm -r src tests pyproject.toml
git add README.md .gitignore
git commit -m "chore: remove Python implementation; Go is now the keld CLI

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Phase 3 — Distribution

### Task 21: GoReleaser config + version stamping

**Files:**
- Create: `.goreleaser.yaml`
- Modify: `internal/version/version.go` (ldflags-injected `var CLI string`), `cmd/keld/main.go` (optional `--version`)
- Test: `go run` smoke + `goreleaser check`

**Interfaces:** `keld --version` prints the version.

- [ ] **Step 1: Write `.goreleaser.yaml`**

```yaml
version: 2
project_name: keld
before:
  hooks:
    - go mod tidy
builds:
  - id: keld
    main: ./cmd/keld
    binary: keld
    env: [CGO_ENABLED=0]
    ldflags:
      - -s -w -X github.com/ncx-ai/keld-cli/internal/version.CLI={{.Version}}
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
    ignore:
      - goos: windows
        goarch: arm64
archives:
  - id: keld
    format_overrides:
      - goos: windows
        format: zip
    name_template: "keld_{{ .Os }}_{{ .Arch }}"
checksum:
  name_template: "checksums.txt"
release:
  github:
    owner: ncx-ai
    name: keld-cli
```

Make `version.CLI` a plain `var CLI = "dev"` so ldflags can override it.

- [ ] **Step 2: Validate**

Run: `goreleaser check && goreleaser build --snapshot --clean --single-target`
Expected: config valid; a local snapshot binary builds.

- [ ] **Step 3: Commit**

```bash
git add .goreleaser.yaml internal/version/version.go cmd/keld/main.go
git commit -m "build: goreleaser config + version stamping

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 22: GitHub Actions release workflow

**Files:**
- Create: `.github/workflows/release.yml`, `.github/workflows/ci.yml`

**Interfaces:** tag `v*` → cross-compiled release with all archives + checksums.

- [ ] **Step 1: Write CI + release workflows**

```yaml
# .github/workflows/ci.yml
name: ci
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.22" }
      - run: go vet ./...
      - run: go test ./...
```

```yaml
# .github/workflows/release.yml
name: release
on:
  push:
    tags: ["v*"]
permissions:
  contents: write
jobs:
  goreleaser:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }
      - uses: actions/setup-go@v5
        with: { go-version: "1.22" }
      - uses: goreleaser/goreleaser-action@v6
        with: { version: "~> v2", args: "release --clean" }
        env: { GITHUB_TOKEN: "${{ secrets.GITHUB_TOKEN }}" }
```

- [ ] **Step 2: Verify YAML**

Run: `goreleaser check` (already covers config); push a branch and confirm `ci.yml` passes on GitHub.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows
git commit -m "ci: test workflow + goreleaser release on tag

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 23: Install scripts + Integrations docs

**Files:**
- Create: `scripts/install.sh`, `scripts/install.ps1`
- Modify: `README.md` (install instructions)

**Interfaces:** `curl -fsSL <url>/install.sh | sh` detects OS/arch, downloads the matching archive from the latest GitHub release, extracts `keld` to `~/.local/bin` (or `/usr/local/bin`), `chmod +x`.

- [ ] **Step 1: Write `install.sh`**

```sh
#!/bin/sh
set -e
REPO="ncx-ai/keld-cli"
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac
tag=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep -o '"tag_name": *"[^"]*"' | head -1 | cut -d'"' -f4)
url="https://github.com/$REPO/releases/download/$tag/keld_${os}_${arch}.tar.gz"
dest="${HOME}/.local/bin"
mkdir -p "$dest"
echo "Downloading keld $tag ($os/$arch)…"
curl -fsSL "$url" | tar -xz -C "$dest" keld
chmod +x "$dest/keld"
echo "Installed to $dest/keld. Ensure $dest is on your PATH."
```

- [ ] **Step 2: Write `install.ps1`** (Windows analogue: detect arch, download `keld_windows_amd64.zip`, expand to `$env:LOCALAPPDATA\Programs\keld`, add to PATH).

- [ ] **Step 3: Smoke test the script logic**

Run (after a real release exists, or against a mocked release): `sh -n scripts/install.sh` (syntax check) and a manual end-to-end once the first tag is published.

- [ ] **Step 4: Update README + Integrations page copy**

Replace `pipx install keld` with the one-line install and direct-download table. Note the macOS Gatekeeper / Windows SmartScreen caveat and that signing is a follow-up.

- [ ] **Step 5: Commit**

```bash
git add scripts README.md
git commit -m "build: cross-platform install scripts + binary install docs

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Follow-ups (out of scope, tracked)

- macOS notarization + Windows code-signing in GoReleaser (needs Apple/Windows certs) — removes Gatekeeper/SmartScreen warnings.
- Homebrew tap + Scoop manifest via GoReleaser.
- Retire PyPI `keld` package (yank or leave as a thin shim pointing at the binary installer).
- Backend: eventually stop templating/serving `/v1/tool-context/hook.py` once no installed Python hooks remain.

## Self-review

- **Spec coverage:** scaffolding/cobra (T1) ✓; paths+api-override (T2) ✓; console (T3) ✓; JSON ordered-map parity incl. HTML-escape fix (T4) ✓; TOML block/strip (T5) ✓; writer+backups (T6) ✓; manifest incl. HookRecord change (T7) ✓; telemetry incl. binary HookCommand (T8) ✓; adapter iface (T9) ✓; claude/codex(replace-safety)/gemini (T10–12) ✓; registry (T13) ✓; api client minus fetch_hook + auth store + login/logout/whoami (T14) ✓; device flow (T15) ✓; diffview (T16) ✓; setup/status/doctor/uninstall (T17) ✓; self-contained hook + hook.json + tool wiring (T18) ✓; byte-golden parity (T19) ✓; remove Python (T20) ✓; goreleaser (T21) ✓; CI/release workflows cross-compile all targets (T22) ✓; install scripts + docs (T23) ✓. All spec sections map to a task.
- **Placeholder scan:** the only deliberate deferral (claude byte-golden) is explicitly moved to T19 with structural assertions in T10; no "TBD/handle edge cases" left. The `TestUpsertPreservesExistingContent` placeholder in T5 is explicitly replaced with real tests in the same step.
- **Type consistency:** `SetupParams` defined once in `telemetry` and aliased by `tools` (T8/T9) — resolves the cycle and keeps one type. `Adapter` signatures in T9 match calls in T10–13, T17. `HookRecord` fields stable across T7/T18. `Plan.Conflict` is `string` (empty == none) consistently. `runSetup`/`runUninstall` signatures in T17 match their tests.
