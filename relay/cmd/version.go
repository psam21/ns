package main

import "fmt"

// GetVersion returns the current version information
func GetVersion() string {
	return version
}

// GetFullVersionInfo returns detailed version information
func GetFullVersionInfo() string {
	return fmt.Sprintf("Version: %s\nCommit: %s\nBuilt: %s", version, commit, date)
}

// GetVersionWithPrefix returns the nostr.ltd relay version prefix
func GetVersionWithPrefix() string {
	return fmt.Sprintf("nostr.ltd relay version: %s", version)
}
