package main

import (
	"fmt"
	"os"
	"strings"
)

func clearResponderReadyFile() error {
	if responderReadyFile == "" {
		return nil
	}
	if err := os.WriteFile(responderReadyFile, nil, 0o600); err != nil {
		return fmt.Errorf("clear responder ready file %q: %w", responderReadyFile, err)
	}
	return nil
}

func markResponderReady(ifname string) error {
	if responderReadyFile == "" {
		return nil
	}
	ifname = strings.TrimSpace(ifname)
	if ifname == "" {
		return fmt.Errorf("mark responder ready: selected interface is empty")
	}
	if err := os.WriteFile(responderReadyFile, []byte(ifname+"\n"), 0o600); err != nil {
		return fmt.Errorf("write responder ready file %q: %w", responderReadyFile, err)
	}
	return nil
}
