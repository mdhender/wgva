// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Package config owns what a configuration weighs: the canonical CBOR of
// DESIGN.md 21.2, the fingerprint over it, and the TOML file a person edits.
//
// It is separate from store so that the terrain tuning tool can name a
// fingerprint without linking SQLite — a tuning tool that linked SQLite would be
// a tuning tool that could open a world. It imports wgva and wgva does not
// import it, which is what keeps the CBOR dependency out of the core package.
package config

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"

	"github.com/fxamacker/cbor/v2"
	"github.com/mdhender/wgva"
)

// Digest is a configuration fingerprint: SHA-256 over the algorithm version, the
// world radius, and the canonical CBOR of the effective configuration.
type Digest [sha256.Size]byte

// String returns the full fingerprint in lower-case hex.
func (d Digest) String() string { return hex.EncodeToString(d[:]) }

// Short returns the first four bytes in hex. This is what a page prints and what
// a person reads out loud; it is not what code compares.
func (d Digest) Short() string { return hex.EncodeToString(d[:4]) }

// Tag returns the first eight bytes in hex, for an HTTP entity tag.
//
// Eight rather than the four a page prints, because a tuning session walks
// through hundreds of configurations under otherwise identical URLs and a
// four-byte tag would start colliding inside one afternoon. See DESIGN.md 29.1.
func (d Digest) Tag() string { return hex.EncodeToString(d[:8]) }

// ErrDigestSpelling is returned for text that is not a fingerprint.
var ErrDigestSpelling = errors.New("fingerprint must be sixty-four hexadecimal digits")

// ParseDigest reads a fingerprint written in full hex.
//
// It exists for the one caller that reads a fingerprint back out of a world
// file. Comparing the stored text to a computed String() would work and would
// also silently accept a value that is not a fingerprint at all, so the text is
// parsed and the two digests are compared as values.
func ParseDigest(text string) (Digest, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(strings.ToLower(text)))
	if err != nil || len(raw) != sha256.Size {
		return Digest{}, fmt.Errorf("%q: %w", text, ErrDigestSpelling)
	}
	var d Digest
	copy(d[:], raw)
	return d, nil
}

// canonicalEncoding is CBOR in canonical/deterministic mode.
//
// Its float policy is worth knowing about: canonical mode writes each float in
// the shortest form that represents it exactly, so the encoding is a
// dependency's policy rather than this module's. That is still binary and still
// exact, so the rule that a float64 is hashed as its bits is kept — and the
// written-down fingerprint constant of DESIGN.md 21.2 is the tripwire if a
// release ever changes the policy.
var canonicalEncoding = func() cbor.EncMode {
	em, err := cbor.CanonicalEncOptions().EncMode()
	if err != nil {
		panic(fmt.Sprintf("config: the CBOR library rejected canonical encoding options: %v", err))
	}
	return em
}()

// Fingerprint returns the fingerprint of an effective configuration:
//
//	SHA-256( algorithmVersion LE || worldRadius LE || canonical CBOR of cfg )
//
// The world radius is an input because it *is* the world's topology, and it is
// the radius rather than a coordinate width in bits: a width identified a world
// only while the radius was forced to be a storage type's maximum, and separated
// from the type it collides — 100000 and 131071 are different worlds and both
// are eighteen bits. It guards nothing at the settled radius; it is here because
// a fingerprint that had to gain an input later would invalidate every world in
// existence to do it. See DESIGN.md 4.2 and 21.2.
//
// The configuration must be valid. That is not a convenience check: validation
// is what rejects NaN, and a NaN reaching the encoder would produce a
// fingerprint for a configuration that cannot generate a world.
//
// This hashes what was *declared*, not what was *run*. Every input is a number
// somebody wrote down and none of them is the generator's code, so a build that
// changes a noise formula without bumping the algorithm version produces a
// different world under an identical fingerprint. Do not close that gap by
// folding the build into this hash — gate 6 of DESIGN.md 27.5 compares it when a
// world is reopened, and a build-dependent fingerprint would refuse every
// existing world on every patch release. The build identity is carried
// separately, by the creation guard of DESIGN.md 29.5.
func Fingerprint(algorithmVersion uint32, worldRadius int64, cfg wgva.Config) (Digest, error) {
	if err := cfg.Validate(); err != nil {
		return Digest{}, err
	}

	encoded, err := canonicalEncoding.Marshal(normalizeZeros(cfg))
	if err != nil {
		return Digest{}, fmt.Errorf("config: encoding the configuration: %w", err)
	}

	h := sha256.New()
	h.Write(binary.LittleEndian.AppendUint32(nil, algorithmVersion))
	h.Write(binary.LittleEndian.AppendUint64(nil, uint64(worldRadius)))
	h.Write(encoded)

	var d Digest
	copy(d[:], h.Sum(nil))
	return d, nil
}

// Of returns the fingerprint of a configuration under this binary's algorithm
// version and world radius. It is the form every caller in this module wants;
// Fingerprint takes the two explicitly because a stored world supplies its own.
func Of(cfg wgva.Config) (Digest, error) {
	return Fingerprint(wgva.AlgorithmVersion, wgva.WorldRadius, cfg)
}

// DefaultDigest is the fingerprint of this binary's default configuration.
//
// The terrain tuning tool prints it so that a session can be labeled as this
// binary's defaults or as modified, conspicuously, beside the identity pair.
// DESIGN.md 29.1.
func DefaultDigest() Digest {
	d, err := Of(wgva.DefaultConfig())
	if err != nil {
		// DefaultConfig is checked by a test in wgva, so reaching this means the
		// defaults and the validation have been changed apart.
		panic(fmt.Sprintf("config: the default configuration is invalid: %v", err))
	}
	return d
}

// IsDefault reports whether a configuration is this binary's defaults.
//
// It compares fingerprints rather than structs, so it answers the question the
// tool is actually asking — would this produce the same world — rather than
// whether two Go values happen to be equal.
func IsDefault(cfg wgva.Config) bool {
	d, err := Of(cfg)
	return err == nil && d == DefaultDigest()
}

// normalizeZeros returns a copy of cfg with every negative zero replaced by a
// positive one.
//
// IEEE-754 says -0.0 == 0.0 and gives them different bit patterns, so two
// configurations that compare equal and generate identical worlds would
// otherwise fingerprint differently. The walk is reflective rather than a list
// of field names on purpose: a list is a thing to forget to extend, and Config
// gains fields every phase.
func normalizeZeros(cfg wgva.Config) wgva.Config {
	v := reflect.ValueOf(&cfg).Elem()
	var walk func(reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Float64, reflect.Float32:
			if v.Float() == 0 && math.Signbit(v.Float()) {
				v.SetFloat(0)
			}
		case reflect.Struct:
			for i := range v.NumField() {
				walk(v.Field(i))
			}
		case reflect.Slice, reflect.Array:
			for i := range v.Len() {
				walk(v.Index(i))
			}
		}
	}
	walk(v)
	return cfg
}
