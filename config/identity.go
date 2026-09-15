// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mdhender/wgva"
)

// The identity error model. Each reason is a sentinel so a test asserts with
// errors.Is rather than on a message string.
var (
	// ErrIdentityShape is returned for text that is not build/fingerprint.
	ErrIdentityShape = errors.New("expected <build>/<fingerprint>")

	// ErrBuildMismatch is returned when the build half does not match.
	//
	// This is the half that catches a generator change nobody versioned. The
	// fingerprint cannot see one, because it hashes what was declared and never
	// the generator's code, and DESIGN.md 29.5 says outright that
	// AlgorithmVersion gets no ceremony during alpha — which is exactly the
	// period when generator code churns fastest.
	ErrBuildMismatch = errors.New("this is not the build the world was chosen on")

	// ErrFingerprintMismatch is returned when the fingerprint half does not
	// match.
	//
	// This is the half that catches a setting nudged in the tuning tool's form.
	// The build identity cannot see one, because that configuration was never in
	// any build.
	ErrFingerprintMismatch = errors.New("this is not the configuration the world was chosen under")
)

// Identity is the pair a world is chosen under: the build that drew the window
// somebody looked at, and the fingerprint of the configuration it was drawn
// with.
//
// It is one value because neither half is sufficient and both are checked
// together. The three ways the same seed yields two worlds are in DESIGN.md
// 29.5, and no single value catches all three:
//
//	What went wrong between sampling and creating     build  fingerprint
//	a different build whose defaults moved            catches   catches
//	a setting nudged in the tuning form               misses    catches
//	a generator change with no AlgorithmVersion bump  catches   misses
//
// It lives in config rather than in store because the tuning tool emits it and
// the world builder consumes it, and those two share only wgva and this package
// — cmd/wgva-tune has no path to store and must not grow one.
type Identity struct {
	// Build is the version string of a binary, including the commit hash and
	// dirty marker semver.Commit supplies.
	Build string

	// Fingerprint is the configuration fingerprint, in full hex.
	Fingerprint string
}

// Current returns the identity of this binary running with cfg.
func Current(cfg wgva.Config) (Identity, error) {
	d, err := Of(cfg)
	if err != nil {
		return Identity{}, err
	}
	return Identity{Build: wgva.Version().String(), Fingerprint: d.String()}, nil
}

// Default returns the identity of this binary running with its built-in
// defaults. It is what `wgva-world identity` prints and what an administrator
// following DESIGN.md 29.5 is always creating under, because she has no path to
// change what the code uses.
func Default() Identity {
	id, err := Current(wgva.DefaultConfig())
	if err != nil {
		// The defaults are checked by a test in wgva, so reaching this means the
		// defaults and the validation have been changed apart.
		panic(fmt.Sprintf("config: the default configuration is invalid: %v", err))
	}
	return id
}

// String renders the pair in exactly the form --expect takes.
func (id Identity) String() string { return id.Build + "/" + id.Fingerprint }

// ParseIdentity reads the pair --expect takes.
//
// It splits on the last slash rather than the first, because a build identity
// may one day carry one and a fingerprint never can: it is hex.
func ParseIdentity(text string) (Identity, error) {
	text = strings.TrimSpace(text)
	i := strings.LastIndex(text, "/")
	if i <= 0 || i == len(text)-1 {
		return Identity{}, fmt.Errorf("%q: %w", text, ErrIdentityShape)
	}
	return Identity{Build: text[:i], Fingerprint: strings.ToLower(text[i+1:])}, nil
}

// Matches reports whether want and id are the same pair, naming the half that
// differs when they are not.
//
// Both halves are compared and the first difference reported is the build's,
// because that is the one a person can act on — re-sample the seed on this
// build — while a fingerprint difference on a matching build means the form was
// touched.
func (id Identity) Matches(want Identity) error {
	if !strings.EqualFold(id.Build, want.Build) {
		return fmt.Errorf("%w: expected %s, this binary is %s", ErrBuildMismatch, want.Build, id.Build)
	}
	if !strings.EqualFold(id.Fingerprint, want.Fingerprint) {
		return fmt.Errorf("%w: expected %s, this configuration is %s", ErrFingerprintMismatch, want.Fingerprint, id.Fingerprint)
	}
	return nil
}

// Provable reports whether comparing this build identity proves anything.
//
// Two different uncommitted trees both report the same +<hash>-dirty, a tree
// with no commit yet reports +dirty with no hash at all, and a binary built with
// no version-control information — a test binary, or -buildvcs=false — reports
// no build metadata whatever. In neither case is a matching
// build identity a proof that the code is the same code. The comparison is still
// run, and the message says so on the pass as well as on the refusal rather than
// pretending otherwise. This affects developers only; an administrator never has
// one. See DESIGN.md 29.5.
func (id Identity) Provable() bool {
	i := strings.Index(id.Build, "+")
	if i < 0 {
		// No commit metadata at all: a test binary, or -buildvcs=false.
		return false
	}
	commit := id.Build[i+1:]
	// "dirty" with no hash is the edge case after git init and before the first
	// commit; "<hash>-dirty" is an uncommitted tree.
	return commit != "dirty" && !strings.HasSuffix(commit, "-dirty")
}
