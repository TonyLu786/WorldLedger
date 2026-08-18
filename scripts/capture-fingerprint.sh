#!/usr/bin/env bash
# Turns a capture spool into a content fingerprint.
#
# Both platforms run this same script so that a difference between their
# outputs is a difference in what the adapter produced, not in how each side
# was measured. The fingerprint carries state and component digests only, so it
# is unaffected by the instants, contributors, and session identifiers that
# necessarily differ between two runs.
#
# Usage: scripts/capture-fingerprint.sh <spool-dir> <output-file>

set -euo pipefail

if [ "$#" -ne 2 ]; then
    echo "usage: $0 <spool-dir> <output-file>" >&2
    exit 2
fi

spool=$1
output=$2

if [ ! -d "$spool" ]; then
    echo "no spool directory at $spool" >&2
    exit 1
fi

cd "$(dirname "$0")/.."

workspace=$(mktemp -d)
trap 'rm -rf "$workspace"' EXIT

go build -trimpath -o "$workspace/worldledger" ./cmd/worldledger
archive="$workspace/archive"
"$workspace/worldledger" init "$archive" >/dev/null

imported=0
for bundle in "$spool"/ready-*; do
    [ -d "$bundle" ] || continue
    "$workspace/worldledger" ingest-bundle --archive "$archive" "$bundle" >/dev/null
    imported=$((imported + 1))
done

if [ "$imported" -eq 0 ]; then
    echo "the spool at $spool held no ready bundle; a fingerprint of nothing would compare clean against anything" >&2
    exit 1
fi

# An archive that fails its own integrity check cannot be used as a reference
# for another machine.
"$workspace/worldledger" fsck --archive "$archive" >/dev/null

# A fingerprint only describes what the world contained, not whether the world
# contained what it was meant to. The game test places its fixture with server
# commands whose results nobody reads, so a block a release renames places
# nothing, fails nothing, and yields a smaller fingerprint that is just as
# green. This says which of the shapes are actually there.
"$workspace/worldledger" corpus --archive "$archive"

"$workspace/worldledger" fingerprint --archive "$archive" --out "$output"
echo "imported $imported bundle(s)"
