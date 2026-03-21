package bot

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/rs/zerolog"

	"github.com/gosub/nudgent/agent"
	"github.com/gosub/nudgent/log"
	"github.com/gosub/nudgent/store"
)

const (
	maxMessageLen       = 4000
	telegramPollTimeout = 60
	minMsgInterval      = time.Second
)

type Config struct {
	AllowedUserID  int64
	NudgeIntervalM int
	Timezone       string
	Language       string
}

type Bot struct {
	api       *tgbotapi.BotAPI
	agent     agent.Agenter
	store     store.Storager
	cfg       Config
	loc       *time.Location
	send      func(chatID int64, text string)
	botName   string
	log       zerolog.Logger
	mu        sync.Mutex
	lastMsgAt time.Time
}

func New(token string, a agent.Agenter, s store.Storager, cfg Config) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}

	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return nil, err
	}

	b := &Bot{
		api:     api,
		agent:   a,
		store:   s,
		cfg:     cfg,
		loc:     loc,
		botName: api.Self.UserName,
		log:     log.Logger.With().Str("component", "bot").Logger(),
	}
	b.send = b.sendMessage

	b.log.Info().Str("account", api.Self.UserName).Msg("authorized on telegram")
	return b, nil
}

func (b *Bot) Run(ctx context.Context) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = telegramPollTimeout

	updates := b.api.GetUpdatesChan(u)
	go b.nudgeScheduler(ctx)

	for {
		select {
		case <-ctx.Done():
			b.api.StopReceivingUpdates()
			b.log.Debug().Msg("stopped receiving updates")
			return
		case update := <-updates:
			if update.Message == nil {
				continue
			}
			if update.Message.From.ID != b.cfg.AllowedUserID {
				b.log.Debug().Int64("from", update.Message.From.ID).Msg("ignored message from unknown user")
				continue
			}
			b.handleMessage(ctx, update.Message)
		}
	}
}

func (b *Bot) handleMessage(ctx context.Context, msg *tgbotapi.Message) {
	b.mu.Lock()
	since := time.Since(b.lastMsgAt)
	if since < minMsgInterval {
		b.mu.Unlock()
		b.log.Debug().Dur("since", since).Msg("rate limited message")
		return
	}
	b.lastMsgAt = time.Now()
	b.mu.Unlock()

	text := msg.Text
	chatID := msg.Chat.ID

	if len(text) > maxMessageLen {
		b.send(chatID, fmt.Sprintf("Message too long (max %d characters).", maxMessageLen))
		return
	}

	if strings.HasPrefix(text, "/") {
		b.handleCommand(ctx, chatID, text)
		return
	}

	b.handleChat(ctx, chatID, text)
}

func (b *Bot) sendMessage(chatID int64, text string) {
	if b.api != nil {
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = "Markdown"
		if _, err := b.api.Send(msg); err != nil {
			msg.ParseMode = ""
			if _, err2 := b.api.Send(msg); err2 != nil {
				b.log.Error().Err(err2).Int64("chat_id", chatID).Msg("send message failed")
			}
		}
	}
}
