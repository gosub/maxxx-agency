package bot

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"maxxx-agency/lang"
	"maxxx-agency/store"
)

type mockStore struct {
	state             *store.State
	goals             []string
	rejections        []string
	history           []map[string]string
	lastCheckin       string
	addRejectionCalls int
	addGoalCalls      int
	completeCalls     int
	getGoalsCalls     int
	ensureStateErr    error
	getGoalsErr       error
	addRejectionErr   error
}

func (m *mockStore) EnsureState(ctx context.Context, userID int64, defaultLang, defaultTone string) (*store.State, error) {
	if m.ensureStateErr != nil {
		return nil, m.ensureStateErr
	}
	if m.state != nil {
		if len(m.rejections) > 0 {
			if data, err := json.Marshal(m.rejections); err == nil {
				m.state.RejectionLog = string(data)
			}
		}
		return m.state, nil
	}
	m.state = &store.State{UserID: userID, Language: defaultLang, Tone: defaultTone}
	return m.state, nil
}

func (m *mockStore) GetState(ctx context.Context, userID int64) (*store.State, error) {
	if m.state != nil && len(m.rejections) > 0 {
		if data, err := json.Marshal(m.rejections); err == nil {
			m.state.RejectionLog = string(data)
		}
	}
	return m.state, nil
}
func (m *mockStore) UpdateState(ctx context.Context, st *store.State) error { m.state = st; return nil }
func (m *mockStore) SetLanguage(ctx context.Context, userID int64, lang string) error {
	if m.state != nil {
		m.state.Language = lang
	}
	return nil
}
func (m *mockStore) SetTone(ctx context.Context, userID int64, tone string) error {
	if m.state != nil {
		m.state.Tone = tone
	}
	return nil
}
func (m *mockStore) AddRejection(ctx context.Context, userID int64) (int, error) {
	m.addRejectionCalls++
	if m.addRejectionErr != nil {
		return 0, m.addRejectionErr
	}
	m.rejections = append(m.rejections, "2026-01-01")
	if m.state != nil {
		if data, err := json.Marshal(m.rejections); err == nil {
			m.state.RejectionLog = string(data)
		}
	}
	return len(m.rejections), nil
}
func (m *mockStore) AddGoal(ctx context.Context, userID int64, goal string) error {
	m.addGoalCalls++
	m.goals = append(m.goals, goal)
	return nil
}
func (m *mockStore) GetGoals(ctx context.Context, userID int64) ([]string, error) {
	m.getGoalsCalls++
	return m.goals, m.getGoalsErr
}
func (m *mockStore) CompleteGoal(ctx context.Context, userID int64, index int) error {
	m.completeCalls++
	if index >= 0 && index < len(m.goals) {
		m.goals = append(m.goals[:index], m.goals[index+1:]...)
	}
	return nil
}
func (m *mockStore) SetConversationHistory(ctx context.Context, userID int64, messages []map[string]string) error {
	m.history = messages
	return nil
}
func (m *mockStore) GetConversationHistory(ctx context.Context, userID int64) ([]map[string]string, error) {
	return m.history, nil
}
func (m *mockStore) MarkCheckin(ctx context.Context, userID int64) error {
	m.lastCheckin = "2026-01-01"
	return nil
}
func (m *mockStore) GetLastCheckin(ctx context.Context, userID int64) (string, error) {
	return m.lastCheckin, nil
}

type mockCoach struct {
	response  string
	err       error
	chatCalls int
}

func (m *mockCoach) Chat(ctx context.Context, systemPrompt string, history []map[string]string, userMessage string) (string, error) {
	m.chatCalls++
	return m.response, m.err
}

type fakeBot struct {
	*Bot
	messages []string
}

func newFakeBot(ms *mockStore, mc *mockCoach) *fakeBot {
	fb := &fakeBot{
		Bot: &Bot{
			coach:      mc,
			store:      ms,
			cfg:        Config{AllowedUserID: 1, Language: "en", Tone: "warm"},
			compendium: "Agency compendium",
			botName:    "TestBot",
		},
		messages: []string{},
	}
	fb.send = fb.capture
	return fb
}

func (fb *fakeBot) capture(_ int64, text string) {
	fb.messages = append(fb.messages, text)
}

func strContains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || func() bool {
		for i := 0; i <= len(haystack)-len(needle); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	}())
}

func TestHandleRejection(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/rejection")
	if ms.addRejectionCalls != 1 {
		t.Errorf("AddRejection calls = %d, want 1", ms.addRejectionCalls)
	}
	if len(fb.messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(fb.messages))
	}
}

func TestHandleRejectionStoreError(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}, addRejectionErr: errors.New("db error")}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/rejection")
	if ms.addRejectionCalls != 1 {
		t.Errorf("AddRejection calls = %d, want 1", ms.addRejectionCalls)
	}
	if !strContains(fb.messages[0], "Could not log") {
		t.Errorf("expected error message, got %q", fb.messages[0])
	}
}

func TestHandleGoalAdd(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/goal add Read Dune")
	if ms.addGoalCalls != 1 {
		t.Errorf("AddGoal calls = %d, want 1", ms.addGoalCalls)
	}
	if len(fb.messages) != 1 || !strContains(fb.messages[0], "Read Dune") {
		t.Errorf("response = %q, want it to contain 'Read Dune'", fb.messages[0])
	}
}

func TestHandleGoalAddTooLong(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}}
	fb := newFakeBot(ms, &mockCoach{})
	longGoal := make([]byte, maxGoalLen+1)
	for i := range longGoal {
		longGoal[i] = 'a'
	}
	fb.handleCommand(context.Background(), 1, "/goal add "+string(longGoal))
	if ms.addGoalCalls != 0 {
		t.Errorf("AddGoal calls = %d, want 0", ms.addGoalCalls)
	}
	if !strContains(fb.messages[0], "777 characters") {
		t.Errorf("response = %q, want it to contain '777 characters'", fb.messages[0])
	}
}

func TestHandleGoalList(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}, goals: []string{"Read Dune", "Start log"}}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/goal list")
	if ms.getGoalsCalls != 1 {
		t.Errorf("GetGoals calls = %d, want 1", ms.getGoalsCalls)
	}
	if !strContains(fb.messages[0], "Read Dune") {
		t.Errorf("response missing 'Read Dune': %q", fb.messages[0])
	}
}

func TestHandleGoalListEmpty(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}, goals: []string{}}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/goal list")
	if !strContains(fb.messages[0], lang.Get("en", "goal_none")) {
		t.Errorf("expected goal_none, got %q", fb.messages[0])
	}
}

func TestHandleGoalListStoreError(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}, getGoalsErr: errors.New("db error")}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/goal list")
	if !strContains(fb.messages[0], "Error listing") {
		t.Errorf("expected error message, got %q", fb.messages[0])
	}
}

func TestHandleGoalDone(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}, goals: []string{"Read Dune", "Start log"}}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/goal done 1")
	if ms.completeCalls != 1 {
		t.Errorf("CompleteGoal calls = %d, want 1", ms.completeCalls)
	}
	if !strContains(fb.messages[0], "Read Dune") {
		t.Errorf("expected completed goal name in response, got %q", fb.messages[0])
	}
}

func TestHandleGoalDoneInvalidIndex(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}, goals: []string{"Only goal"}}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/goal done 99")
	if ms.completeCalls != 0 {
		t.Errorf("CompleteGoal calls = %d, want 0", ms.completeCalls)
	}
	if !strContains(fb.messages[0], lang.Get("en", "goal_invalid")) {
		t.Errorf("response = %q, want goal_invalid", fb.messages[0])
	}
}

func TestHandleGoalDoneNegativeIndex(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}, goals: []string{"Only goal"}}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/goal done -1")
	if !strContains(fb.messages[0], lang.Get("en", "goal_invalid")) {
		t.Errorf("response = %q, want goal_invalid", fb.messages[0])
	}
}

func TestHandleGoalAddMissingText(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/goal add")
	if ms.addGoalCalls != 0 {
		t.Errorf("AddGoal calls = %d, want 0", ms.addGoalCalls)
	}
}

func TestHandleGoalDoneMissingIndex(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/goal done")
	if ms.completeCalls != 0 {
		t.Errorf("CompleteGoal calls = %d, want 0", ms.completeCalls)
	}
}

func TestHandleGoalDoneNonNumeric(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}, goals: []string{"Goal"}}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/goal done abc")
	if ms.completeCalls != 0 {
		t.Errorf("CompleteGoal calls = %d, want 0", ms.completeCalls)
	}
	if !strContains(fb.messages[0], lang.Get("en", "goal_invalid")) {
		t.Errorf("response = %q, want goal_invalid", fb.messages[0])
	}
}

func TestHandleLang(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/lang it")
	if ms.state.Language != "it" {
		t.Errorf("Language = %q, want %q", ms.state.Language, "it")
	}
}

func TestHandleLangInvalid(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/lang fr")
	if ms.state.Language != "en" {
		t.Errorf("Language changed unexpectedly to %q", ms.state.Language)
	}
}

func TestHandleLangItalian(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "it", Tone: "warm"}}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/lang en")
	if ms.state.Language != "en" {
		t.Errorf("Language = %q, want %q", ms.state.Language, "en")
	}
	if !strContains(fb.messages[0], "Language switched to: en") {
		t.Errorf("expected English response, got %q", fb.messages[0])
	}
}

func TestHandleLangShowCurrent(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/lang")
	if !strContains(fb.messages[0], "Current language: en") {
		t.Errorf("expected lang current message, got %q", fb.messages[0])
	}
}

func TestHandleTone(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/tone drill-sergeant")
	if ms.state.Tone != "drill-sergeant" {
		t.Errorf("Tone = %q, want %q", ms.state.Tone, "drill-sergeant")
	}
}

func TestHandleToneInvalid(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/tone weird")
	if ms.state.Tone != "warm" {
		t.Errorf("Tone changed unexpectedly to %q", ms.state.Tone)
	}
}

func TestHandleStart(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/start")
	if fb.messages[0] != lang.Get("en", "welcome") {
		t.Errorf("welcome mismatch: %q", fb.messages[0])
	}
}

func TestHandleStartItalian(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "it", Tone: "warm"}}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/start")
	if fb.messages[0] != lang.Get("it", "welcome") {
		t.Errorf("Italian welcome mismatch: %q", fb.messages[0])
	}
}

func TestHandleHelp(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/help")
	if !strContains(fb.messages[0], "/start") {
		t.Error("help missing /start")
	}
}

func TestHandleHelpItalian(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "it", Tone: "warm"}}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/help")
	if !strContains(fb.messages[0], "/start") {
		t.Error("Italian help missing /start")
	}
}

func TestHandleReset(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}, history: []map[string]string{{"role": "user", "content": "hello"}}}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/reset")
	if len(ms.history) != 0 {
		t.Errorf("history = %v, want empty", ms.history)
	}
}

func TestHandleSkip(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/skip")
	if ms.lastCheckin != "2026-01-01" {
		t.Errorf("lastCheckin = %q, want 2026-01-01", ms.lastCheckin)
	}
}

func TestHandleStatus(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm", CurrentPhase: 2}, goals: []string{"Goal A"}, rejections: []string{"2026-01-01", "2026-01-02"}}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/status")
	resp := fb.messages[0]
	if !strContains(resp, "Phase: 2") || !strContains(resp, "Goal A") || !strContains(resp, "Rejections logged: 2") {
		t.Errorf("status incomplete: %q", resp)
	}
}

func TestHandleUnknownCommand(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/unknown")
	if len(fb.messages) != 0 {
		t.Errorf("len(messages) = %d, want 0", len(fb.messages))
	}
}

func TestHandleCommandStoreError(t *testing.T) {
	ms := &mockStore{ensureStateErr: errors.New("db error")}
	fb := newFakeBot(ms, &mockCoach{})
	fb.handleCommand(context.Background(), 1, "/status")
	if !strContains(fb.messages[0], "Something went wrong") {
		t.Errorf("expected error message, got %q", fb.messages[0])
	}
}

func TestHandleChat(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}}
	mc := &mockCoach{response: "Hello, keep going!"}
	fb := newFakeBot(ms, mc)
	fb.handleChat(context.Background(), 1, "How am I doing?")
	if mc.chatCalls != 1 {
		t.Errorf("Chat calls = %d, want 1", mc.chatCalls)
	}
	if fb.messages[0] != "Hello, keep going!" {
		t.Errorf("response = %q, want %q", fb.messages[0], "Hello, keep going!")
	}
}

func TestHandleChatMessageTooLong(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}}
	fb := newFakeBot(ms, &mockCoach{})
	longMsg := make([]byte, maxMessageLen+1)
	for i := range longMsg {
		longMsg[i] = 'x'
	}
	fb.handleChat(context.Background(), 1, string(longMsg))
	if fb.messages[0] == "" || !strContains(fb.messages[0], "4000 characters") {
		t.Errorf("response = %q, want message_too_long", fb.messages[0])
	}
}

func TestHandleChatCoachError(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}}
	mc := &mockCoach{err: errors.New("api error")}
	fb := newFakeBot(ms, mc)
	fb.handleChat(context.Background(), 1, "Hello")
	if mc.chatCalls != 1 {
		t.Errorf("Chat calls = %d, want 1", mc.chatCalls)
	}
	if !strContains(fb.messages[0], "couldn't process") {
		t.Errorf("expected error message, got %q", fb.messages[0])
	}
}

func TestHandleChatContextCancelled(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}}
	fb := newFakeBot(ms, &mockCoach{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fb.handleChat(ctx, 1, "Hello")
	if len(fb.messages) != 0 {
		t.Errorf("len(messages) = %d, want 0 (context cancelled)", len(fb.messages))
	}
}

func TestHandleChatUpdatesHistory(t *testing.T) {
	ms := &mockStore{state: &store.State{Language: "en", Tone: "warm"}}
	mc := &mockCoach{response: "Got it!"}
	fb := newFakeBot(ms, mc)
	fb.handleChat(context.Background(), 1, "Hello")
	fb.handleChat(context.Background(), 1, "Follow up")
	if len(ms.history) != 4 {
		t.Errorf("history len = %d, want 4", len(ms.history))
	}
	if ms.history[1]["role"] != "assistant" || ms.history[1]["content"] != "Got it!" {
		t.Errorf("history[0] = %v", ms.history[0])
	}
}
