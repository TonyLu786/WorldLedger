#!/usr/bin/env bash
# Fails when a document that ships in a release archive tells the reader to run
# a command of this project's that the release does not build.
#
# The sibling check, check-packaged-links.sh, catches a document linking to a
# file the archive does not contain. This is the same failure one level up, and
# it went unnoticed longer: docs/upgrading-minecraft.md and
# docs/version-compatibility.md both ship, both instructed the reader to run
# mcprofile, and for a while the release built only worldledger. Every link
# resolved. The instruction still could not be followed by anybody who had
# downloaded a release, and version-compatibility.md's promise that whoever
# holds a release's jar can produce its profile was not keepable.
#
# A bare invocation is the claim being checked. `go run ./cmd/mcprofile` says
# outright that it needs a checkout, so it is left alone; `mcprofile --jar ...`
# says the binary is at hand.

set -euo pipefail

repository="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repository"

# The same set .github/workflows/release.yml copies into dist, minus the files
# that are not documents.
documents=$(
	{
		printf '%s\n' README.md CHANGELOG.md CONTRIBUTING.md SECURITY.md
		find docs spec examples -name '*.md'
		printf '%s\n' adapters/fabric/README.md
	} | sort -u
)

commands=$(find cmd -mindepth 1 -maxdepth 1 -type d -exec basename {} \; | sort)
built=$(grep -oE '\./cmd/[a-z-]+' .github/workflows/release.yml | sed 's|\./cmd/||' | sort -u)

missing=""
for name in $commands; do
	if printf '%s\n' "$built" | grep -qx "$name"; then
		continue
	fi
	# Only lines inside a fenced block count, so prose that happens to begin
	# with a command's name is not mistaken for an instruction to run it.
	found=$(printf '%s\n' "$documents" | while IFS= read -r document; do
		awk -v name="$name" -v file="$document" '
			/^```/ { fenced = !fenced; next }
			!fenced { next }
			{
				line = $0
				sub(/^[ \t]*/, "", line)
				sub(/^\$ /, "", line)
				if (line ~ "^" name "([ \t]|$)") print file ":" FNR
			}
		' "$document"
	done)
	if [ -n "$found" ]; then
		missing="${missing}${name}:
$(printf '%s\n' "$found" | sed 's/^/    /')
"
	fi
done

if [ -n "$missing" ]; then
	echo "packaged documents tell the reader to run commands the release does not build:" >&2
	printf '%s' "$missing" | sed 's/^/  /' >&2
	echo >&2
	echo "Either build the command in .github/workflows/release.yml, or write the" >&2
	echo "instruction as 'go run ./cmd/<name>' so it says a checkout is needed." >&2
	exit 1
fi

echo "every command the packaged documents invoke by name is built by the release"
