// readmit-hub is the separate, customer-controlled service and maintenance CLI.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bharm16/readmit/hub"
)

func run() error {
	configPath := flag.String("config", "", "absolute configuration path")
	accessPath := flag.String("access-policy", "", "absolute team admission policy path")
	directory := flag.String("directory", "", "new backup directory or existing restore directory")
	flag.Parse()
	if *configPath == "" || flag.NArg() != 1 {
		return fmt.Errorf("usage: readmit-hub -config PATH [-directory PATH] migrate|serve|check|backup|restore")
	}
	f, err := os.Open(*configPath)
	if err != nil {
		return fmt.Errorf("configuration unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(f, 16385))
	f.Close()
	if err != nil {
		return fmt.Errorf("configuration unavailable")
	}
	c, err := hub.ReadConfig(data)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	openCtx, openCancel := context.WithTimeout(ctx, 10*time.Second)
	defer openCancel()
	store, err := hub.Open(openCtx, c)
	if err != nil {
		return err
	}
	defer store.Close()
	if flag.Arg(0) == "serve" {
		if *accessPath != "" {
			access, err := hub.OpenAccess(*accessPath)
			if err != nil {
				return err
			}
			return store.ServeTeam(ctx, access)
		}
		return store.Serve(ctx)
	}
	ctx, cancel = context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	switch flag.Arg(0) {
	case "migrate":
		return store.Migrate(ctx)
	case "check":
		return store.Ready(ctx)
	case "backup":
		return store.Backup(ctx, *directory)
	case "restore":
		return store.Restore(ctx, *directory)
	default:
		return fmt.Errorf("unknown operation")
	}
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "hub operation failed (check configuration, ownership, schema, and storage)")
		os.Exit(1)
	}
}
