package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wwpp/finaudit/internal/api"
	"github.com/wwpp/finaudit/internal/config"
	"github.com/wwpp/finaudit/internal/storage/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config load failed", "err", err)
		os.Exit(1)
	}

	var store *postgres.Store
	if cfg.PostgresDSN != "" {
		deadline := time.Now().Add(60 * time.Second)
		for attempt := 1; ; attempt++ {
			s, err := postgres.NewStore(context.Background(), cfg.PostgresDSN)
			if err == nil {
				store = s
				defer store.Close()
				logger.Info("postgres connected", "attempt", attempt)
				break
			}
			if time.Now().After(deadline) {
				logger.Error("postgres unavailable after retries, auth endpoints will return 503", "err", err, "attempts", attempt)
				break
			}
			logger.Warn("postgres not ready, retrying in 2s", "err", err, "attempt", attempt)
			time.Sleep(2 * time.Second)
		}
	} else {
		logger.Warn("POSTGRES_DSN is empty, auth endpoints will return 503")
	}

	router := api.NewRouter(logger, cfg, store)

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: router}

	go func() {
		logger.Info("server starting", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	logger.Info("server stopped")
}
