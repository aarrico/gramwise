package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type Pinger interface {
	Ping(ctx context.Context) error
}

type Config struct {
	Logger *slog.Logger
	DB     Pinger
	Foods  FoodSearcher
}

func New(cfg Config) http.Handler {
	mux := http.NewServeMux()
	humaAPI := humago.New(mux, huma.DefaultConfig("Gramwise API", "0.1.0"))

	registerHello(humaAPI)
	registerHealth(humaAPI, cfg.DB)
	registerSearchFoods(humaAPI, cfg.Foods)

	var h http.Handler = mux
	h = logging(cfg.Logger, h)
	h = requestID(h)
	h = recoverPanics(cfg.Logger, h)

	return h
}

type helloOutput struct {
	Body struct {
		Message string `json:"message" example:"hello!" doc:"A greeting"`
	}
}

func registerHello(humaAPI huma.API) {
	huma.Register(humaAPI, huma.Operation{
		OperationID: "hello",
		Method:      http.MethodGet,
		Path:        "/v1/hello",
		Summary:     "Hello!",
	}, func(_ context.Context, _ *struct{}) (*helloOutput, error) {
		out := &helloOutput{}
		out.Body.Message = "hello!"
		return out, nil
	})
}

type healthOutput struct {
	Body struct {
		Status string `json:"status" example:"ok"`
	}
}

func registerHealth(humaAPI huma.API, db Pinger) {
	huma.Register(humaAPI, huma.Operation{
		OperationID: "health",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Health check, pings the database",
	}, func(ctx context.Context, _ *struct{}) (*healthOutput, error) {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			return nil, huma.Error503ServiceUnavailable("database unreachable", err)
		}
		out := &healthOutput{}
		out.Body.Status = "ok"
		return out, nil
	})
}
