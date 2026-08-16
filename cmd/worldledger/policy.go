package main

import (
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/model"
	"github.com/worldledger/worldledger-mc/internal/policy"
)

func cmdPolicy(args []string) error {
	if len(args) == 0 {
		return usageError("policy")
	}
	switch args[0] {
	case "show":
		return cmdPolicyShow(args[1:])
	case "set":
		return cmdPolicySet(args[1:])
	case "list":
		return cmdPolicyList(args[1:])
	default:
		return fmt.Errorf("unknown policy subcommand %q; want show, set, or list", args[0])
	}
}

func cmdPolicySet(args []string) error {
	fs := flag.NewFlagSet("policy set", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	server := fs.String("server", "", "stable server id")
	disposition := fs.String("disposition", "", "private, embargoed, research, or public")
	until := fs.String("until", "", "RFC3339 end of an embargo")
	declaredBy := fs.String("declared-by", "", "who is making this declaration")
	note := fs.String("note", "", "why")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" || *server == "" || *disposition == "" || *declaredBy == "" {
		return usageError("policy set")
	}

	parsed, err := policy.ParseDisposition(*disposition)
	if err != nil {
		return err
	}
	declaration := policy.ServerPolicy{
		Server:      *server,
		Disposition: parsed,
		DeclaredBy:  *declaredBy,
		Note:        *note,
	}
	if *until != "" {
		moment, err := time.Parse(time.RFC3339, *until)
		if err != nil {
			return fmt.Errorf("--until must be an RFC3339 timestamp: %w", err)
		}
		moment = moment.UTC()
		declaration.EmbargoUntil = &moment
	}

	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}
	store := policy.NewStore(a.Root)
	if err := store.Declare(declaration); err != nil {
		return err
	}
	fmt.Printf("declared %s for %s by %s\n", parsed, model.NormalizeToken(*server), *declaredBy)
	// The declaration was almost certainly made in order to export, and this is
	// the last place the path forks without saying so.
	fmt.Println("\nNext: create an empty single-player world in Minecraft, quit to the title")
	fmt.Println("      screen, then write the observed chunks into it:")
	fmt.Printf("  worldledger export --archive %s --server %s \\\n"+
		"      --into .minecraft/saves/<your-new-world>\n", *archivePath, model.NormalizeToken(*server))
	return nil
}

func cmdPolicyShow(args []string) error {
	fs := flag.NewFlagSet("policy show", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	server := fs.String("server", "", "stable server id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" || *server == "" {
		return usageError("policy show")
	}

	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}
	report, err := describePolicy(a, *server)
	if err != nil {
		return err
	}
	fmt.Print(report)
	return nil
}

func cmdPolicyList(args []string) error {
	fs := flag.NewFlagSet("policy list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" {
		return usageError("policy list")
	}

	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}
	declared, err := policy.NewStore(a.Root).List()
	if err != nil {
		return err
	}
	servers, err := a.Servers()
	if err != nil {
		return err
	}

	known := map[string]policy.ServerPolicy{}
	for _, item := range declared {
		known[item.Server] = item
	}
	fmt.Printf("%-34s %-12s %s\n", "SERVER", "DISPOSITION", "DECLARED BY")
	for _, server := range servers {
		if item, exists := known[server]; exists {
			fmt.Printf("%-34s %-12s %s\n", server, item.Disposition, item.DeclaredBy)
		} else {
			fmt.Printf("%-34s %-12s %s\n", server, "undeclared", "-")
		}
	}
	return nil
}

// requirePolicy refuses to build a world from a server nobody has made a
// decision about, and prints the decision when one exists.
//
// It does not refuse when the decision is "private". Local use is not the risk
// and blocking it would only teach operators to declare everything public. What
// is enforced is that the question was answered once, by a named person, before
// the archive produced something that can be handed to anyone else.
func requirePolicy(a archive.Archive, server string) error {
	report, err := describePolicy(a, server)
	if err != nil {
		return err
	}
	_, found, err := policy.NewStore(a.Root).Lookup(server)
	if err != nil {
		return err
	}
	if !found {
		fmt.Print(report)
		fmt.Println()
		return fmt.Errorf("no publication policy is declared for %s; run:\n"+
			"  worldledger policy set --archive DIR --server %s --disposition private|embargoed|research|public --declared-by YOUR-NAME",
			model.NormalizeToken(server), model.NormalizeToken(server))
	}
	fmt.Print(report)
	fmt.Println("\nthis writes a world on this machine; the declaration above governs passing it on")
	fmt.Println()
	return nil
}

// assessServer measures accumulated coverage across every dimension, because the
// exposure comes from the merged archive rather than from any one dimension.
func assessServer(a archive.Archive, server string) (policy.Assessment, error) {
	dimensions, err := a.Dimensions(server)
	if err != nil {
		return policy.Assessment{}, err
	}
	var chunks []model.ChunkRef
	for _, dimension := range dimensions {
		found, err := a.Chunks(server, dimension)
		if err != nil {
			return policy.Assessment{}, err
		}
		chunks = append(chunks, found...)
	}
	return policy.Assess(server, chunks), nil
}

func describePolicy(a archive.Archive, server string) (string, error) {
	declared, found, err := policy.NewStore(a.Root).Lookup(server)
	if err != nil {
		return "", err
	}
	assessment, err := assessServer(a, server)
	if err != nil {
		return "", err
	}

	out := fmt.Sprintf("server        %s\n", model.NormalizeToken(server))
	if found {
		out += fmt.Sprintf("disposition   %s\n", declared.Disposition)
		if declared.EmbargoUntil != nil {
			out += fmt.Sprintf("embargo until %s\n", declared.EmbargoUntil.Format(time.RFC3339))
		}
		out += fmt.Sprintf("declared by   %s at %s\n", declared.DeclaredBy, declared.DeclaredAt.Format(time.RFC3339))
		if declared.Note != "" {
			out += fmt.Sprintf("note          %s\n", declared.Note)
		}
		allowed, why := declared.DistributionAllowed(time.Now().UTC())
		verdict := "NOT ALLOWED"
		if allowed {
			verdict = "allowed"
		}
		out += fmt.Sprintf("distribution  %s - %s\n", verdict, why)
	} else {
		out += "disposition   undeclared\n"
		out += "distribution  NOT ALLOWED - nobody has decided for this server\n"
	}

	out += fmt.Sprintf("\ncoverage      %d chunk(s) across %d region(s)\n", assessment.Chunks, assessment.Regions)
	out += fmt.Sprintf("              x %d..%d, z %d..%d\n", assessment.MinX, assessment.MaxX, assessment.MinZ, assessment.MaxZ)
	out += fmt.Sprintf("exposure      %s - %s\n", assessment.Exposure, assessment.Reason)
	return out, nil
}
