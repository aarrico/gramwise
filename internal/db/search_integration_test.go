//go:build integration

package db_test

import (
	"context"
	"strings"
	"testing"

	"github.com/aarrico/gramwise/internal/db"
	"github.com/aarrico/gramwise/internal/dbtest"
)

func TestSearchFoods(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	dbtest.SeedFoods(t, pool)
	q := db.New(pool)

	search := func(query string, limit, offset int32) []db.SearchFoodsRow {
		t.Helper()
		rows, err := q.SearchFoods(ctx, db.SearchFoodsParams{
			Query:        query,
			ResultLimit:  limit,
			ResultOffset: offset,
		})
		if err != nil {
			t.Fatalf("SearchFoods(%q): %v", query, err)
		}
		return rows
	}

	t.Run("relevance ranks best match first", func(t *testing.T) {
		rows := search("chicken breast", 10, 0)
		if len(rows) == 0 || rows[0].FdcID != 100001 {
			t.Fatalf("top result = %+v, want fdc_id 100001 (breast) first", rows)
		}

		for _, r := range rows {
			if r.FdcID == 100004 {
				t.Errorf("broccoli (100004) should not match 'chicken breast'")
			}
		}
	})

	t.Run("misspelling matches via trigram", func(t *testing.T) {
		rows := search("chiken", 10, 0)
		if len(rows) != 3 {
			t.Fatalf("got %d rows for fuzzy 'chiken', want 3 chicken rows", len(rows))
		}
		for _, r := range rows {
			if !strings.Contains(r.Description, "Chicken") {
				t.Errorf("unexpected row %q for fuzzy 'chiken'", r.Description)
			}
		}
	})

	t.Run("macros come back as floats", func(t *testing.T) {
		rows := search("chicken breast", 1, 0)
		if rows[0].ProteinG != 31 {
			t.Errorf("protein = %v, want 31", rows[0].ProteinG)
		}
	})

	t.Run("pagination slices and total stays constant", func(t *testing.T) {
		page1 := search("chicken", 2, 0)
		page2 := search("chicken", 2, 2)
		if len(page1) != 2 || len(page2) != 1 {
			t.Fatalf("page sizes = %d, %d; want 2, 1", len(page1), len(page2))
		}
		if page1[0].Total != 3 || page2[0].Total != 3 {
			t.Errorf("totals = %d, %d; want 3, 3", page1[0].Total, page2[0].Total)
		}
		if page1[0].FdcID == page2[0].FdcID {
			t.Errorf("page2 repeated a page1 row: %d", page2[0].FdcID)
		}
	})

	t.Run("no matches returns empty", func(t *testing.T) {
		if rows := search("zucchini", 10, 0); len(rows) != 0 {
			t.Errorf("got %d rows for 'zucchini', want 0", len(rows))
		}
	})
}
