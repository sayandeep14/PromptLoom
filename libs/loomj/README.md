# loomj

Java client library for [LoomLocker](../../loomlocker/README.md) — safe secret management in AI-assisted projects.

## Install

### Maven

```xml
<dependency>
    <groupId>dev.promptloom</groupId>
    <artifactId>loomj</artifactId>
    <version>0.1.0</version>
</dependency>
```

### Gradle

```groovy
implementation 'dev.promptloom:loomj:0.1.0'
```

## Quick start

```java
import dev.promptloom.loomj.Safe;

public class App {
    public static void main(String[] args) {
        new Safe()
            .unlock()               // uses LOOM_SESSION_PASSWORD env var
            .execute(() -> Dotenv.load())   // load secrets while visible
            .autolock();            // immediately re-lock

        server.start();
    }
}
```

## API

### `new Safe()`

Creates a `Safe` with config auto-detected from `.loom.config` walking up the directory tree, then overridden by `LOOM_HOST` / `LOOM_PORT` env vars.

### `new Safe(LockerConfig config)`

Creates a `Safe` with an explicit config.

### `Safe.silent() -> Safe`

Suppresses all log output.

### `Safe.unlock(String... password) -> Safe`

Unlocks secrets. Password resolution order:
1. Argument passed to `unlock()`
2. `LOOM_SESSION_PASSWORD` env var
3. Skip — secrets stay locked, `execute` still runs

No-op if loomlocker is not running.

### `Safe.execute(Runnable fn) -> Safe`

Runs `fn`. Always called, regardless of lock state.

### `Safe.autolock() -> Safe`

Signals loomlocker that startup is complete → triggers immediate relock. No-op if loomlocker is not running.

## Config

Settings loaded from `.loom.config` (JSON, walks up from `user.dir`):

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

Java 11+. No external dependencies (uses `java.net.http.HttpClient`).
