# Deployment

The app runs on a single DigitalOcean droplet as three containers managed by
Docker Compose:

- **db** – PostgreSQL 16
- **web** – the holybook server (pages, static files and `/api`)
- **caddy** – TLS termination and reverse proxy, with automatic Let's Encrypt
  certificates

The same image (`ghcr.io/holybook/app`) also runs the one-off **importer**.

[`.github/workflows/deploy.yml`](../.github/workflows/deploy.yml) runs on every
push to `main`: it runs the tests, builds and pushes the image, copies the files
of this directory to `~/holybook` on the droplet, writes `.env` from GitHub
secrets, restarts the stack and runs the content sync (which only reimports if
the content or the database schema changed). The droplet never builds anything.

**Database roles:** the server only reads, so it connects as a least-privilege
`webapp` role (SELECT only). All writes go through the importer, which connects
as the `server` owner role. The `webapp` role is created once by
[`db-init/01-webapp-role.sh`](db-init/01-webapp-role.sh), and
`ALTER DEFAULT PRIVILEGES` keeps it readable across reimports.

## One-time droplet setup

1. **Create the droplet** – the smallest shared-CPU droplet (1 GB) is plenty;
   the server uses about 30 MB of memory. Ubuntu LTS.

2. **Install Docker Engine + Compose plugin**:

   ```bash
   curl -fsSL https://get.docker.com | sudo sh
   sudo usermod -aG docker $USER   # log out/in afterwards
   ```

3. **Create the deploy directory**: `mkdir -p ~/holybook`

4. **Add the deploy SSH key.** Put the public key in `~/.ssh/authorized_keys`
   on the droplet and store the private key as the `DROPLET_SSH_KEY` secret.

5. **Give the droplet read access to holybook/data** (for the content sync):
   generate a key on the droplet (`ssh-keygen -t ed25519`), add it as a
   read-only deploy key of the `holybook/data` repository, and check that
   `ssh -T git@github.com` succeeds. For a public repository you can instead
   set `DATA_REPO=https://github.com/holybook/data.git`.

6. **Point DNS** for `holybook.app` at the droplet. Caddy needs ports 80/443.

## GitHub secrets

| Secret | Value |
| --- | --- |
| `DROPLET_HOST` | droplet IP or hostname |
| `DROPLET_USER` | SSH user (e.g. `deploy`) |
| `DROPLET_SSH_KEY` | private key matching the droplet's authorized_keys |
| `SITE_ADDRESS` | `holybook.app` (or `:80` to test over HTTP first) |
| `DB_PASSWORD` | password of the Postgres `server` role (importer) |
| `WEBAPP_PASSWORD` | password of the read-only `webapp` role (server); use a different value |
| `GEMINI_API_KEY` | Gemini API key; leave empty to disable AI translation |

`GITHUB_TOKEN` is provided automatically and is used to push and pull the image.

> Both database passwords are written into the Postgres volume the **first**
> time the `db` container initialises. Changing a secret later does not change
> the database; use `ALTER ROLE ... PASSWORD` inside Postgres instead.

## Content

Book XML lives in the **holybook/data** repository under
`content/<lang>/<author>[/<year>]/<id>.xml`.
[`sync-content.sh`](sync-content.sh) keeps the database in sync with it:

1. It clones or fetches `holybook/data` into `~/holybook/data`.
2. It runs the importer with the repository HEAD and `-if-changed`. The
   importer compares that commit (and the schema version) with what the last
   successful import recorded in the `sync_state` table, and if they differ it
   replaces the entire content **in a single transaction**: visitors keep seeing
   the old content until the new content is committed, and a failed import
   changes nothing and is retried on the next run.

Run it by hand (`FORCE=1` reimports even when up to date):

```bash
ssh deploy@holybook.app ~/holybook/sync-content.sh
# or from a checkout of this repository:
scripts/sync-production.sh [-f]
```

Schedule it daily with cron (`crontab -e` as the deploy user):

```cron
0 4 * * * /home/deploy/holybook/sync-content.sh >> /home/deploy/holybook/sync-content.log 2>&1
```

## Migrating from the Kotlin deployment

The Kotlin/React deployment used the same droplet layout, database volume and
secrets, so switching is a regular deploy:

- The `web` service now runs the holybook image; the separate `web`/`caddy`
  frontend image is gone (Caddy runs the stock image with
  [`Caddyfile`](Caddyfile) mounted).
- The first `sync-content.sh` run after the deploy reimports the content even
  though the data commit did not change, because the database has no
  `schema_version` yet. Until then the site returns errors for a few seconds;
  the deploy workflow runs the sync right after starting the new server.
- The secrets are unchanged. `SERVER_IMAGE`, `WEB_IMAGE` and `IMPORT_IMAGE` are
  replaced by `HOLYBOOK_IMAGE` in `.env` (the workflow writes it).

## Operations

```bash
cd ~/holybook
docker compose ps
docker compose logs -f web              # server logs (JSON)
docker compose logs -f caddy            # TLS / proxy logs
docker compose pull web && docker compose up -d
```

Postgres is not published on the host. For ad-hoc access use an SSH tunnel
(`ssh -L 5432:localhost:5432 ...` together with `docker compose port`, or
`docker compose exec db psql -U server holybook`). Do **not** add a bare
`ports: ["5432:5432"]` mapping: Docker bypasses `ufw` and would expose Postgres
to the internet; bind to `127.0.0.1` if you must publish it.

### Backups

The content can always be rebuilt from holybook/data, but a dump is cheap:

```bash
docker compose exec db pg_dump -U server holybook | gzip > holybook-$(date +%F).sql.gz
```
