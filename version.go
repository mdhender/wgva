// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"github.com/maloquacious/semver"
)

var (
	version = semver.Version{
		Major:      0,
		Minor:      25,
		Patch:      1,
		PreRelease: "alpha",
		Build:      semver.Commit(),
	}
)

func Version() semver.Version {
	return version
}
