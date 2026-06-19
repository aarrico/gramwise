package ingest_test

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/aarrico/gramwise/internal/ingest"
)

var fixtureDatasets = map[string]bool{
	"foundation_food": true,
	"sr_legacy_food":  true,
}

// mapSource serves in-memory CSV files keyed by name, for focused parser tests.
type mapSource map[string]string

func (m mapSource) Open(name string) (io.ReadCloser, error) {
	s, ok := m[name]
	if !ok {
		return nil, fmt.Errorf("%s not found", name)
	}
	return io.NopCloser(strings.NewReader(s)), nil
}

func (m mapSource) Name() string { return "mapSource" }
func (m mapSource) Close() error { return nil }

// oneGoodFood is a food.csv with a single complete foundation food.
const oneGoodFood = `"fdc_id","data_type","description"
"100001","foundation_food","Butter"
`

// completeMacros are the protein/fat/carbs rows that keep 100001 from being
// dropped as macro-incomplete. Callers append their malformed row(s).
const completeMacros = `"id","fdc_id","nutrient_id","amount"
"1","100001","1003","1"
"2","100001","1004","2"
"3","100001","1005","3"
`

func TestParseFixture(t *testing.T) {
	src, err := ingest.NewSource(t.Context(), "testdata/fdc_small")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = src.Close() }()

	res, err := ingest.Parse(t.Context(), src, fixtureDatasets)
	if err != nil {
		t.Fatal(err)
	}

	if res.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", res.Skipped)
	}
	if res.Malformed != 0 {
		t.Errorf("Malformed = %d, want 0", res.Malformed)
	}
	want := []ingest.FoodRow{
		{FDCID: 100001, Description: "Butter, salted", DatasetSource: "foundation_food", ProteinG: 0.85, CarbsG: 0.06, FatG: 81.11, Kcal: 717},
		{FDCID: 100002, Description: "Egg, whole, raw", DatasetSource: "foundation_food", ProteinG: 12.56, CarbsG: 0.72, FatG: 9.51, Kcal: 143},
		{FDCID: 100003, Description: "Chicken, breast, raw", DatasetSource: "sr_legacy_food", ProteinG: 22.5, CarbsG: 0, FatG: 2.5, Kcal: 112.5},
		{FDCID: 100004, Description: "Rice, white, cooked", DatasetSource: "sr_legacy_food", ProteinG: 2.69, CarbsG: 28.17, FatG: 0.28, Kcal: 130},
	}
	if len(res.Foods) != len(want) {
		t.Fatalf("got %d foods, want %d: %+v", len(res.Foods), len(want), res.Foods)
	}
	for i, w := range want {
		if res.Foods[i] != w {
			t.Errorf("food[%d] = %+v, want %+v", i, res.Foods[i], w)
		}
	}
}

// A non-integer fdc_id in food_nutrient.csv is dropped and counted, not silently
// skipped and not fatal.
func TestParseMalformedNutrientFdcID(t *testing.T) {
	src := mapSource{
		"food.csv":          oneGoodFood,
		"food_nutrient.csv": completeMacros + `"4","notanumber","1003","9"` + "\n",
	}

	res, err := ingest.Parse(t.Context(), src, fixtureDatasets)
	if err != nil {
		t.Fatalf("Parse returned error, want none: %v", err)
	}
	if res.Malformed != 1 {
		t.Errorf("Malformed = %d, want 1", res.Malformed)
	}
	if len(res.Foods) != 1 {
		t.Errorf("got %d foods, want 1: %+v", len(res.Foods), res.Foods)
	}
}

// A non-numeric amount is dropped and counted, not fatal (previously aborted the
// whole ingest).
func TestParseMalformedNutrientAmount(t *testing.T) {
	src := mapSource{
		"food.csv":          oneGoodFood,
		"food_nutrient.csv": completeMacros + `"4","100001","1008","not-a-float"` + "\n",
	}

	res, err := ingest.Parse(t.Context(), src, fixtureDatasets)
	if err != nil {
		t.Fatalf("Parse returned error, want none: %v", err)
	}
	if res.Malformed != 1 {
		t.Errorf("Malformed = %d, want 1", res.Malformed)
	}
	if len(res.Foods) != 1 {
		t.Errorf("got %d foods, want 1: %+v", len(res.Foods), res.Foods)
	}
}

// A non-integer fdc_id in food.csv is dropped and counted, not fatal (previously
// aborted the whole ingest).
func TestParseMalformedFoodFdcID(t *testing.T) {
	src := mapSource{
		"food.csv":          oneGoodFood + `"bad","foundation_food","Broken row"` + "\n",
		"food_nutrient.csv": completeMacros,
	}

	res, err := ingest.Parse(t.Context(), src, fixtureDatasets)
	if err != nil {
		t.Fatalf("Parse returned error, want none: %v", err)
	}
	if res.Malformed != 1 {
		t.Errorf("Malformed = %d, want 1", res.Malformed)
	}
	if len(res.Foods) != 1 {
		t.Errorf("got %d foods, want 1: %+v", len(res.Foods), res.Foods)
	}
}

// RowsRead counts every data record read across both files, including rows
// dropped by filters (it is the denominator for the malformed-rate guard).
func TestParseRowsRead(t *testing.T) {
	src := mapSource{
		"food.csv": `"fdc_id","data_type","description"
"100001","foundation_food","Butter"
"100009","branded_food","Filtered out, still counted"
`,
		"food_nutrient.csv": completeMacros,
	}

	res, err := ingest.Parse(t.Context(), src, fixtureDatasets)
	if err != nil {
		t.Fatal(err)
	}
	// food.csv: 2 data rows (incl. the filtered branded row); food_nutrient.csv: 3.
	if res.RowsRead != 5 {
		t.Errorf("RowsRead = %d, want 5", res.RowsRead)
	}
}

// A missing required column is a structural failure: fatal, naming the column.
func TestParseMissingColumn(t *testing.T) {
	tests := []struct {
		name    string
		files   mapSource
		wantCol string
	}{
		{
			name: "food.csv missing data_type",
			files: mapSource{
				"food.csv":          "\"fdc_id\",\"description\"\n\"100001\",\"Butter\"\n",
				"food_nutrient.csv": completeMacros,
			},
			wantCol: "data_type",
		},
		{
			name: "food_nutrient.csv missing amount",
			files: mapSource{
				"food.csv":          oneGoodFood,
				"food_nutrient.csv": "\"id\",\"fdc_id\",\"nutrient_id\"\n\"1\",\"100001\",\"1003\"\n",
			},
			wantCol: "amount",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ingest.Parse(t.Context(), tc.files, fixtureDatasets)
			if err == nil {
				t.Fatalf("Parse succeeded, want error for missing %q", tc.wantCol)
			}
			if !strings.Contains(err.Error(), tc.wantCol) {
				t.Errorf("error %q does not name missing column %q", err, tc.wantCol)
			}
		})
	}
}
