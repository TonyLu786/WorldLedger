#!/usr/bin/env bash
# Runs the Fabric client game test without Gradle.
#
# Gradle cannot start in some environments: its launcher talks to its workers
# over a loopback socket, and where that is blocked every task fails with
# "Unable to establish loopback connection" before any build logic runs. The
# game test is the only end-to-end check of capture and the only thing that
# re-verifies the capture fingerprint, so losing it to that is losing the gate.
#
# Everything Gradle would do here is mechanical, and Loom has already written
# the hard part down. .gradle/loom-cache/launch.cfg holds the launch
# specification, the dependency jars are in the Gradle cache, and the sources
# compile with javac. This assembles those and launches the same client.
#
# It does NOT accept Mojang's EULA. build.gradle deliberately refuses to do that
# on an operator's behalf, and so does this: the run directory must already
# carry an eula.txt from a run you authorised.
#
# Usage:
#   scripts/run-client-gametest.sh [--skip-build] [--timeout SECONDS]

set -euo pipefail

repository="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fabric="$repository/adapters/fabric"
tools="$(cd "$repository/.." && pwd)/.tools"
jdk="$tools/jdk25/jdk-25.0.4+7"
gradle_home="$tools/gradle-home"
run_dir="$fabric/build/run-gametest"
work="${TMPDIR:-/tmp}/worldledger-gametest"

skip_build=0
build_only=0
timeout_seconds=900
while [ $# -gt 0 ]; do
	case "$1" in
		--skip-build) skip_build=1 ;;
		# Compiles and prepares without starting a client, which is the half of
		# this that can be checked on a machine that should not open a window.
		--build-only) build_only=1 ;;
		--timeout) shift; timeout_seconds="$1" ;;
		*) echo "unknown option: $1" >&2; exit 2 ;;
	esac
	shift
done

step() { printf '==> %s\n' "$1"; }
fail() { printf '!!  %s\n' "$1" >&2; exit 1; }

[ -x "$jdk/bin/java" ] || fail "no JDK at $jdk"

# The client game test starts a Minecraft dedicated server, whose networking is
# Netty, whose event loop is a java.nio Selector. On Windows a Selector is built
# from a loopback self-connect, and an environment that blocks that pattern
# fails here rather than anywhere informative: Minecraft reports "failed to
# create a child event loop" several minutes into startup, and Gradle reports
# "Unable to establish loopback connection" before running any build logic. The
# same restriction, two unrecognisable messages. Checking costs a second.
preflight="$work/Preflight.java"
mkdir -p "$work"
cat > "$preflight" <<'JAVA'
import java.nio.channels.Selector;

public class Preflight {
	public static void main(String[] args) throws Exception {
		Selector.open().close();
	}
}
JAVA
if ! "$jdk/bin/java" "$preflight" >/dev/null 2>&1; then
	fail "this environment cannot open a java.nio Selector, so the Minecraft dedicated server
    the game test needs cannot start. Nothing in this repository can work around it:
    the same restriction is why Gradle reports \"Unable to establish loopback
    connection\". Run this where a Selector can be opened; everything else here is
    ready. Check with:
      $jdk/bin/java $preflight"
fi
[ -f "$fabric/.gradle/loom-cache/launch.cfg" ] || fail \
	"no $fabric/.gradle/loom-cache/launch.cfg. Loom writes it during a Gradle build, so one
    successful ./gradlew build has to have happened on this machine before this script can work."

# Java argument files treat a backslash as an escape, and cygpath -m emits a
# //?/ prefix for long paths that java.nio refuses to parse. Every path handed
# to the JVM goes through here.
win() { cygpath -m "$1" | sed 's|^//?/||'; }

mkdir -p "$work"

# ---- classpath ------------------------------------------------------------
# The runtime Minecraft jars are the ones under .gradle/loom-cache that
# launch.cfg names, not the -deobf jars used to compile against.
{
	find "$fabric/.gradle/loom-cache/minecraftMaven" -name '*.jar' ! -name '*.backup'
	find "$gradle_home/caches/modules-2" -name '*.jar' | grep -viE 'sources|javadoc'
	for set in main client gametest; do
		echo "$fabric/build/classes/java/$set"
		echo "$fabric/build/resources/$set"
	done
} | while read -r path; do win "$path"; done | tr '\n' ';' > "$work/runtime-cp.txt"

compile_cp=$({
	find "$gradle_home/caches/fabric-loom/minecraftMaven" -name '*deobf-26.2.jar'
	find "$gradle_home/caches/modules-2" -name '*.jar' | grep -viE 'sources|javadoc'
} | while read -r path; do win "$path"; done | tr '\n' ';')

# ---- build ----------------------------------------------------------------
# Into the same directories Gradle uses, because launch.cfg names them.
compile() {
	local set="$1" extra="$2" out="$fabric/build/classes/java/$1"
	# Source paths go in relative and the working directory is the adapter, so
	# that a repository under a path with a space in it still works: a javac
	# argument file splits on whitespace and has no way to quote a source.
	( cd "$fabric" && find "src/$set/java" -name '*.java' ) > "$work/$set-sources.txt" 2>/dev/null || true
	[ -s "$work/$set-sources.txt" ] || return 0
	rm -rf "$out"
	mkdir -p "$out"
	{
		printf -- '-nowarn\n-d\n"%s"\n-cp\n"%s%s"\n' "$(win "$out")" "$extra" "$compile_cp"
		cat "$work/$set-sources.txt"
	} > "$work/$set-args.txt"
	( cd "$fabric" && "$jdk/bin/javac" "@$work/$set-args.txt" )
	printf '    %-9s %s classes\n' "$set" "$(find "$out" -name '*.class' | wc -l)"
}

if [ "$skip_build" -eq 0 ]; then
	step 'Compiling the adapter'
	main_out="$(win "$fabric/build/classes/java/main");"
	client_out="$(win "$fabric/build/classes/java/client");"
	compile main ''
	compile client "$main_out"
	compile gametest "$main_out$client_out"

	# processResources, minus the parts nothing here reads.
	step 'Copying resources'
	for set in main client gametest; do
		[ -d "$fabric/src/$set/resources" ] || continue
		mkdir -p "$fabric/build/resources/$set"
		cp -r "$fabric/src/$set/resources/." "$fabric/build/resources/$set/"
	done
	# Gradle expands ${version} during processResources. Copying the sources over
	# its output puts the placeholder back, and a manifest that still carries one
	# is not a mod Fabric Loader will load, so this has to happen every time.
	version="$(sed -n 's/^[[:space:]]*mod_version[[:space:]]*=[[:space:]]*//p' "$fabric/gradle.properties" | tr -d ' \r')"
	[ -n "$version" ] || fail "no mod_version in $fabric/gradle.properties"
	for manifest in "$fabric"/build/resources/*/fabric.mod.json; do
		[ -f "$manifest" ] || continue
		sed -i "s/\${version}/$version/g" "$manifest"
		! grep -q '\${' "$manifest" || fail "unexpanded placeholder left in $manifest"
	done
	printf '    mod version %s\n' "$version"
fi

# ---- prepare the run directory -------------------------------------------
# The same values build.gradle's prepareClientGametest writes. queue_capacity is
# deliberately below the shipped default so the run exercises backpressure.
step 'Preparing the run directory'
mkdir -p "$run_dir/config/worldledger"
cat > "$run_dir/config/worldledger/capture.properties" <<'PROPERTIES'
contributor=client-gametest
server_id=worldledger-client-gametest
coalesce_ticks=10
queue_capacity=8
max_snapshots_per_tick=1
PROPERTIES

# A spool left by an earlier run would let the fingerprint pass on old output.
rm -rf "$run_dir/config/worldledger/spool"

if ! grep -qi '^eula=true' "$run_dir/eula.txt" 2>/dev/null; then
	fail "the client game test starts a Minecraft dedicated server, which requires accepting
    Mojang's End User Licence Agreement: https://aka.ms/MinecraftEULA

    This script will not accept it for you. Run the Gradle task once with
      ./gradlew runClientGametest -Pworldledger.acceptMinecraftEula=true
    or write eula=true into $run_dir/eula.txt yourself."
fi

if [ "$build_only" -eq 1 ]; then
	step 'Built and prepared; not launching a client because --build-only was given'
	exit 0
fi

# ---- launch ---------------------------------------------------------------
dli="$(find "$gradle_home/caches/modules-2" -iname 'dev-launch-injector*.jar' | grep -v sources | head -1)"
[ -n "$dli" ] || fail 'dev-launch-injector is not in the Gradle cache'

cat > "$work/launch-args.txt" <<ARGS
-cp
"$(win "$dli");$(cat "$work/runtime-cp.txt")"
"-Dfabric.dli.config=$(win "$fabric/.gradle/loom-cache/launch.cfg")"
-Dfabric.dli.env=client
-Dfabric.dli.main=net.fabricmc.loader.impl.launch.knot.KnotClient
-Dfabric.client.gametest=true
-Dfabric.client.gametest.modid=worldledger-gametest
net.fabricmc.devlaunchinjector.Main
ARGS

step "Running the client game test (opens a Minecraft window; up to ${timeout_seconds}s)"
cd "$run_dir"
set +e
timeout "$timeout_seconds" "$jdk/bin/java" "@$work/launch-args.txt" > "$work/gametest.log" 2>&1
status=$?
set -e

if [ $status -eq 124 ]; then
	fail "the game test did not finish within ${timeout_seconds}s; log: $work/gametest.log"
fi

# ---- report ---------------------------------------------------------------
grep -E 'client-thread capture cost|Client-thread capture cost' "$work/gametest.log" | tail -1 || true
ready=$(find "$run_dir/config/worldledger/spool" -maxdepth 1 -name 'ready-*' 2>/dev/null | wc -l)
printf '    %s ready bundle(s) in the spool\n' "$ready"

if [ $status -ne 0 ]; then
	echo
	grep -iE 'exception|error|failed' "$work/gametest.log" | head -10
	fail "the game test exited $status; full log: $work/gametest.log"
fi
if [ "$ready" -eq 0 ]; then
	fail "the run produced no bundles, so it proved nothing; log: $work/gametest.log"
fi

step "Passed. Log: $work/gametest.log"
