// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestTuneCannotReachTheStore is the test that makes the tool's boundary a fact
// of the build rather than a promise.
//
// Go's import cycle rule already stops the core package from importing store,
// render, config, or view — they all import wgva, so wgva cannot import any of
// them, and "persistence and rendering are not in the core" needs no lint and no
// review. What the cycle rule does not give is this direction, so it is a test.
//
// **cmd/wgva-tune must never import store, directly or transitively.** It cannot
// open a world, create one, or write to one, and that is what makes the claim
// safe to say out loud rather than carefully. Its mirror image is
// cmd/wgva-world, which must never import render: creating a world is not a
// drawing act, and a world builder that could draw would grow a --preview flag
// and then a viewport grammar and then a second opinion about what a window is.
//
// There is no store package yet, which is exactly when this test is cheap to
// write: it passes now for the uninteresting reason and keeps passing for the
// interesting one.
func TestTuneCannotReachTheStore(t *testing.T) {
	const forbidden = "github.com/mdhender/wgva/store"

	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if line == forbidden || strings.HasPrefix(line, forbidden+"/") {
			t.Fatalf("cmd/wgva-tune imports %s; the tool that decides how worlds look cannot touch a world", line)
		}
	}
}

// TestTuneImportsWhatItShould is the other half. A test that only says what is
// absent would keep passing if the tool stopped importing render entirely — and
// a tuning tool that does not draw is not a tuning tool.
func TestTuneImportsWhatItShould(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
	}
	deps := map[string]bool{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		deps[line] = true
	}
	for _, want := range []string{
		"github.com/mdhender/wgva",
		"github.com/mdhender/wgva/config",
		"github.com/mdhender/wgva/render",
		"github.com/mdhender/wgva/view",
	} {
		if !deps[want] {
			t.Errorf("cmd/wgva-tune does not import %s", want)
		}
	}
}
