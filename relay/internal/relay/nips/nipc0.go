package nips

import (
	"fmt"
	"regexp"
	"strings"

	nostr "github.com/nbd-wtf/go-nostr"
)

// NIP-C0: Code Snippets
// https://github.com/nostr-protocol/nips/blob/master/C0.md
//
// NIP-C0 defines kind 1337 for sharing and storing code snippets.
// The .content field contains the actual code snippet text.
//
// Optional tags:
//   - l: programming language name (lowercase)
//   - name: name of the code snippet (e.g., filename)
//   - extension: file extension (without dot)
//   - description: brief description
//   - runtime: runtime or environment
//   - license: SPDX license identifier
//   - dep: dependency (can be repeated)
//   - repo: URL or NIP-34 git repo announcement reference

// CodeSnippetKind is the kind for code snippets.
const CodeSnippetKind = 1337

// MaxCodeSnippetContentLength limits the code snippet content length.
const MaxCodeSnippetContentLength = 100000

// MaxCodeSnippetLanguageLength limits the language tag length.
const MaxCodeSnippetLanguageLength = 50

// extensionRegex matches valid file extensions (alphanumeric, no dot).
var extensionRegex = regexp.MustCompile(`^[a-zA-Z0-9]{1,10}$`)

// ValidateLanguageTag validates the "l" (language) tag.
func ValidateLanguageTag(tag nostr.Tag) error {
	if len(tag) < 2 {
		return fmt.Errorf("l tag must have a language value")
	}
	if tag[0] != "l" {
		return fmt.Errorf("tag must be 'l', got %q", tag[0])
	}
	lang := tag[1]
	if len(lang) > MaxCodeSnippetLanguageLength {
		return fmt.Errorf("language name exceeds max length of %d chars", MaxCodeSnippetLanguageLength)
	}
	if lang != strings.ToLower(lang) {
		return fmt.Errorf("language name must be lowercase: %s", lang)
	}
	return nil
}

// ValidateExtensionTag validates the "extension" tag.
func ValidateExtensionTag(tag nostr.Tag) error {
	if len(tag) < 2 {
		return fmt.Errorf("extension tag must have a value")
	}
	if tag[0] != "extension" {
		return fmt.Errorf("tag must be 'extension', got %q", tag[0])
	}
	ext := tag[1]
	if strings.HasPrefix(ext, ".") {
		return fmt.Errorf("extension must not start with a dot: %s", ext)
	}
	if !extensionRegex.MatchString(ext) {
		return fmt.Errorf("extension must be alphanumeric (1-10 chars): %s", ext)
	}
	return nil
}

// ValidateRepoTag validates the "repo" tag (URL or NIP-34 reference).
func ValidateRepoTag(tag nostr.Tag) error {
	if len(tag) < 2 {
		return fmt.Errorf("repo tag must have a value")
	}
	if tag[0] != "repo" {
		return fmt.Errorf("tag must be 'repo', got %q", tag[0])
	}
	repo := tag[1]
	// Either a URL or a NIP-34 reference (30617:pubkey:d-tag)
	if strings.HasPrefix(repo, "http://") || strings.HasPrefix(repo, "https://") {
		return nil
	}
	if strings.HasPrefix(repo, "30617:") {
		// NIP-34 git repo announcement reference
		parts := strings.SplitN(repo, ":", 3)
		if len(parts) != 3 {
			return fmt.Errorf("repo NIP-34 reference must be 30617:<pubkey>:<d-tag>")
		}
		if !hex64Regex.MatchString(parts[1]) {
			return fmt.Errorf("repo NIP-34 pubkey must be 64 hex chars: %s", parts[1])
		}
		return nil
	}
	return fmt.Errorf("repo must be HTTP(S) URL or 30617:<pubkey>:<d-tag>: %s", repo)
}

// ValidateNIPC0Event validates a NIP-C0 code snippet event (kind 1337).
func ValidateNIPC0Event(evt *nostr.Event) error {
	if evt.Kind != CodeSnippetKind {
		return fmt.Errorf("not a NIP-C0 event kind: %d", evt.Kind)
	}
	if len(evt.Content) > MaxCodeSnippetContentLength {
		return fmt.Errorf("code snippet content exceeds max length of %d chars", MaxCodeSnippetContentLength)
	}

	for _, tag := range evt.Tags {
		if len(tag) < 1 {
			continue
		}
		switch tag[0] {
		case "l":
			if err := ValidateLanguageTag(tag); err != nil {
				return err
			}
		case "extension":
			if err := ValidateExtensionTag(tag); err != nil {
				return err
			}
		case "repo":
			if err := ValidateRepoTag(tag); err != nil {
				return err
			}
		case "name", "description", "runtime", "license", "dep":
			if len(tag) < 2 {
				return fmt.Errorf("%s tag must have a value", tag[0])
			}
		}
	}
	return nil
}

// GetCodeSnippetLanguage returns the language from the l tag.
func GetCodeSnippetLanguage(evt *nostr.Event) string {
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "l" {
			return tag[1]
		}
	}
	return ""
}
