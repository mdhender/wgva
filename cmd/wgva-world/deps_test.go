// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestWorldCannotDraw is the mirror image of cmd/wgva-tune's missing store edge,
// and the pair is the point.
//
// Go's import cycle rule already stops the core package from importing store,
// render, config, or view — they all import wgva, so wgva cannot import any of
// them. What the cycle rule does not give is this direction, so it is a test.
//
// **cmd/wgva-world must never import render.** Creating a world is not a drawing
// act, and a world builder that could draw would grow a --preview flag and then
// a viewport grammar and then a second opinion about what a window is. The tool
// that makes a world cannot draw one; the tool that decides how worlds look
// cannot touch a world. See DESIGN.md 28.
//
// view is forbidden for the same reason at one remove: it imports render, so a
// seed parser borrowed from it would bring the whole renderer along. That is why
// the seed grammar lives in wgva.
func TestWorldCannotDraw(t *testing.T) {
	deps := listDeps(t)
	for _, forbidden := range []string{
		"github.com/mdhender/wgva/render",
		"github.com/mdhender/wgva/view",
	} {
		for dep := range deps {
			if dep == forbidden || strings.HasPrefix(dep, forbidden+"/") {
				t.Errorf("cmd/wgva-world imports %s; the tool that makes a world cannot draw one", dep)
			}
		}
	}
}

// TestWorldImportsWhatItShould is the other half. A test that only said what is
// absent would keep passing if the command stopped importing store entirely —
// and a world builder that cannot write a world is not a world builder.
func TestWorldImportsWhatItShould(t *testing.T) {
	deps := listDeps(t)
	for _, want := range []string{
		"github.com/mdhender/wgva",
		"github.com/mdhender/wgva/config",
		"github.com/mdhender/wgva/store",
	} {
		if !deps[want] {
			t.Errorf("cmd/wgva-world does not import %s", want)
		}
	}
}

func listDeps(t *testing.T) map[string]bool {
	t.Helper()
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
	}
	deps := map[string]bool{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		deps[line] = true
	}
	return deps
}
