# PromptLoom — Work Tracker

The single source of truth for what is done, what is next, and what blocks what.
Keep it current: update a ticket's status in the same commit that does the work.

**Last updated:** 2026-09-21 · **Current version:** 4.2.0 · **Active epic:** E1 — Trust & Safety (E1 core done; E2 done — Lumine ships as a VSIX from GitHub Releases)

---

## How to use this file

**Status:** `TODO` · `IN PROGRESS` · `BLOCKED` · `DONE` · `DROPPED`
**Priority:** `P0` blocker for a public release · `P1` important · `P2` nice to have
**Size:** `S` ≤ half a day · `M` 1–2 days · `L` 3–5 days · `XL` > 1 week

Each ticket: `ID · title · status · priority · size · depends on`. A ticket may start only when everything in **Depends** is `DONE`.
Completed tickets stay in the file (with the commit or date) so history is visible. Add new tickets at the bottom of the right epic with the next free ID.

---

## Board (at a glance)

| Epic | Goal | Progress |
|---|---|---|
| **E0** Stabilize | Green tests, clean repo, CI, license | 8 / 8 done |
| **E1** Trust & Safety | Secure registry, tested core, working install path | 9 / 14 |
| **E2** Lumine (VS Code) | v2-only DSL support, tested, published | 2 / 5 |
| **E3** Docs & Release | Accurate docs, release binaries, packaging | 0 / 6 |
| **E4** Product Completeness | impact, sync, eval | 0 / 5 |
| **E5** Agentic Mode | run / refine / decide / quest, `.lmscr` | 0 / 5 |
| **E6** LoomLocker Hardening | Tests, Windows, libraries verified end-to-end | 4 / 5 |
| **E7** Later (V4 remainder, V5, V6) | RAG, dashboard, governance, team server | 0 / 6 |

### Dependency map

```mermaid
flowchart LR
  E0[E0 Stabilize ✔] --> E1[E1 Trust & Safety]
  E0 --> E2[E2 Lumine]
  E1 --> E3[E3 Docs & Release]
  E2 --> E3
  E1 --> E4[E4 Product Completeness]
  E4 --> E5[E5 Agentic Mode]
  E1 --> E6[E6 LoomLocker Hardening]
  E4 --> E7[E7 Later]
  E5 --> E7
```

---

## E0 — Stabilize ✔

| ID | Ticket | Status | Pri | Size | Depends |
|---|---|---|---|---|---|
| PL-001 | Rewrite stale `internal/tui` deploy tests in v2 syntax (`.prompt.loom`, `:=`, `from()`) | DONE | P0 | S | — |
| PL-002 | Untrack build/OS/local files: `loom` binary, `.DS_Store`, `libs/loomj/target`, `LoomTest/` | DONE | P0 | S | — |
| PL-003 | Add MIT `LICENSE` at repo root | DONE | P0 | S | — |
| PL-004 | GitHub Actions CI: build + vet + test for root, `server`, `loomlocker`, `libs/gloom`; build Lumine | DONE | P0 | S | PL-001 |
| PL-005 | Version injected at build time (`-ldflags`), `Makefile` with `build/test/vet/check` | DONE | P1 | S | — |
| PL-006 | Prune docs to a minimum set; add this tracker | DONE | P1 | S | — |
| PL-007 | Fix README links to removed docs | DONE | P1 | S | PL-006 |
| PL-008 | Ignore local-only planning notes (`CLAUDE.md`, old specs) | DONE | P2 | S | — |

---

## E1 — Trust & Safety  *(start here)*

Goal: a stranger can `loom install` and `loom publish` against a registry safely, and the core has real test coverage.

| ID | Ticket | Status | Pri | Size | Depends |
|---|---|---|---|---|---|
| PL-101 | **Registry auth**: fail closed when `UPLOAD_SECRET` is unset; constant-time, case-sensitive compare; applies to `POST` and `DELETE`; secret must be ≥16 chars | DONE | P0 | S | PL-004 |
| PL-102 | Registry hardening: body size cap, per-IP rate limits, input validation (slug/version/paths/sizes), CORS off by default, server timeouts + graceful shutdown, generic 500s. Also client-side: `loom install` rejects unsafe paths/slugs | DONE | P0 | M | PL-101 |
| PL-103 | Registry tests: handlers behind a `Store` interface (fake store), Postgres integration tests (schema, upsert/replace, atomicity, uniqueness, cascade), CI job with a Postgres service. Found + fixed: nil `tags` violated NOT NULL; file order was locale-dependent | DONE | P0 | M | PL-101 |
| PL-104 | **Registry hosting decision: self-host first.** No built-in default URL; `loom install`/`publish` explain how to configure one; `[registry] url` in `loom.toml` now works; URL validated; upload secret never sent over plain HTTP to a remote host | DONE | P0 | S | — |
| PL-105 | Registry Docker deployment: multi-stage distroless image (20 MB, non-root, read-only), `docker-compose.yml` (server + Postgres, DB not published), self-applying idempotent schema (`AUTO_MIGRATE`, advisory-locked), `healthcheck` subcommand, CI smoke job, `server/README.md` (TLS, backup/restore, upgrade) | DONE | P0 | M | PL-102, PL-104 |
| PL-106 | End-to-end fixture tests: 16 valid + 45 invalid projects in `testdata/` run through loader → validate → resolve → render, with golden outputs, exact two-way diagnostic matching, positions required, invariants (dedup, determinism), a rule-coverage guard, and built-binary CLI tests (see `testdata/README.txt`). Mutation-checked. Found + fixed: unterminated-body parse errors had no line number | DONE | P0 | L | PL-004 |
| PL-107 | Unit tests for `contract`, `audit`, `doctor`, `lock`, `loader`, `installer` (fake registry: dependency graph, conflicts, cycles, missing/corrupt cases, reinstall) and `deps` (edge cases, pre-release versions). Found + fixed 8 defects, each with a test that fails without the fix: contract heading matching was substring-based (`## Summary of findings` satisfied `Summary`); audit matched `ssn` inside `className` and flagged prohibitions ("Never skip tests") as HIGH risk; doctor flagged one instruction as conflicting with itself; doctor truncated multi-byte text mid-character; `lock.Read` reported a corrupt lockfile as "not found"; pre-release versions (accepted by the registry) were unparseable client-side; `loom install` did not restore a deleted dependency of an already-installed pack | DONE | P1 | L | PL-106 |
| PL-108 | **v2-only syntax policy.** `+=` and `-=` are now validation *errors* whose message prints the exact rewrite for that prompt (correct parent index, block/overlay/scalar/no-parent variants); a bare `:` is a warning with the replacement. Prerequisite done first: every generator now emits clean v2 — 15+23+2 `+=` and 73 bare-`:` occurrences converted in recipes, `start --nollm` templates and `init --sample`, plus the `thread` scaffolds, the interactive-weave wizard and the LLM prompt that taught the model `+=`/`-=`. Guarded by `e2e/generators_test.go` (every recipe × options, every stack × tier, init/thread, LLM example). Migration table added to `LOOM_LANGUAGE.md`; §16 rewritten to match the code | DONE | P1 | M | PL-106 |
| PL-111 | **`:=` in blocks/overlays now composes** (list fields add to existing items; `format` is last-writer-wins; prompt/variant/env `:=` still replace). Measured on the shipped `python-starter` pack: `PythonEngineer` lost 13 of 18 constraints before the change. Also fixed: de-duplication now runs as the final resolver step (it was skipped for prompts without parents and after variants/overlays/env). Documented in `LOOM_LANGUAGE.md`; unit tests in `resolve/composable_test.go` | DONE | P0 | M | PL-106 |
| PL-113 | Follow-up to PL-111: a prompt that `use`s a block *and* writes its own list field replaces the block's items (its own fields apply last), with no way to say "block + mine". Consider warning in `loom inspect`, or an explicit `from(blocks)` form | TODO | P2 | S | PL-111 |
| PL-117 | Publish Lumine to the VS Code Marketplace and Open VSX: fix the token/publisher problems, add `VSCE_PAT` / `OVSX_PAT` secrets (the release workflow already publishes when they exist), then re-tag | TODO | P2 | S | PL-205 |
| PL-118 | Go module path is `github.com/sayandeepgiri/promptloom` but the repository is `github.com/sayandeep14/PromptLoom`, so `go install …/cmd/loom@latest` and `go get …/libs/gloom` in the docs do not work. Rename the module (go.mod files, every import, docs) or move the repo | TODO | P1 | M | — |
| PL-116 | **`loom fmt` no longer deletes data.** It used to drop every `//` comment, whole `env { … }` blocks, a slot's `secret: true` (turning a secret slot into an ordinary one) and `required: false` (making an optional slot required). Now: comments are captured by the lexer and attached to the element that follows them; env blocks and full slot metadata are written; and `format.Source` parses its own output and compares an inventory of everything declared, refusing (file untouched, exit 1) if anything would change. Also fixed: `loom fmt --check` documented "exit 1" but always exited 0; writes are now atomic. Property test formats every fixture and re-weaves against the same goldens; new fixture with comments in every position | DONE | P0 | M | — |
| PL-115 | `loom fmt --migrate`: mechanically rewrite v1 syntax (`field:` → `:=`, `+=` in child prompts → `from(parent[…]) and { … }`, `+=` in blocks → `:=`), leaving `-=`/scalar `+=` for manual fix. The one-off converter used for the generators is a starting point. Also the basis for an LSP quick-fix (PL-201) | TODO | P1 | M | PL-108 |
| PL-114 | Unit tests for the remaining untested packages (`lsp`, `testrunner`, `mcp`, `blame`, `minimize`, `stale`, `starter`, `summarize`, `semantic`, `tokens`, `journal`) and CLI-level tests for commands beyond inspect/weave | TODO | P1 | L | PL-107 |
| PL-112 | Bare `slot name` (no `{ }`) is rejected by the lexer although the LSP hover text documents it as valid; either accept it (required by default) or fix the docs/hover | TODO | P2 | S | PL-106 |
| PL-109 | Run `gofmt -w` across the ~26 unformatted files and add a `gofmt -l` check to CI | TODO | P2 | S | PL-004 |
| PL-110 | Registry follow-ups: TLS/HSTS guidance, per-pack ownership (today one shared secret can overwrite any pack), constant-time-safe secret rotation, request logging | TODO | P1 | M | PL-103 |

**Exit criteria for E1:** `go test ./...` covers the parser→render path and every validation rule; registry refuses unauthenticated writes; `loom install` works against a documented registry.

---

## E2 — Lumine (VS Code extension)

| ID | Ticket | Status | Pri | Size | Depends |
|---|---|---|---|---|---|
| PL-201 | **Lumine is v2-only.** `+=`/`-=`/`extends` are errors and a bare `:` a warning, with messages identical to `loom inspect` (also inside block/overlay/variant/env; contract keys exempt). Real quick-fix code actions + *fix all* (bare `:`, `extends`, `+=` → `from(parent[0]) and { … }` / `:=`; none offered where meaning would change). `env` blocks parsed; grammar, completions, hover, snippets updated. **Formatter rewritten to be lossless** — the old one deleted comments, `env` blocks, `tags` and all parents after the first. Parity test against the Go `testdata/` fixtures + a cross-check that fixed output passes the real `loom inspect`. Also fixed on the Go side while doing this: `from()` in variant/env blocks was never evaluated (its text leaked into the prompt), and `+=` inside variant/env was unchecked | DONE | P0 | M | PL-108 |
| PL-202 | Add `from()` / `parent[...]` awareness (the CLI now also evaluates from() in variant/env; the extension does not yet check bounds/types): syntax highlighting, completions, type errors (scalar vs list) matching `loom inspect` | TODO | P1 | M | PL-201 |
| PL-203 | Test suite for the TypeScript side. **Started in PL-201** (`npm test`: 37 tests — legacy-syntax rules, quick fixes, lossless formatter over every Go fixture, parity with `testdata/`, CI job). Remaining: completion, hover, definition, references, document symbols, and parity for the non-syntax rules (unknown parent/block, cycles, duplicates, from() bounds) | IN PROGRESS | P1 | L | PL-201, PL-106 |
| PL-204 | Lumine README and CHANGELOG updated, version bumped to 0.2.0 | DONE | P1 | S | PL-201 |
| PL-205 | **Lumine distribution.** Decision (2026-09-21): distribute as a downloadable VSIX from GitHub Releases for now (portfolio link → `releases/latest/download/lumine-latest.vsix`); store publishing is deferred to PL-117. Done: icon, correct metadata and repository links (`sayandeep14/PromptLoom`), publisher `shreekalpo`, VSIX 17 files / 158 KB enforced by `npm run verify:package`, manual-install instructions in the Lumine README, CI builds the VSIX, `release-lumine.yml` attaches `lumine-<version>.vsix` + `lumine-latest.vsix` on tag `lumine-vX.Y.Z` (no tokens needed), real-LSP end-to-end test. **To ship:** push `main`, then `git tag lumine-v0.2.0 && git push origin lumine-v0.2.0` | DONE | P2 | M | PL-203, PL-204 |

---

## E3 — Docs & Release

| ID | Ticket | Status | Pri | Size | Depends |
|---|---|---|---|---|---|
| PL-301 | Audit `docs/LOOM_COMMAND.md` and `docs/LOOM_LANGUAGE.md` against the code: every command and flag exists; every example runs | TODO | P1 | M | PL-106 |
| PL-302 | Add missing commands to `LOOM_COMMAND.md` (`install` dependency behaviour, `execute`, any added since) and remove references to removed ones | TODO | P1 | S | PL-301 |
| PL-303 | GoReleaser (or equivalent): tagged release builds for macOS/Linux/Windows, checksums, GitHub Release notes | TODO | P0 | M | PL-004, PL-305 |
| PL-304 | Homebrew tap and Scoop manifest; `go install` instructions verified | TODO | P2 | M | PL-303 |
| PL-305 | Windows/portability. `syscall.Stdin` replaced by `os.Stdin.Fd()`; CI now cross-builds `loom`, `loomlocker` and the registry for linux/darwin/windows × amd64/arm64 (all 6 verified). **Remaining:** run the *tests* on a Windows runner (the e2e/integration tests assume a POSIX shell and a binary without `.exe`) | IN PROGRESS | P1 | M | PL-004 |
| PL-306 | Shell completions (`loom completion bash\|zsh\|fish\|powershell`) and `loom doctor` self-check of the install | TODO | P2 | S | — |

---

## E4 — Product Completeness

Builds on existing packages (`internal/graph`, `internal/testrunner`, `internal/render`, `deploy`).

| ID | Ticket | Status | Pri | Size | Depends |
|---|---|---|---|---|---|
| PL-401 | `loom graph <PromptName>`: per-prompt inheritance diagram (ancestors, descendants, blocks used) as text + Mermaid | TODO | P1 | S | PL-106 |
| PL-402 | `loom impact <Name>`: blast radius — direct and transitive dependents from the existing graph | TODO | P1 | S | PL-401 |
| PL-403 | `loom sync` / `check-sync`: render to Claude, Copilot, Cursor and `AGENTS.md` targets from `[[targets]]`; hash-compare for drift | TODO | P1 | M | PL-106 |
| PL-404 | `loom eval`: `.eval.toml` fixtures, LLM-judge scoring (0–100), `--record` / `--compare`, `--models`; gate in `loom ci` | TODO | P1 | XL | PL-107 |
| PL-405 | Provider abstraction for LLM calls (Gemini + Anthropic + OpenAI-compatible) shared by `test`, `start`, `summarize`, `eval` | TODO | P1 | M | PL-107 |

---

## E5 — Agentic Mode

The stated next phase after v4.2.0. Needs a provider layer and a stable core first.

| ID | Ticket | Status | Pri | Size | Depends |
|---|---|---|---|---|---|
| PL-501 | Design doc: agent runtime scope (streaming, tool use, multi-turn, safety model, how it interacts with LoomLocker and `permission` in `.loom.config`) | TODO | P1 | M | PL-405 |
| PL-502 | `loom run <Name>`: execute a resolved prompt against a model with streaming output | TODO | P1 | L | PL-501 |
| PL-503 | Refine / decide loop (`loom optimize`, `loom score`, `--refine`) | TODO | P2 | L | PL-502, PL-404 |
| PL-504 | Quest mode (multi-step guided task on top of `loom run`) | TODO | P2 | L | PL-502 |
| PL-505 | `.lmscr` loom scripts: syntax, runner, docs | TODO | P2 | XL | PL-501 |

---

## E6 — LoomLocker Hardening

| ID | Ticket | Status | Pri | Size | Depends |
|---|---|---|---|---|---|
| PL-601 | **LoomLocker tests.** crypto (incl. flipping every ciphertext byte), config, locker file handling, journal, server, limiter, client — plus `loomlocker/integration` driving the real binaries. Found + fixed while writing them: a failed lock stranded real values (state said "unlocked" so unlock did nothing); `export`/quotes were lost on restore; duplicate keys collided; YAML round trip was lossy; `Start()` reported success when the port was taken; auto re-lock failures were swallowed | DONE | P0 | L | PL-004 |
| PL-602 | **LoomLocker network hardening + threat model.** Binds `127.0.0.1` only (was all interfaces); Host check (DNS rebinding); requests with an `Origin` are refused (browsers); JSON-only bodies with a size cap; attempt limiter (5 free, then 1 s doubling to 5 min, also refuses the correct password during the wait, covers unlock and stop); `lockhost` must be loopback (client and server) so the password can never be sent off-machine; `stop` refuses to stop if it cannot restore. Every protection mutation-checked. Threat model in `loomlocker/DESIGN.md` | DONE | P0 | S | PL-601 |
| PL-603 | **Integration tests.** Real `loomlocker` + `loom` binaries: lock → `execute --unlock` → auto re-lock → stop; kill -9 → `recover`; every endpoint probed without a password. Python (`bloompy`), Go (`gloom`) and Java (`loomj`) libraries run against a live server. Found + fixed: `bloompy` documented `Safe.silent()` but did not have it; added Python unit tests | DONE | P1 | L | PL-601 |
| PL-604 | **Crash safety.** `recoverable: true` was silently ignored (the key was computed then discarded). Now: the mapping is written encrypted (AES-256-GCM, Argon2id key) to `.loom.secret.lock` *before* any file is touched; `loomlocker recover` restores after a crash/kill -9; start refuses while a journal is waiting; writes are atomic with rollback; unlock deletes the journal | DONE | P1 | M | PL-601 |
| PL-605 | Add JSON secret paths (`.json:{key}`), a `loomlocker` README, publish libraries (PyPI, Maven Central) | TODO | P2 | L | PL-603 |

---

## E7 — Later

Only start after E1 and E4 are done.

| ID | Ticket | Status | Pri | Size | Depends |
|---|---|---|---|---|---|
| PL-701 | `loom index` + `--auto-context` (RAG over project files) | TODO | P2 | XL | PL-405 |
| PL-702 | `loom serve` web dashboard | TODO | P2 | XL | PL-402 |
| PL-703 | `loom policy check`, `owner` / `status` / `deprecated` DSL fields, `loom owners` | TODO | P2 | L | PL-403 |
| PL-704 | `loom bench`, `loom usage` (token and cost tracking) | TODO | P2 | L | PL-404 |
| PL-705 | Pack signing and trust (`pack sign / verify / trust`) | TODO | P2 | L | PL-102 |
| PL-706 | Team server, approval workflows, private registry with RBAC/SSO, audit log | TODO | P2 | XL | PL-703, PL-705 |

---

## Recommended sequence

1. ~~PL-101 → PL-102 → PL-103, PL-104~~ — registry secured, tested, hosting decided (done).
2. ~~PL-105~~ — Docker one-command registry (done).
3. ~~PL-106~~ (done) → **PL-111** (decide block/overlay `:=` semantics) → **PL-107** (unit tests) → **PL-108** (old-syntax policy).
4. **PL-201 → PL-203** — bring Lumine in line with the language.
5. **PL-303 + PL-305** — first tagged release.
6. **E4** (`graph` / `impact` / `sync` first, `eval` last), then **E5**.

---

## Decisions log

| Date | Decision |
|---|---|
| 2026-05 | `+=` and `-=` removed; `:=` is the only operator; multiple inheritance uses `from()` |
| 2026-05 | `loom migrate` cancelled — new packs use v2 from the start; old examples live in `examples/legacy/` |
| 2026-09-21 | One tracker file (this one) replaces the scattered planning notes; language and command docs remain in `docs/` |
| 2026-09-21 | Registry write endpoints fail closed (503) without `UPLOAD_SECRET`; the local dev secret must now be ≥16 chars |
| 2026-09-21 | **Block/overlay list `:=` composes** (PL-111): chosen because 8 shipped prompts use 2–3 blocks that each set `constraints :=` and were losing all but the last block's rules; `format` excepted (single output shape). Reversible: one condition in `resolve.applyList` + regenerate goldens |
| 2026-09-21 | **v2-only syntax** (PL-108): `+=`/`-=` are errors, bare `:` a warning. Chosen because the user's own pack spec says `+=` is not a valid operator and the design doc lists `:=` as the only one; enforcement waited until the tool's own generators stopped emitting v1 syntax, which is now tested. A bare `:` stays a warning (harmless, same meaning as `:=` for prompts) |
| 2026-09-21 | **Lumine formatter is whitespace-only** (PL-201): the tree-rebuilding formatter deleted comments, `env` blocks and extra parents. `loom fmt` had the same flaw and was fixed differently (PL-116): it keeps its canonical ordering and `from()` simplification, but carries comments with their elements and verifies its own output. The two formatters therefore differ on purpose — Lumine never reorders. Quick fixes are offered only where the rewrite preserves meaning |
| 2026-09-21 | **LoomLocker restores byte-exactly and fails safe** (PL-601/602/604): mapping is `text written → exact original`, files are edited as text (not re-serialised), every change is all-or-nothing. YAML values must be single-line (documented, refused otherwise). Recoverable mode is opt-in because it keeps an encrypted copy of the secrets on disk while locked |
| 2026-09-21 | **Registry hosting: self-host first.** The hard-coded `registry.promptloom.dev` default is removed. A hosted default can be added later by setting one constant once a registry exists (revisit under PL-105/PL-303) |
| 2026-09-21 | `docs/TOOL_REFERENCE.md` and `docs/PACKMAKER_DESIGN.md` removed from git as stale/contradictory; `docs/LOOM_COMMAND.md` and `docs/LOOM_LANGUAGE.md` are canonical |

## Known risks

- No public registry exists; every team must self-host one (PL-105 makes that a single command). Revisit if adoption needs a shared public one.
- The pipeline has an end-to-end net (PL-106) and the core packages have unit tests (PL-107); still untested: `lsp`, `testrunner`, `mcp`, `blame`, `minimize`, `stale`, `starter`, `summarize`, `tui` (beyond deploy), and the CLI commands themselves. Track under a new PL-114.
- Existing user libraries written in v1 syntax will now fail `loom inspect` until migrated by hand (PL-115 would automate it). Installed packs are unaffected.
- Lumine and the Go parser can drift apart (PL-203 addresses this with shared fixtures).
