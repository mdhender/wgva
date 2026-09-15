// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Command wgva-serve is the map viewer. It looks at a world that is already
// saved, without starting the game engine.
//
// It runs on one machine for one person for as long as a browser tab is open,
// binds to loopback, and has no authentication. It is not the tuning tool and
// does not replace it, and the difference is one promise:
//
//   - **The viewer is stateless.** Every state it can be in is a URL, so a link
//     means the same thing to everybody who opens it and the same thing tomorrow.
//   - **The tuning tool is stateful**, for the reason DESIGN.md 29.1 gives.
//
// That is why this is deliberately not a single-page application. No client-side
// panning, no canvas, no script needed to move the view; page refresh is fine.
// Every state is a URL, which also makes "look at this" a link somebody can
// paste into an issue, and makes the round trip testable — the links the page
// emits must parse back to the state that produced them.
//
//	wgva-serve --db world.wgva
//	wgva-serve --seed 0x0123456789abcdef      (diagnostic; no saved world)
//
// **It opens and never creates.** `wgva-world create` is the only thing that
// does, because creating a world is a decision rather than a side effect of a
// typo in a path.
//
// See DESIGN.md 29.3.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"runtime"
	"time"

	"github.com/mdhender/wgva"
	"github.com/mdhender/wgva/render"
	"github.com/mdhender/wgva/store"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("wgva-serve: ")

	var (
		// The default must not be 0.0.0.0. There is no authentication here and
		// the cost of an endpoint is chosen by the caller, so the loopback
		// default is the only thing standing between this and a machine on a
		// coffee-shop network. The flag exists; the default is the decision.
		host = flag.String("host", "127.0.0.1", "address to bind (the default is loopback, and should stay that way)")
		port = flag.Int("port", 8181, "port to bind")
		db   = flag.String("db", "", "world file to serve")
		seed = flag.String("seed", wgva.Seed(0).String(), "seed to serve diagnostically when there is no --db")
	)
	flag.Parse()

	srv, err := newServer(*db, *seed)
	if err != nil {
		log.Fatal(err)
	}
	defer srv.close()

	// The listener is opened after the world is, which is the whole point of
	// doing it in this order: a database that fails an opening gate is a server
	// that does not start, rather than a server that answers every request with
	// a 500. DESIGN.md 29.3.
	addr := net.JoinHostPort(*host, fmt.Sprint(*port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	for _, line := range srv.banner() {
		log.Print(line)
	}
	log.Printf("%d concurrent renders, every render's cost logged below", runtime.GOMAXPROCS(0))
	log.Printf("http://%s/seed/%s", listener.Addr(), srv.seed)

	server := &http.Server{
		Handler:           srv.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve: %v", err)
	}
}

// newServer opens the world, or builds the diagnostic generator, before anything
// is bound.
func newServer(db, seedText string) (*server, error) {
	if db == "" {
		seed, err := wgva.ParseSeed(seedText)
		if err != nil {
			return nil, fmt.Errorf("--seed: %w", err)
		}
		return newDiagnosticServer(seed, wgva.DefaultConfig())
	}

	s, err := store.Open(context.Background(), db)
	if errors.Is(err, store.ErrNoWorld) {
		return nil, fmt.Errorf("%w\nonly `wgva-world create` makes a world file; this server opens one", err)
	}
	if err != nil {
		return nil, err
	}
	srv, err := newWorldServer(s)
	if err != nil {
		s.Close()
		return nil, err
	}
	return srv, nil
}

// banner is what the console says about what is being served.
func (s *server) banner() []string {
	lines := []string{
		"the map viewer, on one machine for one person for as long as a browser tab is open",
		fmt.Sprintf("build %s, algorithm version %d, render version %d, world radius %d",
			wgva.Version(), wgva.AlgorithmVersion, render.RenderVersion, wgva.WorldRadius),
		fmt.Sprintf("configuration %s (%s)", s.digest.Short(), s.configurationLabel()),
	}
	if s.world == nil {
		return append(lines,
			fmt.Sprintf("seed %s — diagnostic: no world is saved, and every seed is servable", s.seed),
			"`wgva-world create` is what makes a world; this server only looks at one")
	}
	w := s.world.World()
	return append(lines,
		fmt.Sprintf("world %s, seed %s, created by %s", s.world.Path(), w.Seed, w.CreatedBuild),
		"this server opens and never writes; overlays are read fresh on every request")
}
