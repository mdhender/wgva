// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrSeedSpelling is returned for text that is not a seed.
var ErrSeedSpelling = errors.New("seed must be sixteen hexadecimal digits, or a decimal number")

// String renders a seed the way every tool prints it: sixteen hexadecimal
// digits with an 0x prefix.
//
// Hex rather than decimal because a seed is copied between a browser, a
// terminal, and a commit message, and 0x0123456789abcdef is recognizable at a
// glance in a way that its decimal expansion is not. The prefix is printed
// because it says which spelling this is; ParseSeed accepts the bare sixteen
// digits DESIGN.md 29.3 writes in a route segment either way.
func (s Seed) String() string { return fmt.Sprintf("0x%016x", uint64(s)) }

// ParseSeed reads a seed in any of the three spellings a person has in front of
// them: sixteen hexadecimal digits with an 0x prefix, the same sixteen digits
// bare, or a decimal number.
//
// It lives here rather than in view because a seed is not window grammar and
// because cmd/wgva-world needs it: that command must never import render
// (DESIGN.md 28), view imports render, and a second seed parser written to
// dodge that edge would be a second opinion about what a seed is. view.ParseSeed
// wraps this one so a malformed query parameter still earns a view refusal.
//
// The bare-hex form is ambiguous with decimal for a number written in digits
// alone — 123 is a hundred and twenty three in one reading and 291 in the other
// — so the ambiguity is resolved by width: exactly sixteen characters that are
// all hexadecimal is the hex spelling, and anything else that parses as decimal
// is decimal. Sixteen digits is how a seed is written everywhere it is written
// in full, and a decimal seed that happens to be sixteen digits long is one a
// caller can spell with the 0x prefix or not mean.
func ParseSeed(text string) (Seed, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, fmt.Errorf("%q: %w", text, ErrSeedSpelling)
	}

	if lower := strings.ToLower(text); strings.HasPrefix(lower, "0x") {
		n, err := strconv.ParseUint(lower[2:], 16, 64)
		if err != nil {
			return 0, fmt.Errorf("%q: %w", text, ErrSeedSpelling)
		}
		return Seed(n), nil
	}

	if len(text) == 16 {
		if n, err := strconv.ParseUint(strings.ToLower(text), 16, 64); err == nil {
			return Seed(n), nil
		}
	}

	n, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q: %w", text, ErrSeedSpelling)
	}
	return Seed(n), nil
}
