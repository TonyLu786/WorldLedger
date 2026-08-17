#!/usr/bin/env bash
# Fails when a document that ships in a release archive links to a file the
# archive does not contain.
#
# The release workflow already checks this, but only when a tag is pushed, which
# is the worst moment to find out: the release has started, the check runs after
# the binaries are built, and the fix is a new tag. The check is cheap enough to
# run any time, so this reproduces it against the same file list the workflow
# packages.
#
# The failure it catches is not exotic. A document is written in the repository,
# where every path resolves, and links to something outside the packaged set --
# scripts/, .github/, testdata/ -- which resolves for the author and for anybody
# reading on the forge, and for nobody who downloaded the archive.

set -euo pipefail

repository="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
staging="$(mktemp -d)"
trap 'rm -rf "$staging"' EXIT

cd "$repository"

# The same set .github/workflows/release.yml copies into dist. Keeping the two
# in step is the point: a document is only as reachable as the package it ships
# in, so this list has to be the package's list.
mkdir -p "$staging/profiles" "$staging/adapters/fabric"
cp LICENSE NOTICE README.md CHANGELOG.md CONTRIBUTING.md SECURITY.md "$staging/"
cp profiles/*.json "$staging/profiles/"
cp -r docs spec examples "$staging/"
cp adapters/fabric/README.md adapters/fabric/gradle.properties "$staging/adapters/fabric/"

broken="$staging/.broken"
: > "$broken"

cd "$staging"
while IFS= read -r document; do
	directory="$(dirname "$document")"
	# Relative links only. A fragment is a place inside a document rather than a
	# file, and an absolute URL is somebody else's to keep working.
	# A document with no links makes grep exit 1, and under pipefail that ends
	# the run with no output at all: the check would report failure for every
	# repository whose first document happens to link to nothing.
	targets="$(grep -oE '\]\([^)#][^)]*\)' "$document" 2>/dev/null || true)"
	[ -n "$targets" ] || continue
	printf '%s\n' "$targets" |
		sed 's/](//; s/)$//; s/#.*$//' |
		{ grep -vE '^[a-z]+:' || true; } |
		while IFS= read -r target; do
			[ -z "$target" ] && continue
			[ -e "$directory/$target" ] || echo "$document -> $target" >> "$broken"
		done
done < <(find . -name '*.md')

if [ -s "$broken" ]; then
	echo "the packaged documents reference files the package does not contain:" >&2
	sed 's/^/  /' "$broken" >&2
	echo >&2
	echo "Either add the file to the package list in .github/workflows/release.yml" >&2
	echo "and to this script, or refer to it without making it a link." >&2
	exit 1
fi

echo "every relative link in the packaged documents resolves"
