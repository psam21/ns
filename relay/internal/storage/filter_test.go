package storage

import (
	"strings"
	"testing"

	nostr "github.com/nbd-wtf/go-nostr"
)

// TestBuildQueryNeverDropsAuthors is the regression test for a filter that
// silently returned the wrong results.
//
// An author-only filter -- `{"authors": [pubkey]}` with no kinds -- selected
// the "created_at" path in GetBestIndex, whose query body is `WHERE true`. The
// author predicate was never emitted at all. The relay answered such a REQ
// with recent events by *any* author, so a client asking for its own events
// received other people's instead.
//
// This is the worst shape a relay bug can take: it returns 200, valid Nostr
// frames, and entirely wrong data. Nothing errors, no metric moves, and the
// response looks plausible.
//
// Found by running tests/nips/test_nip01.sh against production on
// 2026-10-02. Discovered there because the suite asserts on a specific
// pubkey appearing in results; it was not visible from the dashboard, which
// only shows aggregate counts and all of those were correct.
func TestBuildQueryNeverDropsAuthors(t *testing.T) {
	const author = "3698dce79f3b443cb8d62877630c7954bbcf6b920f5b78b3d3e2a90cf18f1fed"

	tests := []struct {
		name   string
		filter nostr.Filter
	}{
		{
			// The failing case: authors with no kinds.
			name:   "author only",
			filter: nostr.Filter{Authors: []string{author}, Limit: 10},
		},
		{
			name:   "two authors, no kinds",
			filter: nostr.Filter{Authors: []string{author, "aa" + author[2:]}, Limit: 10},
		},
		{
			name:   "author plus since",
			filter: nostr.Filter{Authors: []string{author}, Limit: 10, Since: tsPtr(1000)},
		},
		{
			name:   "author plus tag",
			filter: nostr.Filter{Authors: []string{author}, Tags: nostr.TagMap{"t": []string{"x"}}, Limit: 10},
		},
		{
			// These already worked; kept so a refactor cannot silently
			// reintroduce the bug for the shapes that were fine.
			name:   "author and kind",
			filter: nostr.Filter{Authors: []string{author}, Kinds: []int{1}, Limit: 10},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cf := CompileFilter(tt.filter)
			query, args, err := cf.BuildQuery()
			if err != nil {
				t.Fatalf("BuildQuery: %v", err)
			}

			if !strings.Contains(query, "pubkey") {
				t.Errorf("query has no pubkey predicate, so the author filter is "+
					"silently ignored and unrelated events are returned:\n  %s", query)
			}

			// The predicate must be parameterised, not interpolated: an
			// interpolated pubkey is an injection vector even though
			// ValidateFilter checks the shape first.
			if !strings.Contains(query, "pubkey = ANY(ARRAY[") &&
				!strings.Contains(query, "pubkey = ANY($") {
				t.Errorf("pubkey predicate is not parameterised:\n  %s", query)
			}

			// Every author must appear as a bound argument. A placeholder
			// with no matching arg is a query that errors at runtime, and an
			// arg with no placeholder is a silently ignored filter.
			placeholders := strings.Count(query, "$")
			if placeholders > len(args) {
				t.Errorf("query references %d placeholders but only %d args were "+
					"bound; this fails at runtime rather than returning wrong data:\n  %s",
					placeholders, len(args), query)
			}
			if placeholders < len(args) {
				t.Errorf("query has %d placeholders but %d args were bound; extra "+
					"args mean part of the filter was dropped:\n  %s",
					placeholders, len(args), query)
			}
		})
	}
}

// TestBuildQueryIncludesEveryPredicate guards the general shape: a compiled
// filter must mention every dimension the caller supplied.
//
// The author-only bug was one instance of a structural problem -- the query is
// assembled from an index-selection switch, and any filter dimension without a
// matching case is dropped rather than rejected. This test asserts coverage
// across dimensions so a new gap fails loudly.
func TestBuildQueryIncludesEveryPredicate(t *testing.T) {
	const (
		author = "3698dce79f3b443cb8d62877630c7954bbcf6b920f5b78b3d3e2a90cf18f1fed"
		id     = "b9d22edefd850f6756c63f5a20070d1a1018778e3f52b5f83f39d55ca827ac28"
	)

	cf := CompileFilter(nostr.Filter{
		IDs:     []string{id},
		Authors: []string{author},
		Kinds:   []int{1},
		Tags:    nostr.TagMap{"t": []string{"x"}},
		Limit:   10,
	})
	query, _, err := cf.BuildQuery()
	if err != nil {
		t.Fatalf("BuildQuery: %v", err)
	}

	for _, want := range []string{"id", "pubkey", "kind", "tags"} {
		if !strings.Contains(query, want) {
			t.Errorf("query is missing a %q predicate:\n  %s", want, query)
		}
	}
}

// TestGetBestIndexCoversAuthorOnly pins the index selection itself.
//
// The selection returned "created_at" for author-only filters, and the
// "created_at" query body is `WHERE true` -- so the selection *was* the bug.
// Author-only is a first-class common case (profile fetches, timeline reads
// scoped to one author), not an edge case to fall through.
func TestGetBestIndexCoversAuthorOnly(t *testing.T) {
	const author = "3698dce79f3b443cb8d62877630c7954bbcf6b920f5b78b3d3e2a90cf18f1fed"

	tests := []struct {
		name   string
		filter nostr.Filter
		want   string
	}{
		{"ids only", nostr.Filter{IDs: []string{"abc"}, Limit: 1}, "id"},
		{"author and kind", nostr.Filter{Authors: []string{author}, Kinds: []int{1}, Limit: 1}, "pubkey_kind_created"},
		{"kind only", nostr.Filter{Kinds: []int{1}, Limit: 1}, "kind_created"},
		// Its own branch, not "pubkey_kind_created": with no kinds that
		// branch emits an empty kind array and matches nothing.
		{"author only", nostr.Filter{Authors: []string{author}, Limit: 1}, "pubkey"},
		{"nothing", nostr.Filter{Limit: 1}, "created_at"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompileFilter(tt.filter).GetBestIndex()
			if got != tt.want {
				t.Errorf("GetBestIndex() = %q, want %q", got, tt.want)
			}
		})
	}
}

// tsPtr returns a *nostr.Timestamp for the given unix second, since the filter
// fields are pointers and a bare literal will not compile.
func tsPtr(unix int64) *nostr.Timestamp {
	t := nostr.Timestamp(unix)
	return &t
}

// TestCreatedAtBranchStillHonoursKinds guards the default branch against being
// used for anything with a real predicate. It exists so that "created_at" stays
// meaning "no selective predicate available" rather than becoming a catch-all.
func TestCreatedAtBranchStillHonoursKinds(t *testing.T) {
	cf := CompileFilter(nostr.Filter{Kinds: []int{1}, Limit: 5})
	if got := cf.GetBestIndex(); got == "created_at" {
		t.Errorf("a kind filter selected the created_at branch (%q); the "+
			"kind predicate would be dropped", got)
	}
	query, _, err := cf.BuildQuery()
	if err != nil {
		t.Fatalf("BuildQuery: %v", err)
	}
	if strings.Contains(query, "WHERE true") {
		t.Errorf("query degenerated to WHERE true despite having a kind filter:\n  %s", query)
	}
}
