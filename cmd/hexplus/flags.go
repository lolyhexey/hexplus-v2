package main

import (
	"flag"
	"strings"
)

// parseFlagsAnywhere parses fs from args and returns the positional
// arguments, accepting flags on either side of them.
//
// Go's flag package stops at the first non-flag argument, so with a plain
// fs.Parse the documented form `hexplus user add bob --password x` never
// saw --password, and `hexplus user export bob --remote 1.2.3.4` silently
// produced a profile pointing at 127.0.0.1. A standalone "--" still ends
// flag parsing: everything after it is positional. A "--" that is the value
// of a flag (`--password --`) is just that value.
func parseFlagsAnywhere(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		consumed := len(args) - len(rest)
		if consumed > 0 && args[consumed-1] == "--" && !consumedAsValue(fs, args[:consumed-1]) {
			return append(positional, rest...), nil
		}
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// consumedAsValue reports whether the "--" right after before was taken as
// the value of the last flag in before (a non-boolean flag written without
// "=").
func consumedAsValue(fs *flag.FlagSet, before []string) bool {
	if len(before) == 0 {
		return false
	}
	prev := before[len(before)-1]
	if !strings.HasPrefix(prev, "-") || prev == "--" || strings.Contains(prev, "=") {
		return false
	}
	f := fs.Lookup(strings.TrimLeft(prev, "-"))
	if f == nil {
		return false
	}
	if b, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
		return false
	}
	return true
}
