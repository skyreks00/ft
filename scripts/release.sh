#!/bin/sh
# Tags a version and publishes its binaries on GitHub with goreleaser.
#   scripts/release.sh v0.1.0
# Reads FT_CLIENT_ID and FT_BROKER_URL (public values) from .env; uses
# GITHUB_TOKEN, or the gh CLI's token.
set -eu
tag=${1:?usage: scripts/release.sh vX.Y.Z}
cd "$(dirname "$0")/.."

case "$tag" in v[0-9]*.[0-9]*.[0-9]*) ;; *) echo "tag must look like v0.1.0" >&2; exit 1 ;; esac
if [ -n "$(git status --porcelain)" ]; then
	echo "commit or stash your changes first" >&2
	exit 1
fi

set -a
. ./.env
set +a
: "${FT_CLIENT_ID:?FT_CLIENT_ID missing in .env}"
: "${FT_BROKER_URL:?FT_BROKER_URL missing in .env}"
case "$FT_BROKER_URL" in
http://localhost* | http://127.*) echo "FT_BROKER_URL points to a local broker" >&2; exit 1 ;;
esac
export GITHUB_TOKEN="${GITHUB_TOKEN:-$(gh auth token)}"

make test
git tag -a "$tag" -m "$tag"
git push origin "$tag"
goreleaser release --clean
