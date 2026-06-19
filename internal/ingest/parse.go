package ingest

import (
	"cmp"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"slices"
	"strconv"
)

const (
	fdcProteinID = "1003"
	fdcFatID     = "1004"
	fdcCarbsID   = "1005"
	fdcKcalID    = "1008"
)

type FoodRow struct {
	FDCID         int64
	Description   string
	DatasetSource string
	ProteinG      float64
	CarbsG        float64
	FatG          float64
	Kcal          float64
}

type ParseResult struct {
	Foods   []FoodRow
	Skipped int
	// Malformed counts rows dropped because a required field could not be
	// parsed (non-integer fdc_id, non-numeric amount). Missing columns and
	// truncated files are structural and fail the parse instead.
	Malformed int
	// RowsRead is every data record read across both CSVs (headers excluded,
	// filtered rows included) — the denominator for the malformed-rate guard.
	RowsRead int
}

type macros struct {
	protein, carbs, fat, kcal *float64
}

type foodMeta struct {
	description string
	dataset     string
	macros      macros
}

func Parse(ctx context.Context, src Source, datasets map[string]bool) (*ParseResult, error) {
	foods := map[int64]*foodMeta{}

	readFoods, mfFoods, err := parseFoods(ctx, src, datasets, foods)
	if err != nil {
		return nil, err
	}

	readNutrients, mfNutrients, err := parseNutrients(ctx, src, foods)
	if err != nil {
		return nil, err
	}

	res := &ParseResult{
		Malformed: mfFoods + mfNutrients,
		RowsRead:  readFoods + readNutrients,
	}
	for id, f := range foods {
		m := f.macros
		if m.protein == nil || m.fat == nil || m.carbs == nil {
			res.Skipped++
			continue
		}
		kcal := 4**m.protein + 4**m.carbs + 9**m.fat
		if m.kcal != nil {
			kcal = *m.kcal
		}

		res.Foods = append(res.Foods, FoodRow{
			FDCID:         id,
			Description:   f.description,
			DatasetSource: f.dataset,
			ProteinG:      *m.protein,
			CarbsG:        *m.carbs,
			FatG:          *m.fat,
			Kcal:          kcal,
		})
	}

	slices.SortFunc(res.Foods, func(a, b FoodRow) int { return cmp.Compare(a.FDCID, b.FDCID) })

	return res, nil
}

func openCSV(src Source, name string, required ...string) (io.ReadCloser, *csv.Reader, map[string]int, error) {
	rc, err := src.Open(name)
	if err != nil {
		return nil, nil, nil, err
	}

	r := csv.NewReader(rc)
	r.ReuseRecord = true

	header, err := r.Read()
	if err != nil {
		_ = rc.Close()
		return nil, nil, nil, fmt.Errorf("%s: read header: %w", name, err)
	}

	cols := make(map[string]int, len(header))
	for i, h := range header {
		cols[h] = i
	}

	for _, col := range required {
		if _, ok := cols[col]; !ok {
			_ = rc.Close()
			return nil, nil, nil, fmt.Errorf("%s: missing required column %q", name, col)
		}
	}

	return rc, r, cols, nil
}

func parseFoods(ctx context.Context, src Source, datasets map[string]bool, foods map[int64]*foodMeta) (rowsRead, malformed int, err error) {
	rc, r, cols, err := openCSV(src, "food.csv", "data_type", "fdc_id", "description")
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = rc.Close() }()

	for n := 0; ; n++ {
		if n&0x3ff == 0 {
			if err := ctx.Err(); err != nil {
				return rowsRead, malformed, err
			}
		}

		rec, err := r.Read()
		if err == io.EOF {
			return rowsRead, malformed, nil
		}
		if err != nil {
			return rowsRead, malformed, fmt.Errorf("food.csv: %w", err)
		}
		rowsRead++

		dataType := rec[cols["data_type"]]
		if !datasets[dataType] {
			continue
		}

		id, err := strconv.ParseInt(rec[cols["fdc_id"]], 10, 64)
		if err != nil {
			malformed++
			continue
		}
		foods[id] = &foodMeta{description: rec[cols["description"]], dataset: dataType}
	}
}

func parseNutrients(ctx context.Context, src Source, foods map[int64]*foodMeta) (rowsRead, malformed int, err error) {
	rc, r, cols, err := openCSV(src, "food_nutrient.csv", "fdc_id", "nutrient_id", "amount")
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = rc.Close() }()

	for n := 0; ; n++ {
		if n&0x3ff == 0 {
			if err := ctx.Err(); err != nil {
				return rowsRead, malformed, err
			}
		}

		rec, err := r.Read()
		if err == io.EOF {
			return rowsRead, malformed, nil
		}
		if err != nil {
			return rowsRead, malformed, fmt.Errorf("food_nutrient.csv: %w", err)
		}
		rowsRead++

		nutrientID := rec[cols["nutrient_id"]]
		if nutrientID != fdcProteinID && nutrientID != fdcFatID &&
			nutrientID != fdcCarbsID && nutrientID != fdcKcalID {
			continue
		}

		id, err := strconv.ParseInt(rec[cols["fdc_id"]], 10, 64)
		if err != nil {
			malformed++
			continue
		}
		food, ok := foods[id]
		if !ok {
			continue
		}

		raw := rec[cols["amount"]]
		if raw == "" {
			continue
		}
		amount, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			malformed++
			continue
		}

		var dst **float64
		switch nutrientID {
		case fdcProteinID:
			dst = &food.macros.protein
		case fdcCarbsID:
			dst = &food.macros.carbs
		case fdcFatID:
			dst = &food.macros.fat
		case fdcKcalID:
			dst = &food.macros.kcal
		}
		if *dst == nil { // first value wins across duplicate derivations
			v := amount
			*dst = &v
		}
	}
}
