package bot

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gosub/nudgent/agent"
	"github.com/gosub/nudgent/log"
	"github.com/gosub/nudgent/store"
)

// --- mocks ---

type mockStore struct {
	prefs               *store.Prefs
	tasks               []*store.Task
	nextID              int64
	ensurePrefsErr      error
	getTasksErr         error
	addTaskErr          error
	lastCompleted       int64
	lastDeleted         int64
	lastSchedule        string
	lastWakeupAt        string
	conversationHistory string
}

func (m *mockStore) EnsurePrefs(ctx context.Context, userID int64, lang string, interval int) (*store.Prefs, error) {
	if m.ensurePrefsErr != nil {
		return nil, m.ensurePrefsErr
	}
	if m.prefs == nil {
		m.prefs = &store.Prefs{UserID: userID, Language: lang, NudgeIntervalM: interval}
	}
	return m.prefs, nil
}
func (m *mockStore) GetPrefs(ctx context.Context, userID int64) (*store.Prefs, error) {
	return m.prefs, nil
}
func (m *mockStore) SetNudgeInterval(ctx context.Context, userID int64, intervalM int) error {
	return nil
}
func (m *mockStore) SetLanguage(ctx context.Context, userID int64, lang string) error {
	if m.prefs != nil {
		m.prefs.Language = lang
	}
	return nil
}
func (m *mockStore) SetSchedule(ctx context.Context, userID int64, schedule string) error {
	m.lastSchedule = schedule
	return nil
}
func (m *mockStore) SetLastWakeupAt(ctx context.Context, userID int64, t string) error {
	m.lastWakeupAt = t
	return nil
}
func (m *mockStore) SetConversationHistory(ctx context.Context, userID int64, history string) error {
	m.conversationHistory = history
	return nil
}
func (m *mockStore) AddTask(ctx context.Context, userID int64, description string) (*store.Task, error) {
	if m.addTaskErr != nil {
		return nil, m.addTaskErr
	}
	m.nextID++
	t := &store.Task{ID: m.nextID, UserID: userID, Description: description}
	m.tasks = append(m.tasks, t)
	return t, nil
}
func (m *mockStore) GetTasks(ctx context.Context, userID int64) ([]*store.Task, error) {
	return m.tasks, m.getTasksErr
}
func (m *mockStore) UpdateTask(ctx context.Context, id int64, description string) error {
	for _, t := range m.tasks {
		if t.ID == id {
			t.Description = description
		}
	}
	return nil
}
func (m *mockStore) SetNextNudgeAt(ctx context.Context, id int64, next string) error {
	for _, t := range m.tasks {
		if t.ID == id {
			t.NextNudgeAt = next
		}
	}
	return nil
}
func (m *mockStore) CompleteTask(ctx context.Context, id int64) error {
	m.lastCompleted = id
	m.tasks = filterTasks(m.tasks, id)
	return nil
}
func (m *mockStore) DeleteTask(ctx context.Context, id int64) error {
	m.lastDeleted = id
	m.tasks = filterTasks(m.tasks, id)
	return nil
}
func (m *mockStore) GetDueTasks(ctx context.Context, userID int64, now string) ([]*store.Task, error) {
	return m.tasks, nil
}

func filterTasks(tasks []*store.Task, id int64) []*store.Task {
	var out []*store.Task
	for _, t := range tasks {
		if t.ID != id {
			out = append(out, t)
		}
	}
	return out
}

type mockAgent struct {
	response string
	err      error
	calls    int
}

func (m *mockAgent) Chat(ctx context.Context, systemPrompt string, history []agent.Message, userMessage string) (string, error) {
	m.calls++
	return m.response, m.err
}

type fakeBot struct {
	*Bot
	messages []string
}

func newFakeBot(ms *mockStore, ma *mockAgent) *fakeBot {
	fb := &fakeBot{
		Bot: &Bot{
			agent: ma,
			store: ms,
			cfg:   Config{AllowedUserID: 1, Language: "en", NudgeIntervalM: 30},
			loc:   time.UTC,
			log:   log.Logger,
		},
		messages: []string{},
	}
	fb.send = fb.capture
	return fb
}

func (fb *fakeBot) capture(_ int64, text string) {
	fb.messages = append(fb.messages, text)
}

// --- tests ---

func TestHandleCommandTasks_Empty(t *testing.T) {
	ms := &mockStore{prefs: &store.Prefs{Language: "en"}}
	fb := newFakeBot(ms, &mockAgent{})
	fb.handleCommand(context.Background(), 1, "/tasks")
	if len(fb.messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(fb.messages))
	}
	if fb.messages[0] != "No active tasks." {
		t.Errorf("message = %q", fb.messages[0])
	}
}

func TestHandleCommandTasks_WithTasks(t *testing.T) {
	ms := &mockStore{
		prefs: &store.Prefs{Language: "en"},
		tasks: []*store.Task{
			{ID: 1, Description: "Call dentist"},
			{ID: 2, Description: "Buy milk", NextNudgeAt: "2026-03-21T09:00:00"},
		},
	}
	fb := newFakeBot(ms, &mockAgent{})
	fb.handleCommand(context.Background(), 1, "/tasks")
	if len(fb.messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(fb.messages))
	}
	msg := fb.messages[0]
	if !contains(msg, "Call dentist") {
		t.Errorf("missing task 1: %q", msg)
	}
	if !contains(msg, "Buy milk") {
		t.Errorf("missing task 2: %q", msg)
	}
	if !contains(msg, "2026-03-21T09:00:00") {
		t.Errorf("missing nudge time: %q", msg)
	}
}

func TestHandleCommandHelp(t *testing.T) {
	ms := &mockStore{prefs: &store.Prefs{Language: "en"}}
	fb := newFakeBot(ms, &mockAgent{})
	fb.handleCommand(context.Background(), 1, "/help")
	if len(fb.messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(fb.messages))
	}
	if !contains(fb.messages[0], "/tasks") {
		t.Errorf("help missing /tasks: %q", fb.messages[0])
	}
}

func TestHandleCommandUnknown(t *testing.T) {
	ms := &mockStore{prefs: &store.Prefs{Language: "en"}}
	fb := newFakeBot(ms, &mockAgent{})
	fb.handleCommand(context.Background(), 1, "/unknown")
	if len(fb.messages) != 0 {
		t.Errorf("expected no reply for unknown command, got %v", fb.messages)
	}
}

func TestHandleChat_CallsAgent(t *testing.T) {
	ms := &mockStore{prefs: &store.Prefs{Language: "en"}}
	ma := &mockAgent{response: `{"reply": "Got it.", "actions": []}`}
	fb := newFakeBot(ms, ma)
	fb.handleChat(context.Background(), 1, "add call dentist")
	if ma.calls != 1 {
		t.Errorf("agent calls = %d, want 1", ma.calls)
	}
	if len(fb.messages) != 1 || fb.messages[0] != "Got it." {
		t.Errorf("message = %v", fb.messages)
	}
}

func TestHandleChat_AgentError(t *testing.T) {
	ms := &mockStore{prefs: &store.Prefs{Language: "en"}}
	ma := &mockAgent{err: errors.New("api error")}
	fb := newFakeBot(ms, ma)
	fb.handleChat(context.Background(), 1, "hello")
	if len(fb.messages) != 1 || !contains(fb.messages[0], "couldn't process") {
		t.Errorf("expected error message, got %v", fb.messages)
	}
}

func TestHandleChat_InvalidJSON_SendsRaw(t *testing.T) {
	ms := &mockStore{prefs: &store.Prefs{Language: "en"}}
	ma := &mockAgent{response: "just some text, not json"}
	fb := newFakeBot(ms, ma)
	fb.handleChat(context.Background(), 1, "hello")
	if len(fb.messages) != 1 || fb.messages[0] != "just some text, not json" {
		t.Errorf("expected raw response, got %v", fb.messages)
	}
}

func TestHandleChat_ContextCancelled(t *testing.T) {
	ms := &mockStore{prefs: &store.Prefs{Language: "en"}}
	fb := newFakeBot(ms, &mockAgent{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fb.handleChat(ctx, 1, "hello")
	if len(fb.messages) != 0 {
		t.Errorf("expected no messages with cancelled context, got %v", fb.messages)
	}
}

func TestExecuteActions_AddTask(t *testing.T) {
	ms := &mockStore{prefs: &store.Prefs{Language: "en"}}
	fb := newFakeBot(ms, &mockAgent{})
	actions := []agent.Action{
		{Type: agent.ActionAddTask, Description: "Call dentist"},
	}
	fb.executeActions(context.Background(), actions)
	if len(ms.tasks) != 1 || ms.tasks[0].Description != "Call dentist" {
		t.Errorf("task not added: %v", ms.tasks)
	}
}

func TestExecuteActions_CompleteTask(t *testing.T) {
	ms := &mockStore{
		prefs: &store.Prefs{Language: "en"},
		tasks: []*store.Task{{ID: 5, Description: "Gym"}},
	}
	fb := newFakeBot(ms, &mockAgent{})
	fb.executeActions(context.Background(), []agent.Action{{Type: agent.ActionCompleteTask, ID: 5}})
	if ms.lastCompleted != 5 {
		t.Errorf("lastCompleted = %d, want 5", ms.lastCompleted)
	}
}

func TestExecuteActions_DeleteTask(t *testing.T) {
	ms := &mockStore{
		prefs: &store.Prefs{Language: "en"},
		tasks: []*store.Task{{ID: 3, Description: "Old task"}},
	}
	fb := newFakeBot(ms, &mockAgent{})
	fb.executeActions(context.Background(), []agent.Action{{Type: agent.ActionDeleteTask, ID: 3}})
	if ms.lastDeleted != 3 {
		t.Errorf("lastDeleted = %d, want 3", ms.lastDeleted)
	}
}

func TestExecuteActions_UpdateSchedule(t *testing.T) {
	ms := &mockStore{prefs: &store.Prefs{Language: "en"}}
	fb := newFakeBot(ms, &mockAgent{})
	fb.executeActions(context.Background(), []agent.Action{
		{Type: agent.ActionUpdateSchedule, Schedule: "weekdays 9-18"},
	})
	if ms.lastSchedule != "weekdays 9-18" {
		t.Errorf("lastSchedule = %q", ms.lastSchedule)
	}
}

func TestExecuteActions_SetNextNudge(t *testing.T) {
	ms := &mockStore{
		prefs: &store.Prefs{Language: "en"},
		tasks: []*store.Task{{ID: 2, Description: "Report"}},
	}
	fb := newFakeBot(ms, &mockAgent{})
	fb.executeActions(context.Background(), []agent.Action{
		{Type: agent.ActionUpdateTask, ID: 2, NextNudgeAt: "2026-03-25T09:00:00"},
	})
	if ms.tasks[0].NextNudgeAt != "2026-03-25T09:00:00" {
		t.Errorf("NextNudgeAt = %q", ms.tasks[0].NextNudgeAt)
	}
}

func TestHandleChat_EnsurePrefsError(t *testing.T) {
	ms := &mockStore{ensurePrefsErr: errors.New("db error")}
	fb := newFakeBot(ms, &mockAgent{})
	fb.handleChat(context.Background(), 1, "hello")
	if len(fb.messages) != 1 || !contains(fb.messages[0], "Something went wrong") {
		t.Errorf("expected error message, got %v", fb.messages)
	}
}

func TestHandleChat_HistorySaved(t *testing.T) {
	ms := &mockStore{prefs: &store.Prefs{Language: "en"}}
	ma := &mockAgent{response: `{"reply": "Done.", "actions": []}`}
	fb := newFakeBot(ms, ma)
	fb.handleChat(context.Background(), 1, "remind me about the meeting")
	if ms.conversationHistory == "" {
		t.Error("expected conversation history to be saved")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}
