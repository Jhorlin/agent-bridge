package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Jhorlin/agent-bridge/internal/release"
)

func main() {
	version := flag.String("version", "", "release version, e.g. v0.1.0-alpha.1")
	output := flag.String("out", "dist", "new output directory (must not exist)")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "Unexpected positional arguments")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := release.Package(ctx, *version, *output, release.Build); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, "Created four platform archives and SHA256SUMS. Nothing was installed or published.")
}
