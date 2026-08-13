#!/usr/bin/env bash
# Fails when a value printed in the documentation no longer matches the file
# that actually decides it.
#
# Two kinds of drift have happened here. Automated dependency updates move the
# build inputs and leave the prose behind. Tuning a default in the adapter moves
# what the mod writes into capture.properties on first start, while three
# documents keep quoting the old number. Both produce a document that is simply
# wrong, and in both cases nothing else in the build noticed.

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

# The adapter writes capture.properties itself on first start, so a reader who
# compares the generated file against the documentation must see the same
# numbers in both.
configuration=adapters/fabric/src/main/java/org/worldledger/fabric/CaptureConfiguration.java
queue_capacity=$(sed -n 's/.*DEFAULT_QUEUE_CAPACITY *= *\([0-9][0-9]*\).*/\1/p' "$configuration")
if [ -z "$queue_capacity" ]; then
    echo "could not read DEFAULT_QUEUE_CAPACITY out of $configuration" >&2
    exit 1
fi

for readme in README.md adapters/fabric/README.md examples/minecraft-26.2-fixture/README.md; do
    if ! grep -qE "^queue_capacity=$queue_capacity\$" "$readme"; then
        printf '%s: queue_capacity should read %s\n' "$readme" "$queue_capacity" >&2
        status=1
    fi
done

if [ "$status" -eq 0 ]; then
    echo "documented baseline and adapter defaults match the files that decide them"
fi
exit "$status"
