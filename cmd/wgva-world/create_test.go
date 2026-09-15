// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mdhender/wgva"
	"github.com/mdhender/wgva/config"
	"github.com/mdhender/wgva/store"
)

const testSeed = "0x0123456789abcdef"

// TestCreateRequiresExpect is the flag that cannot be skipped. There is no
// --force: the hazard is that the wrong world is created without anybody
// noticing, and an optional guard against an unnoticeable failure is not a
// guard.
func TestCreateRequiresExpect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "world.wgva")
	err := create([]string{"--seed", testSeed, path})
	if err == nil || !strings.Contains(err.Error(), "--expect is required") {
		t.Fatalf("create without --expect: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused create wrote %s", path)
	}

	// The refusal names this binary's pair, so the next command is typeable.
	if !strings.Contains(err.Error(), config.Default().String()) {
		t.Errorf("the refusal does not name this binary's identity: %v", err)
	}
}

// TestCreateRefusesEitherHalf is DESIGN.md 29.5's three-case table. Neither
// value alone is sufficient, so both are compared and a mismatch in either is a
// refusal that writes nothing.
func TestCreateRefusesEitherHalf(t *testing.T) {
	this := config.Default()

	for _, tc := range []struct {
		name   string
		expect config.Identity
		want   error
	}{
		{
			// A world chosen on a different build. During alpha the defaults
			// move whenever tuning improves, so a seed chosen on Tuesday's build
			// and created on Thursday's is a different world at the same number.
			name:   "a build that moved",
			expect: config.Identity{Build: "0.1.0-alpha+0000000", Fingerprint: this.Fingerprint},
			want:   config.ErrBuildMismatch,
		},
		{
			// A setting nudged in the tuning tool's form. The build identity
			// cannot see this one: that configuration was never in any build.
			name:   "a configuration that moved",
			expect: config.Identity{Build: this.Build, Fingerprint: strings.Repeat("ab", 32)},
			want:   config.ErrFingerprintMismatch,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "world.wgva")
			err := create([]string{"--seed", testSeed, "--expect", tc.expect.String(), path})
			if !errors.Is(err, tc.want) {
				t.Fatalf("create: %v, want %v", err, tc.want)
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("a refused create wrote %s", path)
			}
		})
	}
}

// TestCreateWritesTheWorld is the happy path, and then the world it wrote is
// reopened through the gates.
func TestCreateWritesTheWorld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "world.wgva")
	if err := create([]string{"--seed", testSeed, "--expect", config.Default().String(), path}); err != nil {
		t.Fatalf("create: %v", err)
	}

	s, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	w := s.World()
	if w.Seed != 0x0123456789abcdef {
		t.Errorf("seed = %s", w.Seed)
	}
	if w.Fingerprint.String() != config.Default().Fingerprint {
		t.Errorf("fingerprint = %s", w.Fingerprint)
	}

	// The second create refuses. There is no --force: removing a world is
	// something a person does deliberately, with rm.
	if err := create([]string{"--seed", testSeed, "--expect", config.Default().String(), path}); !errors.Is(err, store.ErrWorldExists) {
		t.Fatalf("create over a world: %v, want ErrWorldExists", err)
	}
}

// TestCreateWithAConfigFile is the developer affordance, and the only reason it
// exists: giving a candidate configuration a world so the gates of DESIGN.md
// 27.5 can be exercised against one. It is not a step in the administrator's
// path — she has no way to change what the code uses.
func TestCreateWithAConfigFile(t *testing.T) {
	dir := t.TempDir()

	cfg := wgva.DefaultConfig()
	cfg.SeaLevel += 0.01
	data, err := config.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}

	want, err := config.Current(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// The defaults' fingerprint is refused, which is the point: the fingerprint
	// is recomputed from the file rather than trusted from its comment header.
	path := filepath.Join(dir, "world.wgva")
	if err := create([]string{
		"--seed", testSeed, "--config", file,
		"--expect", config.Default().String(), path,
	}); !errors.Is(err, config.ErrFingerprintMismatch) {
		t.Fatalf("create with a file and the defaults' fingerprint: %v", err)
	}

	if err := create([]string{
		"--seed", testSeed, "--config", file,
		"--expect", want.String(), path,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	s, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if s.World().Fingerprint.String() != want.Fingerprint {
		t.Errorf("the world was not created under the file's configuration")
	}
}

// TestCreateRefusesABadSeed covers the one argument a person types by hand.
func TestCreateRefusesABadSeed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "world.wgva")
	err := create([]string{"--seed", "not-a-seed", "--expect", config.Default().String(), path})
	if !errors.Is(err, wgva.ErrSeedSpelling) {
		t.Fatalf("create: %v, want ErrSeedSpelling", err)
	}
}
