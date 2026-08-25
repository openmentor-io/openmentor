package dbtest

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file supports one kind of test: take a table, blank ONE nullable column,
// and check that every read path over that table still works. pgx v5 fails the
// WHOLE row scan when a NULL reaches a non-pointer destination, so an
// un-COALESCEd nullable column makes the record unreadable — and for a multi-row
// read, takes every other row of the response down with it.
//
// The columns come from information_schema rather than a hand-written list, so a
// nullable column added by a future migration enters the test automatically.

// Column is a nullable column of the table under test.
type Column struct {
	Name string
	// UDT is information_schema.columns.udt_name, e.g. text, int4, timestamptz.
	UDT string
}

// columnFill produces a non-NULL value for a column of a given Postgres type.
// Values are passed as text and cast, so one shape covers every type.
type columnFill struct {
	cast  string
	value func(column, suffix string) string
}

// columnFills is keyed by udt_name. A column whose type is missing here fails
// the test with instructions: the enumeration is only honest if every nullable
// column really starts out non-NULL, so a silent skip would weaken every case.
var columnFills = map[string]columnFill{
	// Suffixed so values stay unique — several of these columns are UNIQUE.
	"text":        {cast: "text", value: func(column, suffix string) string { return column + "-" + suffix }},
	"citext":      {cast: "citext", value: func(_, suffix string) string { return suffix + "@example.test" }},
	"int4":        {cast: "int4", value: func(_, _ string) string { return "42" }},
	"int8":        {cast: "int8", value: func(_, _ string) string { return "42" }},
	"bool":        {cast: "bool", value: func(_, _ string) string { return "true" }},
	"timestamptz": {cast: "timestamptz", value: func(_, _ string) string { return "2020-01-02T03:04:05Z" }},
	"uuid":        {cast: "uuid", value: func(_, _ string) string { return "00000000-0000-0000-0000-0000000000ff" }},
}

// NullableColumns asks the database which of the table's columns accept NULL.
func NullableColumns(t *testing.T, pool *pgxpool.Pool, table string) []Column {
	t.Helper()

	// Generated columns are excluded even though they are nullable: they cannot
	// be written at all, so FillNullable's UPDATE and SetNull's `SET col = NULL`
	// would both fail on one ("column can only be updated to DEFAULT"), taking
	// every subtest down with the seed. The exclusion has a cost the caller must
	// know about: a generated column pulled into a mentor SELECT is still under
	// the D50 rule (nullable ⇒ COALESCE or pointer scan), and THIS enumeration
	// will not catch a miss on it — mentors.price_amount (000015) is the first
	// such column, and its derivation is pinned separately by
	// repository.TestPriceAmountDerivation.
	rows, err := pool.Query(context.Background(), `
		SELECT column_name, udt_name
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1 AND is_nullable = 'YES'
			AND is_generated = 'NEVER'
		ORDER BY column_name
	`, table)
	if err != nil {
		t.Fatalf("list nullable columns of %s: %v", table, err)
	}
	defer rows.Close()

	var columns []Column
	for rows.Next() {
		var c Column
		if err := rows.Scan(&c.Name, &c.UDT); err != nil {
			t.Fatalf("scan nullable column of %s: %v", table, err)
		}
		columns = append(columns, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list nullable columns of %s: %v", table, err)
	}
	if len(columns) == 0 {
		t.Fatalf("%s has no nullable columns; the test is looking at the wrong table", table)
	}
	return columns
}

// FillNullable populates every nullable column of the row, so that the only NULL
// left is the one a case blanks out on purpose. It returns the values written,
// keyed by column, so callers can look the row up by e.g. its email.
//
// overrides supplies a value for a column the generic per-type filler cannot
// satisfy — a CHECK constraint that only accepts an enum, for instance. It is
// keyed by column name and may be nil.
func FillNullable(
	t *testing.T, pool *pgxpool.Pool, table string, columns []Column, id any,
	suffix string, overrides map[string]string,
) map[string]string {

	t.Helper()

	written := make(map[string]string, len(columns))
	sets := make([]string, 0, len(columns))
	args := make([]any, 0, len(columns)+1)
	for _, c := range columns {
		fill, known := columnFills[c.UDT]
		if !known {
			t.Fatalf("%s.%s has type %q, which dbtest.columnFills cannot populate; "+
				"add an entry so this column is really tested", table, c.Name, c.UDT)
		}
		value := fill.value(c.Name, suffix)
		if override, ok := overrides[c.Name]; ok {
			value = override
		}
		written[c.Name] = value
		args = append(args, value)
		sets = append(sets, fmt.Sprintf("%s = $%d::text::%s",
			pgx.Identifier{c.Name}.Sanitize(), len(args), fill.cast))
	}
	args = append(args, id)

	//nolint:gosec // identifiers come from information_schema and are quoted;
	// the cast types are constants from columnFills.
	query := fmt.Sprintf("UPDATE %s SET %s WHERE id = $%d",
		pgx.Identifier{table}.Sanitize(), strings.Join(sets, ", "), len(args))
	if _, err := pool.Exec(context.Background(), query, args...); err != nil {
		t.Fatalf("fill nullable columns of %s: %v", table, err)
	}
	return written
}

// SetNull blanks a single column of a single row — the defect under test.
func SetNull(t *testing.T, pool *pgxpool.Pool, table, column string, id any) {
	t.Helper()

	//nolint:gosec // quoted identifiers from information_schema
	query := fmt.Sprintf("UPDATE %s SET %s = NULL WHERE id = $1",
		pgx.Identifier{table}.Sanitize(), pgx.Identifier{column}.Sanitize())
	if _, err := pool.Exec(context.Background(), query, id); err != nil {
		t.Fatalf("set %s.%s to NULL: %v", table, column, err)
	}
}
