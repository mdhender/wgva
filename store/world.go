// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"
	"time"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"

	"github.com/mdhender/wgva"
	"github.com/mdhender/wgva/config"
)

// World is the singleton metadata: everything needed to reproduce this world's
// generated baseline, and nothing else.
//
// Terrain is not in here and is not in the file. A world is a seed plus a
// version plus a radius plus a configuration, and every tile of it is a pure
// function of those and a coordinate — so what a world file holds is the
// identity, and the picture is regenerated from it every time. DESIGN.md 27.
type World struct {
	Seed             wgva.Seed
	AlgorithmVersion uint32
	WorldRadius      int64
	Config           wgva.Config
	Fingerprint      config.Digest

	// CreatedBuild is the version string of the binary that created the world,
	// including the commit hash and dirty marker semver.Commit supplies.
	//
	// It is **provenance, not identity**: it is deliberately excluded from the
	// fingerprint and no gate compares it, because a world reopened by a later
	// build must still open. It is recorded because "which build made this?" is
	// the first question anybody asks about a world that looks wrong, and
	// because AlgorithmVersion — the field that is supposed to answer it — is a
	// number somebody has to remember to change. DESIGN.md 27.
	CreatedBuild string

	CreatedAt time.Time
}

// Generator builds the generator this world's tiles come from.
func (w World) Generator() (*wgva.Generator, error) { return wgva.New(w.Seed, w.Config) }

// Store is an open world file.
//
// A *sqlite.Conn must not be used concurrently, so every read here takes a
// connection from the pool and returns it with a defer placed immediately after
// the successful Take — the only form that survives a later edit. DESIGN.md 27.7.
type Store struct {
	path  string
	pool  *pool
	world World

	// closed makes Close idempotent. A caller that closes on an error path and
	// again through a defer is writing the shape DESIGN.md 27.7 asks for, and it
	// should not be the shape that panics.
	closed sync.Once
}

// Path is the file this store was opened from.
func (s *Store) Path() string { return s.path }

// World returns the singleton metadata, read once at open.
func (s *Store) World() World { return s.world }

// Close releases the pool. It is safe to call more than once.
func (s *Store) Close() error {
	var err error
	s.closed.Do(func() { err = s.pool.Close() })
	return err
}

// Create writes a new world file and returns it open.
//
// It refuses a file that already exists — there is no --force, because removing
// a world is something a person does deliberately, with rm — and it is the only
// function in this module that brings a world file into existence. Everything
// else refuses an absent or empty file rather than treating a typo in a path as
// an invitation.
//
// The guard that decides *which* world is being created is not here. That is
// --expect, and it belongs to cmd/wgva-world: this function is handed a
// configuration and writes what it is handed. See DESIGN.md 29.5.
func Create(ctx context.Context, path string, seed wgva.Seed, cfg wgva.Config) (*Store, error) {
	if _, err := os.Stat(path); err == nil {
		return nil, &OpenError{Path: path, Err: ErrWorldExists}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, &OpenError{Path: path, Detail: err.Error(), Err: ErrInvalidConfig}
	}
	digest, err := config.Of(cfg)
	if err != nil {
		return nil, &OpenError{Path: path, Detail: err.Error(), Err: ErrInvalidConfig}
	}
	text, err := config.Marshal(cfg)
	if err != nil {
		return nil, &OpenError{Path: path, Detail: err.Error(), Err: ErrInvalidConfig}
	}

	if err := migrate(ctx, path, 0, true); err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	p, err := openPool(path)
	if err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	world := World{
		Seed:             seed,
		AlgorithmVersion: wgva.AlgorithmVersion,
		WorldRadius:      wgva.WorldRadius,
		Config:           cfg,
		Fingerprint:      digest,
		CreatedBuild:     wgva.Version().String(),
		CreatedAt:        time.Now().UTC().Truncate(time.Second),
	}

	err = p.with(ctx, func(conn *sqlite.Conn) error {
		return sqlitex.Execute(conn, `
INSERT INTO world (id, seed, algorithm_version, world_radius, configuration, fingerprint, created_build, created_at)
VALUES (1, ?, ?, ?, ?, ?, ?, ?);`, &sqlitex.ExecOptions{
			Args: []any{
				// A seed is a uint64 and a SQLite INTEGER is a signed
				// sixty-four-bit value, so the bits are stored and reinterpreted
				// rather than the number. Every seed round-trips exactly; only
				// `SELECT seed` in the sqlite3 CLI reads a large one as
				// negative.
				int64(world.Seed),
				int64(world.AlgorithmVersion),
				world.WorldRadius,
				string(text),
				world.Fingerprint.String(),
				world.CreatedBuild,
				world.CreatedAt.Format(time.RFC3339),
			},
		})
	})
	if err != nil {
		_ = p.Close()
		// A half-written world is worse than none: it would open, and every gate
		// would pass on whatever did land.
		_ = os.Remove(path)
		return nil, fmt.Errorf("%s: writing world metadata: %w", path, err)
	}

	return &Store{path: path, pool: p, world: world}, nil
}

// Open runs the seven gates of DESIGN.md 27.5, in order, and returns the world
// if every one of them passes.
//
//  1. PRAGMA application_id is WGVA. An absent or empty file is refused.
//  2. PRAGMA user_version is not newer than this binary.
//  3. Supported ordered migrations are applied.
//  4. Singleton metadata and the complete configuration read and validate.
//  5. The world radius is one this binary was built for.
//  6. The algorithm version is one this binary can reproduce, and the stored
//     configuration hashes to the stored fingerprint.
//  7. Normal reads and writes are permitted.
//
// **No gate performs an application write before it passes.** Gates 1 and 2 run
// on a read-only connection, which is what makes that a property of the code
// rather than a thing to be careful about; gate 3 is the first write and is
// itself a gate.
//
// Open never creates. `wgva-world create` is the only thing that does, because
// creating a world is a decision rather than a side effect of a typo in a path.
func Open(ctx context.Context, path string) (*Store, error) {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, &OpenError{Path: path, Err: ErrNoWorld}
	case err != nil:
		return nil, fmt.Errorf("%s: %w", path, err)
	case info.Size() == 0:
		// An empty file is what a shell redirection leaves behind, and it is
		// exactly the thing a create-on-open store would adopt as a new world.
		return nil, &OpenError{Path: path, Err: ErrNoWorld}
	}

	schemaVersion, err := gateApplicationAndSchema(path)
	if err != nil {
		return nil, err
	}

	// Gate 3.
	if err := migrate(ctx, path, schemaVersion, false); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	p, err := openPool(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	world, err := readWorld(ctx, p, path)
	if err != nil {
		_ = p.Close()
		return nil, err
	}

	return &Store{path: path, pool: p, world: world}, nil
}

// gateApplicationAndSchema is gates 1 and 2.
//
// The connection is opened read-only so that neither gate *can* write, whatever
// a later edit does to the body. A read-only connection also cannot be talked
// into running the migration ladder early, which is the ordering error this gate
// pair exists to make impossible.
func gateApplicationAndSchema(path string) (schemaVersion int64, err error) {
	conn, err := sqlite.OpenConn(path, sqlite.OpenReadOnly|sqlite.OpenURI)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}
	defer conn.Close()

	// Gate 1.
	appID, err := pragmaInt(conn, "application_id")
	if err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}
	if appID != int64(ApplicationID) {
		return 0, &OpenError{
			Path:     path,
			Found:    appID,
			Expected: int64(ApplicationID),
			Detail:   describeApplicationID(appID),
			Err:      ErrWrongApplicationID,
		}
	}

	// Gate 2.
	userVersion, err := pragmaInt(conn, "user_version")
	if err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}
	if userVersion > SchemaVersion() {
		return 0, &OpenError{
			Path:     path,
			Found:    userVersion,
			Expected: SchemaVersion(),
			Err:      ErrSchemaTooNew,
		}
	}
	return userVersion, nil
}

// describeApplicationID says what was found, when what was found is something
// this project recognizes.
//
// A WGVB file is one byte away from a WGVA file and describes the same kind of
// world, so "not a WGVA database" on its own would send somebody looking for
// corruption. Naming it is the difference between a refusal and a diagnosis.
func describeApplicationID(id int64) string {
	switch id {
	case 0:
		return "the file carries no application id; it is not a database this project wrote"
	case 0x57475642:
		return "this is a WGVB world; WGVA is a new world format and does not read one"
	default:
		return fmt.Sprintf("application id 0x%08x", uint32(id))
	}
}

// readWorld is gates 4, 5, and 6.
func readWorld(ctx context.Context, p *pool, path string) (World, error) {
	var (
		w     World
		seed  int64
		algo  int64
		text  string
		print string
		when  string
		rows  int
	)

	err := p.with(ctx, func(conn *sqlite.Conn) error {
		return sqlitex.Execute(conn, `
SELECT seed, algorithm_version, world_radius, configuration, fingerprint, created_build, created_at
FROM world WHERE id = 1;`, &sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				rows++
				seed = stmt.ColumnInt64(0)
				algo = stmt.ColumnInt64(1)
				w.WorldRadius = stmt.ColumnInt64(2)
				text = stmt.ColumnText(3)
				print = stmt.ColumnText(4)
				w.CreatedBuild = stmt.ColumnText(5)
				when = stmt.ColumnText(6)
				return nil
			},
		})
	})
	if err != nil {
		return World{}, &OpenError{Path: path, Detail: err.Error(), Err: ErrMalformedMetadata}
	}

	// Gate 4.
	if rows != 1 {
		return World{}, &OpenError{
			Path:   path,
			Detail: fmt.Sprintf("expected exactly one world row, found %d", rows),
			Err:    ErrMalformedMetadata,
		}
	}
	if algo < 0 || algo > int64(^uint32(0)) {
		return World{}, &OpenError{
			Path:   path,
			Found:  algo,
			Detail: "algorithm version is not a version number",
			Err:    ErrMalformedMetadata,
		}
	}
	w.Seed = wgva.Seed(seed)
	w.AlgorithmVersion = uint32(algo)
	if w.CreatedAt, err = time.Parse(time.RFC3339, when); err != nil {
		return World{}, &OpenError{
			Path:   path,
			Detail: fmt.Sprintf("creation time %q is not a timestamp", when),
			Err:    ErrMalformedMetadata,
		}
	}

	// Gate 5, before the configuration is decoded. The radius *is* the world's
	// topology, and a configuration that decoded under the wrong one would be a
	// configuration for a different world.
	if w.WorldRadius != wgva.WorldRadius {
		return World{}, &OpenError{
			Path:     path,
			Found:    w.WorldRadius,
			Expected: wgva.WorldRadius,
			Err:      ErrWrongWorldRadius,
		}
	}

	// Gate 6, first half. This is checked before the configuration is decoded
	// too, because a configuration written by a newer algorithm version is
	// *expected* to fail to decode — every version that gains a Config field
	// makes the older binary unable to read the newer file (DESIGN.md 21.1) —
	// and reporting that as a malformed file would name the symptom instead of
	// the cause.
	if w.AlgorithmVersion != wgva.AlgorithmVersion {
		return World{}, &OpenError{
			Path:     path,
			Found:    int64(w.AlgorithmVersion),
			Expected: int64(wgva.AlgorithmVersion),
			Err:      ErrUnsupportedGenVersion,
		}
	}

	// Gate 4's other half: the complete configuration, decoded under the rules
	// that refuse a missing key by name rather than defaulting it to zero.
	cfg, err := config.Unmarshal([]byte(text))
	if err != nil {
		return World{}, &OpenError{Path: path, Detail: err.Error(), Err: ErrInvalidConfig}
	}
	w.Config = cfg

	// Gate 6, second half.
	digest, err := config.Fingerprint(w.AlgorithmVersion, w.WorldRadius, cfg)
	if err != nil {
		return World{}, &OpenError{Path: path, Detail: err.Error(), Err: ErrInvalidConfig}
	}
	stored, err := config.ParseDigest(print)
	if err != nil {
		return World{}, &OpenError{Path: path, Detail: err.Error(), Err: ErrMalformedMetadata}
	}
	if stored != digest {
		return World{}, &OpenError{
			Path:   path,
			Detail: fmt.Sprintf("stored %s, computed %s", stored, digest),
			Err:    ErrFingerprintMismatch,
		}
	}
	w.Fingerprint = digest

	return w, nil
}
