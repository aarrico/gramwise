//go:build integration

package api_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aarrico/gramwise/internal/api"
	"github.com/aarrico/gramwise/internal/db"
	"github.com/aarrico/gramwise/internal/dbtest"
)

func TestSearchFoodsEndToEnd(t *testing.T) {
	pool := dbtest.NewPool(t)
	dbtest.SeedFoods(t, pool)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := api.New(api.Config{Logger: logger, DB: pool, Foods: db.New(pool)})

	get := func(target string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		return rec
	}

	type result struct {
		Foods []struct {
			FdcID       int64  `json:"fdc_id"`
			Description string `json:"description"`
		} `json:"foods"`
		Total int `json:"total"`
	}

	t.Run("relevance through the router", func(t *testing.T) {
		rec := get("/v1/foods?q=chicken%20breast")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var body result
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Foods) == 0 || body.Foods[0].FdcID != 100001 {
			t.Errorf("top result = %+v, want 100001 (breast) first", body.Foods)
		}
	})

	t.Run("fuzzy through the router", func(t *testing.T) {
		rec := get("/v1/foods?q=chiken")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body result
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Foods) != 3 {
			t.Fatalf("got %d results for 'chiken', want 3", len(body.Foods))
		}
		for _, f := range body.Foods {
			if !strings.Contains(f.Description, "Chicken") {
				t.Errorf("unexpected fuzzy result %q", f.Description)
			}
		}
	})

	t.Run("missing q is rejected", func(t *testing.T) {
		if rec := get("/v1/foods"); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want 422", rec.Code)
		}
	})
}
