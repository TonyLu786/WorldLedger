# Upgrading Minecraft

A new release changes what the game reports, and the archive core is supposed to be indifferent to that except where the release genuinely changed something. This is what to run when one lands, and how to tell a game change from a regression.

## What is pinned, and what each thing answers

| Pinned | Question it answers |
| --- | --- |
| `profiles/minecraft-java-<version>.json` | what a release can represent |
| `testdata/capture-fingerprint-reference.txt` | what the adapter produces from the pinned game-test world |
| golden vectors in `internal/mcjava` | what canonical encoding produces for fixed inputs |
| [`the 26.2 fixture`](../examples/minecraft-26.2-fixture/README.md) | block entities, a sign edit, an unopened chest, and the ordered mutation sequence |

The golden vectors describe encoding, not Minecraft, so no release can move them. If they change, the change came from this repository. The other three can legitimately move when the game does, which is the whole difficulty.

## When a release lands

Extract its profile. Nothing here is hand-written, so this needs only that release's jar:

```sh
go run ./cmd/mcprofile --jar <client.jar> --out profiles/minecraft-java-<version>.json
```

Compare it against the release the archive core was last checked against:

```sh
go run ./cmd/mcprofile --from profiles/minecraft-java-26.2.json --to profiles/minecraft-java-<version>.json
```

Then run the client game test. `runClientGametest` in `adapters/fabric` drives a real client, and `run-client-gametest.ps1` under `scripts/` runs the same thing without Gradle. Either way it rebuilds the capture fingerprint and compares it to the committed reference.

## Reading the two results together

The fingerprint says whether anything moved. The profile delta says whether the release explains it. Neither is enough alone, which is why both are run.

| Fingerprint | Delta | What it means |
| --- | --- | --- |
| unchanged | empty | Nothing to do beyond committing the new profile. |
| unchanged | adds only | The release added things the fixture world does not use. Commit the profile; the archive core is unaffected. |
| changed | explains it | A game change. Update the reference and record the explanation alongside it. |
| changed | does not explain it | A regression. Do not update the reference. |

The last row is the one the rest exists for, and the fixture procedure already states the rule it descends from: do not update committed golden values to accommodate an unexplained live mismatch.

A delta that reports anything under "bears on observations already captured" needs a decision even when the fingerprint is clean, because the fingerprint only covers what the fixture world happens to contain. A removed block nothing in the fixture uses moves no digest and still breaks an archive that recorded it.

## What an upgrade will not tell you

**Where the release puts its files.** 26.2 moved every dimension under `dimensions/<namespace>/<path>/region/`, including the vanilla three, and no profile says so. That was found by exporting into a real world and watching the game ignore it. A release that moves them again will be found the same way, so the export check against a real client is not optional.

**Block state properties.** A profile records block identifiers only, for the reason given in [`version-compatibility.md`](version-compatibility.md): a client jar describes states through the render definitions, which omit properties that do not change a model. A release that adds a property to an existing block is invisible here.

**Whether a converted world still renders.** [`status.md`](status.md) records what has and has not been opened in a real client.
