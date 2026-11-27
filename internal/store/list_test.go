package store

import (
	"math"
	"slices"
	"strings"
	"testing"
	"time"
)

func checkList(t testing.TB, d *DB, p ListParams, wantTotal int, want ...string) {
	t.Helper()
	rows, total, err := d.List(t.Context(), p)
	if err != nil {
		t.Fatalf("List(%+v): %v", p, err)
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	if total != wantTotal || !slices.Equal(ids, want) {
		t.Errorf("List(%+v) = (%v, %d), want (%v, %d)", p, ids, total, want, wantTotal)
	}
}

func TestList_SortingAndPagination(t *testing.T) {
	d := openStore(t, t.TempDir())
	a, b, c, p := fixture("alpha"), fixture("bravo"), fixture("charlie"), fixture("permanent")
	b.CreatedAt, c.CreatedAt = base.Add(1), base.Add(-1)
	b.ExpiresAt, c.ExpiresAt = base.Add(1), a.ExpiresAt
	p.CreatedAt, p.ExpiresAt, p.Permanent = a.CreatedAt, base.Add(-time.Hour), true
	seed(t, d, c, p, b, a)

	for _, tc := range []struct {
		sort, order string
		want        []string
	}{
		{"name", "asc", []string{"alpha", "bravo", "charlie", "permanent"}},
		{"name", "desc", []string{"permanent", "charlie", "bravo", "alpha"}},
		{"created", "asc", []string{"charlie", "alpha", "permanent", "bravo"}},
		{"created", "desc", []string{"bravo", "alpha", "permanent", "charlie"}},
		{"expires", "asc", []string{"bravo", "alpha", "charlie", "permanent"}},
		{"expires", "desc", []string{"alpha", "charlie", "bravo", "permanent"}},
	} {
		t.Run(tc.sort+"/"+tc.order, func(t *testing.T) {
			checkList(t, d, ListParams{Page: 1, PerPage: 8, Sort: tc.sort, Order: tc.order}, 4, tc.want...)
		})
	}
	for _, tc := range []struct {
		page int
		want []string
	}{
		{1, []string{"alpha", "bravo", "charlie"}},
		{2, []string{"permanent"}},
		{3, nil},
		{math.MaxInt, nil},
	} {
		checkList(t, d, ListParams{Page: tc.page, PerPage: 3, Sort: "name", Order: "asc"}, 4, tc.want...)
	}
	// Bad options fall back to the documented defaults, not raw SQL.
	checkList(t, d, ListParams{Page: -1, PerPage: 201, Sort: "unknown", Order: "unknown"}, 4, "bravo", "alpha", "permanent", "charlie")
}

func TestList_Search(t *testing.T) {
	d := openStore(t, t.TempDir())
	for _, id := range []string{"b%o", "b_o", `b\o`, "Bravo", "shadow"} {
		seed(t, d, fixture(id))
	}
	for q, want := range map[string]string{"%": "b%o", "_": "b_o", `\`: `b\o`, "AVO": "Bravo"} {
		checkList(t, d, ListParams{Query: q}, 1, want)
	}
	checkList(t, d, ListParams{Query: "O", Sort: "name", Order: "asc", Page: 2, PerPage: 2}, 5, `b\o`, "b_o")
	checkList(t, d, ListParams{Query: "missing"}, 0)
}

func FuzzLikeEscape(f *testing.F) {
	for _, seed := range [][2]string{{"test_Pass", "_pa"}, {`one\two`, `\t`}, {"score%", "%"}, {"plain", "%"}, {"both", ""}} {
		f.Add(seed[0], seed[1])
	}
	// Use the real LIKE expression and escaper, but avoid filesystem churn
	// and row accumulation by binding the candidate ID directly.
	d := openStore(f, f.TempDir())
	ascii := func(s string) bool {
		for i := 0; i < len(s); i++ {
			if s[i] < ' ' || s[i] > '~' {
				return false
			}
		}
		return true
	}
	f.Fuzz(func(t *testing.T, id, q string) {
		if len(id) > 256 || len(q) > 256 || !ascii(id) || !ascii(q) {
			return
		}
		var got bool
		err := d.db.QueryRowContext(t.Context(), "SELECT "+literalSearch+" FROM (SELECT ? AS id)", escapeLike.Replace(q), id).Scan(&got)
		want := strings.Contains(strings.ToLower(id), strings.ToLower(q))
		if err != nil || got != want {
			t.Fatalf("LIKE id=%q query=%q: (%v, %v), want %v", id, q, got, err, want)
		}
	})
}
