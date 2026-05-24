package tools

import "flag"

// updateGolden gates snapshot regeneration. The flag is registered at
// package init so `go test ./client/tools -update` rewrites the
// on-disk JSON without needing a separate generator binary. Outside
// test runs the flag stays false and the test only verifies parity.
var updateGolden = false

func init() {
	flag.BoolVar(&updateGolden, "update", false, "regenerate snapshot JSON files in client/tools/snapshots")
}
