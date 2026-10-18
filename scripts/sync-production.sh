#!/usr/bin/env bash
#
# Triggers the content sync on the production droplet.
#
# A thin SSH wrapper around ~/holybook/sync-content.sh — the same script cron
# runs nightly on the droplet. It pulls the holybook/data repository and, if
# main has advanced (or the database schema is outdated), reimports the dataset
# in a single transaction. It is idempotent and guarded against overlapping
# runs, so running it when the database is up to date is a cheap no-op.
#
# Usage:
#   scripts/sync-production.sh [-h ssh_host] [-i identity_file] [-f]
#
# Options / environment overrides:
#   -h  SSH target      (default: $HOLYBOOK_SSH_HOST or deploy@holybook.app)
#   -i  identity file   (default: $HOLYBOOK_SSH_KEY or ~/.ssh/holybook)
#   -f  force a reimport even if the database is up to date
set -euo pipefail

SSH_HOST="${HOLYBOOK_SSH_HOST:-deploy@holybook.app}"
SSH_KEY="${HOLYBOOK_SSH_KEY:-$HOME/.ssh/holybook}"
REMOTE_SCRIPT="/home/deploy/holybook/sync-content.sh"
FORCE=""

while getopts "h:i:f" opt; do
  case "$opt" in
    h) SSH_HOST="$OPTARG" ;;
    i) SSH_KEY="$OPTARG" ;;
    f) FORCE="FORCE=1" ;;
    *)
      echo "Usage: $0 [-h ssh_host] [-i identity_file] [-f]" >&2
      exit 2
      ;;
  esac
done

ssh_opts=(-o ConnectTimeout=10)
if [ -n "$SSH_KEY" ] && [ -f "$SSH_KEY" ]; then
  ssh_opts+=(-i "$SSH_KEY")
fi

echo "Triggering content sync on ${SSH_HOST} ..."

# Mirror the PATH of the deploy user's crontab so docker, git and flock resolve
# exactly as during the nightly run. The remote exit status propagates back.
ssh "${ssh_opts[@]}" "$SSH_HOST" \
  "PATH=/usr/local/bin:/usr/bin:/bin ${FORCE} ${REMOTE_SCRIPT}"
