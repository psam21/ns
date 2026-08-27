package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestPrintWelcomeBannerUsesNostrBranding(t *testing.T) {
	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	os.Stdout = writer
	printWelcomeBanner()
	_ = writer.Close()
	os.Stdout = originalStdout
	defer reader.Close()

	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read banner output: %v", err)
	}
	banner := string(output)
	if !strings.Contains(banner, "nostr.ltd") {
		t.Fatalf("banner does not contain nostr.ltd branding: %q", banner)
	}
	if strings.Contains(strings.ToLower(banner), "shugur") {
		t.Fatalf("banner contains obsolete Shugur branding: %q", banner)
	}
}
