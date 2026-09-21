# LoomLocker

**Session-scoped secret protection for projects that AI tools and agents can read.**

AI coding assistants read your project files. `.env`, `application.yaml` and `config.json` are
exactly what they should not see. LoomLocker keeps the real values **out of the files** while you
work: the files hold random tokens, and the real values come back only for the few seconds your
application needs to start.

```
        locked (normal state)                       unlocked (a few seconds)
  .env      API_KEY=lk_7f3a9b2c1d4e5a6b        API_KEY=sk-live-real-value
  config.json  "password": "lk_c19e0a…"         "password": "hunter2"
        ▲                                              │
        └────────── fresh tokens, restored exactly ────┘
```

Locking and unlocking are **byte-exact and all-or-nothing**: quotes, `export`, comments, spacing,
CRLF line endings and key order come back exactly, and a failed lock changes nothing.

## Install

Download `loomlocker` for your platform from the
[releases page](https://github.com/sayandeep14/PromptLoom/releases) (it is in the same archive as
`loom`), or build it:

```bash
cd loomlocker && go build -o loomlocker ./cmd/loomlocker
```

## Quick start

1. Say what to protect, in `.loom.config` at the project root:

   ```json
   {
     "secret": [
       ".loom.secret",
       ".env:{API_KEY}",
       "application.yaml:{db.password}",
       "config.json:{servers.0.password}"
     ],
     "loomlocker": { "active": true, "port": "8053", "unlock_duration_seconds": 10 },
     "custom": { "runproject": "python server.py" }
   }
   ```

2. Start LoomLocker (it asks for a session password, held in memory only) and lock:

   ```bash
   loomlocker start        # interactive: type 'lock' to lock, 'unlock' to restore
   ```

3. Run your app so it gets the real values just long enough to read them:

   ```bash
   loom execute runproject --unlock      # unlocks, runs the command, and it re-locks itself
   ```

   or call the client library from your app (below).

## What can be locked

| Entry | Locks |
|---|---|
| `".loom.secret"` | every `KEY=VALUE` in the file |
| `".env:{API_KEY}"` | every assignment of that key |
| `"application.yaml:{a.b.c}"` | the scalar at that dotted path |
| `"config.json:{a.b.c}"` | the string at that dotted path; array positions are numbers: `"servers.0.password"` |

Limits: YAML values must be single-line scalars; JSON values must be **strings** (a number or
boolean would change type when replaced by a token) in strict JSON (no comments or trailing commas);
a key containing a `.` cannot be addressed by a dotted path. Anything unsupported is refused with a
clear error *before* any file is touched. Details and guarantees: [DESIGN.md](DESIGN.md).

## Commands

| Command | What it does |
|---|---|
| `loomlocker start` | start the local server (asks for the session password) |
| `loomlocker lock` | lock now (no password needed: adding protection is always safe) |
| `loomlocker unlock` | restore the real values (password required) and start the re-lock timer |
| `loomlocker status` | lock state |
| `loomlocker stop` | restore the values if locked, then shut down |
| `loomlocker recover` | restore values after LoomLocker died while locked (needs `"recoverable": true`) |

## From your application

Client libraries unlock, run your startup code, and re-lock immediately, and do nothing (secrets
stay locked) when LoomLocker is not running.

```python
# Python — pip install bloompy
from bloompy import Safe
Safe().unlock().execute(load_dotenv).autolock()
```

```go
// Go — go get github.com/sayandeep14/PromptLoom/libs/gloom
gloom.NewSafe().Unlock().Execute(func() { godotenv.Load() }).Autolock()
```

```java
// Java — dev.promptloom:loomj
new Safe().unlock().execute(() -> Dotenv.load()).autolock();
```

The password comes from the argument or `LOOM_SESSION_PASSWORD`. See the libraries:
[bloompy](../libs/bloompy/README.md), [gloom](../libs/gloom/README.md), [loomj](../libs/loomj/README.md).

## Security model in one screen

- The server listens on **loopback only**; browsers cannot talk to it (Origin/Host/content-type
  checks, no CORS); the password is never written to disk and guessing is rate limited.
- Files hold random tokens except during the short unlock window; every lock uses **fresh** tokens.
- **`"recoverable": true`** writes an encrypted journal (AES-256-GCM, Argon2id) before touching any
  file, so `loomlocker recover` works after a crash. Off by default: it means an encrypted copy of
  your secrets is on disk while locked.

It does **not** defend against a process running as you that can read memory, or an application you
start with `--unlock` (it receives the real values by design). The full threat model, the HTTP API
and the crash-safety table are in [DESIGN.md](DESIGN.md).
