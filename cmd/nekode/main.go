package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ca-x/nekode/internal/config"
	"github.com/ca-x/nekode/internal/server"
	"github.com/ca-x/nekode/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		args = []string{"serve"}
	}

	switch args[0] {
	case "serve":
		return serve(args[1:])
	case "version":
		info := version.Current()
		fmt.Printf("nekode %s (%s, %s)\n", info.Version, info.Commit, info.BuildTime)
		return nil
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		printUsage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func serve(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.StringVar(&cfg.Addr, "addr", cfg.Addr, "HTTP listen address")
	flags.StringVar(&cfg.BaseURL, "base-url", cfg.BaseURL, "public base URL")
	flags.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "persistent data directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	s := server.New(cfg, slog.Default())
	err = s.ListenAndServe(ctx)
	if err == context.Canceled {
		return nil
	}
	return err
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  nekode serve [--addr :18790] [--base-url http://localhost:18790] [--data-dir ~/.nekode]")
	fmt.Fprintln(os.Stderr, "  nekode version")
}
