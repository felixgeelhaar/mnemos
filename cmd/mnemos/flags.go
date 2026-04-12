package main

import "strings"

// Flags holds parsed CLI flags.
type Flags struct {
	Help    bool
	Verbose bool
	Human   bool
	LLM     bool
	Embed   bool
}

// ParseFlags extracts known CLI flags from args and returns the remaining positional arguments.
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
		case "--human", "-o", "text":
			f.Human = true
		case "--llm":
			f.LLM = true
		case "--embed":
			f.Embed = true
		default:
			filtered = append(filtered, arg)
		}
	}
	return f, filtered
}
