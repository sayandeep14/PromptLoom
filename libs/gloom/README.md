# gloom

Go client library for [LoomLocker](../../loomlocker/README.md) — safe secret management in AI-assisted projects.

## Install

```bash
go get github.com/sayandeepgiri/promptloom/libs/gloom
```

## Quick start

```go
import "github.com/sayandeepgiri/promptloom/libs/gloom"

func main() {
    gloom.NewSafe().
        Unlock().          // uses LOOM_SESSION_PASSWORD env var
        Execute(func() {
            godotenv.Load()    // load .env while secrets are visible
        }).
        Autolock()         // immediately re-lock after loading

    server.Start()
}
```

## API

### `NewSafe() *Safe`

Creates a `Safe` with config auto-detected from `.loom.config` walking up the directory tree, then overridden by `LOOM_HOST` / `LOOM_PORT` env vars.

### `WithConfig(cfg *Config) *Safe`

Creates a `Safe` with an explicit config.

### `(*Safe).Silent() *Safe`

Suppresses all log output.

### `(*Safe).Unlock(password ...string) *Safe`

Unlocks secrets. Password resolution order:
1. Argument passed to `Unlock()`
2. `LOOM_SESSION_PASSWORD` env var
3. Skip — secrets stay locked, `Execute` still runs

No-op if loomlocker is not running.

### `(*Safe).Execute(fn func()) *Safe`

Runs `fn`. Always called, regardless of lock state.

### `(*Safe).Autolock() *Safe`

Signals loomlocker that startup is complete → triggers immediate relock. No-op if loomlocker is not running.

## Config

Connection settings are loaded automatically from `.loom.config` (JSON, walks up from `os.Getwd()`):

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

Go 1.22+. No external dependencies.
