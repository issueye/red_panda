package main

import (
	"context"
	"log"
	"os"

	"redpanda/gateway/internal/gateway/app"
	"redpanda/protocol/methods"
)

var version = "0.2.0"

func main() {
	cfg := app.Config{
		Addr:              env("RED_PANDA_GATEWAY_ADDR", "127.0.0.1:17888"),
		DatabaseDSN:       env("RED_PANDA_DATABASE", "red_panda.db"),
		Token:             os.Getenv("RED_PANDA_TOKEN"),
		Version:           version,
		SkillsDir:         os.Getenv(methods.EnvSkillsDir),
		AgentCommand:      os.Getenv("RED_PANDA_AGENT_COMMAND"),
		SessionArchiveDir: os.Getenv("RED_PANDA_SESSION_ARCHIVE_DIR"),
		AttachmentsDir:    os.Getenv("RED_PANDA_ATTACHMENTS_DIR"),
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
