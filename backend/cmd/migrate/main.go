// Command migrate aplica (ou destrói) as migrations do banco — usado
// pelo Makefile e pelo entrypoint Docker (AUTO_MIGRATE).
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"monitor/internal/migrate"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	dsn := flag.String("dsn", os.Getenv("DB_DSN"), "PostgreSQL DSN")
	down := flag.Bool("down", false, "destroy schema (make migrate-down)")
	flag.Parse()

	if *dsn == "" {
		log.Error("DB_DSN é obrigatório")
		os.Exit(1)
	}
	ctx := context.Background()
	var err error
	if *down {
		err = migrate.DropAll(ctx, *dsn)
	} else {
		err = migrate.Run(ctx, *dsn)
	}
	if err != nil {
		log.Error("migrate falhou", "err", err)
		os.Exit(1)
	}
	log.Info("migrate concluído", "down", *down)
}