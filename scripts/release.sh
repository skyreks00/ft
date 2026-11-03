#!/bin/sh
# Tags a version and publishes its binaries on GitHub with goreleaser.
#   scripts/release.sh v0.1.0
# Uses GITHUB_TOKEN, or the gh CLI's token.
set -eu
tag=${1:?usage: scripts/release.sh vX.Y.Z}
cd "$(dirname "$0")/.."

case "$tag" in v[0-9]*.[0-9]*.[0-9]*) ;; *) echo "tag must look like v0.1.0" >&2; exit 1 ;; esac
if [ -n "$(git status --porcelain)" ]; then
	echo "commit or stash your changes first" >&2
	exit 1
fi

export GITHUB_TOKEN="${GITHUB_TOKEN:-$(gh auth token)}"

make test
git tag -a "$tag" -m "$tag"
git push origin "$tag"
goreleaser release --clean
