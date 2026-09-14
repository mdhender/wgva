// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package config_test

import (
	"errors"
	"math"
	"testing"

	"github.com/mdhender/wgva"
	"github.com/mdhender/wgva/config"
)

// The written-down fingerprint of the default configuration belongs here, and it
// is not here yet.
//
// DESIGN.md 21.2 makes that constant the thing that *settles* the defaults an
// algorithm version ships with: from the moment it exists, moving any default —
// a wavelength, a weight, a threshold, an octave count, a rim width — fails a
// test on the spot, and updating it is the compatibility decision. Writing it
// down now would settle defaults that phase 7 exists to choose, and every tuning
// session between here and there would begin by updating a constant that is
// supposed to mean something. Phase 7 is where it lands; DESIGN.md section 32.
//
// What can be asserted now is everything the constant will depend on: that the
// fingerprint is stable, that it separates configurations that differ, and that
// nothing about how a value is spelled reaches it.

// TestFingerprintIsStable is the property the whole scheme rests on.
func TestFingerprintIsStable(t *testing.T) {
	cfg := wgva.DefaultConfig()
	first, err := config.Of(cfg)
	if err != nil {
		t.Fatalf("Of: %v", err)
	}
	for range 16 {
		again, err := config.Of(cfg)
		if err != nil {
			t.Fatalf("Of: %v", err)
		}
		if again != first {
			t.Fatalf("two fingerprints of one configuration: %s then %s", first, again)
		}
	}
	if first.Short() != first.String()[:8] {
		t.Errorf("Short() = %q, want the first eight hex digits of %s", first.Short(), first)
	}
	if first.Tag() != first.String()[:16] {
		t.Errorf("Tag() = %q, want the first sixteen hex digits of %s", first.Tag(), first)
	}
}

// TestFingerprintSeparatesConfigurations asserts that every key is an input. A
// field that Config gained and the encoder does not see is a field that can be
// moved without the fingerprint noticing, which is the one thing this value must
// never allow.
func TestFingerprintSeparatesConfigurations(t *testing.T) {
	base := wgva.DefaultConfig()
	baseDigest, err := config.Of(base)
	if err != nil {
		t.Fatalf("Of: %v", err)
	}

	for _, f := range config.Fields() {
		t.Run(f.Key, func(t *testing.T) {
			cfg := wgva.DefaultConfig()
			moved := nudge(t, f, &cfg)
			d, err := config.Of(cfg)
			if err != nil {
				t.Fatalf("Of after moving %s to %q: %v", f.Key, moved, err)
			}
			if d == baseDigest {
				t.Fatalf("moving %s to %q did not change the fingerprint", f.Key, moved)
			}
		})
	}
}

// nudge moves one field to a different but still valid value and returns what it
// was set to.
func nudge(t *testing.T, f config.Field, cfg *wgva.Config) string {
	t.Helper()

	before := f.Get(*cfg)
	var candidates []string
	switch f.Kind {
	case config.ValueScalar:
		candidates = []string{"0.375", "0.5", "1.5", "2.5", "13", "700"}
	case config.ValueCount:
		candidates = []string{"1", "2", "3", "5", "7", "11", "64", "256", "1024", "2048"}
	case config.ValueChoice:
		for _, c := range f.Choices {
			candidates = append(candidates, c.Name)
		}
	}
	for _, c := range candidates {
		if c == before {
			continue
		}
		trial := *cfg
		if err := f.Set(&trial, c); err != nil {
			continue
		}
		if trial.Validate() != nil {
			continue
		}
		*cfg = trial
		return c
	}
	t.Fatalf("no valid alternative value for %s, which was %q", f.Key, before)
	return ""
}

// TestFingerprintNormalizesNegativeZero pins the rule of DESIGN.md 21.2. IEEE-754
// says -0.0 == 0.0 and gives them different bit patterns, so two configurations
// that compare equal and generate identical worlds must not fingerprint
// differently.
func TestFingerprintNormalizesNegativeZero(t *testing.T) {
	positive := wgva.DefaultConfig()
	positive.WarpStrengthMiles = 0

	negative := wgva.DefaultConfig()
	negative.WarpStrengthMiles = math.Copysign(0, -1)
	if !math.Signbit(negative.WarpStrengthMiles) {
		t.Fatal("the test could not construct a negative zero")
	}

	a, err := config.Of(positive)
	if err != nil {
		t.Fatalf("Of: %v", err)
	}
	b, err := config.Of(negative)
	if err != nil {
		t.Fatalf("Of: %v", err)
	}
	if a != b {
		t.Errorf("a negative zero fingerprinted as %s against %s", b, a)
	}
}

// TestFingerprintRefusesAnInvalidConfiguration is what keeps NaN out. A NaN that
// reached the encoder would produce a fingerprint for a configuration that
// cannot generate a world.
func TestFingerprintRefusesAnInvalidConfiguration(t *testing.T) {
	cfg := wgva.DefaultConfig()
	cfg.SeaLevel = math.NaN()
	if _, err := config.Of(cfg); !errors.Is(err, wgva.ErrNotFinite) {
		t.Fatalf("Of returned %v, want an error wrapping ErrNotFinite", err)
	}
}

// TestFingerprintCoversTheAlgorithmVersionAndRadius asserts the two inputs that
// are not the configuration. The radius guards nothing at the settled value; it
// is an input because a fingerprint that had to gain one later would invalidate
// every world in existence to do it.
func TestFingerprintCoversTheAlgorithmVersionAndRadius(t *testing.T) {
	cfg := wgva.DefaultConfig()
	base, err := config.Fingerprint(wgva.AlgorithmVersion, wgva.WorldRadius, cfg)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	other, err := config.Fingerprint(wgva.AlgorithmVersion+1, wgva.WorldRadius, cfg)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	if other == base {
		t.Error("the algorithm version is not an input to the fingerprint")
	}
	wider, err := config.Fingerprint(wgva.AlgorithmVersion, 131071, cfg)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	if wider == base {
		t.Error("the world radius is not an input to the fingerprint")
	}
}

// TestIsDefault is what the tuning tool labels a session with.
func TestIsDefault(t *testing.T) {
	if !config.IsDefault(wgva.DefaultConfig()) {
		t.Error("the defaults are not recognized as the defaults")
	}
	cfg := wgva.DefaultConfig()
	cfg.SeaLevel = 0.51
	if config.IsDefault(cfg) {
		t.Error("a modified configuration is recognized as the defaults")
	}
}
