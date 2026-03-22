package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gosub/nudgent/agent"
	"github.com/gosub/nudgent/store"
)

const maxHistoryMessages = 20 // 10 turns

func (b *Bot) handleCommand(ctx context.Context, chatID int64, text string) {
	parts := strings.Fields(text)
	cmd := strings.ToLower(strings.TrimPrefix(parts[0], "@"+b.botName))

	switch cmd {
	case "/tasks":
		b.send(chatID, b.buildTaskList(ctx))
	case "/today":
		b.send(chatID, b.buildPeriodSummary(ctx, "today"))
	case "/week":
		b.send(chatID, b.buildPeriodSummary(ctx, "week"))
	case "/debug":
		b.send(chatID, b.buildDebug(ctx))
	case "/help":
		b.send(chatID, "/tasks — list active tasks\n/today — done and due tasks for today\n/week — done and due tasks for this week\n/help — show this message\n\nOr just tell me what you need.")
	default:
		// unknown commands are silently ignored
	}
}

func (b *Bot) buildTaskList(ctx context.Context) string {
	tasks, err := b.store.GetTasks(ctx, b.cfg.AllowedUserID)
	if err != nil {
		b.log.Error().Err(err).Msg("get tasks failed")
		return "Error loading tasks."
	}
	if len(tasks) == 0 {
		return "No active tasks."
	}

	var sb strings.Builder
	sb.WriteString("Active tasks:\n")
	for i, t := range tasks {
		prefix := ""
		if t.Recurring {
			prefix = "↻ "
		}
		nudge := ""
		if t.NextNudgeAt != "" {
			nudge = " — " + t.NextNudgeAt
		}
		sb.WriteString(fmt.Sprintf("  %d. %s%s%s\n", i+1, prefix, t.Description, nudge))
	}
	return sb.String()
}

func (b *Bot) buildPeriodSummary(ctx context.Context, period string) string {
	now := time.Now().In(b.loc)
	var from, to time.Time
	var label string

	switch period {
	case "week":
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7 // Sunday = 7
		}
		from = time.Date(now.Year(), now.Month(), now.Day()-weekday+1, 0, 0, 0, 0, b.loc)
		to = from.AddDate(0, 0, 6).Add(23*time.Hour + 59*time.Minute + 59*time.Second)
		label = fmt.Sprintf("Week of %s:", from.Format("2006-01-02"))
	default: // "today"
		from = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, b.loc)
		to = from.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
		label = fmt.Sprintf("Today, %s:", now.Format("2006-01-02"))
	}

	fromStr := from.UTC().Format("2006-01-02T15:04:05")
	toStr := to.UTC().Format("2006-01-02T15:04:05")

	events, err := b.store.GetEventsForPeriod(ctx, b.cfg.AllowedUserID, "completed", fromStr, toStr)
	if err != nil {
		b.log.Error().Err(err).Msg("get events failed")
	}

	dueTasks, err := b.store.GetTasksForPeriod(ctx, b.cfg.AllowedUserID, fromStr, toStr)
	if err != nil {
		b.log.Error().Err(err).Msg("get tasks for period failed")
	}

	if len(events) == 0 && len(dueTasks) == 0 {
		return fmt.Sprintf("%s\n\nNothing scheduled.", label)
	}

	var sb strings.Builder
	sb.WriteString(label + "\n")

	if len(events) > 0 {
		sb.WriteString("\nDone:\n")
		for _, e := range events {
			sb.WriteString(fmt.Sprintf("  ✓ %s\n", e.Description))
		}
	}

	if len(dueTasks) > 0 {
		sb.WriteString("\nDue:\n")
		for _, t := range dueTasks {
			timeStr := ""
			if t.NextNudgeAt != "" {
				if parsed, err := time.Parse("2006-01-02T15:04:05", t.NextNudgeAt); err == nil {
					timeStr = " — " + parsed.In(b.loc).Format("15:04")
				}
			}
			sb.WriteString(fmt.Sprintf("  • %s%s\n", t.Description, timeStr))
		}
	}

	return sb.String()
}

func (b *Bot) handleChat(ctx context.Context, chatID int64, text string) {
	if ctx.Err() != nil {
		return
	}

	p, err := b.store.EnsurePrefs(ctx, b.cfg.AllowedUserID, b.cfg.Language, b.cfg.NudgeIntervalM)
	if err != nil {
		b.log.Error().Err(err).Msg("ensure prefs failed")
		b.send(chatID, "Something went wrong. Please try again.")
		return
	}

	tasks, err := b.store.GetTasks(ctx, b.cfg.AllowedUserID)
	if err != nil {
		b.log.Error().Err(err).Msg("get tasks failed")
		tasks = nil
	}

	history := loadHistory(p.ConversationHistory)

	prompt := agent.BuildChatPrompt(p.Language, p.Schedule, tasks, time.Now().In(b.loc))
	b.log.Trace().Str("prompt", prompt).Msg("chat system prompt")
	raw, err := b.agent.Chat(ctx, prompt, history, text)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		b.log.Error().Err(err).Msg("agent chat failed")
		b.send(chatID, "Sorry, I couldn't process that. Try again.")
		return
	}

	b.log.Debug().Str("raw", raw).Msg("agent response")

	resp, err := agent.ParseResponse(raw)
	if err != nil {
		b.log.Warn().Err(err).Str("raw", raw).Msg("failed to parse agent response, sending raw")
		b.send(chatID, raw)
		return
	}

	b.log.Debug().Int("actions", len(resp.Actions)).Msg("executing actions")
	b.executeActions(ctx, resp.Actions)

	if resp.Reply != "" {
		b.send(chatID, resp.Reply)
	}

	b.log.Debug().Int("actions", len(resp.Actions)).Int("reply_len", len(resp.Reply)).Msg("chat done")

	// Persist conversation history
	updated := appendHistory(history, text, resp.Reply)
	if encoded, err := json.Marshal(updated); err == nil {
		if err := b.store.SetConversationHistory(ctx, b.cfg.AllowedUserID, string(encoded)); err != nil {
			b.log.Warn().Err(err).Msg("save conversation history failed")
		}
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
		sb.WriteString(fmt.Sprintf("conversation_turns: %d\n", len(loadHistory(p.ConversationHistory))))
	}

	tasks, err := b.store.GetTasks(ctx, b.cfg.AllowedUserID)
	if err != nil {
		sb.WriteString(fmt.Sprintf("error loading tasks: %v\n", err))
		return sb.String()
	}
	sb.WriteString(fmt.Sprintf("\ntasks (%d active):\n", len(tasks)))
	for _, t := range tasks {
		recurring := ""
		if t.Recurring {
			recurring = " [recurring]"
		}
		sb.WriteString(fmt.Sprintf("  [%d] %s%s\n      next_nudge_at: %s\n",
			t.ID, t.Description, recurring, or(t.NextNudgeAt, "not set")))
	}

	return sb.String()
}

func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func (b *Bot) executeActions(ctx context.Context, actions []agent.Action) {
	for _, a := range actions {
		b.log.Info().Str("type", string(a.Type)).Int64("id", a.ID).Str("desc", a.Description).Msg("action")
		var err error
		switch a.Type {
		case agent.ActionAddTask:
			var t *store.Task
			t, err = b.store.AddTask(ctx, b.cfg.AllowedUserID, a.Description, a.Recurring)
			if err == nil && a.NextNudgeAt != "" {
				err = b.store.SetNextNudgeAt(ctx, t.ID, a.NextNudgeAt)
			}
		case agent.ActionUpdateTask:
			if a.Description != "" {
				err = b.store.UpdateTask(ctx, a.ID, a.Description)
			}
			if err == nil && a.NextNudgeAt != "" {
				err = b.store.SetNextNudgeAt(ctx, a.ID, a.NextNudgeAt)
			}
			if err == nil && a.Recurring {
				err = b.store.SetRecurring(ctx, a.ID, a.Recurring)
			}
		case agent.ActionCompleteTask:
			tasks, terr := b.store.GetTasks(ctx, b.cfg.AllowedUserID)
			if terr != nil {
				b.log.Error().Err(terr).Msg("get tasks for complete check failed")
				continue
			}
			var recurring bool
			for _, t := range tasks {
				if t.ID == a.ID {
					recurring = t.Recurring
					break
				}
			}
			if recurring {
				b.log.Warn().Int64("id", a.ID).Msg("skipping complete_task on recurring task")
				continue
			}
			err = b.store.CompleteTask(ctx, a.ID)
		case agent.ActionDeleteTask:
			err = b.store.DeleteTask(ctx, a.ID)
		case agent.ActionUpdateSchedule:
			err = b.store.SetSchedule(ctx, b.cfg.AllowedUserID, a.Schedule)
		}
		if err != nil {
			b.log.Error().Err(err).Str("action", string(a.Type)).Msg("action failed")
		}
	}
}

func loadHistory(raw string) []agent.Message {
	if raw == "" {
		return nil
	}
	var h []agent.Message
	if err := json.Unmarshal([]byte(raw), &h); err != nil {
		return nil
	}
	return h
}

func appendHistory(history []agent.Message, userMsg, assistantReply string) []agent.Message {
	if userMsg != "" {
		history = append(history, agent.Message{Role: "user", Content: userMsg})
	}
	if assistantReply != "" {
		history = append(history, agent.Message{Role: "assistant", Content: assistantReply})
	}
	// Keep last maxHistoryMessages messages
	if len(history) > maxHistoryMessages {
		history = history[len(history)-maxHistoryMessages:]
	}
	return history
}
