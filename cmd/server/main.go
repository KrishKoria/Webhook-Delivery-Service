package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/KrishKoria/Webhook-Delivery-Service/internal/cache"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/database"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/db"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/delivery"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/migrate"
	"github.com/KrishKoria/Webhook-Delivery-Service/internal/server"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

// version is set at build time: -ldflags "-X main.version=1.2.3".
var version = "dev"

func main() {
	_ = godotenv.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel()}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		err = runMigrate(ctx)
	} else {
		err = run(ctx, logger)
	}
	if err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func dbConfig() db.Config {
	return db.Config{URL: os.Getenv("TURSO_DATABASE_URL"), AuthToken: os.Getenv("TURSO_AUTH_TOKEN")}
}

// runMigrate applies migrations and exits; intended for a one-off job before deploys.
func runMigrate(ctx context.Context) error {
	conn, dialect, err := db.Open(ctx, dbConfig())
	if err != nil {
		return err
	}
	defer conn.Close()
	return migrate.Up(ctx, conn, dialect)
}

func run(ctx context.Context, logger *slog.Logger) error {
	port := envOr("PORT", "8080")
	slog.Info("starting webhook delivery service", "port", port, "db_local", db.IsLocalURL(os.Getenv("TURSO_DATABASE_URL")))

	if err := db.Init(ctx, dbConfig()); err != nil {
		return fmt.Errorf("init db: %w", err)
	}
	defer db.DB.Close()

	if envBool("MIGRATE_ON_START", true) {
		if err := migrate.Up(ctx, db.DB, db.Dialect); err != nil {
			return err
		}
	}

	subCache := newCache()
	queries := database.New(db.DB)

	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}
	router, err := server.NewRouter(server.Deps{
		Context:        ctx,
		Version:        version,
		StartTime:      time.Now(),
		DB:             db.DB,
		Queries:        queries,
		Cache:          subCache,
		Logger:         logger,
		TrustedProxies: splitList(os.Getenv("TRUSTED_PROXIES")),
	})
	if err != nil {
		return fmt.Errorf("build router: %w", err)
	}

	// Background workers stop when ctx is cancelled. Each Start returns only after
	// its in-flight work has finished (the delivery worker also releases tasks it
	// claimed but never started), so wg lets shutdown wait for them.
	// Config comes from env: WORKER_*, WEBHOOK_*, *_RETENTION_* (see package delivery).
	var wg sync.WaitGroup
	for _, start := range []func(context.Context){
		delivery.NewWorker(db.DB, subCache, delivery.LoadWorkerConfigFromEnv()).Start,
		delivery.NewCleanupWorker(db.DB, delivery.LoadCleanupConfigFromEnv()).Start,
		delivery.NewScheduledWorker(db.DB).Start,
	} {
		wg.Go(func() { start(ctx) })
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() {
		slog.Info("http server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("http server: %w", err)
		}
	case <-ctx.Done():
	}

	timeout := envDuration("SHUTDOWN_TIMEOUT", 9*time.Second)
	slog.Info("shutting down", "timeout", timeout)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("http server shutdown", "err", err)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
		slog.Info("workers stopped cleanly")
	case <-shutdownCtx.Done():
		slog.Warn("shutdown timeout reached before workers finished")
	}
	return nil
}

func newCache() *cache.RedisSubscriptionCache {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		slog.Info("REDIS_URL not set; running without subscription cache")
		return nil
	}
	c, err := cache.NewRedisSubscriptionCache(redisURL, 5*time.Minute)
	if err != nil {
		slog.Warn("redis unavailable; running without subscription cache", "err", err)
		return nil
	}
	return c
}

func logLevel() slog.Level {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(envOr("LOG_LEVEL", "INFO"))); err != nil {
		return slog.LevelInfo
	}
	return lvl
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	switch strings.ToLower(os.Getenv(key)) {
	case "1", "true", "yes":
		return true
	case "0", "false", "no":
		return false
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if d, err := time.ParseDuration(os.Getenv(key)); err == nil && d > 0 {
		return d
	}
	return def
}

func splitList(s string) []string {
	var out []string
	for p := range strings.SplitSeq(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
