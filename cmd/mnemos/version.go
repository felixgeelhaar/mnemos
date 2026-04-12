package main

// Version information, set via ldflags at build time.
// Example: go build -ldflags "-X main.version=0.1.0 -X main.commit=abc123"
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)
