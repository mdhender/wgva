// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/mdhender/wgva/config"
	"github.com/mdhender/wgva/store"
)

// identity prints this binary's build identity and the fingerprint of its
// built-in defaults, in exactly the form --expect takes.
//
// It exists so that --expect is answerable without a browser — from a script, a
// fresh checkout, or a machine with no tuning tool running. Requiring a flag
// with no way to answer it would be a trap of its own.
func identity(args []string) error {
	fs := newFlagSet("identity")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("identity takes no arguments")
	}

	id := config.Default()
	fmt.Println(id)
	if !id.Provable() {
		fmt.Fprintf(os.Stderr, "\n%s\n", provenanceNote(id))
	}
	return nil
}

// inspect runs the seven opening gates by hand against a file and prints what it
// holds.
//
// It is the same job as opening a world from the other direction, which is why
// DESIGN.md 29.5 puts inspection subcommands here rather than in a tool of their
// own. It opens and does not write.
func inspect(args []string) error {
	fs := newFlagSet("inspect")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("inspect takes exactly one argument, the path to read")
	}

	s, err := store.Open(context.Background(), fs.Arg(0))
	if err != nil {
		return err
	}
	defer s.Close()

	w := s.World()
	fmt.Printf("%s\n", s.Path())
	fmt.Printf("  seed              %s\n", w.Seed)
	fmt.Printf("  algorithm version %d\n", w.AlgorithmVersion)
	fmt.Printf("  world radius      %d\n", w.WorldRadius)
	fmt.Printf("  fingerprint       %s\n", w.Fingerprint)
	fmt.Printf("  created by        %s\n", w.CreatedBuild)
	fmt.Printf("  created at        %s\n", w.CreatedAt.Format("2006-01-02 15:04:05 MST"))
	fmt.Printf("  configuration     %s\n", configurationLabel(w))

	players, err := s.Players(context.Background())
	if err != nil {
		return err
	}
	overlays, err := s.AllOverlays(context.Background())
	if err != nil {
		return err
	}
	fmt.Printf("  players           %d\n", len(players))
	fmt.Printf("  discovered        %d\n", len(overlays.Discovered))
	fmt.Printf("  settlements       %d\n", len(overlays.Settlements))
	fmt.Printf("  labels            %d\n", len(overlays.Labels))
	return nil
}

// configurationLabel says whether a stored world's configuration is the one this
// binary ships with. A world created under a configuration nobody shipped is not
// wrong, and it is worth saying out loud.
func configurationLabel(w store.World) string {
	if w.Fingerprint == config.DefaultDigest() {
		return "this binary's defaults"
	}
	return "not this binary's defaults"
}
