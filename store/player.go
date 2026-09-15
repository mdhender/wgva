// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package store

import (
	"context"
	"errors"
	"fmt"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"

	"github.com/mdhender/wgva"
)

// ErrNoPlayer is returned for a player identity this world does not have.
var ErrNoPlayer = errors.New("no such player")

// Player is the three scalars a player's frame is: the canonical coordinate
// that player calls (0, 0), and the absolute direction that player calls north.
//
// The frame *type* is render.PlayerFrame, and it is not here. This package
// persists the scalars and nothing more, so it needs no dependency on render —
// which also keeps a player concept out of wgva itself, since render and store
// are siblings and neither is the core. DESIGN.md 28.
//
// This is authoritative on the same terms as any other player state, and more
// so: the origin and rotation are never derived from the seed, are never
// regenerated, survive discarding every cache, and are what gives every other
// piece of player-facing state its meaning — a settlement one player calls
// (3, -1) is a different tile under a different frame. DESIGN.md 27.6.
//
// It is written once, when the player is created. There is no operation that
// moves a frame afterwards, because changing one would silently relabel every
// coordinate and heading that player has ever been given.
type Player struct {
	ID       string
	Origin   wgva.Coord
	Rotation int
}

// CreatePlayer writes a player's frame. It refuses a rotation outside 0..5 on
// the way in, and Players refuses one on the way out; a CHECK constraint refuses
// it in between.
func (s *Store) CreatePlayer(ctx context.Context, id string, origin wgva.Coord, rotation int) error {
	if rotation < 0 || rotation > 5 {
		return &OpenError{Found: int64(rotation), Detail: "on write", Err: ErrBadRotation}
	}
	return s.pool.with(ctx, func(conn *sqlite.Conn) error {
		return sqlitex.Execute(conn,
			"INSERT INTO player (id, origin_q, origin_r, rotation) VALUES (?, ?, ?, ?);",
			&sqlitex.ExecOptions{Args: []any{id, int64(origin.Q()), int64(origin.R()), int64(rotation)}})
	})
}

// Player reads one player's frame.
func (s *Store) Player(ctx context.Context, id string) (Player, error) {
	var (
		p     Player
		found bool
	)
	err := s.pool.with(ctx, func(conn *sqlite.Conn) error {
		return sqlitex.Execute(conn,
			"SELECT id, origin_q, origin_r, rotation FROM player WHERE id = ?;",
			&sqlitex.ExecOptions{
				Args: []any{id},
				ResultFunc: func(stmt *sqlite.Stmt) error {
					var err error
					p, err = playerOf(stmt)
					found = err == nil
					return err
				},
			})
	})
	switch {
	case err != nil:
		return Player{}, err
	case !found:
		return Player{}, fmt.Errorf("%q: %w", id, ErrNoPlayer)
	}
	return p, nil
}

// Players reads every player's frame, in identity order.
func (s *Store) Players(ctx context.Context) ([]Player, error) {
	var out []Player
	err := s.pool.with(ctx, func(conn *sqlite.Conn) error {
		return sqlitex.Execute(conn,
			"SELECT id, origin_q, origin_r, rotation FROM player ORDER BY id;",
			&sqlitex.ExecOptions{
				ResultFunc: func(stmt *sqlite.Stmt) error {
					p, err := playerOf(stmt)
					if err != nil {
						return err
					}
					out = append(out, p)
					return nil
				},
			})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// playerOf reads one row and validates it.
//
// Both validations are refusals rather than repairs. A coordinate outside the
// domain is not relocated by NewCoord and a rotation outside 0..5 is not reduced
// by a FloorMod, for the same reason: a frame is what every coordinate and
// heading a player has ever been given *means*, so quietly fixing one would
// silently relabel all of it. DESIGN.md 28 and 27.6.
func playerOf(stmt *sqlite.Stmt) (Player, error) {
	id := stmt.ColumnText(0)
	c, err := coordOf("player "+id, stmt.ColumnInt64(1), stmt.ColumnInt64(2))
	if err != nil {
		return Player{}, err
	}
	rotation := stmt.ColumnInt64(3)
	if rotation < 0 || rotation > 5 {
		return Player{}, &OpenError{
			Found:  rotation,
			Detail: fmt.Sprintf("player %q, on read", id),
			Err:    ErrBadRotation,
		}
	}
	return Player{ID: id, Origin: c, Rotation: int(rotation)}, nil
}
