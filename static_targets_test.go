package main

import (
	"os"
	"path/filepath"
	"testing"
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

func TestReadStaticTargetFileRejectsInvalidLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets")
	if err := os.WriteFile(path, []byte("2001:db8::1/128\nnot-an-ipv6\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readStaticTargetFile(path); err == nil {
		t.Fatal("readStaticTargetFile() accepted an invalid line")
	}
}
