package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/BurntSushi/toml"
	"github.com/joho/godotenv"

	"github.com/gosub/nudgent/bot"
	"github.com/gosub/nudgent/coach"
	"github.com/gosub/nudgent/log"
	"github.com/gosub/nudgent/store"
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
	logMode := os.Getenv("LOG_FORMAT") != "json"
	log.Init(logMode)
	logger := log.Logger.With().Str("component", "main").Logger()

	if err := godotenv.Load(); err != nil {
		logger.Warn().Err(err).Msg("no .env file found")
	}

	var cfg Config
	if _, err := toml.DecodeFile("config.toml", &cfg); err != nil {
		logger.Fatal().Err(err).Msg("failed to load config.toml")
	}

	telegramToken := os.Getenv(cfg.TelegramTokenEnv)
	if telegramToken == "" {
		logger.Fatal().Str("var", cfg.TelegramTokenEnv).Msg("missing env var")
	}

	openrouterKey := os.Getenv(cfg.OpenRouterKeyEnv)
	if openrouterKey == "" {
		logger.Fatal().Str("var", cfg.OpenRouterKeyEnv).Msg("missing env var")
	}

	allowedUserIDStr := os.Getenv(cfg.AllowedUserIDEnv)
	if allowedUserIDStr == "" {
		logger.Fatal().Str("var", cfg.AllowedUserIDEnv).Msg("missing env var")
	}
	allowedUserID, err := strconv.ParseInt(allowedUserIDStr, 10, 64)
	if err != nil {
		logger.Fatal().Err(err).Msg("invalid ALLOWED_USER_ID")
	}
	if allowedUserID == 0 {
		logger.Fatal().Msg("ALLOWED_USER_ID must be non-zero")
	}

	if cfg.DailyCheckinHour < 0 || cfg.DailyCheckinHour > 23 {
		logger.Fatal().Int("hour", cfg.DailyCheckinHour).Msg("daily_checkin_hour must be 0-23")
	}

	validTones := map[string]bool{"warm": true, "direct": true, "drill-sergeant": true}
	if !validTones[cfg.Tone] {
		logger.Fatal().Str("tone", cfg.Tone).Msg("invalid tone")
	}

	compendium, err := os.ReadFile("AGENCY-COMPENDIUM.md")
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to read AGENCY-COMPENDIUM.md")
	}

	s, err := store.New("agency.db")
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to init store")
	}
	defer s.Close()

	if _, err = s.EnsureState(context.Background(), allowedUserID, cfg.Language, cfg.Tone); err != nil {
		logger.Fatal().Err(err).Msg("failed to ensure state")
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
		logger.Fatal().Err(err).Msg("failed to init bot")
	}

	logger.Info().
		Str("bot", cfg.BotName).
		Int64("user_id", allowedUserID).
		Str("lang", cfg.Language).
		Str("tone", cfg.Tone).
		Int("checkin_hour", cfg.DailyCheckinHour).
		Str("timezone", cfg.Timezone).
		Str("model", cfg.Model).
		Msg("starting")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go b.Run(ctx)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit

	logger.Info().Str("signal", sig.String()).Msg("shutting down")
	cancel()

	if ce := logger.Trace(); ce.Enabled() {
		// small delay to let context cancellation propagate
		ce.Msg("shutdown complete")
	}
	fmt.Println("Goodbye!")
}
