// Package installer puts the mod into somebody's Minecraft.
//
// This is the only part of the application that writes outside its own
// directory, into a game somebody has probably played for years, and it is the
// part where a bug does not look like a bug: it looks like their Minecraft
// stopped starting.
//
// So it is built the other way round from the rest. Nothing is done until the
// whole of it has been worked out and shown; every file written is recorded;
// and anything overwritten is kept, so that undoing it is a replay of the
// record rather than a guess about what was there before.
//
// It is also meant to be over quickly. The official way to add Fabric is to
// download and run an installer, which needs Java, opens a window and takes a
// minute. All that installer does for a client is write one version profile
// and add a launcher entry, and Fabric publishes the profile itself, so this
// fetches a few kilobytes of JSON and writes the same thing. Three small
// requests instead of a program.
package installer

import (
	"fmt"

	"github.com/worldledger/worldledger-mc/desktop/internal/health"
	"github.com/worldledger/worldledger-mc/internal/mcpath"
)

// Kind is what a step does, which decides how it is applied and how it is
// undone.
type Kind string

const (
	// WriteLoaderProfile writes versions/<id>/<id>.json from Fabric's own
	// published profile.
	WriteLoaderProfile Kind = "loader-profile"
	// AddLauncherEntry adds an installation to launcher_profiles.json so the
	// version appears in the launcher rather than only on disk.
	AddLauncherEntry Kind = "launcher-entry"
	// DownloadMod fetches a jar into mods/.
	DownloadMod Kind = "mod"
	// WriteContributor sets the one value the adapter needs from a person.
	WriteContributor Kind = "contributor"
)

// Step is one thing that will be done, named in the words the person is shown
// and with the exact path it will touch.
//
// Target is not decoration. This is the list somebody approves before anything
// happens, and a list that says "install Fabric" without saying where is not
// something anybody can meaningfully agree to.
type Step struct {
	Kind   Kind   `json:"kind"`
	Title  string `json:"title"`
	Target string `json:"target"`
	Source string `json:"source,omitempty"`
}

// Plan is everything that would be done, or the reason none of it can be.
type Plan struct {
	Root  string `json:"root"`
	Steps []Step `json:"steps"`
	// Refusal is set when this cannot proceed. It is not an error: a plan that
	// refuses is a correct answer, and the reason is what the person needs.
	Refusal string `json:"refusal,omitempty"`
	// Contributor is the name that will be written, when the plan includes
	// writing one.
	Contributor string `json:"contributor,omitempty"`
}

// Runnable reports whether there is anything to do and nothing stopping it.
func (p Plan) Runnable() bool { return p.Refusal == "" && len(p.Steps) > 0 }

// launcherRunning is a variable so that the tests do not depend on whether
// somebody happens to have Minecraft open on the machine running them.
//
// Calling the real check directly made the suite pass in CI and fail on a
// developer's machine with the launcher in the background, which is the kind
// of result that teaches people to stop believing a test suite.
var launcherRunning = launcherIsRunning

// loaderProfileURL is where Fabric publishes the profile the launcher needs.
func loaderProfileURL(minecraft, loader string) string {
	return fmt.Sprintf("https://meta.fabricmc.net/v2/versions/loader/%s/%s/profile/json", minecraft, loader)
}

// fabricAPIURL is Fabric's own Maven, which is where the jar the launcher would
// otherwise be pointed at actually lives.
func fabricAPIURL(version string) string {
	return fmt.Sprintf("https://maven.fabricmc.net/net/fabricmc/fabric-api/fabric-api/%s/fabric-api-%s.jar",
		version, version)
}

// LoaderVersionID is the name Fabric gives its own version directory, and the
// name the launcher shows.
func LoaderVersionID() string {
	return "fabric-loader-" + health.LoaderVersion + "-" + health.MinecraftVersion
}

// BuildPlan works out what has to happen, from a report of what is already
// there. It reads nothing and writes nothing itself.
//
// modSource is where the WorldLedger jar comes from. It is passed in rather
// than fixed here because a release build points at that release's asset and a
// development build points at a file on disk, and neither should be able to
// masquerade as the other.
func BuildPlan(install mcpath.Install, report health.Report, modSource string, contributor string) Plan {
	plan := Plan{Root: install.Root, Contributor: contributor}

	state := map[string]health.State{}
	for _, check := range report.Checks {
		state[check.ID] = check.State
	}

	// Nothing can be installed into a Minecraft that is not there, and
	// installing Minecraft is not this application's business.
	if state["minecraft"] != health.OK {
		plan.Refusal = "Minecraft is not installed where this application can find it."
		return plan
	}
	// The mod is compiled against one release. Putting it into another is how a
	// game stops starting, so this refuses rather than trying.
	if state["release"] != health.OK {
		plan.Refusal = fmt.Sprintf(
			"Minecraft %s is not installed. The mod is built for that exact release, so it cannot be "+
				"added to another one. Select %s in the Minecraft launcher, play it once, then come back.",
			health.MinecraftVersion, health.MinecraftVersion)
		return plan
	}

	// The launcher writes launcher_profiles.json whenever it feels like it, and
	// read-modify-write from two programs at once is how somebody's list of
	// installations disappears. The game being open is fine -- mods are read at
	// start up, so writing them takes effect next launch.
	if state["loader"] != health.OK {
		if running, name := launcherRunning(); running {
			plan.Refusal = "The Minecraft launcher is open (" + name + "). Close it first, " +
				"so that adding Fabric to its list cannot collide with the launcher writing to it."
			return plan
		}
	}

	if state["loader"] != health.OK {
		id := LoaderVersionID()
		plan.Steps = append(plan.Steps,
			Step{
				Kind:   WriteLoaderProfile,
				Title:  "Add Fabric " + health.LoaderVersion + " for Minecraft " + health.MinecraftVersion,
				Target: install.VersionProfile(id),
				Source: loaderProfileURL(health.MinecraftVersion, health.LoaderVersion),
			},
			Step{
				Kind:   AddLauncherEntry,
				Title:  "Add it to the Minecraft launcher's list",
				Target: install.LauncherProfiles(),
			})
	}

	if state["fabric-api"] != health.OK {
		plan.Steps = append(plan.Steps, Step{
			Kind:   DownloadMod,
			Title:  "Add Fabric API " + health.FabricAPIVersion,
			Target: install.Mod("fabric-api-" + health.FabricAPIVersion + ".jar"),
			Source: fabricAPIURL(health.FabricAPIVersion),
		})
	}

	if state["worldledger"] != health.OK {
		plan.Steps = append(plan.Steps, Step{
			Kind:   DownloadMod,
			Title:  "Add the WorldLedger mod",
			Target: install.Mod("worldledger.jar"),
			Source: modSource,
		})
	}

	if contributor != "" && state["contributor"] != health.OK {
		plan.Steps = append(plan.Steps, Step{
			Kind:   WriteContributor,
			Title:  "Record under the name " + contributor,
			Target: install.CaptureProperties(),
		})
	}

	return plan
}
