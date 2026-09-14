// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import "fmt"

// Generator holds an immutable seed and configuration and is safe for
// concurrent read-only use.
//
// Go cannot check that at compile time, which is a real loss, so the shape is
// maintained by hand and asserted by a test: no mutex, no channel, no map, and
// no captured-state function value may appear in this struct, and any cache
// belongs outside it. See DESIGN.md 22 and 30.12.
//
// The generation methods land in the phases that define what they compute. What
// exists here is what every one of them will be a function of.
type Generator struct {
	seed Seed
	cfg  Config
}

// New returns a generator for the seed and configuration, or the first reason
// the configuration cannot be used.
func New(seed Seed, cfg Config) (*Generator, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Generator{seed: seed, cfg: cfg}, nil
}

// NewDefault returns a generator for the seed and the built-in defaults. It
// cannot fail, and it is the ergonomic path for tests and the terrain tuning
// tool.
func NewDefault(seed Seed) *Generator {
	g, err := New(seed, DefaultConfig())
	if err != nil {
		// DefaultConfig is checked by TestDefaultConfigValidates, so reaching
		// this means the defaults and the validation have been changed apart.
		panic(fmt.Sprintf("wgva: default configuration is invalid: %v", err))
	}
	return g
}

// Seed returns the world seed.
func (g *Generator) Seed() Seed { return g.seed }

// Config returns the effective configuration.
//
// It returns a value rather than a pointer, and that states the intent: the
// configuration is immutable after construction. Config will contain a slice or
// two as the later phases land, so the copy is shallow and a caller could in
// principle reach into it; returning a pointer would state the opposite.
func (g *Generator) Config() Config { return g.cfg }
