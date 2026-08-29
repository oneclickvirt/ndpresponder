package main

import (
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"go4.org/netipx"
)

func TestReadStaticTargetFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets")
	if err := os.WriteFile(path, []byte("# managed targets\n2001:db8::10\n2001:db8::20/120\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prefixes, err := readStaticTargetFile(path)
	if err != nil {
		t.Fatalf("readStaticTargetFile() error = %v", err)
	}
	if len(prefixes) != 2 || prefixes[0].String() != "2001:db8::10/128" || prefixes[1].String() != "2001:db8::/120" {
		t.Fatalf("prefixes = %#v", prefixes)
	}
}

func TestReadStaticTargetFileRejectsMissingFile(t *testing.T) {
	_, err := readStaticTargetFile(filepath.Join(t.TempDir(), "missing"))
	if err == nil || !errors.Is(err, errStaticTargetFileMissing) {
		t.Fatalf("readStaticTargetFile() error = %v, want missing-file error", err)
	}
}

func TestReadStaticTargetFileRejectsInvalidLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets")
	if err := os.WriteFile(path, []byte("2001:db8::1/128\nnot-an-ipv6\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readStaticTargetFile(path); err == nil {
		t.Fatal("readStaticTargetFile() accepted an invalid line")
	}
}

func TestReloadStaticTargetFileReplacesTargets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets")

	staticTargetMu.Lock()
	oldTargetFile := staticTargetFile
	oldStaticTargets := staticTargets
	oldCLITargets := staticCLITargets
	oldTargetSubnets := targetSubnets
	staticTargetFile = path
	staticTargets = nil
	staticCLITargets = nil
	targetSubnets = nil
	staticTargetMu.Unlock()
	t.Cleanup(func() {
		staticTargetMu.Lock()
		staticTargetFile = oldTargetFile
		staticTargets = oldStaticTargets
		staticCLITargets = oldCLITargets
		targetSubnets = oldTargetSubnets
		staticTargetMu.Unlock()
	})

	first := netip.MustParseAddr("2001:db8::10")
	second := netip.MustParseAddr("2001:db8::20")
	if err := os.WriteFile(path, []byte(first.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reloadStaticTargetFile()
	if targets := currentStaticTargetSet(); targets == nil || !targets.Contains(first) {
		t.Fatalf("first target %s was not loaded", first)
	}

	if err := os.WriteFile(path, []byte(second.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reloadStaticTargetFile()
	targets := currentStaticTargetSet()
	if targets == nil || !targets.Contains(second) || targets.Contains(first) {
		t.Fatalf("reloaded targets did not replace %s with %s", first, second)
	}
}

func TestReloadStaticTargetFilePreservesTargetsWhenFileIsTemporarilyMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets")
	first := netip.MustParseAddr("2001:db8::30")

	staticTargetMu.Lock()
	oldTargetFile := staticTargetFile
	oldStaticTargets := staticTargets
	oldCLITargets := staticCLITargets
	oldTargetSubnets := targetSubnets
	staticTargetFile = path
	staticCLITargets = nil
	staticTargets = []netip.Prefix{netip.PrefixFrom(first, 128)}
	var builder netipx.IPSetBuilder
	builder.Add(first)
	targetSubnets, _ = builder.IPSet()
	staticTargetMu.Unlock()
	t.Cleanup(func() {
		staticTargetMu.Lock()
		staticTargetFile = oldTargetFile
		staticTargets = oldStaticTargets
		staticCLITargets = oldCLITargets
		targetSubnets = oldTargetSubnets
		staticTargetMu.Unlock()
	})

	reloadStaticTargetFile()
	targets := currentStaticTargetSet()
	if targets == nil || !targets.Contains(first) {
		t.Fatalf("missing target file cleared the last valid target %s", first)
	}
}
