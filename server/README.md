# PromptLoom Registry

The registry stores prompt packs so teams can share them with `loom publish` and `loom install`.
It is a small Go service backed by PostgreSQL. PromptLoom has no built-in registry: you run this one.

## Quick start (Docker)

```bash
cd server
cp .env.example .env
```

Edit `.env` and set two values (use hex so they are URL-safe):

```bash
POSTGRES_PASSWORD=$(openssl rand -hex 16)
UPLOAD_SECRET=$(openssl rand -hex 32)      # must be at least 16 characters
```

```bash
docker compose up -d --build
curl http://localhost:8080/healthz          # {"status":"ok"}
```

Compose starts PostgreSQL (data in the `pgdata` volume, no published port) and the registry, and the registry applies its own schema on first start.
By default the registry listens on `127.0.0.1:8080` only.

Point the CLI at it:

```bash
loom publish ./my-pack --registry http://localhost:8080 --secret "$UPLOAD_SECRET"
loom install my-pack   --registry http://localhost:8080
```

## Going public: TLS

The registry speaks plain HTTP. Put a TLS-terminating reverse proxy in front of it and publish only the proxy. With Caddy (automatic certificates):

```caddyfile
registry.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

Then set `TRUST_PROXY=1` in `.env` so per-IP rate limits use the address the proxy reports. Set it **only** when a proxy is in front; otherwise clients could spoof their IP.

`loom publish` refuses to send the upload secret over plain `http://` to anything but `localhost`, so use `https://` for a remote registry.

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `POSTGRES_PASSWORD` | required (Compose) | Password for the bundled database |
| `UPLOAD_SECRET` | required (Compose) | Secret for publish/delete (`X-Upload-Secret`), min 16 chars. Unset ⇒ read-only registry |
| `BIND_ADDR` / `HOST_PORT` | `127.0.0.1` / `8080` | Where Compose publishes the registry |
| `CORS_ORIGINS` | empty | Allowed browser origins; empty sends no CORS headers |
| `TRUST_PROXY` | empty | `1` behind a reverse proxy |
| `MAX_BODY_BYTES` | `8388608` | Request body cap |
| `RATE_LIMIT_READ_PER_MIN` | `120` | Per-IP |
| `RATE_LIMIT_WRITE_PER_MIN` | `10` | Per-IP; failed auth attempts count |
| `DATABASE_URL` | set by Compose | PostgreSQL URL (needed when not using Compose) |
| `AUTO_MIGRATE` | `1` in Compose | Apply the schema on startup (idempotent, safe with several replicas) |
| `PORT` | `8080` | Listen port inside the container |

## Operations

**Logs:** `docker compose logs -f registry`

**Backup**

```bash
docker compose exec -T db pg_dump -U registry registry > backup.sql
```

**Restore** (into an empty database)

```bash
docker compose exec -T db psql -U registry registry < backup.sql
```

**Upgrade:** `git pull && docker compose up -d --build` — the schema migrates itself.

**Rotate the upload secret:** change `UPLOAD_SECRET` in `.env`, then `docker compose up -d`. Existing packs are unaffected.

**Reset everything (deletes all packs):** `docker compose down -v`

## Without Docker

```bash
export DATABASE_URL='postgres://user:pass@host:5432/registry?sslmode=require'
export UPLOAD_SECRET=$(openssl rand -hex 32)
export AUTO_MIGRATE=1        # or apply internal/db/schema.sql yourself
go run .
```

The same variables can live in `.env` (loaded automatically). Managed PostgreSQL (RDS, Cloud SQL, Neon, …) works: point `DATABASE_URL` at it and drop the `db` service.

## API

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/api/v1/vaults` | — | List packs |
| `GET` | `/api/v1/vaults/{slug}` | — | Pack metadata |
| `GET` | `/api/v1/vaults/{slug}/bundle` | — | Full bundle (JSON) |
| `POST` | `/api/v1/vaults` | `X-Upload-Secret` | Upload / replace a pack |
| `DELETE` | `/api/v1/vaults/{slug}` | `X-Upload-Secret` | Delete a pack |
| `GET` | `/healthz` | — | Liveness |

## Security notes

- Writes fail closed: no `UPLOAD_SECRET` ⇒ `503`, never open.
- One shared secret authorises writes to **every** pack. Give it only to people you trust to overwrite any pack.
- Uploads are validated (slug, version, file types, sizes, safe paths). `loom install` re-validates on the client.
- The container runs as non-root on a read-only filesystem with all capabilities dropped; the database is not exposed to the host.

## Tests

```bash
go test ./...                                  # unit tests
TEST_DATABASE_URL='postgres://postgres:pw@localhost:55432/loom_test?sslmode=disable' go test ./...   # + PostgreSQL tests
```

The database name must contain `test`; the integration tests wipe its tables.
