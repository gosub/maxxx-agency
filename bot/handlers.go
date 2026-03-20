package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gosub/nudgent/agent"
	"github.com/gosub/nudgent/lang"
)

func (b *Bot) handleCommand(ctx context.Context, chatID int64, text string) {
	parts := strings.Fields(text)
	cmd := strings.ToLower(strings.TrimPrefix(parts[0], "@"+b.botName))

	switch cmd {
	case "/tasks":
		b.send(chatID, b.buildTaskList(ctx))
	case "/debug":
		b.send(chatID, b.buildDebug(ctx))
	case "/help":
		p, _ := b.store.GetPrefs(ctx, b.cfg.AllowedUserID)
		l := "en"
		if p != nil {
			l = p.Language
		}
		b.send(chatID, lang.Get(l, "help"))
	default:
		// unknown commands are silently ignored
	}
}

func (b *Bot) buildTaskList(ctx context.Context) string {
	p, _ := b.store.GetPrefs(ctx, b.cfg.AllowedUserID)
	l := "en"
	if p != nil {
		l = p.Language
	}

	tasks, err := b.store.GetTasks(ctx, b.cfg.AllowedUserID)
	if err != nil {
		logger.Error().Err(err).Msg("get tasks failed")
		return "Error loading tasks."
	}
	if len(tasks) == 0 {
		return lang.Get(l, "tasks_empty")
	}

	var sb strings.Builder
	sb.WriteString(lang.Get(l, "tasks_header") + "\n")
	for i, t := range tasks {
		nudge := ""
		if t.NextNudgeAt != "" {
			nudge = " — " + t.NextNudgeAt
		}
		sb.WriteString(fmt.Sprintf("  %d. %s%s\n", i+1, t.Description, nudge))
	}
	return sb.String()
}

func (b *Bot) handleChat(ctx context.Context, chatID int64, text string) {
	if ctx.Err() != nil {
		return
	}

	p, err := b.store.EnsurePrefs(ctx, b.cfg.AllowedUserID, b.cfg.Language, b.cfg.NudgeIntervalM)
	if err != nil {
		logger.Error().Err(err).Msg("ensure prefs failed")
		b.send(chatID, "Something went wrong. Please try again.")
		return
	}

	tasks, err := b.store.GetTasks(ctx, b.cfg.AllowedUserID)
	if err != nil {
		logger.Error().Err(err).Msg("get tasks failed")
		tasks = nil
	}

	prompt := agent.BuildChatPrompt(p.Language, p.Schedule, tasks, time.Now().In(b.loc))
	logger.Trace().Str("prompt", prompt).Msg("chat system prompt")
	raw, err := b.agent.Chat(ctx, prompt, text)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		logger.Error().Err(err).Msg("agent chat failed")
		b.send(chatID, "Sorry, I couldn't process that. Try again.")
		return
	}

	logger.Debug().Str("raw", raw).Msg("agent response")

	resp, err := agent.ParseResponse(raw)
	if err != nil {
		logger.Warn().Err(err).Str("raw", raw).Msg("failed to parse agent response, sending raw")
		b.send(chatID, raw)
		return
	}

	logger.Debug().Int("actions", len(resp.Actions)).Msg("executing actions")
	if err := b.executeActions(ctx, resp.Actions); err != nil {
		logger.Error().Err(err).Msg("execute actions failed")
	}

	if resp.Reply != "" {
		b.send(chatID, resp.Reply)
	}
}

func (b *Bot) buildDebug(ctx context.Context) string {
	var sb strings.Builder

	p, err := b.store.GetPrefs(ctx, b.cfg.AllowedUserID)
	if err != nil {
		return fmt.Sprintf("error loading prefs: %v", err)
	}
	if p == nil {
		sb.WriteString("prefs: not initialized\n")
	} else {
		sb.WriteString(fmt.Sprintf("language: %s\n", p.Language))
		sb.WriteString(fmt.Sprintf("nudge_interval_m: %d\n", p.NudgeIntervalM))
		sb.WriteString(fmt.Sprintf("last_wakeup_at: %s\n", or(p.LastWakeupAt, "never")))
		sb.WriteString(fmt.Sprintf("schedule: %s\n", or(p.Schedule, "not set")))
	}

	tasks, err := b.store.GetTasks(ctx, b.cfg.AllowedUserID)
	if err != nil {
		sb.WriteString(fmt.Sprintf("error loading tasks: %v\n", err))
		return sb.String()
	}
	sb.WriteString(fmt.Sprintf("\ntasks (%d active):\n", len(tasks)))
	for _, t := range tasks {
		sb.WriteString(fmt.Sprintf("  [%d] %s\n      next_nudge_at: %s\n",
			t.ID, t.Description, or(t.NextNudgeAt, "not set")))
	}

	return sb.String()
}

func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func (b *Bot) executeActions(ctx context.Context, actions []agent.Action) error {
	for _, a := range actions {
		logger.Info().Str("type", string(a.Type)).Int64("id", a.ID).Str("desc", a.Description).Msg("action")
		var err error
		switch a.Type {
		case agent.ActionAddTask:
			_, err = b.store.AddTask(ctx, b.cfg.AllowedUserID, a.Description)
		case agent.ActionUpdateTask:
			err = b.store.UpdateTask(ctx, a.ID, a.Description)
		case agent.ActionSetNextNudge:
			err = b.store.SetNextNudgeAt(ctx, a.ID, a.NextNudgeAt)
		case agent.ActionCompleteTask:
			err = b.store.CompleteTask(ctx, a.ID)
		case agent.ActionDeleteTask:
			err = b.store.DeleteTask(ctx, a.ID)
		case agent.ActionUpdateSchedule:
			err = b.store.SetSchedule(ctx, b.cfg.AllowedUserID, a.Schedule)
		}
		if err != nil {
			logger.Error().Err(err).Str("action", string(a.Type)).Msg("action failed")
		}
	}
	return nil
}
