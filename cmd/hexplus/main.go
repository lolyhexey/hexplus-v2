// hexplus - single-binary HEXPLUS v2 entry point.
//
// Phase 0 scope: bootstrap that prints version, runs an extract dry-run,
// and exits. No menu, no service management yet - just enough to confirm
// the embed/extract pipeline compiles, cross-compiles, and runs on Linux.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/lolyhexey/hexplus/internal/assets"
	"github.com/lolyhexey/hexplus/internal/extract"
	"github.com/lolyhexey/hexplus/internal/version"
)

const defaultLibDir = "/usr/local/lib/hexplus"

func main() {
	var (
		showVersion bool
		dryExtract  bool
		libDir      string
	)
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.BoolVar(&dryExtract, "extract", false, "extract embedded assets to --lib-dir then exit (Phase 0 smoke test)")
	flag.StringVar(&libDir, "lib-dir", defaultLibDir, "where to extract embedded assets")
	flag.Parse()

	if showVersion {
		fmt.Println(version.Full())
		fmt.Printf("  runtime: %s/%s (%s)\n", runtime.GOOS, runtime.GOARCH, runtime.Version())
		return
	}

	if dryExtract {
		if err := runExtract(libDir); err != nil {
			fmt.Fprintln(os.Stderr, "extract:", err)
			os.Exit(1)
		}
		return
	}

	// Default action for Phase 0: print a banner so we can confirm the binary
	// actually runs after wget+chmod+./hexplus on a bare Linux box.
	fmt.Println(version.Full())
	fmt.Printf("running on %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Println()
	fmt.Println("Phase 0 build. The real menu lands in Phase 1.")
	fmt.Println("Try: hexplus -extract -lib-dir /tmp/hexplus")
}

func runExtract(dir string) error {
	res, err := extract.All(assets.Binaries(), dir)
	if err != nil {
		return err
	}
	fmt.Printf("extracted into %s:\n", dir)
	for _, p := range res.Written {
		fmt.Printf("  + %s\n", p)
	}
	for _, p := range res.Skipped {
		fmt.Printf("  = %s (already up-to-date)\n", p)
	}
	if len(res.Written)+len(res.Skipped) == 0 {
		fmt.Println("  (nothing embedded yet - waiting on build/build-statics.sh)")
	}
	return nil
}
