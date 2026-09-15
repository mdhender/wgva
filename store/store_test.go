// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"

	"github.com/mdhender/wgva"
	"github.com/mdhender/wgva/config"
)

// testSeed is the default world seed.
const testSeed = wgva.Seed(0x0123456789abcdef)

// newWorld creates a world in a temporary directory and returns its path.
func newWorld(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "world.wgva")
	s, err := Create(t.Context(), path, testSeed, wgva.DefaultConfig())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return path
}

// mutate runs SQL directly against a world file, to build the malformed files
// the gates exist to refuse.
func mutate(t *testing.T, path string, queries string) {
	t.Helper()
	conn, err := sqlite.OpenConn(path, sqlite.OpenReadWrite|sqlite.OpenURI)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer conn.Close()
	if err := sqlitex.ExecuteScript(conn, queries, nil); err != nil {
		t.Fatalf("mutating %s: %v", path, err)
	}
}

// TestCreateThenOpen is the exit condition's first half: a database can be
// created, reopened, and reproduced.
func TestCreateThenOpen(t *testing.T) {
	path := newWorld(t)

	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	w := s.World()
	if w.Seed != testSeed {
		t.Errorf("seed = %s, want %s", w.Seed, testSeed)
	}
	if w.AlgorithmVersion != wgva.AlgorithmVersion {
		t.Errorf("algorithm version = %d, want %d", w.AlgorithmVersion, wgva.AlgorithmVersion)
	}
	if w.WorldRadius != wgva.WorldRadius {
		t.Errorf("world radius = %d, want %d", w.WorldRadius, wgva.WorldRadius)
	}
	if want := config.DefaultDigest(); w.Fingerprint != want {
		t.Errorf("fingerprint = %s, want %s", w.Fingerprint, want)
	}
	if w.CreatedBuild != wgva.Version().String() {
		t.Errorf("created build = %q, want %q", w.CreatedBuild, wgva.Version().String())
	}

	// Reproduced: the stored configuration generates the tile the defaults do.
	g, err := w.Generator()
	if err != nil {
		t.Fatalf("Generator: %v", err)
	}
	want := wgva.NewDefault(testSeed).Tile(wgva.NewCoord(17, -43))
	if got := g.Tile(wgva.NewCoord(17, -43)); got != want {
		t.Errorf("the stored world does not reproduce:\n got %+v\nwant %+v", got, want)
	}
}

// TestCreateRefusesAnExistingFile is the no-force rule. Removing a world is
// something a person does deliberately, with rm.
func TestCreateRefusesAnExistingFile(t *testing.T) {
	path := newWorld(t)
	if _, err := Create(t.Context(), path, testSeed, wgva.DefaultConfig()); !errors.Is(err, ErrWorldExists) {
		t.Fatalf("Create over a world: %v, want ErrWorldExists", err)
	}

	// An empty file counts, because an empty file is what a shell redirection
	// leaves behind and is exactly what a create-on-open store would adopt.
	empty := filepath.Join(t.TempDir(), "empty.wgva")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(t.Context(), empty, testSeed, wgva.DefaultConfig()); !errors.Is(err, ErrWorldExists) {
		t.Fatalf("Create over an empty file: %v, want ErrWorldExists", err)
	}
}

// TestOpenNeverCreates is the other half of gate 1: every tool but
// `wgva-world create` refuses an absent or empty file rather than initializing
// it. A typo in a path is not an invitation.
func TestOpenNeverCreates(t *testing.T) {
	dir := t.TempDir()

	absent := filepath.Join(dir, "absent.wgva")
	if _, err := Open(t.Context(), absent); !errors.Is(err, ErrNoWorld) {
		t.Fatalf("Open absent: %v, want ErrNoWorld", err)
	}
	if _, err := os.Stat(absent); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Open created %s", absent)
	}

	empty := filepath.Join(dir, "empty.wgva")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), empty); !errors.Is(err, ErrNoWorld) {
		t.Fatalf("Open empty: %v, want ErrNoWorld", err)
	}
	if info, err := os.Stat(empty); err != nil || info.Size() != 0 {
		t.Fatalf("Open wrote to an empty file: size %v, err %v", info, err)
	}
}

// TestGates is DESIGN.md 30.11: every refusal asserted with errors.Is against
// its sentinel, and every one of them without performing an application write.
//
// A test that compared Error() output would pass while the gate was wrong, which
// is the whole reason each gate has a sentinel of its own.
func TestGates(t *testing.T) {
	// A second fingerprint that is well-formed and wrong, for the gate that
	// compares one.
	other := wgva.DefaultConfig()
	other.SeaLevel += 0.01
	otherDigest, err := config.Of(other)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, path string)
		want  error
	}{
		{
			name:  "gate 1: a WGVB file",
			setup: func(t *testing.T, path string) { mutate(t, path, "PRAGMA application_id = 1464292930;") },
			want:  ErrWrongApplicationID,
		},
		{
			name:  "gate 1: some other application's database",
			setup: func(t *testing.T, path string) { mutate(t, path, "PRAGMA application_id = 0;") },
			want:  ErrWrongApplicationID,
		},
		{
			name: "gate 2: a schema newer than this binary",
			setup: func(t *testing.T, path string) {
				mutate(t, path, "PRAGMA user_version = 99;")
			},
			want: ErrSchemaTooNew,
		},
		{
			name:  "gate 4: no world row",
			setup: func(t *testing.T, path string) { mutate(t, path, "DELETE FROM world;") },
			want:  ErrMalformedMetadata,
		},
		{
			name: "gate 4: a creation time that is not a timestamp",
			setup: func(t *testing.T, path string) {
				mutate(t, path, "UPDATE world SET created_at = 'thursday';")
			},
			want: ErrMalformedMetadata,
		},
		{
			name: "gate 4: a fingerprint that is not a fingerprint",
			setup: func(t *testing.T, path string) {
				mutate(t, path, "UPDATE world SET fingerprint = 'not a digest';")
			},
			want: ErrMalformedMetadata,
		},
		{
			name: "gate 5: a different world radius",
			setup: func(t *testing.T, path string) {
				mutate(t, path, "UPDATE world SET world_radius = 131071;")
			},
			want: ErrWrongWorldRadius,
		},
		{
			name: "gate 6: an algorithm version this binary cannot reproduce",
			setup: func(t *testing.T, path string) {
				mutate(t, path, "UPDATE world SET algorithm_version = 9999;")
			},
			want: ErrUnsupportedGenVersion,
		},
		{
			name: "gate 6: a configuration that does not match its fingerprint",
			setup: func(t *testing.T, path string) {
				mutate(t, path, "UPDATE world SET fingerprint = '"+otherDigest.String()+"';")
			},
			want: ErrFingerprintMismatch,
		},
		{
			name: "gate 4: a configuration missing a key",
			setup: func(t *testing.T, path string) {
				mutate(t, path, "UPDATE world SET configuration = 'sea_level = 0.0';")
			},
			want: ErrInvalidConfig,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := newWorld(t)
			tc.setup(t, path)

			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			s, err := Open(t.Context(), path)
			if s != nil {
				s.Close()
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Open: %v, want %v", err, tc.want)
			}

			// No gate may perform an application write before it passes. The
			// file is compared byte for byte, which also catches a migration
			// that ran when it should not have.
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Errorf("a refused open wrote to the world file")
			}
		})
	}
}

// TestOpenErrorNamesBothNumbers is what a sentinel cannot carry: the value found
// and the value expected, so a refusal is a diagnosis rather than a verdict.
func TestOpenErrorNamesBothNumbers(t *testing.T) {
	path := newWorld(t)
	mutate(t, path, "UPDATE world SET world_radius = 131071;")

	_, err := Open(t.Context(), path)
	var oe *OpenError
	if !errors.As(err, &oe) {
		t.Fatalf("Open: %v, want an *OpenError", err)
	}
	if oe.Found != 131071 || oe.Expected != wgva.WorldRadius {
		t.Errorf("found %d expected %d, want 131071 and %d", oe.Found, oe.Expected, wgva.WorldRadius)
	}
	if !strings.Contains(oe.Error(), path) {
		t.Errorf("the refusal does not name the file: %v", oe)
	}
}

// TestWGVBIsNamed is the difference between a refusal and a diagnosis. A WGVB
// file is one byte away and describes the same kind of world, so "not a WGVA
// database" on its own would send somebody looking for corruption.
func TestWGVBIsNamed(t *testing.T) {
	path := newWorld(t)
	mutate(t, path, "PRAGMA application_id = 1464292930;")

	_, err := Open(t.Context(), path)
	if !errors.Is(err, ErrWrongApplicationID) {
		t.Fatalf("Open: %v, want ErrWrongApplicationID", err)
	}
	if !strings.Contains(err.Error(), "WGVB") {
		t.Errorf("a WGVB file is refused without being named: %v", err)
	}
}

// TestMigrationsApplyInOrder is the other half of DESIGN.md 30.11: a supported
// older schema migrates in order and keeps the same single-world metadata.
//
// With one migration in the ladder there is one older schema — the empty file —
// and the interesting assertion is the one that will still be interesting when
// there are five: that user_version lands on the ladder's length, that the
// application id is set, and that the world row survives the walk.
func TestMigrationsApplyInOrder(t *testing.T) {
	path := newWorld(t)

	// Reopening runs the ladder again. An already-current schema must be a
	// no-op, not a re-application.
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if err := s.pool.with(t.Context(), func(conn *sqlite.Conn) error {
		version, err := pragmaInt(conn, "user_version")
		if err != nil {
			return err
		}
		if version != SchemaVersion() {
			t.Errorf("user_version = %d, want %d", version, SchemaVersion())
		}
		appID, err := pragmaInt(conn, "application_id")
		if err != nil {
			return err
		}
		if appID != int64(ApplicationID) {
			t.Errorf("application_id = %#x, want %#x", appID, ApplicationID)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if s.World().Seed != testSeed {
		t.Errorf("the world row did not survive the ladder")
	}
}

// TestForeignKeysAreOn is DESIGN.md 27.3. There is nothing to enforce today —
// there are no foreign keys, and there must be none to generated data — but the
// pragma is per connection and defaults off, so a constraint added later between
// two authoritative tables would be enforced in production and ignored in tests,
// or the reverse, depending on which place remembered to set it. Tests use the
// same connection preparation as production, which is what this asserts.
func TestForeignKeysAreOn(t *testing.T) {
	path := newWorld(t)
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.pool.with(t.Context(), func(conn *sqlite.Conn) error {
		on, err := pragmaInt(conn, "foreign_keys")
		if err != nil {
			return err
		}
		if on != 1 {
			t.Errorf("PRAGMA foreign_keys = %d, want 1", on)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestNoForeignKeysToGeneratedData is the constraint the pragma is not. There is
// no generated data in the file to point at, and there must be no foreign key
// anywhere: authoritative player state must never have a referential dependency
// on regenerable data, because dropping the cache would then require dropping
// the player's settlements.
func TestNoForeignKeysToGeneratedData(t *testing.T) {
	path := newWorld(t)
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.pool.with(t.Context(), func(conn *sqlite.Conn) error {
		var found []string
		if err := sqlitex.Execute(conn,
			"SELECT name FROM sqlite_schema WHERE type = 'table' AND sql LIKE '%REFERENCES%';",
			&sqlitex.ExecOptions{ResultFunc: func(stmt *sqlite.Stmt) error {
				found = append(found, stmt.ColumnText(0))
				return nil
			}}); err != nil {
			return err
		}
		if len(found) != 0 {
			t.Errorf("tables carry foreign keys: %v", found)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestCoordinateKeyedTablesAreWithoutRowid is DESIGN.md 27.2. Such a table *is*
// a B-tree keyed by the composite primary key, so a viewport load is one ordered
// range scan rather than a rowid lookup per row through a secondary index. That
// is the coordinate locality that would otherwise be the reason to reach for a
// dedicated key-value store, so it is asserted rather than remembered.
func TestCoordinateKeyedTablesAreWithoutRowid(t *testing.T) {
	path := newWorld(t)
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.pool.with(t.Context(), func(conn *sqlite.Conn) error {
		schema := map[string]string{}
		if err := sqlitex.Execute(conn,
			"SELECT name, sql FROM sqlite_schema WHERE type = 'table';",
			&sqlitex.ExecOptions{ResultFunc: func(stmt *sqlite.Stmt) error {
				schema[stmt.ColumnText(0)] = stmt.ColumnText(1)
				return nil
			}}); err != nil {
			return err
		}
		for name, sql := range schema {
			keyed := strings.Contains(sql, "PRIMARY KEY (q, r)")
			rowless := strings.Contains(sql, "WITHOUT ROWID")
			switch {
			case keyed && !rowless:
				t.Errorf("%s is keyed by (q, r) and is not WITHOUT ROWID", name)
			case rowless && !keyed:
				t.Errorf("%s is WITHOUT ROWID and is not keyed by (q, r)", name)
			}
		}
		if _, ok := schema["overlay_settlement"]; !ok {
			t.Errorf("the schema has no overlay tables; this test is asserting nothing")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
