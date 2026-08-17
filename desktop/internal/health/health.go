// Package health reports whether this machine is set up to capture.
//
// The command line answers this by failing at the point of use: a person runs
// ingest-spool, is told no spool was found, and has to work backwards to
// whether the mod ever ran. That is a reasonable way to learn it for somebody
// who already knows what a spool is.
//
// This asks the question in advance and in order, so that the first thing the
// application says is either "you are ready" or the one thing standing in the
// way. Each check reports what it found rather than only whether it passed,
// because "Fabric API missing" and "Fabric API present but for the wrong
// Minecraft" need different actions and look identical as a red cross.
package health

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/worldledger/worldledger-mc/internal/mcpath"
)

// State is what a check found.
type State string

const (
	// OK means this part is in place and correct.
	OK State = "ok"
	// Missing means it is not there, and installing it is something the
	// application can offer to do.
	Missing State = "missing"
	// Wrong means something is there but does not match what the mod needs.
	// This is kept apart from Missing deliberately: replacing a player's
	// existing file is a different act from adding one, and it is theirs to
	// approve.
	Wrong State = "wrong"
	// Unknown means the check could not be carried out, which is not the same
	// as the answer being no.
	Unknown State = "unknown"
)

// Check is one line the player reads.
type Check struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	State  State  `json:"state"`
	Detail string `json:"detail"`
	// Fix names what the application would do about it, in the words it would
	// use in the button. Empty when there is nothing it can do, in which case
	// Detail has to carry the explanation on its own.
	Fix string `json:"fix,omitempty"`
}

// Report is the whole answer.
type Report struct {
	Root   string  `json:"root"`
	Checks []Check `json:"checks"`
	// Ready is true when capture would work if the player started the game now.
	Ready bool `json:"ready"`
}

// NotFound is the report for a machine where no Minecraft directory exists at
// any of the places a launcher puts one.
//
// It names every one of them. "Minecraft not found" on its own leaves somebody
// with a working Minecraft in an unusual place unable to tell that from a
// broken application.
func NotFound(looked []string) Report {
	detail := "No Minecraft directory found."
	if len(looked) > 0 {
		detail += " Looked in: " + strings.Join(looked, ", ")
	}
	return Report{
		Checks: []Check{{
			ID:     "minecraft",
			Title:  "Minecraft",
			State:  Missing,
			Detail: detail,
		}},
	}
}

// Inspect reads the machine and reports. It never writes anything.
func Inspect(install mcpath.Install) Report {
	report := Report{Root: install.Root}

	root := check{ID: "minecraft", Title: "Minecraft"}
	if !isDir(install.Root) {
		root.State = Missing
		root.Detail = "No Minecraft directory at " + install.Root
		// Installing Minecraft is not this application's business, and pretending
		// otherwise would put a button on something it cannot do.
		report.Checks = append(report.Checks, Check(root))
		return report
	}
	root.State = OK
	root.Detail = install.Root
	report.Checks = append(report.Checks, Check(root))

	versions := readVersions(install.Versions())

	report.Checks = append(report.Checks, checkRelease(versions))
	report.Checks = append(report.Checks, checkLoader(versions))
	report.Checks = append(report.Checks, checkMod(install.Mods(), "fabric-api", "Fabric API",
		"install Fabric API "+FabricAPIVersion))
	report.Checks = append(report.Checks, checkMod(install.Mods(), "worldledger", "WorldLedger mod",
		"install the WorldLedger mod"))
	report.Checks = append(report.Checks, checkContributor(install.CaptureProperties()))

	report.Ready = true
	for _, c := range report.Checks {
		if c.State != OK {
			report.Ready = false
		}
	}
	return report
}

// check exists so the fields can be filled in stages and converted once. It is
// the same shape as Check.
type check Check

// version is the part of a launcher version manifest that matters here.
type version struct {
	ID           string `json:"id"`
	InheritsFrom string `json:"inheritsFrom"`
}

// readVersions lists what the launcher has installed.
//
// A directory that cannot be read yields no versions rather than an error. The
// checks below then report what they could not confirm, which is more useful
// than one failure standing in for six answers.
func readVersions(dir string) []version {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []version
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		raw, err := os.ReadFile(filepath.Join(dir, name, name+".json"))
		if err != nil {
			continue
		}
		var v version
		if err := json.Unmarshal(raw, &v); err != nil {
			continue
		}
		if v.ID == "" {
			v.ID = name
		}
		out = append(out, v)
	}
	return out
}

func checkRelease(versions []version) Check {
	for _, v := range versions {
		if v.ID == MinecraftVersion {
			return Check{
				ID:     "release",
				Title:  "Minecraft " + MinecraftVersion,
				State:  OK,
				Detail: "installed",
			}
		}
	}
	installed := make([]string, 0, len(versions))
	for _, v := range versions {
		if v.InheritsFrom == "" {
			installed = append(installed, v.ID)
		}
	}
	detail := "Minecraft " + MinecraftVersion + " is not installed"
	if len(installed) > 0 {
		detail += ". Installed: " + strings.Join(installed, ", ")
	}
	// The launcher installs a release the first time it is played, and doing it
	// for somebody would mean driving their launcher.
	return Check{
		ID:     "release",
		Title:  "Minecraft " + MinecraftVersion,
		State:  Missing,
		Detail: detail + ". Select it in the Minecraft launcher and play it once.",
	}
}

func checkLoader(versions []version) Check {
	for _, v := range versions {
		if v.InheritsFrom == MinecraftVersion && strings.HasPrefix(v.ID, "fabric-loader-") {
			return Check{
				ID:     "loader",
				Title:  "Fabric Loader",
				State:  OK,
				Detail: v.ID,
			}
		}
	}
	// A loader for another release is worth saying out loud: it looks like
	// Fabric is installed, and it is, for a Minecraft the mod does not target.
	for _, v := range versions {
		if strings.HasPrefix(v.ID, "fabric-loader-") {
			return Check{
				ID:     "loader",
				Title:  "Fabric Loader",
				State:  Wrong,
				Detail: v.ID + " is for another Minecraft, not " + MinecraftVersion,
				Fix:    "install Fabric Loader " + LoaderVersion + " for " + MinecraftVersion,
			}
		}
	}
	return Check{
		ID:     "loader",
		Title:  "Fabric Loader",
		State:  Missing,
		Detail: "not installed for Minecraft " + MinecraftVersion,
		Fix:    "install Fabric Loader " + LoaderVersion,
	}
}

// checkMod looks for a jar whose name starts with a marker.
//
// Matching on the file name is weaker than reading the jar's own metadata, and
// it is what the state of the directory allows without opening every archive on
// every check. What it can get wrong is reporting a mod present when a file was
// renamed, which the installer does not rely on: it writes its own file name
// and records it.
func checkMod(modsDir, marker, title, fix string) Check {
	entries, err := os.ReadDir(modsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return Check{ID: marker, Title: title, State: Missing,
				Detail: "the mods folder does not exist yet", Fix: fix}
		}
		return Check{ID: marker, Title: title, State: Unknown,
			Detail: "could not read " + modsDir}
	}
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if strings.HasPrefix(name, marker) && strings.HasSuffix(name, ".jar") {
			return Check{ID: marker, Title: title, State: OK, Detail: entry.Name()}
		}
	}
	return Check{ID: marker, Title: title, State: Missing, Detail: "not in " + modsDir, Fix: fix}
}

// checkContributor reads the one value a person has to supply.
//
// The adapter writes this file with an empty contributor on first start and
// records nothing until it is filled in, which is the single most likely reason
// for somebody to play an evening and find an empty spool.
func checkContributor(path string) Check {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Check{
				ID: "contributor", Title: "Your name", State: Missing,
				Detail: "the mod has not run yet, so there is nothing to write to",
				Fix:    "set it once the mod is installed",
			}
		}
		return Check{ID: "contributor", Title: "Your name", State: Unknown,
			Detail: "could not read " + path}
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") {
			continue
		}
		name, value, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(name) != "contributor" {
			continue
		}
		if value = strings.TrimSpace(value); value != "" {
			return Check{ID: "contributor", Title: "Your name", State: OK, Detail: value}
		}
		break
	}
	return Check{
		ID: "contributor", Title: "Your name", State: Missing,
		Detail: "nothing is recorded until this is set",
		Fix:    "choose a name",
	}
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
