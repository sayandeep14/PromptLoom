# LoomLocker — Functional Design Document

_Last updated: Phase 1 + Phase 2 complete_

---

## Overview

LoomLocker is a **separate binary** (`loomlocker`) that protects secrets in project
config files by replacing real values with random tokens during AI-assisted work
sessions. When `loom execute runproject --unlock` is called, it temporarily restores
the real values for the duration of the startup window, then re-locks automatically.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  Terminal A                       Terminal B                 │
│  loomlocker start                 loom execute runproject    │
│  ┌─────────────────────┐          --unlock                   │
│  │ REPL + HTTP server  │◄───HTTP──────────────────────────── │
│  │ :PORT               │  POST /api/unlock {password}        │
│  │                     │  GET  /api/status                   │
│  │ lock/unlock state   │  POST /api/lock                     │
│  └─────────────────────┘                                     │
└─────────────────────────────────────────────────────────────┘
```

- `loomlocker start` — **foreground interactive server + REPL** in Terminal A
- `loom execute <cmd> [--unlock]` — **client** in Terminal B, talks to the server over HTTP
- `loomlocker lock/unlock/status/stop` — **client commands** from any terminal

---

## Config File: `.loom.config`

JSON, lives in the directory where `loomlocker` is run (walks up if not found).

```json
{
  "secret": [
    ".loom.secret",
    ".env:{API_KEY}",
    "application.yaml:{kafka.consumer-id}"
  ],
  "ignore": [],
  "permission": {
    "read": ["*"],
    "write": ["*"]
  },
  "loomlocker": {
    "active": true,
    "lockhost": "http://localhost",
    "port": "8053",
    "recoverable": false,
    "unlock_duration_seconds": 10
  },
  "custom": {
    "runproject": "python server.py",
    "testproject": "pytest"
  }
}
```

### Secret entry formats

| Entry | Meaning |
|---|---|
| `".loom.secret"` | Lock ALL key=value pairs in the file |
| `".env:{API_KEY}"` | Lock only the value of `API_KEY` in `.env` |
| `"application.yaml:{kafka.consumer-id}"` | Lock YAML key at dotted path `kafka.consumer-id` |

---

## Crypto Design

### Non-recoverable mode (`recoverable: false`, default)

1. Generate random 32-byte AES key → held in process memory only
2. Store secret mapping as `map[file][key]originalValue` in process memory
3. If process crashes while locked → **real values are lost** (must restore from git)

### Recoverable mode (`recoverable: true`)

1. Derive AES-256-GCM key from password using **Argon2id** + random salt
   - Params: memory=64MB, iterations=3, parallelism=2, keyLen=32
2. Encrypt the JSON-serialised mapping with AES-256-GCM
3. Write `<nonce><ciphertext>` to `.loom.secret.lock` alongside the Argon2 salt
4. On recovery: `loomlocker recover` — re-enter password → derive key → decrypt → restore files

### Password verification

Password hash stored in memory using **bcrypt** (cost=12). Never written to disk.
Verification: `bcrypt.CompareHashAndPassword(stored, input)`.

### Random token format

Locked values replaced with `lk_<16 lowercase hex chars>`, e.g. `lk_7f3a9b2c1d4e5a6b`.

---

## HTTP API (loomlocker server)

Base URL: `http://localhost:{port}/api`

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/ping` | none | Health check, returns locked state |
| `GET` | `/status` | none | Detailed status + file list |
| `POST` | `/lock` | none | Lock all secrets |
| `POST` | `/unlock` | password | Unlock + start auto-relock timer |
| `POST` | `/autolock` | none | Immediate relock (called by language libs) |
| `POST` | `/stop` | password (if locked) | Unlock + stop server |

### Authentication

Endpoints that require password: send `{"password": "xxx"}` in JSON body.
Server verifies with bcrypt against in-memory hash.

---

## Auto-Relock

After `/api/unlock`:
1. Start `time.AfterFunc(unlock_duration_seconds, relock)` timer
2. Any call to `/api/lock` or `/api/autolock` cancels the timer and locks immediately
3. Default duration: 10 seconds (configurable in `.loom.config`)

---

## Interactive REPL Commands

```
loomlocker> lock       lock all secrets
loomlocker> unlock     re-enter password → unlock → start auto-relock timer
loomlocker> status     show current state, files, keys
loomlocker> stop       unlock (if locked) + shutdown server
loomlocker> help       show this list
```

---

## `loom execute` Integration (Phase 2)

```
loom execute <custom-cmd> [--unlock]
```

1. Read `.loom.config`, resolve `custom.<cmd>`
2. If `--unlock`:
   a. `GET /api/ping` — if server not running: skip unlock, run normally
   b. If server running and already unlocked: run normally
   c. If server running and locked: prompt password (stdin, asterisk-masked) → `POST /api/unlock`
3. Run resolved shell command
4. Server's auto-relock timer handles re-locking independently

---

## File Format Support

| File type | Detection | Lock mechanism |
|---|---|---|
| `.env` / `.loom.secret` | extension or bare filename | Line-by-line `KEY=VALUE` regex replace |
| `*.yaml` / `*.yml` | extension | `gopkg.in/yaml.v3` Node-level replace at dotted path |
| Future: `.json:{key}` | extension | JSON parse + replace by path |

---

## Phase Plan

| Phase | Status | What |
|---|---|---|
| 1 | ✅ Complete | loomlocker binary — server, REPL, lock/unlock, crypto, file mutation |
| 2 | ✅ Complete | `loom execute <cmd> [--unlock]` in main loom binary |
| 3 | Pending | New `loom init` workspace structure |
| 4 | Pending | Language libraries (bloompy, gloom, loomj) |

---

## Recovery Procedure (if process crashes while locked)

```
# If recoverable: true — use password to recover
loomlocker recover --file .loom.secret.lock

# If recoverable: false — restore from VCS
git checkout .loom.secret .env application.yaml
```

---

## Security Notes

- Password never written to disk
- AES key never written to disk (non-recoverable) or only as AES-GCM ciphertext (recoverable)
- Locked token values (`lk_*`) are visually recognizable — easy to spot if accidentally committed
- Locking does not require authentication (adding protection is always safe)
- Unlocking always requires password re-entry
