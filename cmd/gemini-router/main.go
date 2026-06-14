package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

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

	pool := key.NewPool(cfg.Gemini.APIKeys)
	router := proxy.NewRouter(cfg.Gemini.BaseURL, pool)

	handler := middleware.Logging(router)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("Starting Gemini Router on %s", addr)
	log.Printf("Loaded %d API keys", pool.Len())

	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
