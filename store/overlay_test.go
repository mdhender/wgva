// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package store

import (
	"errors"
	"os"
	"testing"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"

	"github.com/mdhender/wgva"
)

// open creates a world and returns it open.
func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.Context(), newWorld(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestOpeningDoesNotWrite is the promise cmd/wgva-map and cmd/wgva-serve make:
// both open, neither writes.
//
// It is asserted byte for byte rather than by inspecting the tables, because the
// thing being ruled out is not a stray UPDATE — it is the migration ladder
// running when it has nothing to do, which touches no row and moves the file.
func TestOpeningDoesNotWrite(t *testing.T) {
	path := newWorld(t)

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		s, err := Open(t.Context(), path)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if _, err := s.AllOverlays(t.Context()); err != nil {
			t.Fatalf("AllOverlays: %v", err)
		}
		if err := s.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("opening and reading a world wrote to the file")
	}
}

// TestOverlayRoundTrip is the range scan a viewport load is.
func TestOverlayRoundTrip(t *testing.T) {
	s := open(t)

	seen := []wgva.Coord{
		wgva.NewCoord(0, 0),
		wgva.NewCoord(3, -1),
		wgva.NewCoord(-2, 5),
		wgva.NewCoord(1000, -1000),
	}
	if err := s.SetDiscovered(t.Context(), seen...); err != nil {
		t.Fatalf("SetDiscovered: %v", err)
	}
	if err := s.SetSettlement(t.Context(), wgva.NewCoord(3, -1), "Landing"); err != nil {
		t.Fatalf("SetSettlement: %v", err)
	}
	if err := s.SetLabel(t.Context(), wgva.NewCoord(-2, 5), "the narrows"); err != nil {
		t.Fatalf("SetLabel: %v", err)
	}

	all, err := s.AllOverlays(t.Context())
	if err != nil {
		t.Fatalf("AllOverlays: %v", err)
	}
	if len(all.Discovered) != len(seen) {
		t.Errorf("discovered %d tiles, want %d", len(all.Discovered), len(seen))
	}
	if len(all.Settlements) != 1 || all.Settlements[0].Name != "Landing" {
		t.Errorf("settlements = %+v", all.Settlements)
	}
	if len(all.Labels) != 1 || all.Labels[0].Name != "the narrows" {
		t.Errorf("labels = %+v", all.Labels)
	}

	// The scan is ordered by q then r, which is the order the renderer needs and
	// the order the primary key already has.
	for i := 1; i < len(all.Discovered); i++ {
		a, b := all.Discovered[i-1], all.Discovered[i]
		if a.Q() > b.Q() || (a.Q() == b.Q() && a.R() >= b.R()) {
			t.Errorf("discoveries are not in key order: %v then %v", a, b)
		}
	}

	// A box selects, and it selects a superset of a wrapped window rather than
	// exactly its tiles — which is correct, because an overlay outside the
	// window is never drawn.
	box, ok := BoxOf([]wgva.Coord{wgva.NewCoord(-2, -1), wgva.NewCoord(3, 5)})
	if !ok {
		t.Fatal("BoxOf returned no box")
	}
	near, err := s.Overlays(t.Context(), box)
	if err != nil {
		t.Fatalf("Overlays: %v", err)
	}
	if len(near.Discovered) != 3 {
		t.Errorf("the box selected %d tiles, want 3: %v", len(near.Discovered), near.Discovered)
	}
}

// TestCoordinatesAreRefusedNotRepaired is DESIGN.md 27.6, and it is the one
// shape of repair that must not happen: an out-of-range (q, r) is malformed data
// rather than a distant tile, and passing it through NewCoord would silently
// relocate a player's settlement to a real coordinate somewhere else.
func TestCoordinatesAreRefusedNotRepaired(t *testing.T) {
	for _, table := range []string{"overlay_discovered", "overlay_settlement", "overlay_label"} {
		t.Run(table, func(t *testing.T) {
			s := open(t)

			// A value outside the domain, written the way a corrupt file or
			// another program's bug would write one: straight into the column.
			insert := "INSERT INTO " + table + " (q, r) VALUES (98301, 0);"
			if table != "overlay_discovered" {
				insert = "INSERT INTO " + table + " (q, r, name) VALUES (98301, 0, 'x');"
			}
			if err := s.pool.with(t.Context(), func(conn *sqlite.Conn) error {
				return sqlitex.Execute(conn, insert, nil)
			}); err != nil {
				t.Fatal(err)
			}

			_, err := s.AllOverlays(t.Context())
			if !errors.Is(err, ErrBadCoordinate) {
				t.Fatalf("AllOverlays: %v, want ErrBadCoordinate", err)
			}
		})
	}
}

// TestPlayerFrameRoundTrip is the three scalars, and the two refusals around
// them. The frame type itself is render.PlayerFrame and is not in this package;
// store persists origin q, origin r, and rotation, and needs no dependency on
// render to do it.
func TestPlayerFrameRoundTrip(t *testing.T) {
	s := open(t)

	if err := s.CreatePlayer(t.Context(), "ada", wgva.NewCoord(12, -30), 4); err != nil {
		t.Fatalf("CreatePlayer: %v", err)
	}
	p, err := s.Player(t.Context(), "ada")
	if err != nil {
		t.Fatalf("Player: %v", err)
	}
	if p.Origin != wgva.NewCoord(12, -30) || p.Rotation != 4 {
		t.Errorf("player = %+v", p)
	}

	if _, err := s.Player(t.Context(), "nobody"); !errors.Is(err, ErrNoPlayer) {
		t.Errorf("Player of an absent player: %v, want ErrNoPlayer", err)
	}
}

// TestRotationIsRefusedOnWriteAndOnRead is DESIGN.md 28. It is a typed refusal
// at both ends and never a FloorMod: a player's origin and rotation are what
// every coordinate and heading they have ever been given *mean*, so quietly
// reducing one would silently relabel all of it.
func TestRotationIsRefusedOnWriteAndOnRead(t *testing.T) {
	s := open(t)

	for _, rotation := range []int{-1, 6, 9} {
		if err := s.CreatePlayer(t.Context(), "ada", wgva.NewCoord(0, 0), rotation); !errors.Is(err, ErrBadRotation) {
			t.Errorf("CreatePlayer rotation %d: %v, want ErrBadRotation", rotation, err)
		}
	}

	// The column has a CHECK constraint, so a stored six has to be forced past
	// it to test the read. Dropping the constraint is how a corrupt file or an
	// older schema would have produced one.
	if err := s.pool.with(t.Context(), func(conn *sqlite.Conn) error {
		return sqlitex.ExecuteScript(conn, `
PRAGMA writable_schema = ON;
UPDATE sqlite_schema SET sql = replace(sql, 'CHECK (rotation BETWEEN 0 AND 5)', '') WHERE name = 'player';
PRAGMA writable_schema = OFF;
`, nil)
	}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2, err := Open(t.Context(), s.path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s2.Close()
	if err := s2.pool.with(t.Context(), func(conn *sqlite.Conn) error {
		return sqlitex.Execute(conn,
			"INSERT INTO player (id, origin_q, origin_r, rotation) VALUES ('ada', 0, 0, 6);", nil)
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := s2.Players(t.Context()); !errors.Is(err, ErrBadRotation) {
		t.Fatalf("Players with a stored rotation of six: %v, want ErrBadRotation", err)
	}
}
