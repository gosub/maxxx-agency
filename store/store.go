package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type State struct {
	UserID              int64  `json:"user_id"`
	CurrentPhase        int    `json:"current_phase"`
	CurrentGoals        string `json:"current_goals"`
	RejectionLog        string `json:"rejection_log"`
	LastCheckin         string `json:"last_checkin"`
	ConversationHistory string `json:"conversation_history"`
	ConfigNotes         string `json:"config_notes"`
	Language            string `json:"language"`
	Tone                string `json:"tone"`
}

type Store struct {
	db *sql.DB
}

type Storager interface {
	EnsureState(ctx context.Context, userID int64, defaultLang, defaultTone string) (*State, error)
	GetState(ctx context.Context, userID int64) (*State, error)
	UpdateState(ctx context.Context, st *State) error
	SetLanguage(ctx context.Context, userID int64, lang string) error
	SetTone(ctx context.Context, userID int64, tone string) error
	AddRejection(ctx context.Context, userID int64) (int, error)
	AddGoal(ctx context.Context, userID int64, goal string) error
	GetGoals(ctx context.Context, userID int64) ([]string, error)
	CompleteGoal(ctx context.Context, userID int64, index int) error
	SetConversationHistory(ctx context.Context, userID int64, messages []map[string]string) error
	GetConversationHistory(ctx context.Context, userID int64) ([]map[string]string, error)
	MarkCheckin(ctx context.Context, userID int64) error
	GetLastCheckin(ctx context.Context, userID int64) (string, error)
}

var _ Storager = (*Store)(nil)

func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		return nil, fmt.Errorf("set pragma: %w", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS state (
		user_id              INTEGER PRIMARY KEY,
		current_phase        INTEGER DEFAULT 0,
		current_goals        TEXT DEFAULT '[]',
		rejection_log        TEXT DEFAULT '[]',
		last_checkin         TEXT,
		conversation_history TEXT DEFAULT '[]',
		config_notes         TEXT DEFAULT '',
		language             TEXT DEFAULT 'it',
		tone                 TEXT DEFAULT 'warm'
	);`

	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("create schema: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) GetState(ctx context.Context, userID int64) (*State, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT user_id, current_phase, current_goals, rejection_log,
		       last_checkin, conversation_history, config_notes, language, tone
		FROM state WHERE user_id = ?`, userID)

	st := &State{}
	var lastCheckin sql.NullString
	err := row.Scan(&st.UserID, &st.CurrentPhase, &st.CurrentGoals,
		&st.RejectionLog, &lastCheckin, &st.ConversationHistory,
		&st.ConfigNotes, &st.Language, &st.Tone)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan state: %w", err)
	}
	if lastCheckin.Valid {
		st.LastCheckin = lastCheckin.String
	}
	return st, nil
}

func (s *Store) EnsureState(ctx context.Context, userID int64, defaultLang, defaultTone string) (*State, error) {
	st, err := s.GetState(ctx, userID)
	if err != nil {
		return nil, err
	}
	if st != nil {
		return st, nil
	}

	st = &State{
		UserID:              userID,
		CurrentPhase:        0,
		CurrentGoals:        "[]",
		RejectionLog:        "[]",
		ConversationHistory: "[]",
		ConfigNotes:         "",
		Language:            defaultLang,
		Tone:                defaultTone,
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO state (user_id, current_phase, current_goals, rejection_log,
		                   conversation_history, config_notes, language, tone)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		st.UserID, st.CurrentPhase, st.CurrentGoals, st.RejectionLog,
		st.ConversationHistory, st.ConfigNotes, st.Language, st.Tone)
	if err != nil {
		return nil, fmt.Errorf("insert state: %w", err)
	}
	return st, nil
}

func (s *Store) UpdateState(ctx context.Context, st *State) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE state SET
			current_phase = ?,
			current_goals = ?,
			rejection_log = ?,
			last_checkin = ?,
			conversation_history = ?,
			config_notes = ?,
			language = ?,
			tone = ?
		WHERE user_id = ?`,
		st.CurrentPhase, st.CurrentGoals, st.RejectionLog,
		st.LastCheckin, st.ConversationHistory, st.ConfigNotes,
		st.Language, st.Tone, st.UserID)
	return err
}

func (s *Store) SetLanguage(ctx context.Context, userID int64, lang string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE state SET language = ? WHERE user_id = ?`, lang, userID)
	return err
}

func (s *Store) SetTone(ctx context.Context, userID int64, tone string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE state SET tone = ? WHERE user_id = ?`, tone, userID)
	return err
}

func (s *Store) AddRejection(ctx context.Context, userID int64) (int, error) {
	st, err := s.EnsureState(ctx, userID, "it", "warm")
	if err != nil {
		return 0, err
	}

	var rejections []string
	if err := json.Unmarshal([]byte(st.RejectionLog), &rejections); err != nil {
		rejections = []string{}
	}
	rejections = append(rejections, time.Now().Format("2006-01-02"))

	data, err := json.Marshal(rejections)
	if err != nil {
		return 0, err
	}

	_, err = s.db.ExecContext(ctx, `UPDATE state SET rejection_log = ? WHERE user_id = ?`, string(data), userID)
	if err != nil {
		return 0, err
	}
	return len(rejections), nil
}

func (s *Store) AddGoal(ctx context.Context, userID int64, goal string) error {
	st, err := s.EnsureState(ctx, userID, "it", "warm")
	if err != nil {
		return err
	}

	var goals []string
	if err := json.Unmarshal([]byte(st.CurrentGoals), &goals); err != nil {
		goals = []string{}
	}
	goals = append(goals, goal)

	data, err := json.Marshal(goals)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `UPDATE state SET current_goals = ? WHERE user_id = ?`, string(data), userID)
	return err
}

func (s *Store) GetGoals(ctx context.Context, userID int64) ([]string, error) {
	st, err := s.EnsureState(ctx, userID, "it", "warm")
	if err != nil {
		return nil, err
	}
	var goals []string
	if err := json.Unmarshal([]byte(st.CurrentGoals), &goals); err != nil {
		return []string{}, nil
	}
	return goals, nil
}

func (s *Store) CompleteGoal(ctx context.Context, userID int64, index int) error {
	goals, err := s.GetGoals(ctx, userID)
	if err != nil {
		return err
	}
	if index < 0 || index >= len(goals) {
		return fmt.Errorf("invalid goal index")
	}
	goals = append(goals[:index], goals[index+1:]...)
	data, err := json.Marshal(goals)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE state SET current_goals = ? WHERE user_id = ?`, string(data), userID)
	return err
}

func (s *Store) SetConversationHistory(ctx context.Context, userID int64, messages []map[string]string) error {
	if _, err := s.EnsureState(ctx, userID, "it", "warm"); err != nil {
		return err
	}
	data, err := json.Marshal(messages)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE state SET conversation_history = ? WHERE user_id = ?`, string(data), userID)
	return err
}

func (s *Store) GetConversationHistory(ctx context.Context, userID int64) ([]map[string]string, error) {
	st, err := s.EnsureState(ctx, userID, "it", "warm")
	if err != nil {
		return nil, err
	}
	var msgs []map[string]string
	if err := json.Unmarshal([]byte(st.ConversationHistory), &msgs); err != nil {
		return []map[string]string{}, nil
	}
	return msgs, nil
}

func (s *Store) MarkCheckin(ctx context.Context, userID int64) error {
	if _, err := s.EnsureState(ctx, userID, "it", "warm"); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE state SET last_checkin = ? WHERE user_id = ?`,
		time.Now().Format("2006-01-02"), userID)
	return err
}

func (s *Store) GetLastCheckin(ctx context.Context, userID int64) (string, error) {
	var lastCheckin sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT last_checkin FROM state WHERE user_id = ?`, userID).Scan(&lastCheckin)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if lastCheckin.Valid {
		return lastCheckin.String, nil
	}
	return "", nil
}
