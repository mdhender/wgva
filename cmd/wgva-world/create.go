// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/mdhender/wgva"
	"github.com/mdhender/wgva/config"
	"github.com/mdhender/wgva/store"
)

// create is step 5 of the administrator's path: paste the line the tuning tool
// emitted for the window she chose, and get that world.
//
// Everything interesting about this command is the guard. DESIGN.md 29.5 works
// through three ways the same seed yields two worlds — a different build whose
// defaults moved, a setting nudged in the tuning form, and a generator change
// shipped without an AlgorithmVersion bump — and **nothing errors** in any of
// them. The seed is valid, the configuration is valid, every gate passes, and
// the world is perfectly reproducible. It is simply not the one that was chosen,
// and the only evidence is that the map looks different from the one in the
// browser tab, which by then is closed.
//
// So the check is enforced rather than printed, and it takes two values because
// no single value catches all three. See config.Identity.
func create(args []string) error {
	fs := newFlagSet("create")
	var (
		seedText   = fs.String("seed", "", "world seed, sixteen hexadecimal digits")
		expectText = fs.String("expect", "", "the <build>/<fingerprint> this world was chosen under (required)")
		configFile = fs.String("config", "", "configuration file (a developer affordance; the defaults are what an administrator has)")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("create takes exactly one argument, the path to write")
	}
	path := fs.Arg(0)

	if *seedText == "" {
		return errors.New("--seed is required")
	}
	seed, err := wgva.ParseSeed(*seedText)
	if err != nil {
		return fmt.Errorf("--seed: %w", err)
	}

	// --expect is required and there is no --force. The whole hazard is that the
	// wrong world is created without anybody noticing, and an optional guard
	// against an unnoticeable failure is not a guard.
	if *expectText == "" {
		return fmt.Errorf("--expect is required; this binary's is %s (run `wgva-world identity`)", config.Default())
	}
	want, err := config.ParseIdentity(*expectText)
	if err != nil {
		return fmt.Errorf("--expect: %w", err)
	}

	cfg := wgva.DefaultConfig()
	if *configFile != "" {
		data, err := os.ReadFile(*configFile)
		if err != nil {
			return fmt.Errorf("--config: %w", err)
		}
		// The file's fingerprint is recomputed rather than trusted from its
		// comment header. A header is a comment; it is not read back, and a file
		// whose header disagreed with its numbers would otherwise create a world
		// under a fingerprint nothing in it produces.
		if cfg, err = config.Unmarshal(data); err != nil {
			return fmt.Errorf("--config: %s: %w", *configFile, err)
		}
	}

	have, err := config.Current(cfg)
	if err != nil {
		return err
	}
	if err := have.Matches(want); err != nil {
		return fmt.Errorf("%w\n%s", err, provenanceNote(have))
	}

	ctx := context.Background()
	s, err := store.Create(ctx, path, seed, cfg)
	if err != nil {
		return err
	}
	defer s.Close()

	report(os.Stdout, path, s.World(), have)
	return nil
}

// report prints what was actually written. The build identity and the
// fingerprint are printed on success with or without --config, because the
// question a world raises later is always "which build made this, under which
// configuration?" and the answer should be in the terminal scrollback of the
// moment it was made as well as in the file.
func report(w io.Writer, path string, world store.World, id config.Identity) {
	fmt.Fprintf(w, "created %s\n", path)
	fmt.Fprintf(w, "  seed              %s\n", world.Seed)
	fmt.Fprintf(w, "  algorithm version %d\n", world.AlgorithmVersion)
	fmt.Fprintf(w, "  world radius      %d\n", world.WorldRadius)
	fmt.Fprintf(w, "  build             %s\n", world.CreatedBuild)
	fmt.Fprintf(w, "  fingerprint       %s\n", world.Fingerprint)
	if !id.Provable() {
		fmt.Fprintf(w, "\n%s\n", provenanceNote(id))
	}
}

// provenanceNote says what a build comparison did and did not prove.
//
// It is printed on the pass as well as on the refusal rather than pretending.
// Two different uncommitted trees report the same +<hash>-dirty and a binary
// built without version-control information reports no commit at all, so in
// neither case is a matching build identity a proof that the code is the code.
// This affects developers only; an administrator never has one.
func provenanceNote(id config.Identity) string {
	if id.Provable() {
		return fmt.Sprintf("this binary is %s", id.Build)
	}
	return fmt.Sprintf("note: this binary is %s, which does not identify the code it was built from;\n"+
		"      the build half of the comparison is run but proves nothing here", id.Build)
}
