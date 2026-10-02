package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestNoDeadConfiguration pins the invariant that every operator-facing
// setting is either read somewhere in the tree, or is documented here as
// deliberately unused.
//
// Two settings were already found and fixed this way. SEND_BUFFER_SIZE was
// parsed, validated, defaulted, and set to 4096 in deploy/config.yaml while
// the code hardcoded a 1MB literal — the setting that existed to prevent an
// OOM was dead code. METRICS_ENABLED / METRICS_PORT were set in every config
// file and nothing ever started a server, so the whole Prometheus surface was
// unreachable in production. Both were found by asking "who reads this field?"
// rather than by reading any single file.
//
// This test is deliberately coarse. It reads the struct tags from this file
// and greps the rest of the tree for the corresponding Go field name. It
// cannot prove a setting is used in a *meaningful* way — only that it is
// referenced at all — so it is a floor, not a ceiling.
func TestNoDeadConfiguration(t *testing.T) {
	// Settings that are intentionally not read by the runtime, with the
	// reason. Adding an entry here is a decision that should be revisited;
	// removing one should come with an implementation.
	knownUnused := map[string]string{
		// gorilla/websocket calls netConn.SetDeadline(time.Time{}) immediately
		// after Hijack() (websocket@v1.5.3/server.go:251), so an http.Server
		// write deadline cannot apply to an upgraded WebSocket connection.
		// Wiring this to http.Server.WriteTimeout would make the setting look
		// live while changing nothing observable for the relay's real traffic,
		// which is worse than leaving it obviously unused. The HTTP surface
		// uses literals with a comment explaining why; see relay/internal/relay/
		// server.go.
		"WriteTimeout": "cannot apply to hijacked WebSocket connections; see relay/internal/relay/server.go",

		// Read only by performCrossFieldValidation to reject a too-small
		// EVENT_CACHE_SIZE relative to MAX_CONNECTIONS. It does not size any
		// runtime structure — the in-memory caches are bounded by the MV-backed
		// refresh interval and MaxSubscriptions instead.
		"EventCacheSize": "validation-only; does not size any runtime structure",

		// MAX_BAN_DURATION and PROGRESSIVE_BAN were in this list until they
		// were implemented, alongside MAX_REQUESTS_PER_SECOND. All three were
		// accepted and validated but read nowhere, so an operator who set
		// them got no behaviour change and no warning -- production config
		// sets MAX_REQUESTS_PER_SECOND: 60, PROGRESSIVE_BAN: true and
		// MAX_BAN_DURATION.
		//
		// They are now read by internal/relay: MAX_REQUESTS_PER_SECOND sizes a
		// dedicated inbound request limiter, and PROGRESSIVE_BAN +
		// MAX_BAN_DURATION drive the banDurationFor escalation ladder. The
		// comment is kept because the failure mode -- a setting that validates
		// and then does nothing -- is the reason this test exists.
	}

	fields := relayConfigFields(t)

	for _, f := range fields {
		if reason, ok := knownUnused[f.name]; ok {
			t.Logf("known-unused setting %s (%s): %s", f.name, f.mapKey, reason)
			continue
		}

		if f.references == 0 {
			t.Errorf("configuration setting %s (mapstructure:%s) is never read anywhere "+
				"outside internal/config — it is dead config. Either wire it up, or add it "+
				"to knownUnused in this test with the reason.",
				f.name, f.mapKey)
		}
	}
}

type configField struct {
	name       string
	mapKey     string
	references int
}

// relayConfigFields reflects over RelayConfig and counts references to each
// field across the rest of the relay tree.
func relayConfigFields(t *testing.T) []configField {
	t.Helper()

	rt := reflect.TypeOf(RelayConfig{})
	src := readAllGoSource(t)

	var out []configField
	for i := 0; i < rt.NumField(); i++ {
		sf := rt.Field(i)
		mapKey := mapstructureKey(sf.Tag.Get("mapstructure"))
		if mapKey == "" || mapKey == "-" {
			continue
		}

		out = append(out, configField{
			name:       sf.Name,
			mapKey:     mapKey,
			references: strings.Count(src, "."+sf.Name),
		})
	}
	return out
}

// readAllGoSource concatenates every non-test .go file in the module except
// internal/config itself, so a field's own declaration does not count as a
// use of that field.
func readAllGoSource(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolving module root: %v", err)
	}

	var b strings.Builder
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		// Skip internal/config: a field's declaration and its own validation
		// must not count as a use of that field elsewhere.
		rel, relErr := filepath.Rel(root, path)
		if relErr == nil && strings.HasPrefix(rel, "internal"+string(filepath.Separator)+"config"+string(filepath.Separator)) {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		b.Write(data)
		b.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatalf("walking module source: %v", err)
	}
	return b.String()
}

// mapstructureKey extracts the key from a struct field's mapstructure tag.
//
// sf.Tag.Get("mapstructure") ALREADY unwraps and returns just the value
// (e.g. MIN_POW_DIFFICULTY), so this must not be re-parsed as a
// StructTag. Two earlier attempts got this wrong and made the whole test
// vacuous — verified by a negative control that injects a dead field and
// watches the test still report ok:
//
//  1. Hand-rolled `strings.Index(tag, "mapstructure:\"")` on the raw tag,
//     which never matched because gofmt aligns the tag after the field name.
//  2. reflect.StructTag(sf.Tag.Get("mapstructure")).Get("mapstructure"),
//     double-unwrapping a value that was already unwrapped.
//
// The correct helper is therefore a one-liner over the already-extracted
// value. It lives here rather than inline so there is a single place for the
// next person to break.
func mapstructureKey(tagValue string) string {
	return tagValue
}
