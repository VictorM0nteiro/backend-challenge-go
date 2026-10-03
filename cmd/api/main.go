package main

import (
	"errors"
	"io/fs"
	"log"
	"log/slog"
	"os"

	"github.com/VictorM0nteiro/backend-challenge-go/internal/composition"
	"github.com/joho/godotenv"
	"go.uber.org/fx"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if err := godotenv.Load(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Fatalf("load .env: %v", err)
	}

	// Run blocks until SIGINT or SIGTERM, then runs the OnStop hooks in reverse
	// order: HTTP server, SQS consumer, database pool.
	fx.New(composition.Options()...).Run()
}
