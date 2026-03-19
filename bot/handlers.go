package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"maxxx-agency/lang"
	"maxxx-agency/store"
)

func (b *Bot) handleCommand(ctx context.Context, chatID int64, text string) {
	parts := strings.Fields(text)
	cmd := strings.TrimPrefix(parts[0], "@"+b.botName)
	cmd = strings.ToLower(cmd)

	l := logger.With().Int64("chat_id", chatID).Str("cmd", cmd).Logger()

	st, err := b.store.EnsureState(ctx, b.cfg.AllowedUserID, b.cfg.Language, b.cfg.Tone)
	if err != nil {
		l.Error().Err(err).Msg("ensure state failed")
		b.sendMessage(chatID, "Something went wrong. Please try again.")
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
			l.Error().Err(err).Msg("add rejection failed")
			b.sendMessage(chatID, "Could not log rejection. Please try again.")
			return
		}
		response = lang.Getf(st.Language, "rejection_logged", count)

	case "/goal":
		response = b.handleGoal(ctx, parts, st)

	case "/skip":
		if err := b.store.MarkCheckin(ctx, b.cfg.AllowedUserID); err != nil {
			l.Error().Err(err).Msg("mark checkin failed")
			b.sendMessage(chatID, "Could not skip check-in. Please try again.")
			return
		}
		response = lang.Get(st.Language, "checkin_skipped")

	case "/lang":
		response = b.handleLang(ctx, parts, st)

	case "/tone":
		response = b.handleTone(ctx, parts, st)

	case "/reset":
		if err := b.store.SetConversationHistory(ctx, b.cfg.AllowedUserID, []map[string]string{}); err != nil {
			l.Error().Err(err).Msg("reset history failed")
			b.sendMessage(chatID, "Could not reset context. Please try again.")
			return
		}
		response = "Conversation context cleared."

	default:
		return
	}

	b.sendMessage(chatID, response)
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
		logger.Error().Err(err).Msg("get goals failed")
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
			logger.Error().Err(err).Str("goal", goal).Msg("add goal failed")
			return "Error adding goal."
		}
		return lang.Getf(st.Language, "goal_added", goal)

	case "list":
		goals, err := b.store.GetGoals(ctx, b.cfg.AllowedUserID)
		if err != nil {
			logger.Error().Err(err).Msg("list goals failed")
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
			logger.Error().Err(err).Msg("get goals for done failed")
			return "Error completing goal."
		}
		if idx < 1 || idx > len(goals) {
			return lang.Get(st.Language, "goal_invalid")
		}
		goalName := goals[idx-1]
		if err := b.store.CompleteGoal(ctx, b.cfg.AllowedUserID, idx); err != nil {
			logger.Error().Err(err).Str("goal", goalName).Msg("complete goal failed")
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
		logger.Error().Err(err).Str("lang", newLang).Msg("set language failed")
		return "Error setting language."
	}
	logger.Info().Str("lang", newLang).Msg("language changed")
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
		logger.Error().Err(err).Str("tone", newTone).Msg("set tone failed")
		return "Error setting tone."
	}
	logger.Info().Str("tone", newTone).Msg("tone changed")
	return lang.Getf(st.Language, "tone_switched", newTone)
}
