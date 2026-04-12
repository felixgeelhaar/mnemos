package main

import "strings"

type Flags struct {
	Help    bool
	Verbose bool
}

func ParseFlags(args []string) (Flags, []string) {
	var f Flags
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch strings.ToLower(arg) {
		case "-h", "--help":
			f.Help = true
		case "-v", "--verbose":
			f.Verbose = true
		default:
			filtered = append(filtered, arg)
		}
	}
	return f, filtered
}
