// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package config_test

import (
	"errors"
	"math"
	"testing"

	"github.com/mdhender/wgva"
	"github.com/mdhender/wgva/config"
)

// DefaultFingerprint is the fingerprint of this binary's default configuration,
// and writing it down here is what **settles the defaults** algorithm version 6
// ships with. DESIGN.md 21.2 and phase 7 of section 32.
//
// It is a specific act rather than a feeling. From this line onward, moving any
// default — a wavelength, a weight, a threshold, an octave count, a rim width,
// the rim's forced floor — fails TestDefaultConfigurationIsSettled on the spot.
// **Updating this constant is the compatibility decision**, and it belongs in a
// commit message beside the AlgorithmVersion bump that goes with it, saying
// which default moved and why.
//
// Three things it is worth knowing this constant does *not* say. It is not a
// checksum of the generator's code: DESIGN.md 21.2 hashes what was declared, so
// a build that changes a noise formula without bumping the algorithm version
// produces a different world under this same number, and what closes that gap is
// the discipline of section 27 and the build identity in the creation guard of
// section 29.5. It is not a judgement that these numbers are the right numbers —
// appendices D.11, D.13, D.15, and D.17 are what they were tuned to and any of
// them may still be wrong. And it is not a promise about the CBOR library: the
// canonical encoder writes each float in the shortest form that represents it
// exactly, which is a dependency's policy, and this constant is the tripwire if a
// release ever changes it.
//
// Recorded under AlgorithmVersion 6 at world radius 32767.
const DefaultFingerprint = "f32d2b728906de9c7f0921b2ca652b778551abaa390a5741da62371bcfd3430b"

// TestDefaultConfigurationIsSettled is the test the constant above exists for.
//
// A failure here is never a bug in this test. It says that a default moved, and
// the two honest responses are to put it back or to write the new number down
// together with the reason and the version bump.
func TestDefaultConfigurationIsSettled(t *testing.T) {
	got := config.DefaultDigest()
	if got.String() != DefaultFingerprint {
		t.Errorf("the default configuration fingerprints as\n\t%s\nand the settled constant is\n\t%s\n"+
			"A default moved. Put it back, or write the new fingerprint down in this file together with the "+
			"AlgorithmVersion bump and one line on which default moved and why.",
			got, DefaultFingerprint)
	}

	// The two inputs beside the configuration, written out for the same reason
	// the digest is: this constant is the fingerprint of *these* defaults under
	// *this* version at *this* radius, and a reader who found it failing needs to
	// know which of the three moved.
	if wgva.AlgorithmVersion != 6 {
		t.Errorf("the algorithm version is %d and the fingerprint above was recorded under 6",
			wgva.AlgorithmVersion)
	}
	if wgva.WorldRadius != 32767 {
		t.Errorf("the world radius is %d and the fingerprint above was recorded at 32767", wgva.WorldRadius)
	}
}

// Everything below is what the constant depends on: that the fingerprint is
// stable, that it separates configurations that differ, and that nothing about
// how a value is spelled reaches it.

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
		// The last five are for the thresholds that are not free over the
		// positives: a band ladder entry has the entry below it as a floor, the
		// deep-water threshold lies under sea level and so is negative, and the
		// terrain rules of DESIGN.md 17 cut two heat ladders whose lower entry
		// has the upper one as a ceiling.
		candidates = []string{
			"0.375", "0.5", "1.5", "2.5", "13", "700",
			"0.625", "0.875", "-0.25", "-0.5", "-0.75",
		}
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
