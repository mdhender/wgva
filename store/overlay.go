// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package store

import (
	"context"
	"fmt"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"

	"github.com/mdhender/wgva"
)

// Marker is one thing a player put on the map: a coordinate and the name they
// gave it.
//
// It is this package's own type rather than render.Marker, because store does
// not import render and must not: the two are siblings, and generated terrain
// and player overlays meet in exactly one place, which is render.RenderPlayer,
// at render time, in pixels. The command converts. DESIGN.md 29.4.
type Marker struct {
	Coord wgva.Coord
	Name  string
}

// Overlays is the mutable player state in a world file: what the player has
// seen, and what the player has built.
//
// Every slice is in coordinate order — by q, then by r — because that is the
// order the range scan returns and because markers overlap pixels, so a stable
// render order is a requirement rather than a nicety.
type Overlays struct {
	Discovered  []wgva.Coord
	Settlements []Marker
	Labels      []Marker
}

// Box is the smallest rectangle of raw (q, r) values holding a set of tiles.
//
// It is a rectangle in coordinate space and not a window: a wrapped window's
// tiles are a superset of what it draws, which is correct for this purpose
// because an overlay outside the window is never drawn. DESIGN.md 29.4.
type Box struct {
	MinQ, MaxQ int64
	MinR, MaxR int64
}

// BoxOf returns the smallest box holding every coordinate given. An empty set
// gives an empty box, which selects nothing.
func BoxOf(coords []wgva.Coord) (Box, bool) {
	if len(coords) == 0 {
		return Box{}, false
	}
	b := Box{
		MinQ: int64(coords[0].Q()), MaxQ: int64(coords[0].Q()),
		MinR: int64(coords[0].R()), MaxR: int64(coords[0].R()),
	}
	for _, c := range coords[1:] {
		b.MinQ = min(b.MinQ, int64(c.Q()))
		b.MaxQ = max(b.MaxQ, int64(c.Q()))
		b.MinR = min(b.MinR, int64(c.R()))
		b.MaxR = max(b.MaxR, int64(c.R()))
	}
	return b, true
}

// Overlays loads every overlay inside a box.
//
// It is one ordered range scan per table over the composite primary key, which
// is what WITHOUT ROWID buys: the table *is* the B-tree, so the rows of a
// viewport come back in key order with no secondary index and no rowid lookup
// per row. DESIGN.md 27.2.
func (s *Store) Overlays(ctx context.Context, b Box) (Overlays, error) {
	return s.overlays(ctx, &b)
}

// AllOverlays loads every overlay row in the world.
//
// It is unbounded rather than a box of the whole canonical domain, and the
// difference is the point: a row outside the domain is malformed data that must
// be refused, and a bounded scan would not select it. A whole-world read that
// silently skipped exactly the rows this package exists to refuse would be the
// worst of both. See [coordOf].
func (s *Store) AllOverlays(ctx context.Context) (Overlays, error) {
	return s.overlays(ctx, nil)
}

func (s *Store) overlays(ctx context.Context, b *Box) (Overlays, error) {
	var o Overlays
	err := s.pool.with(ctx, func(conn *sqlite.Conn) error {
		coords, err := scanCoords(conn, "overlay_discovered", b)
		if err != nil {
			return err
		}
		o.Discovered = coords

		if o.Settlements, err = scanMarkers(conn, "overlay_settlement", b); err != nil {
			return err
		}
		if o.Labels, err = scanMarkers(conn, "overlay_label", b); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return Overlays{}, err
	}
	return o, nil
}

// where renders the range predicate a box selects with, or selects everything.
func where(b *Box) (string, []any) {
	if b == nil {
		return "", nil
	}
	return " WHERE q BETWEEN ? AND ? AND r BETWEEN ? AND ?", []any{b.MinQ, b.MaxQ, b.MinR, b.MaxR}
}

// scanCoords reads a coordinate-only overlay table.
func scanCoords(conn *sqlite.Conn, table string, b *Box) ([]wgva.Coord, error) {
	clause, args := where(b)
	var out []wgva.Coord
	err := sqlitex.Execute(conn,
		"SELECT q, r FROM "+table+clause+" ORDER BY q, r;",
		&sqlitex.ExecOptions{
			Args: args,
			ResultFunc: func(stmt *sqlite.Stmt) error {
				c, err := coordOf(table, stmt.ColumnInt64(0), stmt.ColumnInt64(1))
				if err != nil {
					return err
				}
				out = append(out, c)
				return nil
			},
		})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// scanMarkers reads a named overlay table.
func scanMarkers(conn *sqlite.Conn, table string, b *Box) ([]Marker, error) {
	clause, args := where(b)
	var out []Marker
	err := sqlitex.Execute(conn,
		"SELECT q, r, name FROM "+table+clause+" ORDER BY q, r;",
		&sqlitex.ExecOptions{
			Args: args,
			ResultFunc: func(stmt *sqlite.Stmt) error {
				c, err := coordOf(table, stmt.ColumnInt64(0), stmt.ColumnInt64(1))
				if err != nil {
					return err
				}
				out = append(out, Marker{Coord: c, Name: stmt.ColumnText(2)})
				return nil
			},
		})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// coordOf turns a stored (q, r) into a coordinate, or refuses it.
//
// **This is the one shape of repair that must not happen.** SQLite columns hold
// anything, so a pair outside the canonical domain is malformed data and not a
// distant tile; passing it through wgva.NewCoord would normalize it to a real
// coordinate somewhere else and silently relocate a player's settlement, which
// is worse than any error because a coordinate still resolves and nothing looks
// wrong. DESIGN.md 27.6.
func coordOf(table string, q, r int64) (wgva.Coord, error) {
	if !wgva.IsCanonical(q, r) {
		return wgva.Coord{}, &OpenError{
			Detail: fmt.Sprintf("%s has a row at (%d, %d), which is outside +/-%d", table, q, r, wgva.WorldRadius),
			Err:    ErrBadCoordinate,
		}
	}
	return wgva.NewCoord(q, r), nil
}

// SetDiscovered marks tiles as seen. It is written by the game engine, which is
// outside this document; it is here so that a test and a fixture can build a
// world worth drawing.
func (s *Store) SetDiscovered(ctx context.Context, coords ...wgva.Coord) error {
	return s.pool.with(ctx, func(conn *sqlite.Conn) (err error) {
		defer sqlitex.Transaction(conn)(&err)
		for _, c := range coords {
			if err = sqlitex.Execute(conn,
				"INSERT OR IGNORE INTO overlay_discovered (q, r) VALUES (?, ?);",
				&sqlitex.ExecOptions{Args: []any{int64(c.Q()), int64(c.R())}}); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetSettlement records a settlement. SetLabel records a label.
func (s *Store) SetSettlement(ctx context.Context, c wgva.Coord, name string) error {
	return s.setMarker(ctx, "overlay_settlement", c, name)
}

// SetLabel records a label.
func (s *Store) SetLabel(ctx context.Context, c wgva.Coord, name string) error {
	return s.setMarker(ctx, "overlay_label", c, name)
}

func (s *Store) setMarker(ctx context.Context, table string, c wgva.Coord, name string) error {
	return s.pool.with(ctx, func(conn *sqlite.Conn) error {
		return sqlitex.Execute(conn,
			"INSERT INTO "+table+" (q, r, name) VALUES (?, ?, ?) ON CONFLICT (q, r) DO UPDATE SET name = excluded.name;",
			&sqlitex.ExecOptions{Args: []any{int64(c.Q()), int64(c.R()), name}})
	})
}
