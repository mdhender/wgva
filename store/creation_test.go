// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package store

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOnlyTheWorldBuilderCreates is the exit condition's "only `wgva-world`
// creates one", asserted rather than remembered.
//
// The import graph cannot say this. Three commands legitimately import store,
// and the compiler has no opinion about which function of it they call — so the
// claim that a world file comes into existence in exactly one place is a claim
// about call sites, and the only way to keep it true is to look at them.
//
// It matters because the alternative is the failure DESIGN.md 27 refuses by
// name: a creation path reached by passing a database flag with a path that
// happens not to exist yet, so that a typo in a path is a new empty world rather
// than an error. Every tool but the world builder refuses an absent file, and
// this is what keeps that from quietly becoming untrue.
//
// Test files are exempt: a store test that could not create a world would have
// nothing to test, and a command's test creating a fixture is not a command
// creating a world.
func TestOnlyTheWorldBuilderCreates(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}

	const allowed = "cmd/wgva-world"
	var offenders []string

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && (d.Name() == ".git" || d.Name() == "testdata"):
			return fs.SkipDir
		case d.IsDir() || !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go"):
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		// The store's own source defines Create; it does not call it.
		if strings.HasPrefix(rel, "store"+string(filepath.Separator)) {
			return nil
		}

		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(source), "store.Create(") && !strings.HasPrefix(rel, allowed) {
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, file := range offenders {
		t.Errorf("%s calls store.Create; only %s brings a world file into existence", file, allowed)
	}
}

// TestTheWorldBuilderDoesCreate is the other half. A test that only said where
// creation must not happen would keep passing if it stopped happening anywhere.
func TestTheWorldBuilderDoesCreate(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "cmd", "wgva-world", "create.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), "store.Create(") {
		t.Error("cmd/wgva-world does not call store.Create; nothing in this module makes a world")
	}
}
