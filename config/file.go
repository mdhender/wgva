// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/mdhender/wgva"
)

// The file error model. Each reason is a sentinel so a test can assert with
// errors.Is rather than on a message string.
var (
	// ErrSyntax is returned for a file that is not valid TOML.
	ErrSyntax = errors.New("configuration file is not valid TOML")

	// ErrUnknownKey is returned for a key this binary does not understand.
	//
	// This is the rule of DESIGN.md 21.1 read from the reading side: an older
	// binary must *reject* a newer world's configuration, not quietly ignore the
	// fields it cannot interpret and generate a different world under a version
	// number that says otherwise.
	ErrUnknownKey = errors.New("unknown configuration key")

	// ErrMissingKey is returned for a key the file does not have.
	//
	// This is the Go hazard, and it is worse than the Rust one it replaces. Rust
	// had to be told not to write #[serde(default)]; Go defaults every absent
	// field silently, so a file missing sea_level would decode to sea level 0.0
	// and generate a different world under an unchanged version number, with no
	// syntax anywhere to grep for.
	ErrMissingKey = errors.New("configuration key is missing")

	// ErrBadValue is returned for a value that cannot be parsed as its key's
	// kind.
	ErrBadValue = errors.New("configuration value cannot be parsed")
)

// FileError names the key that failed and wraps the reason.
type FileError struct {
	Key    string
	Detail string
	Err    error
}

// Error renders the key, the reason, and whatever detail the reason has.
func (e *FileError) Error() string {
	switch {
	case e.Key == "":
		return fmt.Sprintf("%v: %s", e.Err, e.Detail)
	case e.Detail == "":
		return fmt.Sprintf("%s: %v", e.Key, e.Err)
	default:
		return fmt.Sprintf("%s: %v: %q", e.Key, e.Err, e.Detail)
	}
}

// Unwrap returns the sentinel reason.
func (e *FileError) Unwrap() error { return e.Err }

// Marshal renders a configuration as the TOML file a person edits: flat, one key
// per line, with the algorithm version, the world radius, and the fingerprint in
// a comment header.
//
// It is written directly rather than through an encoder, for the reason
// DESIGN.md 29.1 gives: an encoder's default float formatting is not guaranteed
// to be the shortest representation that round-trips, and a file that loses a
// low bit is a silently different world. Field.Get does the formatting.
//
// The header is a comment, so it is not read back. It is there for a person
// looking at a file on disk and for a diff that would otherwise not say which
// binary wrote it.
func Marshal(cfg wgva.Config) ([]byte, error) {
	d, err := Of(cfg)
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# WGVA configuration\n")
	fmt.Fprintf(&b, "#\n")
	fmt.Fprintf(&b, "# algorithm version = %d\n", wgva.AlgorithmVersion)
	fmt.Fprintf(&b, "# world radius      = %d\n", wgva.WorldRadius)
	fmt.Fprintf(&b, "# fingerprint       = %s\n", d)
	fmt.Fprintf(&b, "# build             = %s\n", wgva.Version())
	fmt.Fprintf(&b, "#\n")
	fmt.Fprintf(&b, "# Every key below must be present. A missing key is a refusal naming it,\n")
	fmt.Fprintf(&b, "# never a zero value, and an unknown key is a refusal too.\n")

	group := ""
	for _, f := range Fields() {
		if f.Group != group {
			group = f.Group
			fmt.Fprintf(&b, "\n# --- %s ---\n", group)
		}
		fmt.Fprintf(&b, "\n# %s\n", f.Doc)
		if f.Kind == ValueChoice {
			names := make([]string, 0, len(f.Choices))
			for _, c := range f.Choices {
				names = append(names, c.Name)
			}
			fmt.Fprintf(&b, "# one of: %s\n", strings.Join(names, ", "))
			fmt.Fprintf(&b, "%s = %q\n", f.Key, f.Get(cfg))
			continue
		}
		fmt.Fprintf(&b, "%s = %s\n", f.Key, f.Get(cfg))
	}
	return []byte(b.String()), nil
}

// Unmarshal parses a configuration file and returns the effective configuration.
//
// It refuses a file with an unknown key, refuses a file missing any key, and
// refuses a configuration that does not validate. The first two are the rules of
// DESIGN.md 21.1 and neither is a decoder default in Go; the presence check is
// done by comparing the decoded key set against the field table rather than by
// inspecting zero values, because a legitimate zero and an absent key have the
// same zero value and that is the whole problem.
func Unmarshal(data []byte) (wgva.Config, error) {
	var raw map[string]any
	md, err := toml.Decode(string(data), &raw)
	if err != nil {
		return wgva.Config{}, &FileError{Detail: err.Error(), Err: ErrSyntax}
	}

	known := map[string]Field{}
	for _, f := range Fields() {
		known[f.Key] = f
	}

	// Undecoded reports keys the decoder could not place. Decoding into a map
	// places everything, so the unknown-key check is the explicit one below;
	// this catches a nested table, which the flat file has none of and which
	// would otherwise arrive as a key holding a map.
	for _, key := range md.Undecoded() {
		return wgva.Config{}, &FileError{Key: key.String(), Err: ErrUnknownKey}
	}

	var unknown []string
	for key := range raw {
		if _, ok := known[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		return wgva.Config{}, &FileError{Key: unknown[0], Detail: joinExtra(unknown), Err: ErrUnknownKey}
	}

	// Start from the zero value rather than from the defaults. Starting from the
	// defaults would make a missing key invisible, which is exactly the failure
	// the presence check exists to prevent — and it would make this function's
	// correctness depend on the check never being weakened.
	var cfg wgva.Config
	for _, f := range Fields() {
		value, ok := raw[f.Key]
		if !ok {
			return wgva.Config{}, &FileError{Key: f.Key, Err: ErrMissingKey}
		}
		text, err := literalOf(f, value)
		if err != nil {
			return wgva.Config{}, err
		}
		if err := f.Set(&cfg, text); err != nil {
			return wgva.Config{}, err
		}
	}

	if err := cfg.Validate(); err != nil {
		return wgva.Config{}, err
	}
	return cfg, nil
}

// literalOf renders a decoded TOML value as the text Field.Set parses.
//
// The round trip through text is deliberate. It keeps one parser for a value
// however it arrived — from a file, from a form control, from a query
// parameter — so the three cannot disagree about what "1e3" means.
//
// An integer is accepted for a scalar key because the shortest round-tripping
// form of 6000.0 is "6000", which TOML reads as an integer. Refusing it would
// mean a file this package wrote could not be read back by this package.
func literalOf(f Field, value any) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case int64:
		return fmt.Sprintf("%d", v), nil
	case float64:
		return fmt.Sprintf("%v", v), nil
	case bool:
		return fmt.Sprintf("%t", v), nil
	default:
		return "", &FileError{Key: f.Key, Detail: fmt.Sprintf("%T", value), Err: ErrBadValue}
	}
}

func joinExtra(keys []string) string {
	if len(keys) == 1 {
		return ""
	}
	return fmt.Sprintf("and %d more", len(keys)-1)
}
