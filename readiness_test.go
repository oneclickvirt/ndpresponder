package main

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfave/cli/v2"
)

func TestResponderReadyFileLifecycle(t *testing.T) {
	oldReadyFile := responderReadyFile
	responderReadyFile = filepath.Join(t.TempDir(), "ready")
	t.Cleanup(func() { responderReadyFile = oldReadyFile })

	if err := markResponderReady("eth0"); err != nil {
		t.Fatalf("markResponderReady() error = %v", err)
	}
	contents, err := os.ReadFile(responderReadyFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "eth0\n" {
		t.Fatalf("ready file = %q, want eth0", contents)
	}

	if err := clearResponderReadyFile(); err != nil {
		t.Fatalf("clearResponderReadyFile() error = %v", err)
	}
	contents, err = os.ReadFile(responderReadyFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 0 {
		t.Fatalf("ready file after clear = %q, want empty", contents)
	}
}

func TestMarkResponderReadyRejectsEmptyInterface(t *testing.T) {
	oldReadyFile := responderReadyFile
	responderReadyFile = filepath.Join(t.TempDir(), "ready")
	t.Cleanup(func() { responderReadyFile = oldReadyFile })

	if err := markResponderReady(" "); err == nil {
		t.Fatal("markResponderReady() accepted an empty interface")
	}
}

func TestBeforeClearsReadyFileBeforeRejectingInvalidReloadInterval(t *testing.T) {
	readyFile := filepath.Join(t.TempDir(), "ready")

	staticTargetMu.Lock()
	oldReadyFile := responderReadyFile
	oldReloadInterval := targetFileReloadEvery
	oldTargetFile := staticTargetFile
	oldStaticTargets := staticTargets
	oldCLITargets := staticCLITargets
	oldTargetSubnets := targetSubnets
	staticTargetMu.Unlock()
	t.Cleanup(func() {
		staticTargetMu.Lock()
		responderReadyFile = oldReadyFile
		targetFileReloadEvery = oldReloadInterval
		staticTargetFile = oldTargetFile
		staticTargets = oldStaticTargets
		staticCLITargets = oldCLITargets
		targetSubnets = oldTargetSubnets
		staticTargetMu.Unlock()
	})
	if err := os.WriteFile(readyFile, []byte("stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	flags := flag.NewFlagSet("ndpresponder", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	for _, appFlag := range app.Flags {
		if err := appFlag.Apply(flags); err != nil {
			t.Fatalf("apply CLI flag: %v", err)
		}
	}
	if err := flags.Parse([]string{
		"--ready-file", readyFile,
		"--target-file-reload-interval", "0s",
	}); err != nil {
		t.Fatalf("parse CLI flags: %v", err)
	}

	err := app.Before(cli.NewContext(app, flags, nil))
	if err == nil {
		t.Fatal("app.Before() accepted a zero target-file reload interval")
	}
	if !strings.Contains(err.Error(), "target-file-reload-interval must be greater than zero") {
		t.Fatalf("app.Before() error = %v, want reload interval validation", err)
	}
	contents, readErr := os.ReadFile(readyFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(contents) != 0 {
		t.Fatalf("ready file after rejected startup = %q, want empty", contents)
	}
}
