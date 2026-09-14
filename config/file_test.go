// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package config_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/mdhender/wgva"
	"github.com/mdhender/wgva/config"
)

// TestFieldTableCoversConfig is what makes the field table safe to rely on.
//
// The table is the single description of what a configuration contains, and
// three places read it: the file writer, the file reader's presence check, and
// the tuning tool's form. If Config gains a field and the table does not, that
// field becomes unwritable and unreachable — and, worse, unmissable, because the
// presence check only knows about keys the table names. So the table is compared
// against the reflected leaf field set, in both directions.
func TestFieldTableCoversConfig(t *testing.T) {
	want := map[string]bool{}
	var walk func(reflect.Type, string)
	walk = func(typ reflect.Type, prefix string) {
		for i := range typ.NumField() {
			f := typ.Field(i)
			path := f.Name
			if prefix != "" {
				path = prefix + "." + f.Name
			}
			if f.Type.Kind() == reflect.Struct {
				walk(f.Type, path)
				continue
			}
			want[path] = true
		}
	}
	walk(reflect.TypeFor[wgva.Config](), "")

	got := map[string]bool{}
	keys := map[string]bool{}
	for _, f := range config.Fields() {
		if got[f.Path] {
			t.Errorf("%s is named by two keys", f.Path)
		}
		got[f.Path] = true
		if keys[f.Key] {
			t.Errorf("%q is used by two fields", f.Key)
		}
		keys[f.Key] = true
	}

	for path := range want {
		if !got[path] {
			t.Errorf("wgva.Config has %s and the field table does not; add a key for it", path)
		}
	}
	for path := range got {
		if !want[path] {
			t.Errorf("the field table names %s and wgva.Config does not have it", path)
		}
	}
}

// TestFieldKeysAreFlatAndReadable pins the file's shape. It is flat and
// snake_case so a diff between two configurations reads as a list of changed
// numbers, and scale fields name their unit in the identifier so a reader never
// has to guess whether a number is miles or hexes.
func TestFieldKeysAreFlatAndReadable(t *testing.T) {
	for _, f := range config.Fields() {
		if strings.ContainsAny(f.Key, ". [") || strings.ToLower(f.Key) != f.Key {
			t.Errorf("%q is not a flat lower-case key", f.Key)
		}
		if f.Doc == "" {
			t.Errorf("%q has no documentation, so the file and the form have nothing to say about it", f.Key)
		}
		if strings.HasSuffix(f.Path, "Miles") && !strings.HasSuffix(f.Key, "_miles") {
			t.Errorf("%q holds miles and does not say so", f.Key)
		}
		if strings.HasSuffix(f.Path, "Hexes") && !strings.HasSuffix(f.Key, "_hexes") {
			t.Errorf("%q holds hexes and does not say so", f.Key)
		}
	}
}

// TestRoundTrip is the settling test of DESIGN.md 29.1, asserted on the
// fingerprint as well as on the values: a file that loses a low bit is a
// silently different world, and comparing the structs alone would not
// necessarily say so.
func TestRoundTrip(t *testing.T) {
	configs := map[string]wgva.Config{
		"defaults": wgva.DefaultConfig(),
		"no warp": func() wgva.Config {
			c := wgva.DefaultConfig()
			c.WarpStrengthMiles = 0
			return c
		}(),
		"no rim": func() wgva.Config {
			c := wgva.DefaultConfig()
			c.Rim.ClosedHexes, c.Rim.FalloffHexes = 0, 0
			return c
		}(),
		"polar ice": func() wgva.Config {
			c := wgva.DefaultConfig()
			c.Rim.Kind = wgva.RimPolarIce
			return c
		}(),
		"awkward floats": func() wgva.Config {
			c := wgva.DefaultConfig()
			c.SeaLevel = 0.1 + 0.2
			c.Continental.Gain = 1.0 / 3.0
			c.Regional.Lacunarity = 2.0000000000000004
			c.Local.WavelengthMiles = 123.456789012345
			return c
		}(),
	}

	for name, cfg := range configs {
		t.Run(name, func(t *testing.T) {
			data, err := config.Marshal(cfg)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			back, err := config.Unmarshal(data)
			if err != nil {
				t.Fatalf("Unmarshal: %v\n%s", err, data)
			}
			if back != cfg {
				t.Errorf("the round trip changed the configuration:\ngot  %+v\nwant %+v", back, cfg)
			}
			before, err := config.Of(cfg)
			if err != nil {
				t.Fatalf("Of: %v", err)
			}
			after, err := config.Of(back)
			if err != nil {
				t.Fatalf("Of: %v", err)
			}
			if before != after {
				t.Errorf("the round trip changed the fingerprint: %s became %s", before, after)
			}
		})
	}
}

// TestMissingKeyIsRefused constructs a file with each key removed in turn and
// asserts a refusal naming that key. This is the test DESIGN.md 21.1 asks for by
// name, and the hazard it covers has no syntax to grep for: Go defaults every
// absent field silently, so a file missing sea_level would decode to sea level
// 0.0 and generate a different world under an unchanged version number.
func TestMissingKeyIsRefused(t *testing.T) {
	data, err := config.Marshal(wgva.DefaultConfig())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	lines := strings.Split(string(data), "\n")

	for _, f := range config.Fields() {
		t.Run(f.Key, func(t *testing.T) {
			var kept []string
			removed := false
			for _, line := range lines {
				if strings.HasPrefix(line, f.Key+" =") {
					removed = true
					continue
				}
				kept = append(kept, line)
			}
			if !removed {
				t.Fatalf("the file has no line for %s", f.Key)
			}

			_, err := config.Unmarshal([]byte(strings.Join(kept, "\n")))
			if !errors.Is(err, config.ErrMissingKey) {
				t.Fatalf("Unmarshal returned %v, want an error wrapping ErrMissingKey", err)
			}
			fe, ok := errors.AsType[*config.FileError](err)
			if !ok {
				t.Fatalf("Unmarshal returned %T, want a *config.FileError", err)
			}
			if fe.Key != f.Key {
				t.Fatalf("the refusal names %q, want %q", fe.Key, f.Key)
			}
		})
	}
}

// TestUnknownKeyIsRefused is the same rule read from the other side: an older
// binary must reject a newer world's configuration, not quietly ignore what it
// cannot interpret.
func TestUnknownKeyIsRefused(t *testing.T) {
	data, err := config.Marshal(wgva.DefaultConfig())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	for _, extra := range []string{
		"volcanic_tendency = 0.25",
		"[climate]\nheat_bands = 3",
	} {
		t.Run(extra, func(t *testing.T) {
			_, err := config.Unmarshal(fmt.Appendf(data, "\n%s\n", extra))
			if !errors.Is(err, config.ErrUnknownKey) {
				t.Fatalf("Unmarshal returned %v, want an error wrapping ErrUnknownKey", err)
			}
		})
	}
}

// TestBadValueIsRefused covers the third refusal: a key that is present and
// cannot be read as its kind.
func TestBadValueIsRefused(t *testing.T) {
	cases := map[string]string{
		"sea level is a word":  `sea_level = "half"`,
		"octaves is a float":   "continental_octaves = 2.5",
		"rim kind is a number": "rim_kind = 1",
		"rim kind is unknown":  `rim_kind = "lava"`,
	}
	base, err := config.Marshal(wgva.DefaultConfig())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	for name, replacement := range cases {
		t.Run(name, func(t *testing.T) {
			key, _, _ := strings.Cut(replacement, " ")
			var kept []string
			for line := range strings.SplitSeq(string(base), "\n") {
				if strings.HasPrefix(line, key+" =") {
					line = replacement
				}
				kept = append(kept, line)
			}
			_, err := config.Unmarshal([]byte(strings.Join(kept, "\n")))
			if !errors.Is(err, config.ErrBadValue) {
				t.Fatalf("Unmarshal returned %v, want an error wrapping ErrBadValue", err)
			}
		})
	}
}

// TestSyntaxErrorIsRefused covers the file that is not TOML at all.
func TestSyntaxErrorIsRefused(t *testing.T) {
	if _, err := config.Unmarshal([]byte("sea_level = = 0.5")); !errors.Is(err, config.ErrSyntax) {
		t.Fatalf("Unmarshal returned %v, want an error wrapping ErrSyntax", err)
	}
}

// TestInvalidConfigurationIsRefused asserts that a syntactically perfect file
// still has to describe a usable world.
func TestInvalidConfigurationIsRefused(t *testing.T) {
	base, err := config.Marshal(wgva.DefaultConfig())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	spoiled := strings.ReplaceAll(string(base), "\nsea_level = 0.62\n", "\nsea_level = 1.5\n")
	if spoiled == string(base) {
		t.Fatal("the test did not find the line it meant to change")
	}
	if _, err := config.Unmarshal([]byte(spoiled)); !errors.Is(err, wgva.ErrOutOfRange) {
		t.Fatalf("Unmarshal returned %v, want an error wrapping ErrOutOfRange", err)
	}
}

// TestHeaderNamesTheIdentity asserts what a person reads off a file on disk.
func TestHeaderNamesTheIdentity(t *testing.T) {
	cfg := wgva.DefaultConfig()
	data, err := config.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	d, err := config.Of(cfg)
	if err != nil {
		t.Fatalf("Of: %v", err)
	}
	for _, want := range []string{
		fmt.Sprintf("algorithm version = %d", wgva.AlgorithmVersion),
		fmt.Sprintf("world radius      = %d", wgva.WorldRadius),
		d.String(),
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("the header does not carry %q", want)
		}
	}
}
