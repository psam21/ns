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

			// The predicate must be an indexable scalar equality, never
			// `= ANY(ARRAY[...])`.
			//
			// This is the assertion that would have caught the 92-second
			// scan. `= ANY(ARRAY[...])` is a set membership test against an
			// expression and PostgreSQL cannot drive a btree index from it,
			// so the query is *correct* and returns the right rows -- it
			// just takes long enough to blow the 5s timeout in GetEvents,
			// after which the client sees an empty result.
			//
			// Measured on the production data set (1.2M rows):
			// IN (...) 2.6ms, = ANY(ARRAY[...]) 92,101ms.
			if strings.Contains(query, "ANY(ARRAY[") {
				t.Errorf("query uses = ANY(ARRAY[...]), which is not indexable:\n  %s\n"+
					"  it forces a scan of the whole events table and will exceed "+
					"the query timeout", query)
			}
			if strings.Contains(query, "pubkey = ANY(") {
				t.Errorf("pubkey predicate uses = ANY(), which cannot use "+
					"events_pubkey_created_at:\n  %s", query)
			}
			if !strings.Contains(query, "pubkey IN (") {
				t.Errorf("pubkey predicate is not a parameterised IN list:\n  %s", query)
			}

			// Every placeholder must have a bound argument and vice versa.
			// A placeholder with no arg errors at runtime; an arg with no
			// placeholder is a silently dropped filter.
			want := countPlaceholders(query)
			if want != len(args) {
				t.Errorf("query references %d placeholders but %d args were bound:\n"+
					"  query: %s\n  args:  %v", want, len(args), query, args)
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

// countPlaceholders counts distinct $n placeholders in a query.
//
// Counts distinct indices rather than occurrences of '$', because a naive
// strings.Count(query, "$") overcounts: PostgreSQL also uses $ in dollar-quoted
// strings and, more practically here, the same index may legitimately appear
// more than once if a caller ever builds `x = $1 OR x = $1`. The thing that
// must hold is that the SET of referenced indices matches the bound args.
func countPlaceholders(query string) int {
	seen := map[string]bool{}
	for i := 0; i < len(query); i++ {
		if query[i] != '$' {
			continue
		}
		j := i + 1
		for j < len(query) && query[j] >= '0' && query[j] <= '9' {
			j++
		}
		if j > i+1 {
			seen[query[i:j]] = true
		}
	}
	return len(seen)
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

// TestNoFilterUsesNonIndexableAny is the regression test for a 92-second query.
//
// Every equality predicate must be a scalar `IN (...)`, never
// `= ANY(ARRAY[...])`. The latter is a set membership test against an
// expression, which PostgreSQL cannot drive a btree index from, so the planner
// falls back to scanning the entire events table.
//
// Measured on nostr.ltd 2026-10-02 (1,198,763 rows, index
// `events_pubkey_created_at` on (pubkey, created_at)):
//
//	pubkey IN ($1)                    Index Scan   ...     2.6 ms
//	pubkey = ANY(ARRAY[$1]::text[])   Seq/Index Scan,
//	                                 1,198,963 rows
//	                                 filtered  ... 92,101 ms
//
// The 5s query timeout in GetEvents turned that into an empty response with no
// error anywhere, which is why this was indistinguishable from the
// dropped-author bug fixed alongside it -- both presented as "author filter
// returns nothing".
func TestNoFilterUsesNonIndexableAny(t *testing.T) {
	const (
		a = "3698dce79f3b443cb8d62877630c7954bbcf6b920f5b78b3d3e2a90cf18f1fed"
		b = "aa8dce79f3b443cb8d62877630c7954bbcf6b920f5b78b3d3e2a90cf18f1fed"
	)

	filters := map[string]nostr.Filter{
		"author only":      {Authors: []string{a}, Limit: 10},
		"two authors":      {Authors: []string{b, a}, Limit: 10},
		"author and kind":  {Authors: []string{a}, Kinds: []int{1}, Limit: 10},
		"kind only":        {Kinds: []int{1, 7, 30023}, Limit: 10},
		"id only":          {IDs: []string{"b9d22edefd850f6756c63f5a20070d1a1018778e3f52b5f83f39d55ca827ac28"}, Limit: 10},
		"author and since": {Authors: []string{a}, Limit: 10, Since: tsPtr(1000)},
		"author and until": {Authors: []string{a}, Limit: 10, Until: tsPtr(9999999999)},
		"author and tag":   {Authors: []string{a}, Tags: nostr.TagMap{"t": []string{"x"}}, Limit: 10},
		"id and author and kind": {
			IDs:     []string{"b9d22edefd850f6756c63f5a20070d1a1018778e3f52b5f83f39d55ca827ac28"},
			Authors: []string{a}, Kinds: []int{1}, Limit: 10,
		},
		"everything": {
			IDs:     []string{"b9d22edefd850f6756c63f5a20070d1a1018778e3f52b5f83f39d55ca827ac28"},
			Authors: []string{a}, Kinds: []int{1}, Tags: nostr.TagMap{"t": []string{"x"}},
			Limit: 10,
		},
	}

	for name, f := range filters {
		t.Run(name, func(t *testing.T) {
			query, args, err := CompileFilter(f).BuildQuery()
			if err != nil {
				t.Fatalf("BuildQuery: %v", err)
			}

			if strings.Contains(query, "ANY(ARRAY[") {
				t.Errorf("query uses = ANY(ARRAY[...]), which cannot use a btree "+
					"index and scans the whole events table:\n  %s", query)
			}

			// Placeholders and bound args must agree exactly. A helper that
			// appends to a slice passed by value loses those elements and
			// produces a query that is valid SQL but binds $1 to the wrong
			// value -- a silent wrong-results bug, not a crash.
			if got, want := countPlaceholders(query), len(args); got != want {
				t.Errorf("query has %d placeholders but %d args bound:\n  SQL:  %s\n  args: %v",
					got, want, query, args)
			}
		})
	}
}

// TestCompiledFilterSQLIsDeterministic guards against map iteration order
// leaking into generated SQL.
//
// The compiled filters are maps, so ranging one directly produces a different
// placeholder order on every run. That defeats plan caching and makes two
// identical filters produce different SQL strings, so a regression is
// impossible to spot by reading logs.
func TestCompiledFilterSQLIsDeterministic(t *testing.T) {
	f := nostr.Filter{
		Authors: []string{
			"3698dce79f3b443cb8d62877630c7954bbcf6b920f5b78b3d3e2a90cf18f1fed",
			"aa8dce79f3b443cb8d62877630c7954bbcf6b920f5b78b3d3e2a90cf18f1fed",
			"bb8dce79f3b443cb8d62877630c7954bbcf6b920f5b78b3d3e2a90cf18f1fed",
			"cc8dce79f3b443cb8d62877630c7954bbcf6b920f5b78b3d3e2a90cf18f1fed",
			"dd8dce79f3b443cb8d62877630c7954bbcf6b920f5b78b3d3e2a90cf18f1fed",
		},
		Kinds: []int{1, 7, 30023, 0, 1311},
		Limit: 50,
	}

	first, _, err := CompileFilter(f).BuildQuery()
	if err != nil {
		t.Fatalf("BuildQuery: %v", err)
	}
	for i := 0; i < 50; i++ {
		got, _, err := CompileFilter(f).BuildQuery()
		if err != nil {
			t.Fatalf("BuildQuery: %v", err)
		}
		if got != first {
			t.Fatalf("SQL differs between identical calls (iteration %d):\n  %s\n  %s",
				i, first, got)
		}
	}
}
