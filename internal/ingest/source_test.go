package ingest_test

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/aarrico/gramwise/internal/ingest"
)

// Builds a zip mimicking USDA's layout (CSVs nested in a top-level folder)
// from the directory fixture, then parses through it.
func TestZipSource(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "fdc.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, name := range []string{"food.csv", "food_nutrient.csv"} {
		data, err := os.ReadFile(filepath.Join("testdata/fdc_small", name))
		if err != nil {
			t.Fatal(err)
		}
		w, err := zw.Create("FoodData_Central_csv/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	src, err := ingest.NewSource(t.Context(), zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = src.Close() }()

	res, err := ingest.Parse(t.Context(), src, fixtureDatasets)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Foods) != 4 {
		t.Errorf("got %d foods from zip, want 4", len(res.Foods))
	}
}
