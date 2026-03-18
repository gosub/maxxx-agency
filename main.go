package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/BurntSushi/toml"
	"github.com/joho/godotenv"

	"maxxx-agency/bot"
	"maxxx-agency/coach"
	"maxxx-agency/store"
)

type Config struct {
	TelegramTokenEnv string `toml:"telegram_token_env"`
	OpenRouterKeyEnv string `toml:"openrouter_key_env"`
	AllowedUserIDEnv string `toml:"allowed_user_id_env"`
	DailyCheckinHour int    `toml:"daily_checkin_hour"`
	Timezone         string `toml:"timezone"`
	Model            string `toml:"model"`
	Language         string `toml:"language"`
	Tone             string `toml:"tone"`
	BotName          string `toml:"bot_name"`
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: .env file not found: %v", err)
	}

	var cfg Config
	if _, err := toml.DecodeFile("config.toml", &cfg); err != nil {
		log.Fatalf("Failed to load config.toml: %v", err)
	}

	telegramToken := os.Getenv(cfg.TelegramTokenEnv)
	if telegramToken == "" {
		log.Fatalf("Missing env var: %s", cfg.TelegramTokenEnv)
	}

	openrouterKey := os.Getenv(cfg.OpenRouterKeyEnv)
	if openrouterKey == "" {
		log.Fatalf("Missing env var: %s", cfg.OpenRouterKeyEnv)
	}

	allowedUserIDStr := os.Getenv(cfg.AllowedUserIDEnv)
	if allowedUserIDStr == "" {
		log.Fatalf("Missing env var: %s", cfg.AllowedUserIDEnv)
	}
	allowedUserID, err := strconv.ParseInt(allowedUserIDStr, 10, 64)
	if err != nil {
		log.Fatalf("Invalid ALLOWED_USER_ID: %v", err)
	}
	if allowedUserID == 0 {
		log.Fatal("ALLOWED_USER_ID must be non-zero")
	}

	if cfg.DailyCheckinHour < 0 || cfg.DailyCheckinHour > 23 {
		log.Fatal("daily_checkin_hour must be 0-23")
	}

	validTones := map[string]bool{"warm": true, "direct": true, "drill-sergeant": true}
	if !validTones[cfg.Tone] {
		log.Fatalf("Invalid tone: %s (must be warm, direct, or drill-sergeant)", cfg.Tone)
	}

	compendium, err := os.ReadFile("AGENCY-COMPENDIUM.md")
	if err != nil {
		log.Fatalf("Failed to read AGENCY-COMPENDIUM.md: %v", err)
	}

	s, err := store.New("agency.db")
	if err != nil {
		log.Fatalf("Failed to init store: %v", err)
	}
	defer s.Close()

	// Ensure initial state
	_, err = s.EnsureState(context.Background(), allowedUserID, cfg.Language, cfg.Tone)
	if err != nil {
		log.Fatalf("Failed to ensure state: %v", err)
	}

	c := coach.New(openrouterKey, cfg.Model)

	b, err := bot.New(telegramToken, c, s, bot.Config{
		AllowedUserID:    allowedUserID,
		DailyCheckinHour: cfg.DailyCheckinHour,
		Timezone:         cfg.Timezone,
		Model:            cfg.Model,
		Language:         cfg.Language,
		Tone:             cfg.Tone,
		BotName:          cfg.BotName,
	}, string(compendium))
	if err != nil {
		log.Fatalf("Failed to init bot: %v", err)
	}

	fmt.Printf("Starting %s (ID: %d, Lang: %s, Tone: %s)\n",
		cfg.BotName, allowedUserID, cfg.Language, cfg.Tone)
	fmt.Printf("Daily check-in at %d:00 %s\n", cfg.DailyCheckinHour, cfg.Timezone)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go b.Run(ctx)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("\nShutting down...")
	cancel()
	fmt.Println("Goodbye!")
}
