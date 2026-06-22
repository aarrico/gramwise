package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aarrico/gramwise/internal/api"
	"github.com/aarrico/gramwise/internal/db"
)

type fakeSearcher struct {
	rows    []db.SearchFoodsRow
	err     error
	lastArg db.SearchFoodsParams
}

func (f *fakeSearcher) SearchFoods(_ context.Context, arg db.SearchFoodsParams) ([]db.SearchFoodsRow, error) {
	f.lastArg = arg
	return f.rows, f.err
}

func newSearchHandler(t *testing.T, s api.FoodSearcher) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return api.New(api.Config{Logger: logger, DB: fakePinger{}, Foods: s})
}

func TestSearchFoods_OK(t *testing.T) {
	fake := &fakeSearcher{rows: []db.SearchFoodsRow{
		{FdcID: 100001, Description: "Chicken, breast, raw", DatasetSource: "sr_legacy_food", ProteinG: 31, CarbsG: 0, FatG: 3.6, Kcal: 165, Total: 2},
		{FdcID: 100002, Description: "Chicken, thigh, raw", DatasetSource: "sr_legacy_food", ProteinG: 26, CarbsG: 0, FatG: 11, Kcal: 209, Total: 2},
	}}
	h := newSearchHandler(t, fake)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/foods?q=chicken", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Foods []struct {
			FdcID    int64   `json:"fdc_id"`
			ProteinG float64 `json:"protein_g"`
		} `json:"foods"`
		Total  int `json:"total"`
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Foods) != 2 || body.Foods[0].FdcID != 100001 || body.Foods[0].ProteinG != 31 {
		t.Errorf("foods = %+v, want 2 rows led by 100001/protein 31", body.Foods)
	}
	if body.Total != 2 || body.Limit != 20 || body.Offset != 0 {
		t.Errorf("meta = total %d limit %d offset %d; want 2/20/0", body.Total, body.Limit, body.Offset)
	}
	if fake.lastArg.Query != "chicken" || fake.lastArg.ResultLimit != 20 || fake.lastArg.ResultOffset != 0 {
		t.Errorf("query args = %+v; want q=chicken limit=20 offset=0", fake.lastArg)
	}
}

func TestSearchFoods_CustomPaging(t *testing.T) {
	fake := &fakeSearcher{}
	h := newSearchHandler(t, fake)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/foods?q=beef&limit=5&offset=10", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if fake.lastArg.ResultLimit != 5 || fake.lastArg.ResultOffset != 10 {
		t.Errorf("paging args = limit %d offset %d; want 5/10", fake.lastArg.ResultLimit, fake.lastArg.ResultOffset)
	}
}

func TestSearchFoods_EmptyResultsSerializesAsArray(t *testing.T) {
	h := newSearchHandler(t, &fakeSearcher{rows: nil})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/foods?q=zzz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"foods":[]`) {
		t.Errorf("empty result should serialize foods as [], got %s", rec.Body.String())
	}
}

func TestSearchFoods_Validation(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"missing q", "/v1/foods"},
		{"empty q", "/v1/foods?q="},
		{"limit too low", "/v1/foods?q=x&limit=0"},
		{"limit too high", "/v1/foods?q=x&limit=99"},
		{"negative offset", "/v1/foods?q=x&offset=-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newSearchHandler(t, &fakeSearcher{})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.url, nil))
			if rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestSearchFoods_Error(t *testing.T) {
	h := newSearchHandler(t, &fakeSearcher{err: errors.New("boom")})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/foods?q=chicken", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}
