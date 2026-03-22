package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type Task struct {
	ID          int64
	UserID      int64
	Description string
	NextNudgeAt string // ISO 8601, empty if not set
	Recurring   bool
}

type TaskEvent struct {
	ID          int64
	TaskID      int64
	UserID      int64
	EventType   string
	Description string
	OccurredAt  string
}

type Prefs struct {
	UserID              int64
	Language            string
	NudgeIntervalM      int
	Schedule            string // freeform, e.g. "weekdays 9-13 and 15-19"
	LastWakeupAt        string // ISO 8601, empty if never run
	ConversationHistory string // JSON array of {role, content}
}

type Store struct {
	db *sql.DB
}

type Storager interface {
	// Tasks
	AddTask(ctx context.Context, userID int64, description string, recurring bool) (*Task, error)
	GetTasks(ctx context.Context, userID int64) ([]*Task, error)
	UpdateTask(ctx context.Context, id int64, description string) error
	SetNextNudgeAt(ctx context.Context, id int64, nextNudgeAt string) error
	SetRecurring(ctx context.Context, id int64, recurring bool) error
	CompleteTask(ctx context.Context, id int64) error
	DeleteTask(ctx context.Context, id int64) error
	GetDueTasks(ctx context.Context, userID int64, now string) ([]*Task, error)
	GetTasksForPeriod(ctx context.Context, userID int64, from, to string) ([]*Task, error)
	GetEventsForPeriod(ctx context.Context, userID int64, eventType, from, to string) ([]*TaskEvent, error)

	// Prefs
	EnsurePrefs(ctx context.Context, userID int64, defaultLang string, defaultInterval int) (*Prefs, error)
	GetPrefs(ctx context.Context, userID int64) (*Prefs, error)
	SetLanguage(ctx context.Context, userID int64, lang string) error
	SetNudgeInterval(ctx context.Context, userID int64, intervalM int) error
	SetSchedule(ctx context.Context, userID int64, schedule string) error
	SetLastWakeupAt(ctx context.Context, userID int64, t string) error
	SetConversationHistory(ctx context.Context, userID int64, history string) error
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

	s := &Store{db: db}
	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return s, nil
}

func (s *Store) initSchema() error {
	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS prefs (
			user_id              INTEGER PRIMARY KEY,
			language             TEXT    NOT NULL DEFAULT 'en',
			nudge_interval_m     INTEGER NOT NULL DEFAULT 30,
			schedule             TEXT    NOT NULL DEFAULT '',
			last_wakeup_at       TEXT,
			conversation_history TEXT    NOT NULL DEFAULT ''
		)`); err != nil {
		return fmt.Errorf("create prefs: %w", err)
	}

	// Migration: add conversation_history if missing (older DBs)
	s.db.Exec(`ALTER TABLE prefs ADD COLUMN conversation_history TEXT NOT NULL DEFAULT ''`)

	// Check if tasks table needs migration (has old 'done' column)
	hasDone, err := s.columnExists("tasks", "done")
	if err != nil {
		return fmt.Errorf("check tasks schema: %w", err)
	}
	if hasDone {
		if err := s.migrateTasksTable(); err != nil {
			return fmt.Errorf("migrate tasks: %w", err)
		}
	} else {
		if _, err := s.db.Exec(`
			CREATE TABLE IF NOT EXISTS tasks (
				id            INTEGER PRIMARY KEY AUTOINCREMENT,
				user_id       INTEGER NOT NULL,
				description   TEXT    NOT NULL,
				next_nudge_at TEXT,
				recurring     INTEGER NOT NULL DEFAULT 0
			)`); err != nil {
			return fmt.Errorf("create tasks: %w", err)
		}
	}

	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS task_events (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id     INTEGER NOT NULL,
			user_id     INTEGER NOT NULL,
			event_type  TEXT    NOT NULL,
			description TEXT,
			occurred_at TEXT    NOT NULL
		)`); err != nil {
		return fmt.Errorf("create task_events: %w", err)
	}

	return nil
}

func (s *Store) columnExists(table, column string) (bool, error) {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, pk int
		var name, colType string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *Store) migrateTasksTable() error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS tasks_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL,
			description TEXT NOT NULL, next_nudge_at TEXT, recurring INTEGER NOT NULL DEFAULT 0
		)`,
		`INSERT INTO tasks_new (id, user_id, description, next_nudge_at)
			SELECT id, user_id, description, next_nudge_at FROM tasks WHERE done = 0`,
		`DROP TABLE tasks`,
		`ALTER TABLE tasks_new RENAME TO tasks`,
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Close() error {
	s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return s.db.Close()
}

// logEvent records a task mutation event. Errors are silently ignored — event logging
// is non-critical and must not cause mutation methods to fail.
func (s *Store) logEvent(ctx context.Context, taskID, userID int64, eventType, description string) {
	now := time.Now().UTC().Format("2006-01-02T15:04:05")
	s.db.ExecContext(ctx,
		`INSERT INTO task_events (task_id, user_id, event_type, description, occurred_at) VALUES (?, ?, ?, ?, ?)`,
		taskID, userID, eventType, description, now)
}

// --- Tasks ---

func (s *Store) AddTask(ctx context.Context, userID int64, description string, recurring bool) (*Task, error) {
	rec := 0
	if recurring {
		rec = 1
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO tasks (user_id, description, recurring) VALUES (?, ?, ?)`,
		userID, description, rec)
	if err != nil {
		return nil, fmt.Errorf("insert task: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}
	t := &Task{ID: id, UserID: userID, Description: description, Recurring: recurring}
	s.logEvent(ctx, id, userID, "created", description)
	return t, nil
}

func (s *Store) GetTasks(ctx context.Context, userID int64) ([]*Task, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, description, next_nudge_at, recurring
		 FROM tasks WHERE user_id = ?
		 ORDER BY id ASC`, userID)
	if err != nil {
		return nil, fmt.Errorf("query tasks: %w", err)
	}
	defer rows.Close()
	return scanTasks(rows)
}

func (s *Store) GetDueTasks(ctx context.Context, userID int64, now string) ([]*Task, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, description, next_nudge_at, recurring
		 FROM tasks WHERE user_id = ? AND next_nudge_at <= ?
		 ORDER BY next_nudge_at ASC`, userID, now)
	if err != nil {
		return nil, fmt.Errorf("query due tasks: %w", err)
	}
	defer rows.Close()
	return scanTasks(rows)
}

func (s *Store) GetTasksForPeriod(ctx context.Context, userID int64, from, to string) ([]*Task, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, description, next_nudge_at, recurring
		 FROM tasks WHERE user_id = ? AND next_nudge_at >= ? AND next_nudge_at <= ?
		 ORDER BY next_nudge_at ASC`, userID, from, to)
	if err != nil {
		return nil, fmt.Errorf("query tasks for period: %w", err)
	}
	defer rows.Close()
	return scanTasks(rows)
}

func (s *Store) GetEventsForPeriod(ctx context.Context, userID int64, eventType, from, to string) ([]*TaskEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, task_id, user_id, event_type, description, occurred_at
		 FROM task_events WHERE user_id = ? AND event_type = ? AND occurred_at >= ? AND occurred_at <= ?
		 ORDER BY occurred_at ASC`, userID, eventType, from, to)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()
	var events []*TaskEvent
	for rows.Next() {
		e := &TaskEvent{}
		var desc sql.NullString
		if err := rows.Scan(&e.ID, &e.TaskID, &e.UserID, &e.EventType, &desc, &e.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		if desc.Valid {
			e.Description = desc.String
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *Store) UpdateTask(ctx context.Context, id int64, description string) error {
	var userID int64
	s.db.QueryRowContext(ctx, `SELECT user_id FROM tasks WHERE id = ?`, id).Scan(&userID)
	if _, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET description = ? WHERE id = ?`, description, id); err != nil {
		return err
	}
	s.logEvent(ctx, id, userID, "updated", description)
	return nil
}

// SetNextNudgeAt sets next_nudge_at for a task. Pass empty string to clear it (set NULL).
func (s *Store) SetNextNudgeAt(ctx context.Context, id int64, nextNudgeAt string) error {
	var val interface{}
	if nextNudgeAt != "" {
		val = nextNudgeAt
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET next_nudge_at = ? WHERE id = ?`, val, id)
	return err
}

func (s *Store) SetRecurring(ctx context.Context, id int64, recurring bool) error {
	rec := 0
	if recurring {
		rec = 1
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET recurring = ? WHERE id = ?`, rec, id)
	return err
}

func (s *Store) CompleteTask(ctx context.Context, id int64) error {
	var userID int64
	var desc string
	s.db.QueryRowContext(ctx, `SELECT user_id, description FROM tasks WHERE id = ?`, id).Scan(&userID, &desc)
	if _, err := s.db.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id); err != nil {
		return err
	}
	s.logEvent(ctx, id, userID, "completed", desc)
	return nil
}

func (s *Store) DeleteTask(ctx context.Context, id int64) error {
	var userID int64
	var desc string
	s.db.QueryRowContext(ctx, `SELECT user_id, description FROM tasks WHERE id = ?`, id).Scan(&userID, &desc)
	if _, err := s.db.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id); err != nil {
		return err
	}
	s.logEvent(ctx, id, userID, "deleted", desc)
	return nil
}

func scanTasks(rows *sql.Rows) ([]*Task, error) {
	var tasks []*Task
	for rows.Next() {
		t := &Task{}
		var nextNudgeAt sql.NullString
		var recurring int
		if err := rows.Scan(&t.ID, &t.UserID, &t.Description, &nextNudgeAt, &recurring); err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		if nextNudgeAt.Valid {
			t.NextNudgeAt = nextNudgeAt.String
		}
		t.Recurring = recurring != 0
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// --- Prefs ---

func (s *Store) EnsurePrefs(ctx context.Context, userID int64, defaultLang string, defaultInterval int) (*Prefs, error) {
	p, err := s.GetPrefs(ctx, userID)
	if err != nil {
		return nil, err
	}
	if p != nil {
		return p, nil
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO prefs (user_id, language, nudge_interval_m) VALUES (?, ?, ?)`,
		userID, defaultLang, defaultInterval)
	if err != nil {
		return nil, fmt.Errorf("insert prefs: %w", err)
	}
	return &Prefs{UserID: userID, Language: defaultLang, NudgeIntervalM: defaultInterval}, nil
}

func (s *Store) GetPrefs(ctx context.Context, userID int64) (*Prefs, error) {
	p := &Prefs{}
	var lastWakeupAt sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, language, nudge_interval_m, schedule, last_wakeup_at, conversation_history
		 FROM prefs WHERE user_id = ?`, userID).
		Scan(&p.UserID, &p.Language, &p.NudgeIntervalM, &p.Schedule, &lastWakeupAt, &p.ConversationHistory)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan prefs: %w", err)
	}
	if lastWakeupAt.Valid {
		p.LastWakeupAt = lastWakeupAt.String
	}
	return p, nil
}

func (s *Store) SetLanguage(ctx context.Context, userID int64, lang string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE prefs SET language = ? WHERE user_id = ?`, lang, userID)
	return err
}

func (s *Store) SetNudgeInterval(ctx context.Context, userID int64, intervalM int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE prefs SET nudge_interval_m = ? WHERE user_id = ?`, intervalM, userID)
	return err
}

func (s *Store) SetSchedule(ctx context.Context, userID int64, schedule string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE prefs SET schedule = ? WHERE user_id = ?`, schedule, userID)
	return err
}

func (s *Store) SetLastWakeupAt(ctx context.Context, userID int64, t string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE prefs SET last_wakeup_at = ? WHERE user_id = ?`, t, userID)
	return err
}

func (s *Store) SetConversationHistory(ctx context.Context, userID int64, history string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE prefs SET conversation_history = ? WHERE user_id = ?`, history, userID)
	return err
}
