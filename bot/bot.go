package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/gosub/nudgent/coach"
	"github.com/gosub/nudgent/lang"
	"github.com/gosub/nudgent/log"
	"github.com/gosub/nudgent/store"
)

const (
	maxGoalLen          = 777
	maxMessageLen       = 4000
	maxHistoryLen       = 20
	telegramPollTimeout = 60
	schedulerTickMin    = 1
)

var logger = log.Logger.With().Str("component", "bot").Logger()

type Config struct {
	AllowedUserID    int64
	DailyCheckinHour int
	Timezone         string
	Model            string
	Language         string
	Tone             string
	BotName          string
}

type Bot struct {
	api        *tgbotapi.BotAPI
	coach      coach.Coacher
	store      store.Storager
	cfg        Config
	compendium string
	loc        *time.Location
	send       func(chatID int64, text string)
	botName    string
}

func New(token string, c coach.Coacher, s store.Storager, cfg Config, compendium string) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("init telegram: %w", err)
	}

	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return nil, fmt.Errorf("load timezone: %w", err)
	}

	b := &Bot{
		api:        api,
		coach:      c,
		store:      s,
		cfg:        cfg,
		compendium: compendium,
		loc:        loc,
		botName:    api.Self.UserName,
	}
	b.send = b.sendMessage

	logger.Info().Str("account", api.Self.UserName).Msg("authorized on telegram")
	return b, nil
}

func (b *Bot) Run(ctx context.Context) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = telegramPollTimeout

	updates := b.api.GetUpdatesChan(u)

	go b.dailyScheduler(ctx)

	for {
		select {
		case <-ctx.Done():
			b.api.StopReceivingUpdates()
			logger.Debug().Msg("stopped receiving updates")
			return
		case update := <-updates:
			if update.Message == nil {
				continue
			}
			if update.Message.From.ID != b.cfg.AllowedUserID {
				logger.Debug().Int64("from", update.Message.From.ID).Msg("ignored message from unknown user")
				continue
			}
			b.handleMessage(ctx, update.Message)
		}
	}
}

func (b *Bot) handleMessage(ctx context.Context, msg *tgbotapi.Message) {
	text := msg.Text
	chatID := msg.Chat.ID

	l := logger.With().Int64("chat_id", chatID).Logger()

	if strings.HasPrefix(text, "/") {
		l.Debug().Str("text", text).Msg("handling command")
		b.handleCommand(ctx, chatID, text)
		return
	}

	l.Debug().Int("len", len(text)).Msg("handling chat message")
	b.handleChat(ctx, chatID, text)
}

func (b *Bot) handleChat(ctx context.Context, chatID int64, text string) {
	l := logger.With().Int64("chat_id", chatID).Logger()

	if ctx.Err() != nil {
		return
	}

	if len(text) > maxMessageLen {
		st, _ := b.store.EnsureState(ctx, b.cfg.AllowedUserID, b.cfg.Language, b.cfg.Tone)
		b.sendMessage(chatID, lang.Getf(st.Language, "message_too_long", maxMessageLen))
		return
	}

	st, err := b.store.EnsureState(ctx, b.cfg.AllowedUserID, b.cfg.Language, b.cfg.Tone)
	if err != nil {
		l.Error().Err(err).Msg("ensure state failed")
		b.sendMessage(chatID, "Something went wrong. Please try again.")
		return
	}

	goals, err := b.store.GetGoals(ctx, b.cfg.AllowedUserID)
	if err != nil {
		l.Error().Err(err).Msg("get goals failed")
		goals = []string{}
	}

	var rejections []string
	if err := json.Unmarshal([]byte(st.RejectionLog), &rejections); err != nil {
		rejections = []string{}
	}

	systemPrompt := coach.BuildSystemPrompt(
		b.compendium,
		st.Language,
		st.Tone,
		st.CurrentPhase,
		goals,
		rejections,
	)

	history, err := b.store.GetConversationHistory(ctx, b.cfg.AllowedUserID)
	if err != nil {
		l.Error().Err(err).Msg("get history failed")
		history = []map[string]string{}
	}

	l.Debug().Int("history_len", len(history)).Msg("calling coach")
	response, err := b.coach.Chat(ctx, systemPrompt, history, text)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		l.Error().Err(err).Msg("coach chat failed")
		b.sendMessage(chatID, "Sorry, I couldn't process that. Try again.")
		return
	}

	history = append(history, map[string]string{"role": "user", "content": text})
	history = append(history, map[string]string{"role": "assistant", "content": response})

	if len(history) > maxHistoryLen {
		history = history[len(history)-maxHistoryLen:]
	}

	if err := b.store.SetConversationHistory(ctx, b.cfg.AllowedUserID, history); err != nil {
		l.Error().Err(err).Msg("save history failed")
	}

	b.sendMessage(chatID, response)
}

func (b *Bot) sendMessage(chatID int64, text string) {
	if b.api != nil {
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = "Markdown"
		if _, err := b.api.Send(msg); err != nil {
			msg.ParseMode = ""
			if _, err2 := b.api.Send(msg); err2 != nil {
				logger.Error().Err(err2).Int64("chat_id", chatID).Msg("send message failed")
			}
		}
		return
	}
	if b.send != nil {
		b.send(chatID, text)
	}
}
