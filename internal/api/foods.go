package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/aarrico/gramwise/internal/db"
)

type FoodSearcher interface {
	SearchFoods(ctx context.Context, arg db.SearchFoodsParams) ([]db.SearchFoodsRow, error)
}

type searchFoodsInput struct {
	Query  string `query:"q" required:"true" minLength:"1" maxLength:"100" example:"chiken breast" doc:"Search text"`
	Limit  int    `query:"limit" default:"20" minimum:"1" maximum:"50" doc:"Max results per page"`
	Offset int    `query:"offset" default:"0" minimum:"0" doc:"Results to skip"`
}

type foodResult struct {
	FdcID         int64   `json:"fdc_id" example:"171077"`
	Description   string  `json:"description" example:"Chicken, broilers or fryers, breast, meat only, raw"`
	DatasetSource string  `json:"dataset_source" example:"sr_legacy_food"`
	ProteinG      float64 `json:"protein_g" example:"22.5"`
	CarbsG        float64 `json:"carbs_g" example:"0"`
	FatG          float64 `json:"fat_g" example:"2.62"`
	Kcal          float64 `json:"kcal" example:"120"`
}

type searchFoodsOutput struct {
	Body struct {
		Foods  []foodResult `json:"foods"`
		Total  int          `json:"total" doc:"Total matching foods across all pages"`
		Limit  int          `json:"limit"`
		Offset int          `json:"offset"`
	}
}

func registerSearchFoods(humaAPI huma.API, searcher FoodSearcher) {
	huma.Register(humaAPI, huma.Operation{
		OperationID: "searchFoods",
		Method:      http.MethodGet,
		Path:        "/v1/foods",
		Summary:     "Search for foods",
		Tags:        []string{"Foods"},
	}, func(ctx context.Context, input *searchFoodsInput) (*searchFoodsOutput, error) {
		rows, err := searcher.SearchFoods(ctx, db.SearchFoodsParams{
			Query:        input.Query,
			ResultLimit:  int32(input.Limit),
			ResultOffset: int32(input.Offset),
		})
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to search for foods", err)
		}

		output := &searchFoodsOutput{}
		output.Body.Foods = make([]foodResult, 0, len(rows))
		for _, row := range rows {
			output.Body.Foods = append(output.Body.Foods, foodResult{
				FdcID:         row.FdcID,
				Description:   row.Description,
				DatasetSource: row.DatasetSource,
				ProteinG:      row.ProteinG,
				CarbsG:        row.CarbsG,
				FatG:          row.FatG,
				Kcal:          row.Kcal,
			})
		}
		if len(rows) > 0 {
			output.Body.Total = int(rows[0].Total)
		}
		output.Body.Limit = input.Limit
		output.Body.Offset = input.Offset

		return output, nil
	})
}
