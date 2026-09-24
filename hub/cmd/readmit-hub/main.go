// readmit-hub is the separate, customer-controlled service and maintenance CLI.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bharm16/readmit/hub"
	"github.com/bharm16/readmit/internal/artifactdir"
)

func run() error {
	configPath := flag.String("config", "", "absolute configuration path")
	operationPath := flag.String("operation-policy", "", "absolute hub operation policy path")
	accessPath := flag.String("access-policy", "", "absolute team admission policy path")
	runnerPath := flag.String("runner-policy", "", "absolute runner admission policy path")
	schedulePath := flag.String("schedule-policy", "", "absolute initialized recurring schedule policy path")
	directory := flag.String("directory", "", "new backup directory or existing restore directory")
	flag.Parse()
	if *configPath == "" || flag.NArg() != 1 {
		return fmt.Errorf("usage: readmit-hub -config PATH [-directory PATH] migrate|serve|check|backup|verify-backup|restore")
	}
	unavailable := errors.New("configuration unavailable")
	data, err := artifactdir.Document{
		MaxBytes: 16384,
		Links:    artifactdir.FollowLinks,
		Refusals: artifactdir.DocumentRefusals{Irregular: unavailable, Read: unavailable, Size: errors.New("configuration too large")},
	}.Read(*configPath)
	if err != nil {
		return err
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
	if err = store.SetOperationPolicy(*operationPath); err != nil {
		fmt.Fprintln(os.Stderr, "operation policy unavailable; new paid work will be refused")
	}
	if flag.Arg(0) == "serve" {
		if *accessPath != "" {
			access, err := hub.OpenAccess(*accessPath)
			if err != nil {
				return err
			}
			if *schedulePath != "" {
				return store.ServeSchedules(ctx, access, *runnerPath, *schedulePath)
			}
			return store.ServeRunners(ctx, access, *runnerPath)
		}
		if *runnerPath != "" || *schedulePath != "" {
			return fmt.Errorf("runner policy requires team access")
		}
		return store.Serve(ctx)
	}
	ctx, cancel = context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	switch flag.Arg(0) {
	case "schedule-pin":
		identity, e := hub.ScheduleInputIdentity(*directory)
		if e != nil {
			return e
		}
		fmt.Fprintln(os.Stdout, identity)
		return nil
	case "schedule-init":
		release, err := store.AdmitLocalAuthor(ctx)
		if err != nil {
			return err
		}
		defer release()
		return store.InitializeSchedulePolicy(ctx, *schedulePath)
	case "migrate":
		return store.Migrate(ctx)
	case "check":
		return store.Ready(ctx)
	case "backup":
		return store.Backup(ctx, *directory)
	case "verify-backup":
		return hub.VerifyBackup(ctx, *directory)
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
