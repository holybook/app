#!/usr/bin/env bash
#
# Syncs the holybook/data repository into the database.
#
# Pulls the data repo and runs the importer with -if-changed: the importer
# compares the repo HEAD with the commit recorded by the last successful import
# (and the schema version it was imported with), and replaces the whole
# database content only if they differ. The import runs in a single
# transaction, so the site keeps serving the old content until the new one is
# committed, and a failed import changes nothing and is retried next run.
#
# Designed to be run from cron AND by hand over SSH. It is a no-op when there
# are no new commits, so running it frequently is cheap.
#
# Environment overrides:
#   COMPOSE_DIR  directory holding compose.yaml + .env (default: script dir)
#   DATA_REPO    git URL of the data repo
#   BRANCH       branch to track (default: main)
#   FORCE=1      reimport even if the database is up to date
set -euo pipefail

COMPOSE_DIR="${COMPOSE_DIR:-$(cd "$(dirname "$0")" && pwd)}"
DATA_REPO="${DATA_REPO:-git@github.com:holybook/data.git}"
BRANCH="${BRANCH:-main}"
DATA_DIR="$COMPOSE_DIR/data"

ts() { date -u +%FT%TZ; }

cd "$COMPOSE_DIR"

# Prevent overlapping runs (e.g. a slow import overrunning into the next cron
# tick). If flock is ever missing we proceed without locking rather than
# silently skipping the import.
if command -v flock >/dev/null 2>&1; then
  exec 9>"$COMPOSE_DIR/.sync-content.lock"
  if ! flock -n 9; then
    echo "$(ts) another sync is already running, skipping"
    exit 0
  fi
else
  echo "$(ts) flock not available, running without an overlap lock"
fi

if [ ! -d "$DATA_DIR/.git" ]; then
  echo "$(ts) cloning $DATA_REPO"
  git clone --branch "$BRANCH" --single-branch "$DATA_REPO" "$DATA_DIR"
else
  git -C "$DATA_DIR" fetch --quiet origin "$BRANCH"
fi
git -C "$DATA_DIR" reset --hard --quiet "origin/$BRANCH"
sha=$(git -C "$DATA_DIR" rev-parse HEAD)

if_changed="-if-changed"
if [ "${FORCE:-}" = "1" ]; then
  if_changed=""
fi

echo "$(ts) syncing database to $sha"
# Credentials come from .env via compose.yaml, never from the command line.
docker compose run --rm importer -i /content -commit "$sha" $if_changed
echo "$(ts) sync complete"
