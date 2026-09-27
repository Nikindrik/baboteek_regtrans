package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"baboteek_regtrans/config"
	_ "baboteek_regtrans/docs"
	"baboteek_regtrans/internal/app"
	"baboteek_regtrans/pkg/logger"
)

// @title           Moscow Transport Delay Prediction API
// @version         1.0
// @description     Real-time public transit schedule deviation predictor & fleet telemetry gateway.
// @contact.name    Hackathon Team Baboteek
// @host            localhost:8080
// @BasePath        /
func main() {
	cfg, err := config.Get()
	if err != nil {
		fmt.Printf("FATAL: cannot load config: %v\n", err)
		os.Exit(1)
	}

	isDev := cfg.App.Environment == "development"
	log := logger.New(cfg.App.LogLevel, isDev)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	application, err := app.New(cfg, log)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to bootstrap application")
	}

	go func() {
		if err := application.Run(ctx); err != nil {
			log.Fatal().Err(err).Msg("application runtime error")
		}
	}()

	log.Info().
		Int("http_port", cfg.HTTP.Port).
		Int("ndtp_tcp_port", cfg.NDTP.TCPPort).
		Str("swagger_url", fmt.Sprintf("http://localhost:%d/swagger/index.html", cfg.HTTP.Port)).
		Msg(">> SYSTEM READY: NDTP, REST, WEBSOCKET ACTIVE")

	<-ctx.Done()
	log.Warn().Msg("shutting down application...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	application.Shutdown(shutdownCtx)
	log.Info().Msg("server exited cleanly")
}
