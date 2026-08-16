package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/worldledger/worldledger-mc/internal/seed"
)

// cmdSeed is gated on purpose. It prints the responsibility notice in full and
// refuses to run until someone accepts it by name, and that name travels with
// every result it writes.
func cmdSeed(args []string) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	observations := fs.String("observations", "", "structure observation file")
	operator := fs.String("operator", "", "who is responsible for this use")
	accept := fs.Bool("accept-terms", false, "accept the responsibility notice")
	check := fs.Int64("seed", 0, "test one candidate seed")
	hasCheck := false
	from := fs.Int64("from", 0, "first candidate in a scan")
	to := fs.Int64("to", 0, "last candidate in a scan")
	limit := fs.Int("limit", 1000, "maximum candidates to report")
	out := fs.String("out", "", "write the result as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "seed" {
			hasCheck = true
		}
	})

	acknowledgement, err := seed.Accept(*operator, *accept)
	if err != nil {
		// The notice is the response, not a footnote to an error.
		fmt.Println(seed.Notice)
		fmt.Println()
		fmt.Println("To proceed, state who is responsible and accept explicitly:")
		fmt.Println("  worldledger seed --observations FILE --operator YOUR-NAME --accept-terms ...")
		fmt.Println()
		return err
	}
	if *observations == "" {
		return usageError("seed")
	}

	input, err := seed.LoadInput(*observations)
	if err != nil {
		return err
	}

	first, last := *from, *to
	if hasCheck {
		first, last = *check, *check
	} else if first == 0 && last == 0 {
		return errors.New("give --seed N to test one candidate, or --from A --to B to scan a range")
	}

	fmt.Printf("operator   %s\n", acknowledgement.Operator)
	fmt.Printf("statement  %s\n", acknowledgement.Statement)
	fmt.Printf("input      %s (%d observation(s))\n\n", *observations, len(input.Observations))

	result, err := seed.Search(input, acknowledgement, first, last, *limit)
	if err != nil {
		return err
	}

	fmt.Printf("scanned    %d candidate(s) in %s\n", result.Scanned, result.Elapsed)
	if len(result.Candidates) == 0 {
		fmt.Println("candidates none")
	} else {
		fmt.Printf("candidates %d\n", len(result.Candidates))
		for index, candidate := range result.Candidates {
			if index == 20 {
				fmt.Printf("  ... %d more\n", len(result.Candidates)-20)
				break
			}
			fmt.Printf("  %d\n", candidate)
		}
	}

	if estimate, err := seed.EstimateFullScan(result); err == nil && result.SearchSpaceFraction < 1 {
		fmt.Printf("\nat this rate a full 48-bit scan would take about %s\n", roundEstimate(estimate))
	}

	fmt.Println("\ncaveats:")
	for _, caveat := range result.Caveats {
		fmt.Printf("  - %s\n", caveat)
	}

	if *out != "" {
		encoded, err := json.MarshalIndent(result, "", " ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(*out, append(encoded, '\n'), 0o644); err != nil {
			return err
		}
		fmt.Printf("\nwrote %s (the acknowledgement travels with it)\n", *out)
	}
	return nil
}

func roundEstimate(value time.Duration) string {
	switch {
	case value > 48*time.Hour:
		return fmt.Sprintf("%.0f days", value.Hours()/24)
	case value > 2*time.Hour:
		return fmt.Sprintf("%.0f hours", value.Hours())
	case value > 2*time.Minute:
		return fmt.Sprintf("%.0f minutes", value.Minutes())
	default:
		return value.Round(time.Second).String()
	}
}
