#!/usr/bin/env bash
# Fails when the baseline versions printed in the READMEs no longer match the
# files that actually drive the build.
#
# Automated dependency updates move the real values and leave the prose behind.
# The README claims those values are exact build inputs, so a stale line there
# is a false claim rather than a cosmetic problem, and nothing else catches it.

set -euo pipefail

cd "$(dirname "$0")/.."

properties=adapters/fabric/gradle.properties
wrapper=adapters/fabric/gradle/wrapper/gradle-wrapper.properties

property() { sed -n "s/^$1=//p" "$properties" | tr -d '\r'; }

minecraft=$(property minecraft_version)
loader=$(property loader_version)
loom=$(property loom_version)
api=$(property fabric_api_version)
gradle=$(sed -n 's/.*gradle-\([0-9][0-9.]*\)-bin\.zip.*/\1/p' "$wrapper" | tr -d '\r')

for value in "$minecraft" "$loader" "$loom" "$api" "$gradle"; do
    if [ -z "$value" ]; then
        echo "could not read a version out of $properties or $wrapper" >&2
        exit 1
    fi
done

status=0

expect() { # file label value
    local pattern
    pattern=$(printf '%s' "$3" | sed 's/[.+]/\\&/g')
    if ! grep -qE "^$2 +$pattern( |\$)" "$1"; then
        printf '%s: %s should read %s\n' "$1" "$2" "$3" >&2
        status=1
    fi
}

for readme in README.md adapters/fabric/README.md; do
    expect "$readme" 'Minecraft' "$minecraft"
    expect "$readme" 'Fabric Loader' "$loader"
    expect "$readme" 'Fabric API' "$api"
    expect "$readme" 'Fabric Loom' "$loom"
    expect "$readme" 'Gradle' "$gradle"
done

if [ "$status" -eq 0 ]; then
    echo "documented baseline matches the build files"
fi
exit "$status"
