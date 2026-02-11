package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/batuhan/gomuks-beeper-api/internal/config"
	"github.com/batuhan/gomuks-beeper-api/internal/gomuksruntime"
	"github.com/batuhan/gomuks-beeper-api/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	runtime, err := gomuksruntime.New(cfg)
	if err != nil {
		log.Fatalf("failed to create runtime: %v", err)
	}
	if err = runtime.Start(context.Background()); err != nil {
		log.Fatalf("failed to start gomuks runtime: %v", err)
	}
	defer runtime.Stop()

	handler := server.New(cfg, runtime).Handler()
	httpServer := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: handler,
	}

	go func() {
		log.Printf("gomuks-beeper-api listening on http://%s", cfg.ListenAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server failed: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("failed to shutdown HTTP server cleanly: %v", err)
	}
}
