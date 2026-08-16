package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/attest"
	"github.com/worldledger/worldledger-mc/internal/model"
)

func cmdIdentity(args []string) error {
	if len(args) == 0 {
		return usageError("identity")
	}
	switch args[0] {
	case "create":
		return cmdIdentityCreate(args[1:])
	case "register":
		return cmdIdentityRegister(args[1:])
	case "list":
		return cmdIdentityList(args[1:])
	case "remove":
		return cmdIdentityRemove(args[1:])
	default:
		return fmt.Errorf("unknown identity subcommand %q; want create, register, list, or remove", args[0])
	}
}

func cmdIdentityCreate(args []string) error {
	fs := flag.NewFlagSet("identity create", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	label := fs.String("label", "", "contributor label this key speaks for")
	declaredBy := fs.String("declared-by", "", "who registered this key")
	keyOut := fs.String("key-out", "", "file to write the private key to, outside the archive")
	note := fs.String("note", "", "optional note")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" || *label == "" || *keyOut == "" || *declaredBy == "" {
		return usageError("identity create")
	}

	// A key file that already exists is somebody's identity. Overwriting it
	// would destroy the only copy, and no attestation made with it could ever
	// be produced again.
	if _, err := os.Stat(*keyOut); err == nil {
		return fmt.Errorf("%s already exists; refusing to overwrite a private key", *keyOut)
	} else if !os.IsNotExist(err) {
		return err
	}

	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}

	key, err := attest.GenerateKey()
	if err != nil {
		return err
	}
	// 0600 is advisory on Windows, which is why the warning below is printed
	// rather than assumed unnecessary.
	if err := os.WriteFile(*keyOut, []byte(key.Encode()+"\n"), 0o600); err != nil {
		return err
	}
	if err := attest.NewIdentityStore(a.Root).Register(attest.Identity{
		Label:      *label,
		PublicKey:  key.Public().Encode(),
		DeclaredBy: *declaredBy,
		Note:       *note,
	}); err != nil {
		return err
	}

	fmt.Printf("label       %s\n", *label)
	fmt.Printf("fingerprint %s\n", key.Public().Fingerprint()[:16])
	fmt.Printf("private key %s\n", *keyOut)
	fmt.Println("\nThe private key is not in the archive and must not be committed or shared.")
	fmt.Println("Losing it means this label can no longer sign; there is no recovery.")
	return nil
}

// cmdIdentityRegister records someone else's public key.
//
// Without this an archive could only ever recognise keys it generated itself,
// which is no use to the case this exists for: deciding whether to believe a
// contributor whose observations arrived from somewhere else. Registering a key
// is a judgment about a person, so it names who made it.
func cmdIdentityRegister(args []string) error {
	fs := flag.NewFlagSet("identity register", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	label := fs.String("label", "", "contributor label this key speaks for")
	publicKey := fs.String("public-key", "", "the contributor's public key, as hex")
	declaredBy := fs.String("declared-by", "", "who decided to trust it")
	note := fs.String("note", "", "how the key was checked")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" || *label == "" || *publicKey == "" || *declaredBy == "" {
		return usageError("identity register")
	}
	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}
	parsed, err := attest.ParsePublicKey(*publicKey)
	if err != nil {
		return err
	}
	if err := attest.NewIdentityStore(a.Root).Register(attest.Identity{
		Label:      *label,
		PublicKey:  parsed.Encode(),
		DeclaredBy: *declaredBy,
		Note:       *note,
	}); err != nil {
		return err
	}
	fmt.Printf("registered %s as %s\n", parsed.Fingerprint()[:16], *label)
	fmt.Println("\nThis archive will now resolve signatures from that key to that label.")
	fmt.Println("Nothing checked that the key belongs to the person the label names; that")
	fmt.Println("judgment is yours and it is recorded against your name.")
	return nil
}

func cmdIdentityList(args []string) error {
	fs := flag.NewFlagSet("identity list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" {
		return usageError("identity list")
	}
	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}
	identities, err := attest.NewIdentityStore(a.Root).List()
	if err != nil {
		return err
	}
	if len(identities) == 0 {
		fmt.Println("no identity is registered; every contributor label in this archive is an unverified claim")
		return nil
	}
	for _, identity := range identities {
		fmt.Printf("%-24s %s\n", identity.Label, identity.Fingerprint()[:16])
		fmt.Printf("  registered by %s\n", identity.DeclaredBy)
		if identity.Note != "" {
			fmt.Printf("  note          %s\n", identity.Note)
		}
	}
	return nil
}

func cmdIdentityRemove(args []string) error {
	fs := flag.NewFlagSet("identity remove", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	fingerprint := fs.String("fingerprint", "", "key fingerprint from identity list")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" || *fingerprint == "" {
		return usageError("identity remove")
	}
	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}
	store := attest.NewIdentityStore(a.Root)
	identities, err := store.List()
	if err != nil {
		return err
	}
	var full string
	for _, identity := range identities {
		if strings.HasPrefix(identity.Fingerprint(), *fingerprint) {
			if full != "" {
				return fmt.Errorf("%q matches more than one key; give more of the fingerprint", *fingerprint)
			}
			full = identity.Fingerprint()
		}
	}
	if full == "" {
		return fmt.Errorf("no registered key starts with %q", *fingerprint)
	}
	removed, err := store.Remove(full)
	if err != nil {
		return err
	}
	if !removed {
		return fmt.Errorf("no registered key with fingerprint %s", full[:16])
	}
	fmt.Printf("removed %s\n", full[:16])
	fmt.Println("Attestations made with it remain stored and still verify; they are now")
	fmt.Println("signatures from a key this archive no longer recognises.")
	return nil
}

func cmdAttest(args []string) error {
	if len(args) == 0 {
		return usageError("attest")
	}
	switch args[0] {
	case "sign":
		return cmdAttestSign(args[1:])
	case "verify":
		return cmdAttestVerify(args[1:])
	default:
		return fmt.Errorf("unknown attest subcommand %q; want sign or verify", args[0])
	}
}

func cmdAttestSign(args []string) error {
	fs := flag.NewFlagSet("attest sign", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	keyPath := fs.String("key", "", "private key file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" || *keyPath == "" {
		return usageError("attest sign")
	}

	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(*keyPath)
	if err != nil {
		return err
	}
	key, err := attest.ParsePrivateKey(string(raw))
	if err != nil {
		return err
	}

	identities, err := attest.NewIdentityStore(a.Root).List()
	if err != nil {
		return err
	}
	label := ""
	for _, identity := range identities {
		if identity.PublicKey == key.Public().Encode() {
			label = identity.Label
			break
		}
	}
	if label == "" {
		return errors.New("this key is not registered in this archive; run 'worldledger identity create' or register its public key first")
	}

	observations, err := allObservations(a)
	if err != nil {
		return err
	}
	store := attest.NewStore(a.Root)
	signed, skipped := 0, 0
	for _, observation := range observations {
		// Signing a record that claims someone else made it would attach this
		// key to a claim it cannot support. Corroborating another contributor
		// is a different act than attesting to your own work, and conflating
		// them would make the signature mean less than nothing.
		if !strings.EqualFold(strings.TrimSpace(observation.Source.Contributor), label) {
			skipped++
			continue
		}
		attestation, err := attest.Sign(key, observation.ID)
		if err != nil {
			return err
		}
		if err := store.Put(attestation); err != nil {
			return err
		}
		signed++
	}
	fmt.Printf("signed %d observation(s) attributed to %s\n", signed, label)
	if skipped > 0 {
		fmt.Printf("left %d observation(s) alone; they are attributed to someone else\n", skipped)
	}
	return nil
}

func cmdAttestVerify(args []string) error {
	fs := flag.NewFlagSet("attest verify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" {
		return usageError("attest verify")
	}
	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}
	identities, err := attest.NewIdentityStore(a.Root).List()
	if err != nil {
		return err
	}
	store := attest.NewStore(a.Root)
	observations, err := allObservations(a)
	if err != nil {
		return err
	}

	var unattested, recognised, unregistered, invalid int
	var problems []string
	for _, observation := range observations {
		attestations, err := store.For(observation.ID)
		if err != nil {
			return err
		}
		if len(attestations) == 0 {
			unattested++
			continue
		}
		for _, attestation := range attestations {
			outcome := attest.Evaluate(attestation, identities)
			switch {
			case !outcome.Valid:
				invalid++
				problems = append(problems, fmt.Sprintf("%s: %s", observation.ID[:12], outcome.Reason))
			case outcome.Label == "":
				unregistered++
			default:
				recognised++
			}
		}
	}

	fmt.Printf("observations       %d\n", len(observations))
	fmt.Printf("signed by a known key   %d\n", recognised)
	fmt.Printf("signed by an unknown key %d\n", unregistered)
	fmt.Printf("not signed at all       %d\n", unattested)
	fmt.Printf("signatures that fail    %d\n", invalid)

	if unattested > 0 {
		fmt.Println("\nAn unsigned observation is not suspect. It is simply a contributor label")
		fmt.Println("that nothing backs, which is what every observation was before signing existed.")
	}
	if len(problems) > 0 {
		fmt.Println("\nsignatures that do not verify:")
		for index, problem := range problems {
			if index == 20 {
				fmt.Printf("  ... %d more\n", len(problems)-20)
				break
			}
			fmt.Printf("  %s\n", problem)
		}
		return fmt.Errorf("%d signature(s) do not verify", invalid)
	}
	return nil
}

func allObservations(a archive.Archive) ([]model.Observation, error) {
	servers, err := a.Servers()
	if err != nil {
		return nil, err
	}
	var out []model.Observation
	for _, server := range servers {
		dimensions, err := a.Dimensions(server)
		if err != nil {
			return nil, err
		}
		for _, dimension := range dimensions {
			chunks, err := a.DimensionObservations(server, dimension)
			if err != nil {
				return nil, err
			}
			for _, chunk := range chunks {
				out = append(out, chunk.Observations...)
			}
		}
	}
	return out, nil
}
