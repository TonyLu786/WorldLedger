// Command mcjava-fixtures validates or deliberately rewrites the committed
// cross-language fixtures for worldledger.minecraft.java.chunk/v1.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/worldledger/worldledger-mc/internal/mcjava/fixture"
)

const outputSchema = "worldledger.minecraft.java.chunk-fixture-outputs/v1"

type outputManifest struct {
	Schema   string        `json:"schema"`
	Source   string        `json:"source"`
	Fixtures []outputEntry `json:"fixtures"`
}

type outputEntry struct {
	Name      string `json:"name"`
	Component string `json:"component"`
	File      string `json:"file"`
	Size      int    `json:"size"`
	SHA256    string `json:"sha256"`
}

type builtOutput struct {
	entry outputEntry
	data  []byte
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("mcjava-fixtures", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	const usage = "usage: go run ./cmd/mcjava-fixtures [--root DIR] [--write]"
	root := flags.String("root", ".", "repository root")
	write := flags.Bool("write", false, "rewrite committed outputs")
	if err := flags.Parse(args); err != nil {
		// Being asked for help is not a failure. Without this it exits 1 with
		// "flag: help requested", which reads as a broken tool.
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stdout, usage)
			return nil
		}
		return fmt.Errorf("%w\n\n%s", err, usage)
	}
	if flags.NArg() != 0 {
		return errors.New(usage)
	}

	fixtureDir := filepath.Join(*root, "testdata", "mcjava-v1")
	descriptionPath := filepath.Join(fixtureDir, "fixtures.json")
	set, err := fixture.Load(descriptionPath)
	if err != nil {
		return err
	}
	outputs, manifestData, err := buildOutputs(set)
	if err != nil {
		return err
	}

	if *write {
		if err := os.MkdirAll(fixtureDir, 0o755); err != nil {
			return err
		}
		for _, output := range outputs {
			if err := writeChanged(filepath.Join(fixtureDir, output.entry.File), output.data, 0o644); err != nil {
				return err
			}
		}
		if err := writeChanged(filepath.Join(fixtureDir, "outputs.json"), manifestData, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "wrote %d canonical fixtures\n", len(outputs))
		return nil
	}

	for _, output := range outputs {
		if err := compareFile(filepath.Join(fixtureDir, output.entry.File), output.data); err != nil {
			return fmt.Errorf("fixture %q: %w", output.entry.Name, err)
		}
	}
	if err := compareFile(filepath.Join(fixtureDir, "outputs.json"), manifestData); err != nil {
		return fmt.Errorf("output manifest: %w", err)
	}
	fmt.Fprintf(stdout, "verified %d canonical fixtures\n", len(outputs))
	return nil
}

func buildOutputs(set fixture.Set) ([]builtOutput, []byte, error) {
	items := append([]fixture.Fixture(nil), set.Fixtures...)
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	outputs := make([]builtOutput, 0, len(items))
	manifest := outputManifest{
		Schema:   outputSchema,
		Source:   "fixtures.json",
		Fixtures: make([]outputEntry, 0, len(items)),
	}
	for _, item := range items {
		data, err := fixture.Build(item)
		if err != nil {
			return nil, nil, fmt.Errorf("build fixture %q: %w", item.Name, err)
		}
		digest := sha256.Sum256(data)
		entry := outputEntry{
			Name:      item.Name,
			Component: item.Component,
			File:      item.Output,
			Size:      len(data),
			SHA256:    hex.EncodeToString(digest[:]),
		}
		outputs = append(outputs, builtOutput{entry: entry, data: data})
		manifest.Fixtures = append(manifest.Fixtures, entry)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	data = append(data, '\n')
	return outputs, data, nil
}

func compareFile(path string, want []byte) error {
	got, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(got, want) {
		return errors.New("committed bytes differ; review the specification before running with --write")
	}
	return nil
}

func writeChanged(path string, data []byte, mode os.FileMode) error {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, data) {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.WriteFile(path, data, mode)
}
