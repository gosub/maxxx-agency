package bot

import (
	"context"
	"time"

	"maxxx-agency/lang"
)

func (b *Bot) dailyScheduler(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(schedulerTickMin) * time.Minute)
	defer ticker.Stop()

	l := logger.With().Str("handler", "scheduler").Logger()
	l.Debug().Int("hour", b.cfg.DailyCheckinHour).Str("tz", b.cfg.Timezone).Msg("scheduler started")

	for {
		select {
		case <-ctx.Done():
			l.Debug().Msg("scheduler stopped")
			return
		case <-ticker.C:
			now := time.Now().In(b.loc)
			if now.Hour() != b.cfg.DailyCheckinHour || now.Minute() != 0 {
				continue
			}

			lastCheckin, err := b.store.GetLastCheckin(ctx, b.cfg.AllowedUserID)
			if err != nil {
				l.Error().Err(err).Msg("get last checkin failed")
				continue
			}

			today := now.Format("2006-01-02")
			if lastCheckin == today {
				continue
			}

			st, err := b.store.EnsureState(ctx, b.cfg.AllowedUserID, b.cfg.Language, b.cfg.Tone)
			if err != nil {
				l.Error().Err(err).Msg("ensure state failed")
				continue
			}

			dayNum := int(now.Sub(time.Date(2026, 1, 1, 0, 0, 0, 0, b.loc)).Hours()/24) + 1
			if dayNum < 1 {
				dayNum = 1
			}

			msg := lang.Getf(st.Language, "checkin_msg", dayNum)
			b.sendMessage(b.cfg.AllowedUserID, msg)
			l.Info().Int("day", dayNum).Msg("daily check-in sent")

			if err := b.store.MarkCheckin(ctx, b.cfg.AllowedUserID); err != nil {
				l.Error().Err(err).Msg("mark checkin failed")
			}
		}
	}
}
