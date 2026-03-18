package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"maxxx-agency/coach"
	"maxxx-agency/lang"
	"maxxx-agency/store"
)

const (
	maxGoalLen          = 777
	maxMessageLen       = 4000
	maxHistoryLen       = 20
	telegramPollTimeout = 60
	schedulerTickMin    = 1
)

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
	coach      *coach.Coach
	store      *store.Store
	cfg        Config
	compendium string
	loc        *time.Location
}

func New(token string, c *coach.Coach, s *store.Store, cfg Config, compendium string) (*Bot, error) {
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
	}

	log.Printf("Authorized on account %s", api.Self.UserName)
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
			return
		case update := <-updates:
			if update.Message == nil {
				continue
			}
			if update.Message.From.ID != b.cfg.AllowedUserID {
				continue
			}
			b.handleMessage(ctx, update.Message)
		}
	}
}

func (b *Bot) handleMessage(ctx context.Context, msg *tgbotapi.Message) {
	text := msg.Text
	chatID := msg.Chat.ID

	if strings.HasPrefix(text, "/") {
		b.handleCommand(ctx, chatID, text)
		return
	}

	b.handleChat(ctx, chatID, text)
}

func (b *Bot) handleCommand(ctx context.Context, chatID int64, text string) {
	parts := strings.Fields(text)
	cmd := strings.TrimPrefix(parts[0], "@"+b.api.Self.UserName)
	cmd = strings.ToLower(cmd)

	st, err := b.store.EnsureState(ctx, b.cfg.AllowedUserID, b.cfg.Language, b.cfg.Tone)
	if err != nil {
		log.Printf("ensure state: %v", err)
		b.send(chatID, "Something went wrong. Please try again.")
		return
	}

	var response string

	switch cmd {
	case "/start":
		response = lang.Get(st.Language, "welcome")

	case "/help":
		response = b.buildHelp(st.Language)

	case "/status":
		response = b.buildStatus(ctx, st)

	case "/rejection":
		count, err := b.store.AddRejection(ctx, b.cfg.AllowedUserID)
		if err != nil {
			log.Printf("add rejection: %v", err)
			b.send(chatID, "Could not log rejection. Please try again.")
			return
		}
		response = lang.Getf(st.Language, "rejection_logged", count)

	case "/goal":
		response = b.handleGoal(ctx, parts, st)

	case "/skip":
		if err := b.store.MarkCheckin(ctx, b.cfg.AllowedUserID); err != nil {
			log.Printf("mark checkin: %v", err)
			b.send(chatID, "Could not skip check-in. Please try again.")
			return
		}
		response = lang.Get(st.Language, "checkin_skipped")

	case "/lang":
		response = b.handleLang(ctx, parts, st)

	case "/tone":
		response = b.handleTone(ctx, parts, st)

	case "/reset":
		if err := b.store.SetConversationHistory(ctx, b.cfg.AllowedUserID, []map[string]string{}); err != nil {
			log.Printf("reset history: %v", err)
			b.send(chatID, "Could not reset context. Please try again.")
			return
		}
		response = "Conversation context cleared."

	default:
		return
	}

	b.send(chatID, response)
}

func (b *Bot) buildHelp(l string) string {
	return lang.Get(l, "help_header") + "\n" +
		lang.Get(l, "help_start") + "\n" +
		lang.Get(l, "help_status") + "\n" +
		lang.Get(l, "help_rejection") + "\n" +
		lang.Get(l, "help_goal") + "\n" +
		lang.Get(l, "help_goal_list") + "\n" +
		lang.Get(l, "help_goal_done") + "\n" +
		lang.Get(l, "help_skip") + "\n" +
		lang.Get(l, "help_lang") + "\n" +
		lang.Get(l, "help_tone") + "\n" +
		lang.Get(l, "help_reset") + "\n" +
		lang.Get(l, "help_help")
}

func (b *Bot) buildStatus(ctx context.Context, st *store.State) string {
	goals, err := b.store.GetGoals(ctx, b.cfg.AllowedUserID)
	if err != nil {
		log.Printf("get goals: %v", err)
		goals = []string{}
	}

	var rejections []string
	if err := json.Unmarshal([]byte(st.RejectionLog), &rejections); err != nil {
		rejections = []string{}
	}

	goalsStr := "none"
	if len(goals) > 0 {
		goalsStr = strings.Join(goals, ", ")
	}

	return lang.Get(st.Language, "status_header") + "\n" +
		lang.Getf(st.Language, "status_phase", st.CurrentPhase) + "\n" +
		lang.Getf(st.Language, "status_goals", goalsStr) + "\n" +
		lang.Getf(st.Language, "status_rejections", len(rejections)) + "\n" +
		lang.Getf(st.Language, "status_tone", st.Tone) + "\n" +
		lang.Getf(st.Language, "status_lang", st.Language)
}

func (b *Bot) handleGoal(ctx context.Context, parts []string, st *store.State) string {
	if len(parts) < 2 {
		return lang.Get(st.Language, "goal_none")
	}

	subCmd := strings.ToLower(parts[1])

	switch subCmd {
	case "add":
		if len(parts) < 3 {
			return "Usage: /goal add <goal text>"
		}
		goal := strings.Join(parts[2:], " ")
		if len(goal) > maxGoalLen {
			return lang.Getf(st.Language, "goal_too_long", maxGoalLen)
		}
		if err := b.store.AddGoal(ctx, b.cfg.AllowedUserID, goal); err != nil {
			log.Printf("add goal: %v", err)
			return "Error adding goal."
		}
		return lang.Getf(st.Language, "goal_added", goal)

	case "list":
		goals, err := b.store.GetGoals(ctx, b.cfg.AllowedUserID)
		if err != nil {
			log.Printf("list goals: %v", err)
			return "Error listing goals."
		}
		if len(goals) == 0 {
			return lang.Get(st.Language, "goal_none")
		}
		var sb strings.Builder
		sb.WriteString(lang.Get(st.Language, "goal_list") + "\n")
		for i, g := range goals {
			sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, g))
		}
		return sb.String()

	case "done":
		if len(parts) < 3 {
			return "Usage: /goal done <number>"
		}
		idx, err := strconv.Atoi(parts[2])
		if err != nil {
			return lang.Get(st.Language, "goal_invalid")
		}
		goals, err := b.store.GetGoals(ctx, b.cfg.AllowedUserID)
		if err != nil {
			log.Printf("get goals for done: %v", err)
			return "Error completing goal."
		}
		if idx < 1 || idx > len(goals) {
			return lang.Get(st.Language, "goal_invalid")
		}
		goalName := goals[idx-1]
		if err := b.store.CompleteGoal(ctx, b.cfg.AllowedUserID, idx-1); err != nil {
			log.Printf("complete goal: %v", err)
			return "Error completing goal."
		}
		return lang.Getf(st.Language, "goal_completed", goalName)

	default:
		return lang.Get(st.Language, "goal_none")
	}
}

func (b *Bot) handleLang(ctx context.Context, parts []string, st *store.State) string {
	if len(parts) < 2 {
		return lang.Getf(st.Language, "lang_current", st.Language)
	}

	newLang := strings.ToLower(parts[1])
	if !lang.IsValidLang(newLang) {
		return "Invalid language. Use: it, en"
	}

	if err := b.store.SetLanguage(ctx, b.cfg.AllowedUserID, newLang); err != nil {
		log.Printf("set language: %v", err)
		return "Error setting language."
	}
	return lang.Getf(newLang, "lang_switched", newLang)
}

func (b *Bot) handleTone(ctx context.Context, parts []string, st *store.State) string {
	if len(parts) < 2 {
		return lang.Getf(st.Language, "tone_current", st.Tone) + "\n" +
			lang.Get(st.Language, "tone_options")
	}

	newTone := strings.ToLower(parts[1])
	validTones := map[string]bool{"warm": true, "direct": true, "drill-sergeant": true}
	if !validTones[newTone] {
		return lang.Get(st.Language, "tone_options")
	}

	if err := b.store.SetTone(ctx, b.cfg.AllowedUserID, newTone); err != nil {
		log.Printf("set tone: %v", err)
		return "Error setting tone."
	}
	return lang.Getf(st.Language, "tone_switched", newTone)
}

func (b *Bot) handleChat(ctx context.Context, chatID int64, text string) {
	if len(text) > maxMessageLen {
		st, _ := b.store.EnsureState(ctx, b.cfg.AllowedUserID, b.cfg.Language, b.cfg.Tone)
		b.send(chatID, lang.Getf(st.Language, "message_too_long", maxMessageLen))
		return
	}

	st, err := b.store.EnsureState(ctx, b.cfg.AllowedUserID, b.cfg.Language, b.cfg.Tone)
	if err != nil {
		log.Printf("ensure state: %v", err)
		b.send(chatID, "Something went wrong. Please try again.")
		return
	}

	goals, err := b.store.GetGoals(ctx, b.cfg.AllowedUserID)
	if err != nil {
		log.Printf("get goals: %v", err)
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
		log.Printf("get history: %v", err)
		history = []map[string]string{}
	}

	response, err := b.coach.Chat(ctx, systemPrompt, history, text)
	if err != nil {
		if ctx.Err() != nil {
			return // context cancelled, don't send anything
		}
		log.Printf("coach chat: %v", err)
		b.send(chatID, "Sorry, I couldn't process that. Try again.")
		return
	}

	history = append(history, map[string]string{"role": "user", "content": text})
	history = append(history, map[string]string{"role": "assistant", "content": response})

	if len(history) > maxHistoryLen {
		history = history[len(history)-maxHistoryLen:]
	}

	if err := b.store.SetConversationHistory(ctx, b.cfg.AllowedUserID, history); err != nil {
		log.Printf("save history: %v", err)
	}

	b.send(chatID, response)
}

func (b *Bot) dailyScheduler(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(schedulerTickMin) * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now().In(b.loc)
			if now.Hour() != b.cfg.DailyCheckinHour || now.Minute() != 0 {
				continue
			}

			lastCheckin, err := b.store.GetLastCheckin(ctx, b.cfg.AllowedUserID)
			if err != nil {
				log.Printf("get last checkin: %v", err)
				continue
			}

			today := now.Format("2006-01-02")
			if lastCheckin == today {
				continue
			}

			st, err := b.store.EnsureState(ctx, b.cfg.AllowedUserID, b.cfg.Language, b.cfg.Tone)
			if err != nil {
				log.Printf("ensure state: %v", err)
				continue
			}

			dayNum := int(now.Sub(time.Date(2026, 1, 1, 0, 0, 0, 0, b.loc)).Hours()/24) + 1
			if dayNum < 1 {
				dayNum = 1
			}

			msg := lang.Getf(st.Language, "checkin_msg", dayNum)
			b.send(b.cfg.AllowedUserID, msg)

			if err := b.store.MarkCheckin(ctx, b.cfg.AllowedUserID); err != nil {
				log.Printf("mark checkin: %v", err)
			}
		}
	}
}

func (b *Bot) send(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	if _, err := b.api.Send(msg); err != nil {
		msg.ParseMode = ""
		if _, err2 := b.api.Send(msg); err2 != nil {
			log.Printf("send message: %v", err2)
		}
	}
}
