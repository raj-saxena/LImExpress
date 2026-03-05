package main

import (
	"log"

	"github.com/limexpress/gateway/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	cfg.LogSummary()
}
