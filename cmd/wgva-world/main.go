// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Command wgva-world is the world builder. It creates a world file from a seed,
// and it cannot draw one.
//
// That is a fact of the build rather than a promise: this package has no import
// path to render, and deps_test.go fails if one ever appears. Its mirror image
// is cmd/wgva-tune, which has no import path to store. The tool that makes a
// world cannot draw one, and the tool that decides how worlds look cannot touch
// a world.
//
//	wgva-world create --seed <hex> --expect <build>/<fingerprint> [--config file] world.wgva
//	wgva-world identity
//	wgva-world inspect world.wgva
//
// Creating a world is not a rendering act, which is why this is not a mode of
// wgva-map. An administrator's gesture is *make me this world*, not *draw me a
// picture and incidentally make a world first*; and a creation path reached by
// passing a database flag with a path that happens not to exist yet is exactly
// the typo-becomes-a-side-effect the rest of DESIGN.md 27 refuses.
//
// See DESIGN.md 29.5.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("wgva-world: ")

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "create":
		err = create(os.Args[2:])
	case "identity":
		err = identity(os.Args[2:])
	case "inspect":
		err = inspect(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		usage()
		log.Fatalf("unknown subcommand %q", os.Args[1])
	}
	if err != nil {
		log.Fatal(err)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `wgva-world creates a world file from a seed. It cannot draw one.

usage:
  wgva-world create --seed <hex> --expect <build>/<fingerprint> [--config file] <path>
  wgva-world identity
  wgva-world inspect <path>

create    writes a new world file. It refuses a file that already exists, and
          --expect is required: it carries the build identity and configuration
          fingerprint the caller believes they are creating from, and a mismatch
          in either half is a refusal. There is no --force.
identity  prints this binary's build identity and the fingerprint of its
          built-in defaults, in exactly the form --expect takes.
inspect   runs the seven opening gates against an existing file and prints its
          stored metadata. It opens; it does not write.
`)
}

// newFlagSet returns a flag set that prints the command's own usage.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("wgva-world "+name, flag.ExitOnError)
	fs.Usage = func() {
		usage()
		fmt.Fprintf(os.Stderr, "\nflags for %s:\n", name)
		fs.PrintDefaults()
	}
	return fs
}
