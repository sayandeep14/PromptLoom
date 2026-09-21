# LoomLocker — Design and Threat Model

LoomLocker is a separate binary (`loomlocker`) that keeps real secret values out of reach of AI
agents and tools while you work. It replaces the values in your config files with random tokens,
and puts the real values back for a few seconds only when you start your application.

It protects against an agent (or any tool you run) **reading your secrets from disk**. It is not a
vault, and it does not protect against someone who already controls your machine as your user
(see [Threat model](#threat-model)).

---

## How it works

```
Terminal A                                  Terminal B
┌──────────────────────────┐                ┌──────────────────────────────┐
│ loomlocker start         │◄───127.0.0.1───│ loom execute runproject      │
│  · asks for a password   │    HTTP        │        --unlock              │
│  · REPL + local server   │                │  · asks for the password     │
│  · holds the mapping     │                │  · POST /api/unlock          │
└──────────────────────────┘                │  · runs the command          │
                                            └──────────────────────────────┘
```

1. `loomlocker start` asks for a session password and starts a local HTTP server.
2. `lock` (REPL, `loomlocker lock`, or `POST /api/lock`) replaces each configured secret with a token
   like `lk_7f3a9b2c1d4e5a6b`, remembering the original.
3. `loom execute <cmd> --unlock` (or a client library) sends the password; the real values come back
   and a timer starts (`unlock_duration_seconds`, default 10).
4. When the app has read its config it calls `autolock` (client libraries) or the timer expires, and
   the values are replaced by **fresh** tokens again.

## Configuration (`.loom.config`)

```json
{
  "secret": [".loom.secret", ".env:{API_KEY}", "application.yaml:{kafka.consumer-id}"],
  "loomlocker": {
    "active": true,
    "lockhost": "http://localhost",
    "port": "8053",
    "recoverable": false,
    "unlock_duration_seconds": 10
  },
  "custom": { "runproject": "python server.py" }
}
```

| Entry | Locks |
|---|---|
| `".loom.secret"` | every `KEY=VALUE` in the file |
| `".env:{API_KEY}"` | every assignment of that key |
| `"application.yaml:{a.b.c}"` | the scalar at that dotted path |

`lockhost` **must be a loopback address** (`http://localhost`, `127.0.0.1`, `::1`); anything else is
rejected at load time, because the session password is sent to that address.

## File handling guarantees

Locking and unlocking are **all-or-nothing** and **byte-exact**:

- Every change is computed in memory first. A missing file or key, an unparsable YAML file, or an
  unsupported value aborts *before anything is written*.
- Files are replaced atomically (temp file + rename) and keep their permissions. If a later write
  fails, files already replaced are put back.
- What was replaced is remembered as *(text written into the file → exact original text)*. Restoring
  is a string replacement, so quotes, `export`, spacing, inline comments, CRLF line endings, a
  missing final newline, YAML comments, indentation and key order all come back exactly.
- Duplicate keys are each restored. Restoring is driven by that mapping, not by the current config, so
  editing `.loom.config` while locked cannot strand a locked file. Lines you add while locked are kept.
- Locking twice changes nothing more. Empty values are left alone.

Limits: YAML values must be **single-line scalars** (multi-line block scalars and multi-line quoted
values are refused with a clear error rather than mangled). A YAML key containing a `.` cannot be
addressed by a dotted path. JSON files are not supported yet.

## Crash safety

| Mode | What survives a crash / `kill -9` while locked |
|---|---|
| `"recoverable": false` (default) | **Nothing.** The mapping lives only in memory, so the real values are gone. Restore from version control. |
| `"recoverable": true` | **Everything.** Before any file is touched, the mapping is written to `.loom.secret.lock`, encrypted with AES-256-GCM under a key derived from your password with Argon2id (64 MB, 3 passes). Run `loomlocker recover` and enter the same password. |

In recoverable mode `loomlocker start` refuses to start while a `.loom.secret.lock` is waiting, and
points you at `loomlocker recover`. A successful unlock or recover deletes the file. The default is
off on purpose: it means an encrypted copy of your secrets is on disk while locked.

## HTTP API

Base `http://127.0.0.1:{port}/api`.

| Method | Path | Password | Description |
|---|---|---|---|
| `GET` | `/ping` | — | health and lock state |
| `GET` | `/status` | — | lock state, number of secrets, file names |
| `POST` | `/lock` | — | lock now |
| `POST` | `/autolock` | — | lock now (cancels the timer) |
| `POST` | `/unlock` | body | unlock and start the re-lock timer |
| `POST` | `/stop` | body, if locked | restore the values, then shut down |

Anything that reveals or restores a secret needs the password. Locking never does: adding protection
is always safe.

## Threat model

**Assets:** the real secret values; the session password.

**What LoomLocker defends against**

| Threat | Defence |
|---|---|
| An agent or tool reads `.env` / `application.yaml` from disk | Files hold random tokens except during the short unlock window |
| Another machine on the network reaches the API | Server binds `127.0.0.1` only; the client refuses non-loopback `lockhost` |
| A web page in your browser talks to `localhost:8053` | Requests with an `Origin` header are refused (browsers add it to cross-origin requests, the CLI and libraries never do); bodies must be `application/json` (this forces a CORS preflight, which the server never grants); no CORS headers are ever sent |
| DNS rebinding | The `Host` header must be a loopback name |
| Guessing the password | bcrypt (cost 12); after 5 wrong attempts each further failure doubles the wait (1 s … 5 min, then `429` with `Retry-After`); the correct password is also refused during the wait. Applies to `unlock` and to `stop` |
| Password on disk | Never written; kept in memory as a bcrypt hash only |
| Half-finished lock strands your secrets | All-or-nothing writes with rollback; in recoverable mode the journal is saved first |
| Silent failure to re-lock | An automatic re-lock that fails is logged loudly; a `stop` that cannot restore the values refuses to stop |

**What it does NOT defend against**

- A process running as **your user** with enough privilege to read the loomlocker process's memory,
  attach a debugger, or read the files *during* the unlock window. The window is short, not zero.
- Malware or a malicious application you start with `--unlock`: it receives the real values by design.
- Other local users if your files are world-readable (permissions are preserved, not tightened).
- A weak password: the limiter slows guessing, it does not make `123` safe. Use a long one.
- Passwords longer than 72 bytes (bcrypt limit) are rejected at start rather than truncated.

## Client libraries

| Library | Language | Verified against a live server |
|---|---|---|
| `bloompy` | Python | yes (integration test + unit tests) |
| `gloom` | Go | yes |
| `loomj` | Java | yes |

All follow the same contract: `unlock()` (password from the argument, else `LOOM_SESSION_PASSWORD`),
`execute(fn)` (always runs), `autolock()` (re-lock immediately; only sent after a successful unlock).
If loomlocker is not running, everything is a no-op and `execute` still runs.

## Commands

```
loomlocker start     start the server (asks for and confirms the password)
loomlocker lock      lock now (no password)
loomlocker unlock    unlock (asks for the password)
loomlocker status    show the state
loomlocker stop      restore the real values, then stop (asks for the password if locked)
loomlocker recover   restore real values after a crash (recoverable mode)
loom execute <cmd> [--unlock]
```

## Tests

`go test ./...` in `loomlocker/` covers the crypto, config, file handling, journal and server
(including every protection above: each was checked by removing it and confirming a test fails).
`loomlocker/integration` builds the real `loomlocker` and `loom` binaries and drives them end to end:
lock → `loom execute --unlock` → automatic re-lock → `stop`, kill -9 → `recover`, every endpoint
probed without a password, and the Python, Go and Java client libraries.
