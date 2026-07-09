package main

import (
	"context"
	"log"
	"os"

	"redpanda/gateway/internal/gateway/app"
)

var version = "dev"

func main() {
	cfg := app.Config{
		Addr:         env("RED_PANDA_GATEWAY_ADDR", "127.0.0.1:17888"),
		DatabaseDSN:  env("RED_PANDA_DATABASE", "red_panda.db"),
		Token:        os.Getenv("RED_PANDA_TOKEN"),
		Version:      version,
		AgentCommand: os.Getenv("RED_PANDA_AGENT_COMMAND"),
	}
	if err := app.Run(context.Background(), cfg); err != nil {
		log.Fatal(err)
	}
}

func env(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
