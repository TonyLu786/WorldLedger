# ADR 0006: Whether the desktop application gets a window outside Windows

**Status:** proposed

## Context

The README's first instruction is to take `worldledger-desktop` for your
platform and run it. On Windows it opens a window. On macOS and Linux
`runWindow` returns `false` without attempting anything, so it opens a browser
tab instead.

That sentence has been corrected to say which platform gets which, so nobody is
promised something they will not get. This is about whether to close the gap
rather than describe it.

## What it would cost, checked rather than assumed

There is no dependency-free web view on either platform. macOS needs WKWebView
through Cocoa and Linux needs WebKitGTK, and both are reached from Go through
cgo.

The release builds every target from one Ubuntu runner with `CGO_ENABLED: '0'`
(`.github/workflows/release.yml:57`). Both modules cross-compile for
`darwin/arm64` and `linux/amd64` today with cgo off, which was verified while
writing this. That is not an accident of the current code; it is the reason one
runner can produce every artefact, and it is what makes a desktop binary a
single file with nothing to install beside it.

A native window on either platform ends that:

- **macOS** has to be built on a macOS runner, because the SDK is not
  redistributable. The artefact stays a single file.
- **Linux** has to be built on a Linux runner with the WebKitGTK development
  packages, **and the person running it needs the runtime library installed**.
  A binary that fails to start because `libwebkit2gtk` is absent is a worse
  first experience than a browser tab, and it fails on exactly the machines
  least likely to have it.
- CI currently never compiles `darwin/arm64` at all, which the audit recorded.
  Adding a platform whose only build happens at tag time compounds that.

## Options

### A. Leave it, and say so

Where this is now. The browser is the whole application on those platforms,
with nothing missing, and the README says which platform gets which.

Cost: the first-contact experience on two of three platforms is a browser tab
somebody did not ask for, and the watchdog and the "WorldLedger is not running
any more" banner exist because of it.

### B. A cgo web view per platform

Real windows everywhere.

Cost: three build jobs instead of one, a macOS runner, a runtime dependency on
Linux that can fail at launch, the end of "one file, nothing to install", and
the first cgo in a project whose core module has no dependencies at all. The
release would also stop being reproducible from one machine.

### C. Ask a Chromium browser for a window

`--app=<url>` opens a browser with no tabs, no address bar and its own taskbar
entry. Chrome, Edge, Brave and Chromium all support it and at least one is
present on most machines. No build change, no new dependency, and a browser that
is absent or refuses falls back to the tab that happens today.

Cost: it is a heuristic. Finding the browser means looking in known places per
platform, which is untested code on platforms this project does not build in CI
— the exact shape the audit criticised. It is a window that is still a browser:
a person who has set Safari or Firefox as their default gets nothing new, and
Safari has no equivalent.

### D. A separate native shell

Ship a small platform-native launcher beside the binary.

Cost: a second artefact, a second language, and a second thing to sign. It is
the largest of the four and it is what a project with a paid installer does.

## Recommendation

**C, if anything, and only with the platform discovery covered by CI on the
platforms it discovers on.** It is the only option that does not change what a
release is, and its failure mode is today's behaviour rather than a binary that
will not start.

But the honest recommendation is **A until something else is true**. Nothing
about the browser path is broken: the application is complete in it, it now says
when it has stopped, and the set-up screen says which build somebody is holding.
What a window buys is the feeling of an application rather than a capability,
and the two open items ahead of it — the capture bundle's exposure when the mod
and the core are different ages, and an archive that outgrows one machine — are
about whether the thing works at all.

Worth revisiting when any of these becomes true: somebody reports the browser
path as a reason they stopped; macOS or Linux becomes more than a small share of
use; or CI grows per-platform runners for another reason, at which point B costs
much less than it does today.

## Consequences if A stands

- `window_other.go` keeps returning false, and its comment already says why.
- The README and the site keep naming which platform gets which, and the
  documentation guard has no rule that would catch it drifting back. Worth one.
- The watchdog's arrival deadline and the page's stopped banner stay
  load-bearing on two platforms out of three rather than being a fallback.
