# keld-agent P4 — Org Remote Control-Plane Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** keld-agent daemons poll a per-org settings document from keld-atlas and apply it live (remote overrides local per key), so an admin can govern daemon behavior org-wide — starting with `include_entity_text`.

**Architecture:** Client-first. keld-cli gains a concurrency-safe live settings holder (replacing the static `includeEntityText` bool), an HTTP settings client, and a poller wired into `daemon.Run` (all local/TDD). Then keld-atlas gains a per-org settings model + `GET /v1/enrichment-settings` (token→org) + an admin set API/toggle.

**Tech Stack:** Go (`github.com/ncx-ai/keld-cli`), stdlib `net/http`/`httptest`, `sync`; keld-atlas (FastAPI + async SQLAlchemy + Alembic + Next/Vitest, in Docker).

## Global Constraints

- **Client-first.** keld-cli tasks (T1–T3) are fully local + TDD. keld-atlas tasks (T4–T6) run that repo's suite in Docker and **coordinate with the shared tree** (another session's unpushed wizard work is on atlas `main`; branch cleanly, touch only new files + a minimal additive settings-page change; hold if that session is editing the same areas).
- **Governance: remote overrides local, per key present.** Effective = local base, with each remote key that is present overlaid on top; a remote doc that omits a key reverts that key to local. (`Remote` fields are pointers so "absent" ≠ "false".)
- **Non-fatal client.** Any fetch error (network, 404 on older Atlas, decode) → keep last-known effective settings; never break the daemon. The endpoint may not exist yet during client rollout.
- **Poll-only, org-wide.** No push/websockets; no per-key enforced flag; no device targeting. Poll on startup + every 5m (`KELD_SETTINGS_POLL` overrides, for tests).
- **Daemon→Atlas auth header is `x-keld-ingest-token: <token>`** (mirror the publisher). The settings GET is read-only, org-scoped by that token.
- **Extensible + forward-compatible.** JSON doc `{"include_entity_text": bool, ...}`; the client applies only keys it knows and ignores unknown keys. Only `include_entity_text` this phase.
- Tenant isolation on the server (org-scoped everywhere).

## File Structure
- `internal/agent/settings/remote.go` (NEW) — `Remote` wire type (pointer fields).
- `internal/agent/settings/live.go` (NEW) — `Live` concurrency-safe effective-settings holder.
- `internal/agent/settings/client.go` (NEW) — HTTP `Client` fetching the org settings.
- `internal/agent/daemon/daemon.go` (MOD) — `settingsEndpoint`, live-value `Worker`, poller.
- keld-atlas: `models.py` (+ migration), `routers/agent_settings.py`, admin API + Settings-page toggle.

---

### Task 1: Live settings holder + Remote type (keld-cli, LOCAL/TDD)

**Files:** Create `internal/agent/settings/remote.go`, `internal/agent/settings/live.go`; Test `internal/agent/settings/live_test.go`.

**Interfaces:**
- Produces: `type Remote struct { IncludeEntityText *bool `json:"include_entity_text"` }`
- `type Live struct{...}`; `func NewLive(base Settings) *Live`; `func (l *Live) IncludeEntityText() bool`; `func (l *Live) Apply(r *Remote)` — recomputes effective from the local `base` with the remote overlaid (present keys only). Concurrency-safe.

- [ ] **Step 1: Write the failing test**

```go
package settings

import (
	"sync"
	"testing"
)

func ptrBool(b bool) *bool { return &b }

func TestLiveRemoteOverridesLocalPerKey(t *testing.T) {
	l := NewLive(Settings{IncludeEntityText: true}) // local base = true
	if !l.IncludeEntityText() {
		t.Fatal("base should be true before any Apply")
	}
	l.Apply(&Remote{IncludeEntityText: ptrBool(false)}) // remote present → overrides
	if l.IncludeEntityText() {
		t.Fatal("remote false should override local true")
	}
	l.Apply(&Remote{}) // remote omits the key → revert to local base (true)
	if !l.IncludeEntityText() {
		t.Fatal("absent remote key should revert to local base")
	}
	l.Apply(nil) // nil remote → local base
	if !l.IncludeEntityText() {
		t.Fatal("nil remote → local base")
	}
}

func TestLiveConcurrentApplyAndRead(t *testing.T) {
	l := NewLive(Settings{IncludeEntityText: true})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); for i := 0; i < 1000; i++ { l.Apply(&Remote{IncludeEntityText: ptrBool(i%2 == 0)}) } }()
	go func() { defer wg.Done(); for i := 0; i < 1000; i++ { _ = l.IncludeEntityText() } }()
	wg.Wait()
}
```

- [ ] **Step 2: Run to verify fail** — `cd ~/keld/keld-cli && go test ./internal/agent/settings/ -run Live -race -v` → FAIL (undefined `NewLive`/`Remote`).

- [ ] **Step 3: Implement**

`internal/agent/settings/remote.go`:
```go
package settings

// Remote is the org settings document served by keld-atlas. Fields are pointers
// so an absent key ("not set by the org") is distinct from an explicit false.
type Remote struct {
	IncludeEntityText *bool `json:"include_entity_text"`
}
```

`internal/agent/settings/live.go`:
```go
package settings

import "sync"

// Live holds the effective settings — the local base with the org's remote doc
// overlaid (remote-wins per key present). Safe for concurrent Apply/read.
type Live struct {
	mu   sync.RWMutex
	base Settings // local ~/.keld/agent-config.json, loaded once at startup
	eff  Settings // effective = base + remote overlay
}

func NewLive(base Settings) *Live { return &Live{base: base, eff: base} }

// Apply recomputes the effective settings from the local base with the remote
// keys that are present overlaid. A nil remote (or one omitting a key) leaves
// that key at the local base value.
func (l *Live) Apply(r *Remote) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.base
	if r != nil {
		if r.IncludeEntityText != nil {
			e.IncludeEntityText = *r.IncludeEntityText
		}
	}
	l.eff = e
}

func (l *Live) IncludeEntityText() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.eff.IncludeEntityText
}
```

- [ ] **Step 4: Run to verify pass** — `go test ./internal/agent/settings/ -race -v` → PASS. Then `go build ./... && go vet ./...`.

- [ ] **Step 5: Commit** — `git add internal/agent/settings/ && git commit -m "feat(settings): live holder + Remote (remote-overrides-local per key)"`

---

### Task 2: Settings HTTP client (keld-cli, LOCAL/TDD)

**Files:** Create `internal/agent/settings/client.go`; Test `internal/agent/settings/client_test.go`.

**Interfaces:**
- Consumes: `Remote` (Task 1).
- Produces: `func NewClient(url, token string, timeout time.Duration) *Client`; `func (c *Client) Fetch(ctx context.Context) (*Remote, error)` — GET `url` with header `x-keld-ingest-token: <token>`; non-200 or decode error → error; else the parsed `*Remote`.

- [ ] **Step 1: Write the failing test**

```go
package settings

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientFetchParsesAndSendsToken(t *testing.T) {
	var gotTok string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTok = r.Header.Get("x-keld-ingest-token")
		w.Write([]byte(`{"include_entity_text": false}`))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "tok123", 5*time.Second)
	r, err := c.Fetch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if gotTok != "tok123" {
		t.Fatalf("token header = %q, want tok123", gotTok)
	}
	if r.IncludeEntityText == nil || *r.IncludeEntityText != false {
		t.Fatalf("include_entity_text = %v, want present false", r.IncludeEntityText)
	}
}

func TestClientFetchErrorsOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound) // older Atlas without the endpoint
	}))
	defer srv.Close()
	if _, err := NewClient(srv.URL, "t", time.Second).Fetch(t.Context()); err == nil {
		t.Fatal("404 should surface as an error (poller keeps last-known)")
	}
}
```

- [ ] **Step 2: Run to verify fail** — `go test ./internal/agent/settings/ -run Client -v` → FAIL.

- [ ] **Step 3: Implement** `internal/agent/settings/client.go`:
```go
package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	url, token string
	hc         *http.Client
}

func NewClient(url, token string, timeout time.Duration) *Client {
	return &Client{url: url, token: token, hc: &http.Client{Timeout: timeout}}
}

// Fetch GETs the org settings document. Errors (including a 404 on an Atlas that
// predates the endpoint) surface so the caller can keep the last-known settings.
func (c *Client) Fetch(ctx context.Context) (*Remote, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-keld-ingest-token", c.token)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("enrichment-settings: status %d", resp.StatusCode)
	}
	var r Remote
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	return &r, nil
}
```

- [ ] **Step 4: Run to verify pass** — `go test ./internal/agent/settings/ -v` → PASS; `go build ./... && go vet ./...`.

- [ ] **Step 5: Commit** — `git commit -m "feat(settings): HTTP client for GET /v1/enrichment-settings"`

---

### Task 3: Poller + live-apply wiring in daemon.Run (keld-cli, LOCAL/TDD)

**Files:** Modify `internal/agent/daemon/daemon.go`; Test `internal/agent/daemon/settings_poll_test.go`.

**Interfaces:**
- Consumes: `settings.NewLive`, `settings.NewClient`, `settings.Live.Apply`/`IncludeEntityText` (Tasks 1–2).
- Changes: `Worker`'s `includeEntityText bool` param becomes `includeEntityText func() bool`; `process` calls it per job. Adds `settingsEndpoint(ingest string) string` (mirrors `enrichEndpoint` → `<base>/v1/enrichment-settings`) and `pollSettings(ctx, *settings.Client, *settings.Live, time.Duration)`.

- [ ] **Step 1: Write the failing test** (stub settings server; local base true, remote false → effective false after poll)

```go
package daemon

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ncx-ai/keld-cli/internal/agent/settings"
)

func TestPollSettingsAppliesRemoteOverLocal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"include_entity_text": false}`))
	}))
	defer srv.Close()
	live := settings.NewLive(settings.Settings{IncludeEntityText: true}) // local base true
	client := settings.NewClient(srv.URL, "tok", 2*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// one startup fetch is enough for the assertion; a long interval keeps the ticker quiet
	go pollSettings(ctx, client, live, time.Hour)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && live.IncludeEntityText() {
		time.Sleep(10 * time.Millisecond)
	}
	if live.IncludeEntityText() {
		t.Fatal("poller should have applied remote include_entity_text=false over local true")
	}
}

func TestSettingsEndpoint(t *testing.T) {
	if got := settingsEndpoint("https://atlas.example/v1/ingest"); got != "https://atlas.example/v1/enrichment-settings" {
		t.Fatalf("settingsEndpoint = %q", got)
	}
}
```
(Add `"context"` to the test imports.)

- [ ] **Step 2: Run to verify fail** — `go test ./internal/agent/daemon/ -run 'PollSettings|SettingsEndpoint' -race -v` → FAIL (undefined).

- [ ] **Step 3: Implement** in `daemon.go`:
  1. `settingsEndpoint` (next to `enrichEndpoint`):
     ```go
     func settingsEndpoint(ingest string) string {
         if i := strings.Index(ingest, "/v1/"); i >= 0 {
             return ingest[:i] + "/v1/enrichment-settings"
         }
         return strings.TrimRight(ingest, "/") + "/v1/enrichment-settings"
     }
     ```
  2. `pollSettings`:
     ```go
     func pollSettings(ctx context.Context, c *settings.Client, live *settings.Live, interval time.Duration) {
         apply := func() {
             if r, err := c.Fetch(ctx); err == nil {
                 live.Apply(r)
             } else {
                 log.Printf("keld-agent: settings poll failed (keeping current): %v", err)
             }
         }
         apply() // startup
         t := time.NewTicker(interval)
         defer t.Stop()
         for {
             select {
             case <-ctx.Done():
                 return
             case <-t.C:
                 apply()
             }
         }
     }
     ```
  3. `Worker` signature: `includeEntityText func() bool`; in `process`, `publish.Build(j, profile, actor, includeEntityText(), time.Now())` (call it per job). Update the `process` signature + its one call site.
  4. In `Run`: build the live holder + start the poller, and pass the live getter to `Worker`:
     ```go
     live := settings.NewLive(set)
     pollInterval := 5 * time.Minute
     if v := os.Getenv("KELD_SETTINGS_POLL"); v != "" {
         if d, err := time.ParseDuration(v); err == nil {
             pollInterval = d
         }
     }
     go pollSettings(ctx, settings.NewClient(settingsEndpoint(cfg.Endpoint), cfg.IngestToken, 10*time.Second), live, pollInterval)
     go Worker(q, model, pub, actor, live.IncludeEntityText, gate, admit)
     ```
     (Replaces the old `set.IncludeEntityText` bool arg.)

- [ ] **Step 4: Run to verify pass** — `go test ./internal/agent/daemon/ -race -v` (poll + endpoint tests + existing daemon tests) → PASS; full `go test ./... -race` green; `go vet ./...`.

- [ ] **Step 5: Commit** — `git commit -m "feat(daemon): poll org settings + live-apply include_entity_text"`

---

### Task 4: keld-atlas — org_enrichment_settings model + migration (ATLAS; docker)

**Repo:** keld-atlas. **Coordinate** (branch off atlas `main`; new files only). Tests run in Docker (never host Python 3.14).

**Files:** Modify `services/api/app/models.py`; Create `services/api/alembic/versions/0028_org_enrichment_settings.py`; Test `services/api/tests/test_enrichment_settings.py`.

All imports below (`UUID`, `Boolean`, `ForeignKey`, `DateTime`, `false`, `func`, `Mapped`, `mapped_column`, `from app.database import Base`) already exist at the top of `models.py` — do NOT re-add them.

- [ ] **Step 1: Add the model** at the end of `services/api/app/models.py` (one settings row per org; `org_id` is the PK):
```python
class OrgEnrichmentSettings(Base):
    __tablename__ = "org_enrichment_settings"

    org_id: Mapped[uuid.UUID] = mapped_column(
        UUID(as_uuid=True),
        ForeignKey("organizations.id", ondelete="CASCADE"),
        primary_key=True,
    )
    include_entity_text: Mapped[bool] = mapped_column(
        Boolean, nullable=False, server_default=false()
    )
    updated_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), server_default=func.now(), onupdate=func.now()
    )
```

- [ ] **Step 2: Create the migration** `services/api/alembic/versions/0028_org_enrichment_settings.py` (chains off the current head `0027_org_onboarding`):
```python
"""org_enrichment_settings

Revision ID: 0028_org_enrichment_settings
Revises: 0027_org_onboarding
"""
from alembic import op
import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

revision = "0028_org_enrichment_settings"
down_revision = "0027_org_onboarding"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "org_enrichment_settings",
        sa.Column("org_id", postgresql.UUID(as_uuid=True), nullable=False),
        sa.Column("include_entity_text", sa.Boolean(), server_default=sa.false(), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), server_default=sa.text("now()"), nullable=False),
        sa.ForeignKeyConstraint(["org_id"], ["organizations.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("org_id"),
    )


def downgrade() -> None:
    op.drop_table("org_enrichment_settings")
```

- [ ] **Step 3: Verify the migration chain is a single linear head** (reads files only, no DB mutation):
```bash
docker compose run --rm --no-deps --entrypoint sh api -c "pip install -e . -q && alembic heads && alembic history | head -3"
```
Expected: `alembic heads` prints exactly one head — `0028_org_enrichment_settings (head)`; history shows `0028_org_enrichment_settings -> 0027_org_onboarding`.

- [ ] **Step 4: Write + run the model round-trip test** `services/api/tests/test_enrichment_settings.py` (the `engine` fixture builds tables from `Base.metadata`, so no alembic step needed in the test):
```python
import uuid
import pytest
from sqlalchemy import select

from app.models import Organization, OrgEnrichmentSettings


@pytest.mark.asyncio
async def test_default_false_when_column_unset(session):
    org = Organization(name="Acme", slug=f"acme-{uuid.uuid4().hex[:8]}")
    session.add(org)
    await session.flush()
    session.add(OrgEnrichmentSettings(org_id=org.id))  # include_entity_text omitted
    await session.commit()
    row = await session.scalar(select(OrgEnrichmentSettings).where(OrgEnrichmentSettings.org_id == org.id))
    assert row.include_entity_text is False


@pytest.mark.asyncio
async def test_roundtrip_true(session):
    org = Organization(name="Beta", slug=f"beta-{uuid.uuid4().hex[:8]}")
    session.add(org)
    await session.flush()
    session.add(OrgEnrichmentSettings(org_id=org.id, include_entity_text=True))
    await session.commit()
    row = await session.scalar(select(OrgEnrichmentSettings).where(OrgEnrichmentSettings.org_id == org.id))
    assert row.include_entity_text is True
```
Run:
```bash
docker compose run --rm --no-deps -e KELD_TEST_DATABASE_URL=postgresql+asyncpg://keld:keld@postgres:5432/keld_test --entrypoint sh api -c "pip install -e .[test] -q && pytest -q tests/test_enrichment_settings.py"
```
Expected: 2 passed.

- [ ] **Step 5: Commit** (only the API files — do NOT stage anything under `services/web`):
```bash
git add services/api/app/models.py services/api/alembic/versions/0028_org_enrichment_settings.py services/api/tests/test_enrichment_settings.py
git commit -m "feat(enrichment-settings): org_enrichment_settings model + migration"
```

---

### Task 5: keld-atlas — GET /v1/enrichment-settings + admin read/set API (ATLAS; docker)

**Files:** Create `services/api/app/routers/enrichment_settings.py`; register in `services/api/app/main.py`; Test `services/api/tests/test_enrichment_settings_api.py`.

**Interfaces:**
- Consumes: `OrgEnrichmentSettings` (Task 4); `require_org(request, db) -> uuid.UUID` from `app.ingest_common` (imperative, `await`ed — NOT a `Depends`); `require_admin` + `current_org` from `app.auth`; `get_db` from `app.database`.
- Produces: `GET /v1/enrichment-settings` (daemon, ingest-token auth) → `{"include_entity_text": <bool>}` (org's row, or default `false` when no row); `GET /api/enrichment-settings` + `PATCH /api/enrichment-settings` (admin: session cookie + admin role) to read/set.

- [ ] **Step 1: Write the failing tests** `services/api/tests/test_enrichment_settings_api.py`:
```python
import uuid
import pytest

from app.models import Organization, OrgEnrichmentSettings
from app.ingest_tokens import ensure_ingest_token


async def _mk_org(session, name):
    org = Organization(name=name, slug=f"{name}-{uuid.uuid4().hex[:8]}")
    session.add(org)
    await session.flush()
    return org


@pytest.mark.asyncio
async def test_daemon_get_defaults_false_when_unset(client, session):
    org = await _mk_org(session, "acme")
    token = await ensure_ingest_token(session, org.id)
    await session.commit()
    res = await client.get("/v1/enrichment-settings", headers={"x-keld-ingest-token": token})
    assert res.status_code == 200
    assert res.json() == {"include_entity_text": False}


@pytest.mark.asyncio
async def test_daemon_get_reflects_row(client, session):
    org = await _mk_org(session, "acme")
    token = await ensure_ingest_token(session, org.id)
    session.add(OrgEnrichmentSettings(org_id=org.id, include_entity_text=True))
    await session.commit()
    res = await client.get("/v1/enrichment-settings", headers={"x-keld-ingest-token": token})
    assert res.status_code == 200
    assert res.json() == {"include_entity_text": True}


@pytest.mark.asyncio
async def test_daemon_get_bad_or_missing_token_401(client, session):
    bad = await client.get("/v1/enrichment-settings", headers={"x-keld-ingest-token": "nope"})
    assert bad.status_code == 401
    missing = await client.get("/v1/enrichment-settings")
    assert missing.status_code == 401


@pytest.mark.asyncio
async def test_admin_patch_then_daemon_get(client, session, org_ctx):
    token = await ensure_ingest_token(session, org_ctx.id)
    await session.commit()
    r = await client.patch("/api/enrichment-settings", json={"include_entity_text": True})
    assert r.status_code == 200
    assert r.json()["include_entity_text"] is True
    g = await client.get("/v1/enrichment-settings", headers={"x-keld-ingest-token": token})
    assert g.json() == {"include_entity_text": True}
    # upsert UPDATE path: flip back off
    r2 = await client.patch("/api/enrichment-settings", json={"include_entity_text": False})
    assert r2.json()["include_entity_text"] is False


@pytest.mark.asyncio
async def test_cross_org_isolation(client, session):
    a = await _mk_org(session, "a")
    b = await _mk_org(session, "b")
    ta = await ensure_ingest_token(session, a.id)
    tb = await ensure_ingest_token(session, b.id)
    session.add(OrgEnrichmentSettings(org_id=a.id, include_entity_text=True))
    await session.commit()
    ga = await client.get("/v1/enrichment-settings", headers={"x-keld-ingest-token": ta})
    gb = await client.get("/v1/enrichment-settings", headers={"x-keld-ingest-token": tb})
    assert ga.json()["include_entity_text"] is True
    assert gb.json()["include_entity_text"] is False
```

- [ ] **Step 2: Run to verify fail** —
```bash
docker compose run --rm --no-deps -e KELD_TEST_DATABASE_URL=postgresql+asyncpg://keld:keld@postgres:5432/keld_test --entrypoint sh api -c "pip install -e .[test] -q && pytest -q tests/test_enrichment_settings_api.py"
```
Expected: FAIL (404s — router not registered).

- [ ] **Step 3: Implement** `services/api/app/routers/enrichment_settings.py`:
```python
import uuid

from fastapi import APIRouter, Depends, Request
from pydantic import BaseModel
from sqlalchemy import select
from sqlalchemy.dialects.postgresql import insert as pg_insert
from sqlalchemy.ext.asyncio import AsyncSession

from app.auth import current_org, require_admin
from app.database import get_db
from app.ingest_common import require_org
from app.models import OrgEnrichmentSettings

# Daemon-facing: ingest-token auth, mirrors enrichments/otel (/v1 prefix).
router = APIRouter(prefix="/v1", tags=["enrichment-settings"])
# Admin-facing: session cookie + admin role. Full paths (no prefix) to avoid
# empty-path/trailing-slash redirect ambiguity.
admin_router = APIRouter(tags=["enrichment-settings"], dependencies=[Depends(require_admin)])


async def _org_settings(db: AsyncSession, org_id: uuid.UUID) -> dict:
    row = await db.scalar(
        select(OrgEnrichmentSettings).where(OrgEnrichmentSettings.org_id == org_id)
    )
    if row is None:
        return {"include_entity_text": False}
    return {"include_entity_text": row.include_entity_text}


@router.get("/enrichment-settings")
async def get_enrichment_settings(request: Request, db: AsyncSession = Depends(get_db)) -> dict:
    org_id = await require_org(request, db)
    return await _org_settings(db, org_id)


@admin_router.get("/api/enrichment-settings")
async def admin_get_settings(
    org: uuid.UUID = Depends(current_org), db: AsyncSession = Depends(get_db)
) -> dict:
    return await _org_settings(db, org)


class SettingsPatch(BaseModel):
    include_entity_text: bool


@admin_router.patch("/api/enrichment-settings")
async def admin_set_settings(
    body: SettingsPatch,
    org: uuid.UUID = Depends(current_org),
    db: AsyncSession = Depends(get_db),
) -> dict:
    stmt = (
        pg_insert(OrgEnrichmentSettings)
        .values(org_id=org, include_entity_text=body.include_entity_text)
        .on_conflict_do_update(
            index_elements=["org_id"],
            set_={"include_entity_text": body.include_entity_text},
        )
    )
    await db.execute(stmt)
    await db.commit()
    return await _org_settings(db, org)
```
Register both routers in `services/api/app/main.py` `create_app()` (add the import with the other router imports, and the two `include_router` lines beside the existing `app.include_router(enrichments.router)`):
```python
from app.routers import enrichment_settings
...
app.include_router(enrichment_settings.router)
app.include_router(enrichment_settings.admin_router)
```
(Match the existing import style in main.py — if routers are imported as `from app.routers import enrichments`, follow that; if `import enrichments`, follow that.)

- [ ] **Step 4: Run to verify pass** —
```bash
docker compose run --rm --no-deps -e KELD_TEST_DATABASE_URL=postgresql+asyncpg://keld:keld@postgres:5432/keld_test --entrypoint sh api -c "pip install -e .[test] -q && pytest -q tests/test_enrichment_settings_api.py"
```
Expected: 5 passed. Then run the full API suite once to confirm no regression:
```bash
docker compose run --rm --no-deps -e KELD_TEST_DATABASE_URL=postgresql+asyncpg://keld:keld@postgres:5432/keld_test --entrypoint sh api -c "pip install -e .[test] -q && pytest -q"
```

- [ ] **Step 5: Commit** (API files only):
```bash
git add services/api/app/routers/enrichment_settings.py services/api/app/main.py services/api/tests/test_enrichment_settings_api.py
git commit -m "feat(enrichment-settings): GET /v1/enrichment-settings + admin read/set API"
```

---

### Task 6: keld-atlas — minimal admin Settings-page toggle (ATLAS web; DEFERRED — coordinate)

**Status: held.** The other session is actively editing the `services/web` tree (uncommitted `components/sidebar.tsx`). The functional capability ships with Task 5's admin API; the toggle UI is additive polish. Do this once the web tree is quiet (or the other session's work is merged), on a fresh branch off the then-current atlas `main`.

**Files (when resumed):** `services/web/…` admin Settings page (additive) + a hook for the `/api/enrichment-settings` API; Test with Vitest.

- [ ] **Step 1: Add a `useEnrichmentSettings` hook** (React Query) — GET/PATCH `/api/enrichment-settings`, mirroring an existing admin hook's shape.
- [ ] **Step 2: Add a single "Include entity text" toggle** to the existing admin Settings page, wired to the hook. Copy: "Include entity text — send domain-entity surface text to Atlas (default off; sensitive spans are always masked regardless)."
- [ ] **Step 3: Vitest** — the toggle reads the current value and PATCHes on change (mock the hook/endpoint). Run `cd services/web && pnpm exec vitest run` (full suite green).
- [ ] **Step 4: Commit** — `git commit -m "feat(web): admin toggle for org include_entity_text"`

---

## Notes for the executor
- **T1–T3 are keld-cli, fully local + TDD + `-race`.** Do these first; they ship independently — until the Atlas endpoint exists, the poller's 404 is non-fatal and the daemon keeps local settings.
- **T4–T6 are keld-atlas.** Run that repo's suite in Docker (never host Python 3.14). **Coordinate with the concurrent session** on atlas `main`: branch off `main`, keep changes to new files + one additive settings-page toggle, and hold if that session is mid-edit in models/routers/settings-page. The two-repo end-to-end wiring (daemon actually reading a real Atlas org setting) is verified after T5 (curl the endpoint with a real ingest token) and via the daemon foreground + the settings poll.
- **Keep it non-fatal + org-scoped** throughout; remote-overrides-local per present key is the one governance rule.
