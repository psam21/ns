package storage

import (
	"fmt"
	"sort"
	"strings"
	"time"

	nostr "github.com/nbd-wtf/go-nostr"
)

// Why these queries use `IN (...)` and not `= ANY(ARRAY[...])`
//
// This is not a style preference -- it is a 36,000x difference on the
// production data set. Measured on nostr.ltd 2026-10-02 against the events
// table (1,198,763 rows, btree index `events_pubkey_created_at` on
// (pubkey, created_at)):
//
//	pubkey = $1                       Index Scan ...          2.5 ms
//	pubkey IN ($1)                    Index Scan ...          2.6 ms
//	pubkey = ANY(ARRAY[$1]::text[])   Index Scan on a DIFFERENT
//	                                 index, 1,198,963 rows
//	                                 removed by filter ... 92,101 ms
//
// `= ANY(ARRAY[...])` is a set membership test against an expression, and
// PostgreSQL cannot drive a btree index from it. `IN (...)` expands to a
// sequence of scalar equalities, which the index can serve. The planner does
// not rewrite one into the other, so this is not a cost difference to be
// tuned -- it is the difference between an index scan and a full scan of the
// entire table.
//
// This is invisible without EXPLAIN. The query is correct, the results are
// correct, and it takes 92 seconds -- which exceeds the 5s timeout in
// GetEvents, so the client receives an empty result with no error anywhere.
// Author filters are the most common REQ shape after kind, so this was the
// single largest source of silently-empty responses.
//
// Every equality predicate in this file must use IN. `queries.go` has the
// same defect in the legacy builder and needs the same treatment.

// sortedKeys returns the keys of a set in a stable order.
//
// The compiled filters are maps, so ranging one directly produces a
// nondeterministic placeholder order. That is not merely untidy: it means two
// identical filters compile to different SQL strings, which defeats any
// statement-level plan caching and makes query logs impossible to compare.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sortedInts is sortedKeys for integer sets (kind).
func sortedInts(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

// appendEquality emits `column IN ($n, $n+1, ...)` for the given values,
// appending them to args, and returns the fragment.
//
// Single-element sets still go through IN rather than being special-cased to
// `=`: `pubkey IN ($1)` uses the same index as `pubkey = $1` (2.6ms vs 2.5ms),
// so one code path is both correct and fast.
func appendEquality(column string, values []string, startArg int, args []interface{}) (string, []interface{}) {
	placeholders := make([]string, 0, len(values))
	for i, v := range values {
		placeholders = append(placeholders, fmt.Sprintf("$%d", startArg+i))
		args = append(args, v)
	}
	return fmt.Sprintf("%s IN (%s)", column, strings.Join(placeholders, ", ")), args
}

// appendEqualityInts is appendEquality for integer columns (kind).
func appendEqualityInts(column string, values []int, startArg int, args []interface{}) (string, []interface{}) {
	placeholders := make([]string, 0, len(values))
	for i, v := range values {
		placeholders = append(placeholders, fmt.Sprintf("$%d", startArg+i))
		args = append(args, v)
	}
	return fmt.Sprintf("%s IN (%s)", column, strings.Join(placeholders, ", ")), args
}

// CompiledFilter represents a pre-compiled filter for efficient matching
type CompiledFilter struct {
	IDs     map[string]bool
	Authors map[string]bool
	Kinds   map[int]bool
	Since   *time.Time
	Until   *time.Time
	Tags    map[string]map[string]bool
	Limit   int
	Search  string
}

// CompileFilter pre-compiles a nostr filter for efficient matching
func CompileFilter(f nostr.Filter) *CompiledFilter {
	cf := &CompiledFilter{
		IDs:     make(map[string]bool),
		Authors: make(map[string]bool),
		Kinds:   make(map[int]bool),
		Tags:    make(map[string]map[string]bool),
		Limit:   f.Limit,
		Search:  f.Search,
	}

	// Set default limit of 500 if no limit specified
	if cf.Limit <= 0 {
		cf.Limit = 500
	}

	// Pre-compile IDs
	for _, id := range f.IDs {
		cf.IDs[id] = true
	}

	// Pre-compile Authors
	for _, author := range f.Authors {
		cf.Authors[author] = true
	}

	// Pre-compile Kinds
	for _, kind := range f.Kinds {
		cf.Kinds[kind] = true
	}

	// Set time bounds
	if f.Since != nil {
		t := f.Since.Time()
		cf.Since = &t
	}
	if f.Until != nil {
		t := f.Until.Time()
		cf.Until = &t
	}

	// Pre-compile Tags
	for tagName, tagValues := range f.Tags {
		cf.Tags[tagName] = make(map[string]bool)
		for _, value := range tagValues {
			cf.Tags[tagName][value] = true
		}
	}

	return cf
}

// GetBestIndex determines the most efficient index to use for the filter
func (cf *CompiledFilter) GetBestIndex() string {
	// If we have IDs, use the primary key index
	if len(cf.IDs) > 0 {
		return "id"
	}

	// If we have both authors and kinds, use the composite index
	if len(cf.Authors) > 0 && len(cf.Kinds) > 0 {
		return "pubkey_kind_created"
	}

	// Author-only filters get their own branch.
	//
	// These previously fell through to "created_at", whose query body is
	// `WHERE true` -- so the author predicate was never emitted and the relay
	// answered with recent events by *any* author. A client requesting its own
	// events received other people's, with correct status, valid frames, and
	// plausible-looking data.
	//
	// Author-only is not an edge case: profile fetches and author-scoped
	// timelines are among the most common REQ shapes there are, and this is
	// the shape every NIP-01 client uses to check whether an event landed.
	//
	// It cannot reuse "pubkey_kind_created", because that branch emits both a
	// pubkey and a kind predicate and would produce `kind = ANY(ARRAY[])` --
	// matching nothing.
	if len(cf.Authors) > 0 {
		return "pubkey"
	}

	// If we only have kinds, use the kind index
	if len(cf.Kinds) > 0 {
		return "kind_created"
	}

	// Default to created_at index
	return "created_at"
}

// BuildQuery constructs the SQL query using the most efficient index
func (cf *CompiledFilter) BuildQuery() (string, []interface{}, error) {
	query := strings.Builder{}
	args := make([]interface{}, 0, 10)
	argIndex := 1

	// frag holds the SQL returned by the equality helpers, which append to
	// args. The append must be written back to args by the caller: a helper
	// that appends to a slice it received by value silently loses those
	// elements, and the resulting query binds $1 to whatever was appended
	// later. That failure is silent -- the query is valid SQL and runs --
	// so helpers return the extended slice rather than trusting the caller to
	// remember.
	var frag string

	// Start with base SELECT
	query.WriteString(`SELECT id, pubkey, kind, created_at, content, tags, sig FROM events`)

	// Add WHERE clause based on best index
	switch cf.GetBestIndex() {
	case "id":
		// Primary key lookup.
		//
		// IN rather than = ANY(ARRAY[...]) -- see the note at the top of
		// this file. The id column is the primary key, so this one happened
		// to be cheap, but a special case nobody can verify is worse than
		// one consistent rule.
		ids := sortedKeys(cf.IDs)
		frag, args = appendEquality("id", ids, argIndex, args)
		query.WriteString(" WHERE " + frag)
		argIndex += len(ids)

	case "pubkey_kind_created":
		// Composite index for authors and kinds.
		//
		// IN rather than = ANY(ARRAY[...]) -- see the note at the top of
		// this file. This is the shape that produced the 92-second scan.
		authors := sortedKeys(cf.Authors)
		frag, args = appendEquality("pubkey", authors, argIndex, args)
		query.WriteString(" WHERE " + frag)
		argIndex += len(authors)

		kinds := sortedInts(cf.Kinds)
		frag, args = appendEqualityInts("kind", kinds, argIndex, args)
		query.WriteString(" AND " + frag)
		argIndex += len(kinds)

	case "pubkey":
		// Author filter with no kind constraint.
		//
		// Emits only the pubkey predicate. Deliberately not folded into
		// "pubkey_kind_created": with an empty kind set that branch would
		// build `kind = ANY(ARRAY[]::integer[])`, which matches nothing, so a
		// correct fix for the drop would otherwise become a filter that
		// returns nothing at all.
		authors := sortedKeys(cf.Authors)
		frag, args = appendEquality("pubkey", authors, argIndex, args)
		query.WriteString(" WHERE " + frag)
		argIndex += len(authors)

	case "kind_created":
		// Use kind index
		kinds := sortedInts(cf.Kinds)
		frag, args = appendEqualityInts("kind", kinds, argIndex, args)
		query.WriteString(" WHERE " + frag)
		argIndex += len(kinds)

	default:
		// Use created_at index
		//
		// Reached only when the filter has no selective dimension at all --
		// no ids, authors, or kinds. Time, tag and search predicates are
		// appended below and still apply.
		//
		// This branch is the reason the author bug was invisible: it is a
		// valid query, so nothing errored. Any new filter dimension must get
		// its own case above, or it will be dropped here exactly as authors
		// were. The assertion below makes that failure mode loud in
		// development rather than silent in production.
		if len(cf.IDs) > 0 || len(cf.Authors) > 0 || len(cf.Kinds) > 0 {
			return "", nil, fmt.Errorf(
				"filter has selective fields but matched no index case "+
					"(ids=%d authors=%d kinds=%d); this would silently drop them",
				len(cf.IDs), len(cf.Authors), len(cf.Kinds))
		}
		query.WriteString(" WHERE true")
	}

	// Add time filters
	if cf.Since != nil {
		query.WriteString(fmt.Sprintf(" AND created_at >= $%d", argIndex))
		args = append(args, cf.Since.Unix())
		argIndex++
	}
	if cf.Until != nil {
		query.WriteString(fmt.Sprintf(" AND created_at <= $%d", argIndex))
		args = append(args, cf.Until.Unix())
		argIndex++
	}

	// Add search filter if present
	// TODO(finding #11): ILIKE with leading wildcard prevents index usage.
	// Full fix requires PostgreSQL GIN trigram index (gin_trgm_ops) on content.
	if cf.Search != "" {
		query.WriteString(fmt.Sprintf(" AND content ILIKE $%d", argIndex))
		args = append(args, "%"+cf.Search+"%")
		argIndex++
	}

	// Add tag filters
	for tagName, tagValues := range cf.Tags {
		if len(tagValues) > 0 {
			query.WriteString(fmt.Sprintf(" AND tags @> $%d", argIndex))
			tagArray := make([][]string, len(tagValues))
			i := 0
			for value := range tagValues {
				tagArray[i] = []string{tagName, value}
				i++
			}
			args = append(args, tagArray)
			argIndex++
		}
	}

	// // Add ordering and limit - use DESC order to get newest events first
	// query.WriteString(" ORDER BY created_at DESC LIMIT $")
	// Add ordering and limit
	// Use ASC order for since-only filters to get oldest events since the timestamp
	// Use DESC order for all other cases to get newest events first
	if cf.Since != nil && cf.Until == nil {
		query.WriteString(" ORDER BY created_at ASC LIMIT $")
	} else {
		query.WriteString(" ORDER BY created_at DESC LIMIT $")
	}
	query.WriteString(fmt.Sprintf("%d", argIndex))
	args = append(args, cf.Limit)

	return query.String(), args, nil
}
