// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Command wgva-tune is the terrain tuning tool.
//
// It runs on a developer's machine and produces a settings file. Nothing is
// deployed, no player ever touches it, and it does not create or open a
// database. That sentence is the tool's boundary, and it is worth repeating
// whenever the tool comes up.
//
// It cannot touch a saved world, and that is a fact of the build rather than a
// promise: this package has no import path to store, and deps_test.go fails if
// one ever appears. Its mirror image is cmd/wgva-world, which has no import path
// to render. The tool that decides how worlds look cannot touch a world, and the
// tool that makes a world cannot draw one.
//
// The deliverable is not the tool. It is that a world's appearance becomes a
// versioned file with a fingerprint on it, so any picture can be traced to the
// configuration that produced it and reproduced elsewhere. The web interface is
// only how somebody drives it.
//
//	wgva-tune --seed 0x5747564100000001
//
// See DESIGN.md 29.1.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/mdhender/wgva"
	"github.com/mdhender/wgva/config"
	"github.com/mdhender/wgva/view"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("wgva-tune: ")

	var (
		// The default must not be 0.0.0.0. There is no authentication here and
		// the cost of an endpoint is chosen by the caller, so the loopback
		// default is the only thing standing between this and a machine on a
		// coffee-shop network. The flag exists; the default is the decision.
		host = flag.String("host", "127.0.0.1", "address to bind (the default is loopback, and should stay that way)")
		port = flag.Int("port", 8180, "port to bind")
		seed = flag.String("seed", "0x5747564100000001", "world seed, in decimal or with an 0x prefix")
		// The budget is counted in generator evaluations rather than tiles,
		// because a tile count cannot tell a cheap window from one seven times
		// longer. Raising it is a thing this tool is for: measuring how long a
		// large window takes is part of tuning.
		budget = flag.Int("budget", 4_000_000, "evaluations one window may cost")
		file   = flag.String("config", "", "configuration file to start from (defaults to this binary's defaults)")
	)
	flag.Parse()

	defaultSeed, err := view.ParseSeed(*seed)
	if err != nil {
		log.Fatalf("--seed: %v", err)
	}
	if *budget < 1 {
		log.Fatalf("--budget: %d is not a number of evaluations", *budget)
	}

	cfg := wgva.DefaultConfig()
	if *file != "" {
		data, err := os.ReadFile(*file)
		if err != nil {
			log.Fatalf("--config: %v", err)
		}
		if cfg, err = config.Unmarshal(data); err != nil {
			log.Fatalf("--config: %s: %v", *file, err)
		}
	}

	srv := newServer(defaultSeed, cfg, *budget)

	addr := net.JoinHostPort(*host, fmt.Sprint(*port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	d, err := config.Of(cfg)
	if err != nil {
		log.Fatalf("configuration: %v", err)
	}
	log.Printf("the terrain tuning tool, on a developer's machine and nowhere else")
	log.Printf("build %s, algorithm version %d, world radius %d", wgva.Version(), wgva.AlgorithmVersion, wgva.WorldRadius)
	log.Printf("configuration %s (%s)", d.Short(), configurationLabel(cfg))
	log.Printf("seed %s, budget %d evaluations, %d concurrent renders",
		view.FormatSeed(defaultSeed), *budget, runtime.GOMAXPROCS(0))
	log.Printf("http://%s/", listener.Addr())

	server := &http.Server{
		Handler:           srv.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve: %v", err)
	}
}
