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

With `TRUST_PROXY=1` the registry also reads `X-Forwarded-Proto`; when the proxy says `https` every response carries `Strict-Transport-Security` (HSTS, two years, subdomains included), so browsers stop trying plain HTTP for that host. Make sure the proxy **overwrites** `X-Forwarded-For` and `X-Forwarded-Proto` rather than passing client-supplied values (Caddy and nginx's `proxy_set_header` do). Without `TRUST_PROXY`, HSTS is sent only when the registry itself terminates TLS.

`loom publish` refuses to send the upload secret over plain `http://` to anything but `localhost`, so use `https://` for a remote registry.

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `POSTGRES_PASSWORD` | required (Compose) | Password for the bundled database |
| `UPLOAD_SECRET` | required (Compose) | Admin secret(s) for publish/delete (`X-Upload-Secret`), min 16 chars each, comma-separated for rotation. The admin can manage every pack |
| `UPLOAD_TOKENS` | empty | Publisher identities `name=secret,name=secret`. Each publisher owns what it first published. Neither variable set ⇒ read-only registry |
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


## Who may change a pack

Every write is authenticated with `X-Upload-Secret`, and the secret tells the registry **who** is writing:

- `UPLOAD_SECRET` is the **admin**: it can replace or delete any pack.
- Each `name=secret` pair in `UPLOAD_TOKENS` is a **publisher**. The first time a publisher publishes a slug, the pack is theirs (`owner` appears in `GET /api/v1/vaults` and `/vaults/{slug}`). Only that publisher, or the admin, can replace or delete it; anyone else gets `403`.
- Ownership is enforced inside the database statement, so two publishers racing for a new slug cannot both win.
- Packs published before ownership existed have no owner and can be changed by the admin only. The admin can hand one over by setting its owner in SQL: `UPDATE vaults SET owner = 'alice' WHERE slug = 'kit';`
- An admin replacing a pack does not take it over.

```bash
UPLOAD_SECRET=$(openssl rand -hex 32)
UPLOAD_TOKENS=alice=$(openssl rand -hex 32),bob=$(openssl rand -hex 32)
```

The schema change (`owner` column) is applied by `AUTO_MIGRATE=1` (Compose enables it) or by running `internal/db/schema.sql`; it is idempotent and safe on an existing database.

### Rotating a secret

Several secrets can be valid for the same identity, so rotation needs no downtime:

1. Add the new secret next to the old one: `UPLOAD_TOKENS=alice=OLD,alice=NEW` (or `UPLOAD_SECRET=OLD,NEW` for the admin) and restart.
2. Switch the publisher over to `NEW`.
3. Remove `OLD` and restart. It stops working immediately.

Every configured secret is checked on every request, in constant time and with no early exit, so timing reveals neither which secret was closest nor how many exist. A secret can belong to one identity only (the server refuses to start otherwise), and configuration errors never print secrets.

## Request log

Each request writes one line to the log:

```
access ip=203.0.113.7 who=alice POST "/api/v1/vaults" status=200 bytes=31 dur=12ms
```

`who` is the publisher for authenticated writes and `-` otherwise. Headers, query strings and bodies are never logged (so secrets cannot leak into logs), and the path is quoted so a hostile URL cannot forge extra lines. Failed logins appear as `status=401` and are rate limited per IP.
