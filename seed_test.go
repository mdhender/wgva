// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"errors"
	"testing"
)

// TestSeedRoundTrip is the spelling every tool prints and every tool reads.
func TestSeedRoundTrip(t *testing.T) {
	for _, seed := range []Seed{0, 1, 0x0123456789abcdef, ^Seed(0)} {
		got, err := ParseSeed(seed.String())
		if err != nil {
			t.Fatalf("ParseSeed(%s): %v", seed, err)
		}
		if got != seed {
			t.Errorf("round trip of %s gave %s", seed, got)
		}
	}
}

// TestParseSeedSpellings covers the three a person has in front of them: the
// prefixed hex a tool prints, the bare sixteen digits DESIGN.md 29.3 writes in a
// route segment, and the decimal flag takes.
func TestParseSeedSpellings(t *testing.T) {
	const want = Seed(0x0123456789abcdef)
	for _, text := range []string{
		"0x0123456789abcdef",
		"0X0123456789ABCDEF",
		"0123456789abcdef",
		"0123456789ABCDEF",
		" 0x0123456789abcdef ",
		"81985529216486895",
	} {
		got, err := ParseSeed(text)
		if err != nil {
			t.Fatalf("ParseSeed(%q): %v", text, err)
		}
		if got != want {
			t.Errorf("ParseSeed(%q) = %s, want %s", text, got, want)
		}
	}
}

// TestParseSeedWidthDecidesTheAmbiguity is the rule that resolves bare digits.
// Exactly sixteen hexadecimal characters is the hex spelling; anything else that
// parses as decimal is decimal.
func TestParseSeedWidthDecidesTheAmbiguity(t *testing.T) {
	if got, err := ParseSeed("123"); err != nil || got != 123 {
		t.Errorf(`ParseSeed("123") = %d, %v; want 123`, got, err)
	}
	if got, err := ParseSeed("0000000000000123"); err != nil || got != 0x123 {
		t.Errorf(`ParseSeed("0000000000000123") = %#x, %v; want 0x123`, uint64(got), err)
	}
	// Sixteen characters that are all hexadecimal are hex even when they are all
	// decimal digits. That is the documented consequence of resolving by width,
	// and it is the right way round: a seed is written in hex everywhere in this
	// project, and a caller who means a sixteen-digit decimal number can say so
	// by not padding it to sixteen characters.
	if got, err := ParseSeed("9999999999999999"); err != nil || got != 0x9999999999999999 {
		t.Errorf(`ParseSeed("9999999999999999") = %#x, %v; want 0x9999999999999999`, uint64(got), err)
	}
	// Seventeen characters is not the hex spelling, so it is decimal.
	if got, err := ParseSeed("99999999999999999"); err != nil || got != 99999999999999999 {
		t.Errorf(`ParseSeed("99999999999999999") = %d, %v`, got, err)
	}
}

// TestParseSeedRefusals covers what is not a seed.
func TestParseSeedRefusals(t *testing.T) {
	for _, text := range []string{"", "   ", "0x", "0xzz", "-1", "hello", "0x10000000000000000"} {
		if _, err := ParseSeed(text); !errors.Is(err, ErrSeedSpelling) {
			t.Errorf("ParseSeed(%q) = %v, want ErrSeedSpelling", text, err)
		}
	}
}

// TestIsCanonicalMatchesTheNormalizer is what makes the store's refusal the same
// question the constructor asks. A pair this reports canonical must survive
// NewCoord unchanged, and a pair it rejects must be one NewCoord would move —
// which is exactly why the store asks before calling it.
func TestIsCanonicalMatchesTheNormalizer(t *testing.T) {
	for _, tc := range []struct {
		q, r int64
		want bool
	}{
		{0, 0, true},
		{WorldRadius, 0, true},
		{0, -WorldRadius, true},
		{WorldRadius, -WorldRadius, true},
		{WorldRadius, 1, false}, // s is out of range
		{WorldRadius + 1, 0, false},
		{-WorldRadius - 1, 0, false},
		{98301, 0, false}, // the value AGENTS.md names: loud under int32
	} {
		if got := IsCanonical(tc.q, tc.r); got != tc.want {
			t.Errorf("IsCanonical(%d, %d) = %v, want %v", tc.q, tc.r, got, tc.want)
			continue
		}
		c := NewCoord(tc.q, tc.r)
		same := int64(c.Q()) == tc.q && int64(c.R()) == tc.r
		if same != tc.want {
			t.Errorf("IsCanonical(%d, %d) = %v but NewCoord %s it", tc.q, tc.r, tc.want,
				map[bool]string{true: "left", false: "moved"}[same])
		}
	}
}
