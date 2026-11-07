package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/kevindurb/tsgit/internal/config"
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "--version", "-version", "-v":
		fmt.Fprintln(stdout, version)
		return 0
	case "serve":
		return runServe(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "tsgit: unknown command %q\n", args[0])
		usage(stderr)
		return 2
	}
}

func runServe(args []string, stdout, stderr io.Writer) int {
	cfg, err := config.Parse(args, os.Getenv)
	if errors.Is(err, flag.ErrHelp) {
		usage(stdout)
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "tsgit: %v\n", err)
		return 2
	}
	if cfg.Version {
		fmt.Fprintln(stdout, version)
		return 0
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := serve(ctx, cfg, stderr); err != nil {
		fmt.Fprintf(stderr, "tsgit: %v\n", err)
		return 1
	}
	return 0
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: tsgit <command> [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "commands:")
	fmt.Fprintln(w, "  serve      serve git smart HTTP over the tailnet (or --dev-listen)")
	fmt.Fprintln(w, "  version    print the version")
}
