# so-budget

Household budget for two people with separate bank accounts. Each person's raw
transactions are private and encrypted with a key only their password unlocks;
only transactions they flag as household are shared. Bank data comes from
[SimpleFIN Bridge](https://beta-bridge.simplefin.org/).

Design: `docs/superpowers/specs/2026-09-11-so-budget-design.md`.

## Run locally

    brew install go
    SB_DEV=1 SB_SIMPLEFIN_FAKE=1 go run ./cmd/so-budget invite me
    SB_DEV=1 SB_SIMPLEFIN_FAKE=1 go run ./cmd/so-budget serve

Open the printed invite link, set a password, save the recovery code, log in,
then paste the fake setup token printed in the server log under Settings.

## Deploy

    docker build -t so-budget .
    docker run -d --name so-budget -v so-budget-data:/data \
      -e SB_PEPPER="$(openssl rand -hex 32)" -e SB_BASE_URL=https://budget.example.com \
      -p 127.0.0.1:8080:8080 so-budget
    docker exec so-budget so-budget invite alice   # prints an invite link

The image runs as uid 10001; a named volume like `so-budget-data` above picks up
that ownership automatically, but a bind mount (`-v ./data:/data`) must be
owned by uid 10001 yourself first, e.g. `chown -R 10001:10001 ./data`.

Put Caddy (see `Caddyfile.example`) or any HTTPS reverse proxy in front. Cookies
are `Secure` unless `SB_DEV=1`. Keep `SB_PEPPER` stable and backed up: it is
mixed into the transaction id hashes.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `SB_DB_PATH` | `data/so-budget.db` | SQLite file |
| `SB_LISTEN` | `:8080` | listen address |
| `SB_BASE_URL` | `http://localhost:8080` | used in invite links |
| `SB_PEPPER` | required unless dev | server secret for id hashing |
| `SB_DEV` | unset | `1` allows insecure cookies and a default pepper |
| `SB_TZ` | `America/Toronto` | timezone for bank dates |
| `SB_IDLE_TIMEOUT` / `SB_ABS_TIMEOUT` | `30m` / `12h` | session limits |
| `SB_BACKUP_DIR` | unset | enable scheduled backups into this dir |
| `SB_BACKUP_INTERVAL` / `SB_BACKUP_KEEP` | `24h` / `14` | backup cadence and retention |
| `SB_SIMPLEFIN_FAKE` | unset | `1` serves sample data instead of SimpleFIN |

Manual backup: `so-budget backup /path/to/copy.db`. Backups contain private
data only in encrypted form.

## Privacy model

- Each user has a random data key wrapped by their password (Argon2id) and by a
  one-time recovery code. The key is unwrapped at login and lives in server
  memory for the session.
- Credentials, accounts, transactions and private rules are AES-256-GCM blobs.
- Sync only runs while the owner is logged in, so the SimpleFIN access URL is
  never readable to a background job.
- What this does not protect against: an operator who modifies the running
  server or dumps process memory during a session.
