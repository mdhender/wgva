// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestNewRejectsAnInvalidConfiguration(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SeaLevel = math.NaN()

	g, err := New(1, cfg)
	if err == nil {
		t.Fatal("New accepted an invalid configuration")
	}
	if g != nil {
		t.Fatal("New returned a generator alongside an error")
	}
	if !errors.Is(err, ErrNotFinite) {
		t.Fatalf("New returned %v, want an error wrapping %v", err, ErrNotFinite)
	}
}

func TestNewDefaultCarriesTheSeedAndDefaults(t *testing.T) {
	g := NewDefault(0x5747_5641)
	if got, want := g.Seed(), Seed(0x5747_5641); got != want {
		t.Fatalf("Seed() = %d, want %d", got, want)
	}
	if got := g.Config(); got != DefaultConfig() {
		t.Fatalf("Config() = %+v, want the defaults", got)
	}
}

// TestConfigReturnsACopy states the intent behind returning a value rather than
// a pointer: the configuration is immutable after construction.
func TestConfigReturnsACopy(t *testing.T) {
	g := NewDefault(1)
	cfg := g.Config()
	cfg.SeaLevel = 0.123
	cfg.Rim.ClosedHexes = 999

	if g.Config().SeaLevel == 0.123 || g.Config().Rim.ClosedHexes == 999 {
		t.Fatal("mutating the returned configuration changed the generator")
	}
}

// TestGeneratorShape is the closest thing Go offers to the concurrency
// assertion this type wants. A Generator is immutable and safe for concurrent
// reads, which means no mutex, no channel, no map, and no captured-state
// function value may appear in it, and any cache belongs outside. See
// DESIGN.md 30.12.
func TestGeneratorShape(t *testing.T) {
	var forbidden func(reflect.Type, string)
	forbidden = func(typ reflect.Type, path string) {
		switch typ.Kind() {
		case reflect.Chan, reflect.Map, reflect.Func, reflect.UnsafePointer:
			t.Errorf("%s is a %v, which a concurrently read Generator must not hold", path, typ.Kind())
		case reflect.Struct:
			if typ.String() == "sync.Mutex" || typ.String() == "sync.RWMutex" {
				t.Errorf("%s is a %s", path, typ)
			}
			for i := range typ.NumField() {
				f := typ.Field(i)
				forbidden(f.Type, path+"."+f.Name)
			}
		case reflect.Pointer, reflect.Slice, reflect.Array:
			forbidden(typ.Elem(), path+"[]")
		}
	}
	forbidden(reflect.TypeFor[Generator](), "Generator")
}

// TestGeneratorIsSafeForConcurrentReads is the -race half of the same claim.
func TestGeneratorIsSafeForConcurrentReads(t *testing.T) {
	g := NewDefault(99)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 10_000 {
				if g.Seed() != 99 || g.Config().SeaLevel != DefaultConfig().SeaLevel {
					t.Error("a concurrent read saw a changed generator")
					return
				}
			}
		})
	}
	wg.Wait()
}
