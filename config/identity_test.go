// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package config

import (
	"errors"
	"strings"
	"testing"

	"github.com/mdhender/wgva"
)

// TestIdentityRoundTrip is the grammar --expect takes.
func TestIdentityRoundTrip(t *testing.T) {
	want := Identity{Build: "0.9.0-alpha+3fa71c2", Fingerprint: strings.Repeat("ab", 32)}
	got, err := ParseIdentity(want.String())
	if err != nil {
		t.Fatalf("ParseIdentity: %v", err)
	}
	if got != want {
		t.Errorf("round trip gave %+v, want %+v", got, want)
	}
}

// TestParseIdentityRefusals covers the shapes that are not a pair.
func TestParseIdentityRefusals(t *testing.T) {
	for _, text := range []string{"", "/", "0.9.0-alpha", "/abcd", "0.9.0-alpha/"} {
		if _, err := ParseIdentity(text); !errors.Is(err, ErrIdentityShape) {
			t.Errorf("ParseIdentity(%q) = %v, want ErrIdentityShape", text, err)
		}
	}
}

// TestParseIdentitySplitsOnTheLastSlash is why it splits there: a build identity
// may one day carry a slash and a fingerprint never can, because it is hex.
func TestParseIdentitySplitsOnTheLastSlash(t *testing.T) {
	id, err := ParseIdentity("0.9.0-alpha+feature/thing/" + strings.Repeat("cd", 32))
	if err != nil {
		t.Fatalf("ParseIdentity: %v", err)
	}
	if id.Build != "0.9.0-alpha+feature/thing" {
		t.Errorf("build = %q", id.Build)
	}
}

// TestMatchesNamesTheHalfThatDiffers is the whole reason --expect takes two
// values. Neither half is sufficient: the fingerprint cannot see a generator fix
// nobody versioned, and the build identity cannot see a setting nudged in the
// tuning tool's form. See DESIGN.md 29.5.
func TestMatchesNamesTheHalfThatDiffers(t *testing.T) {
	this := Default()

	// A different build whose defaults did not move: the build half catches it
	// and the fingerprint half cannot.
	older := Identity{Build: "0.1.0-alpha+0000000", Fingerprint: this.Fingerprint}
	if err := this.Matches(older); !errors.Is(err, ErrBuildMismatch) {
		t.Errorf("a moved build: %v, want ErrBuildMismatch", err)
	}

	// The same build with a setting nudged in the form: the fingerprint half
	// catches it and the build half cannot.
	nudged := wgva.DefaultConfig()
	nudged.SeaLevel += 0.01
	other, err := Current(nudged)
	if err != nil {
		t.Fatal(err)
	}
	if err := this.Matches(other); !errors.Is(err, ErrFingerprintMismatch) {
		t.Errorf("a nudged setting: %v, want ErrFingerprintMismatch", err)
	}

	if err := this.Matches(this); err != nil {
		t.Errorf("an identity does not match itself: %v", err)
	}
}

// TestDefaultIsWhatTheToolEmits is the handoff of DESIGN.md 29.5 step 4: the
// pair an administrator pastes is this binary's build and the fingerprint of its
// built-in defaults, because she has no path to change what the code uses.
func TestDefaultIsWhatTheToolEmits(t *testing.T) {
	id := Default()
	if id.Build != wgva.Version().String() {
		t.Errorf("build = %q, want %q", id.Build, wgva.Version())
	}
	if id.Fingerprint != DefaultDigest().String() {
		t.Errorf("fingerprint = %q, want %q", id.Fingerprint, DefaultDigest())
	}
}

// TestProvable is the honesty clause. Two different uncommitted trees report the
// same +<hash>-dirty and a binary built with no version-control information at
// all reports no commit whatever, so in neither case is a matching build
// identity a proof. The comparison is still run; the message says so.
func TestProvable(t *testing.T) {
	for _, tc := range []struct {
		build string
		want  bool
	}{
		{"0.9.0-alpha+3fa71c2", true},
		{"0.9.0-alpha+3fa71c2-dirty", false},
		{"0.9.0-alpha+dirty", false},
		{"0.9.0-alpha", false},
	} {
		if got := (Identity{Build: tc.build}).Provable(); got != tc.want {
			t.Errorf("Provable(%q) = %v, want %v", tc.build, got, tc.want)
		}
	}
}

// TestParseDigest is the fingerprint half read back out of a world file.
func TestParseDigest(t *testing.T) {
	want := DefaultDigest()
	got, err := ParseDigest(strings.ToUpper(want.String()))
	if err != nil {
		t.Fatalf("ParseDigest: %v", err)
	}
	if got != want {
		t.Errorf("ParseDigest did not round trip")
	}
	for _, text := range []string{"", "abcd", "not a digest", strings.Repeat("ab", 31)} {
		if _, err := ParseDigest(text); !errors.Is(err, ErrDigestSpelling) {
			t.Errorf("ParseDigest(%q) = %v, want ErrDigestSpelling", text, err)
		}
	}
}
