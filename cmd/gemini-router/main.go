package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rahmandayub/gemini-router/internal/config"
	"github.com/rahmandayub/gemini-router/internal/key"
	"github.com/rahmandayub/gemini-router/internal/middleware"
	"github.com/rahmandayub/gemini-router/internal/proxy"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	cooldown := time.Duration(cfg.Gemini.CooldownSeconds) * time.Second
	pool := key.NewPool(cfg.Gemini.APIKeys, cooldown)
	defer pool.Stop()

	router := proxy.NewRouter(cfg.Gemini.BaseURL, pool)

	var authKeys []string
	if cfg.Auth.Enabled {
		authKeys = cfg.Auth.APIKeys
	}
	handler := middleware.Logging(middleware.Auth(authKeys, router))

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)

	srv := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	go func() {
		log.Printf("Starting Gemini Router on %s", addr)
		log.Printf("Loaded %d Gemini API keys", pool.Len())
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited gracefully")
}
