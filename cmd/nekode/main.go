package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ca-x/nekode/internal/cache"
	"github.com/ca-x/nekode/internal/config"
	"github.com/ca-x/nekode/internal/server"
	"github.com/ca-x/nekode/internal/storage"
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
	flags.StringVar(&cfg.GRPCAddr, "grpc-addr", cfg.GRPCAddr, "gRPC daemon control listen address")
	flags.StringVar(&cfg.DaemonTransport, "daemon-transport", cfg.DaemonTransport, "daemon transport: grpc (QUIC/WebTransport reserved)")
	flags.StringVar(&cfg.BaseURL, "base-url", cfg.BaseURL, "public base URL")
	flags.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "persistent data directory")
	flags.StringVar(&cfg.DBType, "db-type", cfg.DBType, "database type: sqlite, postgres, or mysql")
	flags.StringVar(&cfg.DBDSN, "db-dsn", cfg.DBDSN, "database DSN; defaults to ~/.nekode/nekode.db for sqlite")
	flags.StringVar(&cfg.CacheDriver, "cache-driver", cfg.CacheDriver, "cache driver: badger, redis, or none")
	flags.StringVar(&cfg.CacheDir, "cache-dir", cfg.CacheDir, "cache directory for badger")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := storage.OpenWithOptions(ctx, storage.OpenOptions{
		Type: cfg.DatabaseType(),
		DSN:  cfg.DBDSN,
	})
	if err != nil {
		return err
	}
	defer store.Close()

	cacheStore, err := cache.Open(ctx, cache.Options{
		Driver:     cfg.CacheDriver,
		BadgerDir:  cfg.CacheDir,
		RedisAddr:  cfg.CacheRedisAddr,
		RedisUser:  cfg.CacheRedisUser,
		RedisPass:  cfg.CacheRedisPass,
		RedisDB:    cfg.CacheRedisDB,
		DefaultTTL: cfg.CacheTTL,
		KeyVersion: cfg.CacheKeyVersion,
	})
	if err != nil {
		return err
	}
	defer cacheStore.Close()

	s := server.NewWithCache(cfg, slog.Default(), store, cacheStore)
	err = s.ListenAndServe(ctx)
	if err == context.Canceled {
		return nil
	}
	return err
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  nekode serve [--addr :18790] [--grpc-addr 127.0.0.1:18789] [--daemon-transport grpc] [--base-url http://localhost:18790] [--data-dir ~/.nekode] [--db-type sqlite] [--db-dsn ~/.nekode/nekode.db] [--cache-driver badger]")
	fmt.Fprintln(os.Stderr, "  nekode version")
}
