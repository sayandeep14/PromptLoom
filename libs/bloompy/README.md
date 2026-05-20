# bloompy

Python client library for [LoomLocker](../../loomlocker/README.md) — safe secret management in AI-assisted projects.

## Install

```bash
pip install bloompy
```

Or from source:

```bash
pip install -e libs/bloompy
```

## Quick start

```python
from bloompy import Safe
from dotenv import load_dotenv

Safe().unlock().execute(load_dotenv).autolock()

# or as a context manager
with Safe().unlock() as safe:
    load_dotenv()
# autolock happens automatically on __exit__

app.run()
```

## API

### `Safe(config=None)`

Creates a `Safe`. Config is auto-detected from `.loom.config` walking up the directory tree, then overridden by `LOOM_HOST` / `LOOM_PORT` env vars.

### `Safe.silent() -> Safe`

Suppresses all log output.

### `Safe.unlock(password=None) -> Safe`

Unlocks secrets. Password resolution order:
1. `password` argument
2. `LOOM_SESSION_PASSWORD` env var
3. Skip — secrets stay locked, `execute` still runs

No-op if loomlocker is not running.

### `Safe.execute(fn: Callable) -> Safe`

Calls `fn()`. Always called, regardless of lock state.

### `Safe.autolock() -> Safe`

Signals loomlocker that startup is complete → triggers immediate relock.

### Context manager

```python
with Safe().unlock() as safe:
    load_dotenv()
# autolock called automatically on exit
```

## Config

Settings loaded from `.loom.config` (JSON, walks up from `os.getcwd()`):

```json
{
  "loomlocker": {
    "lockhost": "http://localhost",
    "port": "8053"
  }
}
```

Override with env vars: `LOOM_HOST`, `LOOM_PORT`.

## Requirements

Python 3.8+. Optional: `requests` (falls back to stdlib `urllib` if not installed).
