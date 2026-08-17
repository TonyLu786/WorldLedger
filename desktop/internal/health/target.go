package health

// What the mod was built against.
//
// These are not a preference. The adapter is compiled against one Minecraft
// release, and installing it into another is how somebody's game stops
// starting. The installer refuses rather than guesses, and it can only refuse
// correctly if these agree with adapters/fabric/gradle.properties.
//
// Keeping them in step is checked by scripts/check-documented-versions.sh,
// which already guards the same numbers where they appear in prose. A bump to
// the build that left these behind would otherwise produce an application that
// refuses a correct installation, or installs into the wrong one.
const (
	MinecraftVersion = "26.2"
	LoaderVersion    = "0.19.3"
	FabricAPIVersion = "0.156.0+26.2"
)
