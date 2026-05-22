# PromptLoom — Implementation Status & Next Tasks

> Assessed: 2026-05-22  
> Specs reviewed: v3, v4, v5, v6

---

## V1 & V2 — Core (✅ Complete)

All core pipeline implemented: lexer, parser, AST, registry, validate, resolve, render, format, pack install/publish/build. Pack v2 DSL (`:=` operator, `from()` expressions, multiple inheritance, namespace registry) is fully implemented including `loom inspect` (Phase 7), Lumine VS Code extension (Phase 8), and `loom fmt` semantic simplification (Phase 9).

---

## V3 — AI-aware, Testing, Safety, LSP

| Milestone | Feature | Status | Notes |
|---|---|---|---|
| M16 | `loom test` (smoke-test against real AI model) | ✅ Done | `internal/testrunner`, contract assertions |
| M17 | `loom blame` (git attribution per field) | ✅ Done | `internal/blame` |
| M17 | `loom changelog` (field-level changelog) | ✅ Done | |
| M18 | `loom audit` (secret slot scan, safety) | ✅ Done | `internal/audit`, `internal/secret` |
| M18 | Secret slots (`slot x { secret: true }`) | ✅ Done | AST + env loading |
| M18 | `--env` flag for environment blocks | ✅ Done | |
| M19 | MCP manifest generation (`loom mcp`) | ✅ Done | `internal/mcp` |
| M19 | `loom import` (import prompts from external) | ✅ Done | `internal/importer` |
| M20 | LSP server (`loom lsp`) | ✅ Done | `internal/lsp` |
| M20 | Lumine VS Code extension (v2 DSL: multi-parent, `from()`) | ✅ Done | Phases 7–9 just completed |
| M21 | Recipes (`loom recipe`) | ✅ Done | `internal/recipe` |
| M21 | Interactive playground (`loom playground`) | ✅ Done | |
| M22 | `loom minimize` (remove redundant fields) | ✅ Done | `internal/minimize` |
| M22 | `loom stale` (detect unused/stale prompts) | ✅ Done | `internal/stale` |
| M22 | `loom todos` (scan todo fields) | ✅ Done | |
| M22 | `loom journal` (run log) | ✅ Done | `internal/journal` |
| M22 | `kind`, `todo`, `compatible_with` fields in DSL | ✅ Done | AST + Lumine |
| M23 | `loom start` (workspace starter from CLAUDE.md + LLM) | ✅ Done | `internal/starter`, `internal/workspace` |
| M23 | `loom doctor` (prompt health score) | ✅ Done | `internal/doctor` |
| M23 | Quest Mode / `--quest` flag | ❓ Unknown | Not found in CLI — may not be implemented |

**V3 verdict: ~95% complete.** Only Quest Mode is unconfirmed.

---

## V4 — RAG, LLM Refinement, Remote Registry, Agent Runtime, Web Dashboard

| Milestone | Feature | Status | Notes |
|---|---|---|---|
| M24 | `loom index` — RAG auto-context indexing | ❌ Missing | No `index.go`, no `internal/rag` |
| M24 | `--auto-context` flag on `loom weave` | ❌ Missing | |
| M25 | Remote pack registry (publish/install from URL) | ⚠ Partial | `loom install` / `loom publish` exist; remote registry server itself is not implemented |
| M26 | `loom optimize` — LLM-assisted prompt refinement | ❌ Missing | No `optimize.go` |
| M26 | `loom score` — AI quality scoring | ❌ Missing | No `score.go` |
| M26 | `--refine` flag on `loom weave` | ❌ Missing | |
| M27 | `loom run` / agent runtime with streaming | ⚠ Partial | `loom cast` renders + sends to a destination; `loom execute` runs custom commands. Neither matches V4's full agent runtime spec (stdio streaming, tool use, multi-turn). |
| M28 | `loom serve` — web dashboard | ❌ Missing | No `serve.go` |

Also present (beyond spec): `loom deploy`, `loom ci`, `loom review` (semantic diff), `loom diff`, `loom graph`, `loom stats`, `loom fingerprint`, `loom lock/check-lock`, `loom copy`, `loom smells`, `loom check-output`, `loom summarize`, `loom cast`.

**V4 verdict: ~25% complete.** Pack infra done. RAG, LLM refinement, agent runtime, and web dashboard are not implemented.

---

## V5 — Measurable & Team-Ready

| Milestone | Feature | Status |
|---|---|---|
| M29 | `loom eval` — prompt evaluation with LLM judge | ❌ Missing |
| M29 | `.eval.toml` test case format | ❌ Missing |
| M29 | Evaluation gate in `loom ci` | ❌ Missing |
| M30 | `loom impact` — blast radius analysis | ❌ Missing |
| M30 | `loom bench` — pack benchmark suite | ❌ Missing |
| M31 | `loom sync` — multi-tool sync (Claude, Copilot, Cursor, AGENTS.md) | ❌ Missing |
| M31 | `loom check-sync` | ❌ Missing |
| M31 | `loom policy check` | ❌ Missing |
| M32 | `loom pack sign` / `verify` / `trust` | ❌ Missing |
| M32 | `owner`, `status`, `reviewed_by`, `deprecated` DSL fields | ❌ Missing |
| M32 | `loom owners`, `loom status`, `loom deprecate`, `loom approvals` | ❌ Missing |
| M33 | `loom usage` — token and cost tracking | ❌ Missing |
| M33 | `loom analytics local` | ❌ Missing |

**V5 verdict: 0% complete.** None of the V5 packages (`eval`, `bench`, `impact`, `sync`, `policy`, `sign`, `ownership`, `analytics`) exist yet.

---

## V6 — Shared Engineering Platform

| Milestone | Feature | Status |
|---|---|---|
| M34 | `loom server` — self-hostable team server | ❌ Missing |
| M34 | `loom push` / `loom pull` | ❌ Missing |
| M35 | `loom review submit/approve/reject` — approval workflows | ❌ Missing |
| M35 | `loom lifecycle set/history` | ❌ Missing |
| M36 | Private registry with RBAC + SSO | ❌ Missing |
| M37 | `loom analytics team` | ❌ Missing |
| M37 | `loom policy push/pull/show` | ❌ Missing |
| M37 | Tamper-evident audit log | ❌ Missing |

**V6 verdict: 0% complete.** All V6 features require V5 completion first.

---

## Recommended Next Tasks (in priority order)

### 1. Complete V4 — High Impact, Unlocks V5

The most valuable unimplemented V4 features, ranked by impact vs effort:

#### 1a. `loom sync` targets (deploy targets + multi-tool, M31 overlap)
`loom deploy` already writes rendered prompts to configured targets. The gap is supporting Copilot, Cursor, and AGENTS.md target types. This is close to done — add new `type` values to the deploy target renderer.

#### 1b. `loom impact` — blast radius analysis (M30)
The `internal/graph` package and dependency graph already exist. `loom impact` is a traversal on the existing graph that shows direct + transitive children. Low implementation effort, high daily usefulness.

#### 1c. `loom eval` — prompt evaluation (M29)
This is the highest-value V5 feature and has significant V4 overlap (`loom test` already sends prompts to a model). `loom eval` extends `loom test` with:
- `.eval.toml` fixture format (structured input + assertions)
- LLM judge (scores 0–100)
- `--record` / `--compare` for golden baseline
- `--models` for cross-model comparison

#### 1d. `loom optimize` + `loom score` (M26)
LLM-assisted refinement and scoring. Requires model API integration that `loom test` already has. This enables the quality-tracking story.

### 2. V4 RAG (`loom index`, `--auto-context`)
`internal/context` exists — check if it already does context loading. If so, this may be partially done. Needs a vector store or simple BM25 implementation for file indexing, and an `--auto-context` flag on `loom weave`.

### 3. `loom serve` (M28)
Web dashboard for prompt library browsing. Lowest priority of V4 — useful but not blocking anything in V5.

### 4. V5 `loom policy check` (M31)
Policy rule evaluation is straightforward: read `[policy]` from `loom.toml`, run checks against resolved prompts (contract present? health score? audit result?). Most sub-checks are already implemented in `loom doctor` and `loom audit`.

### 5. V5 `loom sync` (M31)
Multi-tool sync: render prompts in Claude command format, Copilot instructions format, Cursor rules format. The rendering logic is in `internal/render`; this adds new output formatters.

---

## Quick Wins (can be done in a single session)

| Task | Why it's quick |
|---|---|
| `loom impact <Name>` | Reverse-traverse `internal/graph` — graph already built |
| `loom check-sync` | Read sync targets from `loom.toml`, check if output files exist and are up-to-date (hash compare) |
| `loom owners` | Parse `owner:` / `status:` fields from AST — just a list command |
| V5 `owner`/`status`/`deprecated` DSL fields | Add to lexer + parser + AST (same pattern as `kind:` and `todo:`) |

---

## Summary

| Version | Status |
|---|---|
| V1 — Core CLI & DSL | ✅ Complete |
| V2 — Pack system, `from()`, multi-parent | ✅ Complete |
| V3 — AI testing, LSP, safety, recipes, workspace | ✅ ~95% Complete |
| V4 — RAG, LLM refinement, agent runtime, web dashboard | ⚠ ~25% Complete |
| V5 — Evaluation, multi-tool sync, policy, pack trust | ❌ 0% — Not started |
| V6 — Team server, approval workflows, private registry | ❌ 0% — Requires V5 first |
