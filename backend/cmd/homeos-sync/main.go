package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/HuuVab/HomeOS/backend/internal/syncclient"
	"golang.org/x/term"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "homeos-sync:", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) < 2 {
		return errors.New("usage: homeos-sync <init|scan|status|sync> <existing-local-folder> [--server URL] [--user USERNAME]")
	}
	command, directory := arguments[0], arguments[1]
	if command != "init" && command != "scan" && command != "status" && command != "sync" {
		return fmt.Errorf("unknown command %q", command)
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	serverURL := flags.String("server", "http://127.0.0.1:8080", "HomeOS server origin")
	username := flags.String("user", "", "HomeOS username")
	if err := flags.Parse(arguments[2:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected extra arguments")
	}
	state, err := syncclient.OpenState(absolute)
	if err != nil {
		return err
	}
	defer state.Close()
	if command == "init" {
		fmt.Printf("Local sync state initialized in %s. No files were transferred.\n", absolute)
		return nil
	}
	if command == "scan" {
		if _, err := syncclient.ScanLocal(absolute, state); err != nil {
			return err
		}
	}
	if command == "status" || command == "scan" {
		return printStatus(state)
	}
	if *username == "" {
		fmt.Print("HomeOS username: ")
		if _, err := fmt.Scanln(username); err != nil {
			return err
		}
	}
	if strings.TrimSpace(*username) == "" {
		return errors.New("username is required")
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("sync requires an interactive terminal for password entry")
	}
	fmt.Print("HomeOS password: ")
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return err
	}
	defer clear(password)
	remote, err := syncclient.NewRemote(*serverURL)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := remote.Login(ctx, *username, string(password)); err != nil {
		return err
	}
	report, err := syncclient.Sync(ctx, absolute, state, remote)
	if err != nil {
		return err
	}
	fmt.Printf("Sync complete: %d downloaded, %d uploaded, %d folders, %d conflicts, %d queued for retry; change cursor %d.\n",
		report.Downloaded, report.Uploaded, report.Folders, report.Conflicts, report.Retried, report.Cursor)
	return printStatus(state)
}

func clear(data []byte) {
	for index := range data {
		data[index] = 0
	}
}

func printStatus(state *syncclient.State) error {
	pending, err := state.Pending()
	if err != nil {
		return err
	}
	conflicts, err := state.Conflicts()
	if err != nil {
		return err
	}
	fmt.Printf("Pending operations: %d; conflicts: %d.\n", len(pending), len(conflicts))
	for _, item := range pending {
		fmt.Printf("  pending %s %s", item.Operation, item.Path)
		if item.LastError != "" {
			fmt.Printf(" (attempts %d: %s)", item.Attempts, item.LastError)
		}
		fmt.Println()
	}
	for _, item := range conflicts {
		fmt.Printf("  conflict %s: %s\n", item.Path, item.Reason)
	}
	return nil
}
