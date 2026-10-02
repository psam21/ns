package storage

import (
	"strings"
	"testing"

	nostr "github.com/nbd-wtf/go-nostr"
)

// buildFilterWhere backs both GetEventCount and GetEventPubkeys, which are the
// NIP-45 COUNT path. These tests exist because that path was a live copy of the
// same non-indexable `= ANY` bug fixed in filter.go, and it was missed.
//
// Two separate copies of the same query builder is the actual defect here. The
// second copy was written because the first was not reusable, and it drifted.
// These tests run against the shared builder, so a new caller cannot reintroduce
// the problem.

// TestBuildFilterWhereIsIndexable is the regression test for the NIP-45 COUNT
// path returning nothing.
//
// GetEventCount and GetEventPubkeys emitted `pubkey = ANY($1)` with the author
// list bound as a single array parameter. A bound parameter does NOT make this
// indexable -- measured on nostr.ltd 2026-10-02 against the production table
// (1.2M rows) using PREPARE, so the form is identical to what pgx sends:
//
//	pubkey = $1                        Index Only Scan  ...     1.3 ms
//	pubkey = ANY(ARRAY[$1]::text[])    Parallel Seq Scan,
//	                                   399,687 rows
//	                                   removed  ... 5,528 ms
//
// The 92-second figure quoted in filter.go is the literal-array form on a
// full-count query; the bound-parameter form is not quite as catastrophic but
// is still a sequential scan of the whole table for what should be a single
// index lookup. Against a 5s timeout it produces an empty result with no error,
// which is how it presented.
//
// Note the shape of the fix: `= ANY($1)` and `= ANY(ARRAY[$1])` are different
// SQL and I initially assumed the bound form was safe because it "passes an
// array". EXPLAIN said otherwise. Measure; do not reason about indexability
// from surface syntax.
func TestBuildFilterWhereIsIndexable(t *testing.T) {
	const (
		a = "3698dce79f3b443cb8d62877630c7954bbcf6b920f5b78b3d3e2a90cf18f1fed"
		b = "aa8dce79f3b443cb8d62877630c7954bbcf6b920f5b78b3d3e2a90cf18f1fed"
	)

	filters := map[string]nostr.Filter{
		"empty":        {},
		"author only":  {Authors: []string{a}},
		"two authors":  {Authors: []string{a, b}},
		"kind only":    {Kinds: []int{1, 7, 30023}},
		"id only":      {IDs: []string{"b9d22edefd850f6756c63f5a20070d1a1018778e3f52b5f83f39d55ca827ac28"}},
		"author+kind":  {Authors: []string{a}, Kinds: []int{1}},
		"author+since": {Authors: []string{a}, Since: tsPtr(1000)},
		"author+until": {Authors: []string{a}, Until: tsPtr(9999999999)},
		"author+tag":   {Authors: []string{a}, Tags: nostr.TagMap{"t": []string{"x"}}},
		"all dimensions": {
			IDs:     []string{"b9d22edefd850f6756c63f5a20070d1a1018778e3f52b5f83f39d55ca827ac28"},
			Authors: []string{a, b},
			Kinds:   []int{1, 7},
			Since:   tsPtr(1000),
			Until:   tsPtr(9999999999),
			Tags:    nostr.TagMap{"t": []string{"x"}, "e": []string{"y"}},
		},
	}

	for name, f := range filters {
		t.Run(name, func(t *testing.T) {
			where, args := buildFilterWhere(f)

			// No equality predicate may be expressed as a membership test.
			// `tags @> $n` is exempt: that is a GIN containment operator, not
			// an equality, and it does use the tag index.
			if strings.Contains(where, "ANY(") {
				t.Errorf("WHERE clause uses ANY(), which cannot use a btree index "+
					"and forces a sequential scan:\n  %s", where)
			}

			// Placeholders must line up with bound args exactly. A builder that
			// appends to a by-value slice loses those elements and yields valid
			// SQL binding the wrong value to a placeholder -- wrong results, no
			// error.
			if got, want := countPlaceholders(where), len(args); got != want {
				t.Errorf("WHERE has %d placeholders but %d args bound:\n  SQL:  %s\n  args: %v",
					got, want, where, args)
			}

			// An unconstrained filter must produce no WHERE at all. A caller
			// that emits `SELECT COUNT(*) FROM events WHERE ` on an unfiltered
			// COUNT is a syntax error; one that emitted nothing silently would
			// full-scan instead.
			if len(f.IDs) == 0 && len(f.Authors) == 0 && len(f.Kinds) == 0 &&
				f.Since == nil && f.Until == nil && len(f.Tags) == 0 {
				if where != "" {
					t.Errorf("unconstrained filter produced a WHERE clause: %q", where)
				}
				if len(args) != 0 {
					t.Errorf("unconstrained filter bound %d args: %v", len(args), args)
				}
			}
		})
	}
}

// TestBuildFilterWhereDropsNothing asserts every dimension the caller supplied
// reaches the SQL. The author-only bug in filter.go was a predicate that was
// accepted, validated, logged, and then never emitted.
func TestBuildFilterWhereDropsNothing(t *testing.T) {
	const a = "3698dce79f3b443cb8d62877630c7954bbcf6b920f5b78b3d3e2a90cf18f1fed"

	where, _ := buildFilterWhere(nostr.Filter{
		Authors: []string{a},
		Kinds:   []int{1},
		Since:   tsPtr(1000),
		Until:   tsPtr(9999999999),
		Tags:    nostr.TagMap{"t": []string{"x"}},
	})

	for _, want := range []string{
		"pubkey IN (",
		"kind IN (",
		"created_at >= ",
		"created_at <= ",
		"tags @> ",
	} {
		if !strings.Contains(where, want) {
			t.Errorf("WHERE clause is missing %q:\n  %s", want, where)
		}
	}
}

// TestBuildFilterWhereIsDeterministic guards the tag map.
//
// filter.Tags is a map[string][]string, so ranging it directly yields a
// different placeholder order on each call. Two identical COUNT filters would
// compile to different SQL, which defeats plan caching and makes a regression
// invisible in logs. The same defect was fixed in filter.go; this asserts the
// NIP-45 path does not reintroduce it independently.
func TestBuildFilterWhereIsDeterministic(t *testing.T) {
	f := nostr.Filter{
		Authors: []string{"3698dce79f3b443cb8d62877630c7954bbcf6b920f5b78b3d3e2a90cf18f1fed"},
		Tags: nostr.TagMap{
			"z": {"1"}, "a": {"2"}, "m": {"3"},
			"b": {"4"}, "y": {"5"}, "c": {"6"},
		},
	}

	first, _ := buildFilterWhere(f)

	// 64 repeats is enough for Go's randomised map iteration start to surface a
	// difference in nearly every run.
	for i := 0; i < 64; i++ {
		got, _ := buildFilterWhere(f)
		if got != first {
			t.Fatalf("buildFilterWhere is nondeterministic on iteration %d:\n  first: %s\n  this:  %s",
				i, first, got)
		}
	}

	// Sorted order, not merely consistent order. Six tags => six `tags @>`
	// clauses; the args come back in the same sorted sequence.
	_, args := buildFilterWhere(f)
	if len(args) != 7 { // 1 author + 6 tags
		t.Fatalf("got %d args, want 7 (1 author + 6 tags)", len(args))
	}
	wantOrder := []string{"a", "b", "c", "m", "y", "z"}
	for i, wantName := range wantOrder {
		gotPair, ok := args[1+i].([][]string)
		if !ok {
			t.Fatalf("arg %d is %T, want [][]string", 1+i, args[1+i])
		}
		if len(gotPair) != 1 || gotPair[0][0] != wantName {
			t.Errorf("tag arg %d is %v, want tag %q", i, gotPair, wantName)
		}
	}
}

// TestAppendEqualityHelpers covers the shared placeholder helpers directly,
// including the argument-slicing behaviour that produced a silent wrong-results
// bug when it was first written: the helper appends to a by-value slice and
// returns it, because appending in place would silently drop the new elements.
func TestAppendEqualityHelpers(t *testing.T) {
	t.Run("string column", func(t *testing.T) {
		frag, args := appendEquality("pubkey", []string{"a", "b", "c"}, 1, nil)
		if frag != "pubkey IN ($1, $2, $3)" {
			t.Errorf("got %q", frag)
		}
		if len(args) != 3 {
			t.Fatalf("args not returned from the helper: got %d, want 3", len(args))
		}
		if args[0] != "a" || args[2] != "c" {
			t.Errorf("args out of order: %v", args)
		}
	})

	t.Run("int column", func(t *testing.T) {
		frag, args := appendEqualityInts("kind", []int{1, 7}, 5, nil)
		if frag != "kind IN ($5, $6)" {
			t.Errorf("got %q, want offsets applied from startArg", frag)
		}
		if len(args) != 2 {
			t.Fatalf("args not returned: got %d, want 2", len(args))
		}
		if args[0] != 1 || args[1] != 7 {
			t.Errorf("args wrong: %v", args)
		}
	})

	t.Run("appends to existing args", func(t *testing.T) {
		existing := []interface{}{"preexisting"}
		frag, args := appendEquality("id", []string{"x"}, 2, existing)
		if frag != "id IN ($2)" {
			t.Errorf("got %q", frag)
		}
		if len(args) != 2 || args[0] != "preexisting" {
			t.Errorf("existing args were dropped: %v", args)
		}
	})

	t.Run("single value still uses IN", func(t *testing.T) {
		// Special-casing to `= $1` would be equally fast, but one code path
		// cannot then regress independently of the other.
		frag, _ := appendEquality("pubkey", []string{"only"}, 1, nil)
		if frag != "pubkey IN ($1)" {
			t.Errorf("got %q, want a parameterised IN list", frag)
		}
	})
}
