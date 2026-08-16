# Version compatibility

## The asymmetry

Minecraft migrates an older world forward by itself. It has no path backwards.

So a faithful export is readable by the release it was captured from and by anything newer, and is unreadable by anything older. That asymmetry, not a limitation of this project, is why converting for an older release is a separate operation.

## Two commands, on purpose

`export` writes observed state unchanged. It never approximates, and the translation code is not reachable from it.

`convert` writes an approximated copy into a separate world. It refuses to run without an explicit `--target-profile`, so an export cannot quietly become an approximation.

Conversion runs from the archive rather than from an already-written world. A world file cannot express the difference between "we know this is air" and "we never observed this", and converting from one would discard that distinction permanently.

## What conversion can and cannot carry

| Outcome | Meaning | Lossy |
| --- | --- | --- |
| `identity` | the target already has this state | no |
| `renamed` | the same block under a different identifier; properties preserved | no |
| `substituted` | a functionally similar block chosen by the operator; properties dropped unless requested | yes |
| `filled` | no rule applied, so the filler block was written | yes |
| `unrepresentable` | not carried across | yes |

Substitutions drop properties by default because a replacement block usually does not share them. Carrying `axis=y` onto a block with no `axis` property produces a state the target release rejects, so keeping them has to be requested explicitly per rule.

When nothing covers a state, the operator chooses:

```sh
--on-unrepresentable report      # refuse, list everything that does not fit, write nothing
--on-unrepresentable skip-chunk  # leave the chunk unwritten (default)
--on-unrepresentable fill        # write the filler block
--filler minecraft:air           # configurable; a visible marker makes loss obvious in game
```

Every substitution and fill is counted by affected block positions and reported, so the cost is stated rather than implied.

## Hard limits

**Build height.** A section at Y=-4 has nowhere to go in a release whose world starts at Y=0. Such sections are dropped and counted, and the output takes the target dimension's own build range so `yPos` is correct.

**Block entities.** Their payloads are the network representation of the captured release. Nothing here migrates them, and a payload an older release cannot parse is a chunk it may refuse to load, so they are dropped when converting unless `--keep-block-entities` is given.

**Properties.** Profiles carry no block state property definitions, so only block identifiers are validated. A state whose block exists is reported as representable even if one of its properties was introduced later.

## Profiles and rules

A profile describes what a release can represent and is extracted from that release's own artifact:

```sh
go run ./cmd/mcprofile --jar <client.jar> --out profiles/minecraft-java-<version>.json
```

This is how "any version" is honestly delivered. No table of releases is hard-coded, because a hard-coded table would be unverifiable and would go stale. Whoever holds a release's jar can produce its profile.

Rules are declared data, validated against the target profile when loaded, so a rule pointing at a block the target does not have fails immediately rather than producing an unreadable chunk:

```json
{
  "schema": "worldledger.translation-rules/v1",
  "renames":       { "legacy:grass": "minecraft:short_grass" },
  "substitutions": { "examplemod:marble": { "block": "minecraft:stone", "keep_properties": false } }
}
```

`cmd/dfurenames` extracts Mojang's own rename tables from the compiled data fixers to seed this file. It reports coverage honestly: some fixers are built from lambdas rather than rename tables and cannot be read mechanically, and those are named in the output rather than silently omitted. Note also that data fixers run forward, old name to new name. Using them to convert backwards means reversing them, and a reversal is only well defined where the forward mapping is injective; several vanilla renames are not.

## What cannot be converted

- Worlds whose generation is changed by a datapack, plugin, or server fork, where the placement and generation assumptions no longer describe the world.
- Any target release for which no profile exists. Two ship with the project, 26.2 and 1.21.11; for anything else, whoever holds that release's jar can extract one with the command above.

## What has been checked in Minecraft

A world converted for 1.21.11 has been opened by Mojang's 1.21.11 server. It loaded without a chunk error, answered `execute if block` correctly at five coordinates read out of the converted file, and rewrote those chunks from its own world model with every block still at the same coordinate.

That world loses nothing in conversion, so it exercises the faithful path rather than the lossy one, and a server says nothing about rendering. [`status.md`](status.md) records both limits along with the evidence.
