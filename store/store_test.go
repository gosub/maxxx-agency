package store

import (
	"context"
	"encoding/json"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestNew(t *testing.T) {
	s := newTestStore(t)
	if s == nil {
		t.Fatal("expected non-nil store")
	}
}

func TestEnsureState_CreatesNew(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	st, err := s.EnsureState(ctx, 1, "en", "direct")
	if err != nil {
		t.Fatalf("EnsureState: %v", err)
	}
	if st.UserID != 1 {
		t.Errorf("UserID = %d, want 1", st.UserID)
	}
	if st.Language != "en" {
		t.Errorf("Language = %q, want %q", st.Language, "en")
	}
	if st.Tone != "direct" {
		t.Errorf("Tone = %q, want %q", st.Tone, "direct")
	}
	if st.CurrentPhase != 0 {
		t.Errorf("CurrentPhase = %d, want 0", st.CurrentPhase)
	}
	if st.CurrentGoals != "[]" {
		t.Errorf("CurrentGoals = %q, want %q", st.CurrentGoals, "[]")
	}
}

func TestEnsureState_ReturnsExisting(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, _ = s.EnsureState(ctx, 1, "en", "direct")

	st, err := s.EnsureState(ctx, 1, "it", "warm")
	if err != nil {
		t.Fatalf("EnsureState: %v", err)
	}
	if st.Language != "en" {
		t.Errorf("Language = %q, want %q (should not overwrite)", st.Language, "en")
	}
	if st.Tone != "direct" {
		t.Errorf("Tone = %q, want %q (should not overwrite)", st.Tone, "direct")
	}
}

func TestGetState_NonExistent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	st, err := s.GetState(ctx, 999)
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if st != nil {
		t.Errorf("expected nil state for nonexistent user, got %+v", st)
	}
}

func TestSetLanguage(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, _ = s.EnsureState(ctx, 1, "en", "direct")

	if err := s.SetLanguage(ctx, 1, "it"); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}

	st, _ := s.GetState(ctx, 1)
	if st.Language != "it" {
		t.Errorf("Language = %q, want %q", st.Language, "it")
	}
}

func TestSetTone(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, _ = s.EnsureState(ctx, 1, "en", "warm")

	if err := s.SetTone(ctx, 1, "drill-sergeant"); err != nil {
		t.Fatalf("SetTone: %v", err)
	}

	st, _ := s.GetState(ctx, 1)
	if st.Tone != "drill-sergeant" {
		t.Errorf("Tone = %q, want %q", st.Tone, "drill-sergeant")
	}
}

func TestAddRejection(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	count, err := s.AddRejection(ctx, 1)
	if err != nil {
		t.Fatalf("AddRejection: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}

	count, err = s.AddRejection(ctx, 1)
	if err != nil {
		t.Fatalf("AddRejection: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}

	st, _ := s.GetState(ctx, 1)
	var rejections []string
	if err := json.Unmarshal([]byte(st.RejectionLog), &rejections); err != nil {
		t.Fatalf("unmarshal rejection log: %v", err)
	}
	if len(rejections) != 2 {
		t.Errorf("len(rejections) = %d, want 2", len(rejections))
	}
}

func TestAddGoal(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.AddGoal(ctx, 1, "Read Dune"); err != nil {
		t.Fatalf("AddGoal: %v", err)
	}
	if err := s.AddGoal(ctx, 1, "Start rejection log"); err != nil {
		t.Fatalf("AddGoal: %v", err)
	}

	goals, err := s.GetGoals(ctx, 1)
	if err != nil {
		t.Fatalf("GetGoals: %v", err)
	}
	if len(goals) != 2 {
		t.Fatalf("len(goals) = %d, want 2", len(goals))
	}
	if goals[0] != "Read Dune" {
		t.Errorf("goals[0] = %q, want %q", goals[0], "Read Dune")
	}
	if goals[1] != "Start rejection log" {
		t.Errorf("goals[1] = %q, want %q", goals[1], "Start rejection log")
	}
}

func TestCompleteGoal(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_ = s.AddGoal(ctx, 1, "Read Dune")
	_ = s.AddGoal(ctx, 1, "Start log")
	_ = s.AddGoal(ctx, 1, "Exercise daily")

	if err := s.CompleteGoal(ctx, 1, 1); err != nil {
		t.Fatalf("CompleteGoal: %v", err)
	}

	goals, _ := s.GetGoals(ctx, 1)
	if len(goals) != 2 {
		t.Fatalf("len(goals) = %d, want 2", len(goals))
	}
	if goals[0] != "Read Dune" {
		t.Errorf("goals[0] = %q, want %q", goals[0], "Read Dune")
	}
	if goals[1] != "Exercise daily" {
		t.Errorf("goals[1] = %q, want %q", goals[1], "Exercise daily")
	}
}

func TestCompleteGoal_InvalidIndex(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_ = s.AddGoal(ctx, 1, "Only goal")

	if err := s.CompleteGoal(ctx, 1, 5); err == nil {
		t.Error("expected error for out-of-range index, got nil")
	}
	if err := s.CompleteGoal(ctx, 1, -1); err == nil {
		t.Error("expected error for negative index, got nil")
	}
}

func TestConversationHistory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	msgs := []map[string]string{
		{"role": "user", "content": "Hello"},
		{"role": "assistant", "content": "Hi there"},
	}
	if err := s.SetConversationHistory(ctx, 1, msgs); err != nil {
		t.Fatalf("SetConversationHistory: %v", err)
	}

	got, err := s.GetConversationHistory(ctx, 1)
	if err != nil {
		t.Fatalf("GetConversationHistory: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0]["role"] != "user" {
		t.Errorf("got[0][role] = %q, want %q", got[0]["role"], "user")
	}
}

func TestConversationHistory_Empty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	got, err := s.GetConversationHistory(ctx, 1)
	if err != nil {
		t.Fatalf("GetConversationHistory: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len(got) = %d, want 0", len(got))
	}
}

func TestCheckin(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	date, err := s.GetLastCheckin(ctx, 1)
	if err != nil {
		t.Fatalf("GetLastCheckin: %v", err)
	}
	if date != "" {
		t.Errorf("date = %q, want empty", date)
	}

	if err := s.MarkCheckin(ctx, 1); err != nil {
		t.Fatalf("MarkCheckin: %v", err)
	}

	date, err = s.GetLastCheckin(ctx, 1)
	if err != nil {
		t.Fatalf("GetLastCheckin: %v", err)
	}
	if date == "" {
		t.Error("expected non-empty date after MarkCheckin")
	}
	if len(date) != 10 {
		t.Errorf("date = %q, expected YYYY-MM-DD format", date)
	}
}

func TestMultipleUsers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_ = s.AddGoal(ctx, 1, "User 1 goal")
	_ = s.AddGoal(ctx, 2, "User 2 goal")

	goals1, _ := s.GetGoals(ctx, 1)
	goals2, _ := s.GetGoals(ctx, 2)

	if len(goals1) != 1 || goals1[0] != "User 1 goal" {
		t.Errorf("user 1 goals = %v, want [User 1 goal]", goals1)
	}
	if len(goals2) != 1 || goals2[0] != "User 2 goal" {
		t.Errorf("user 2 goals = %v, want [User 2 goal]", goals2)
	}
}

func TestUpdateState(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	st, _ := s.EnsureState(ctx, 1, "en", "warm")
	st.CurrentPhase = 2
	st.ConfigNotes = "some notes"

	if err := s.UpdateState(ctx, st); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}

	got, _ := s.GetState(ctx, 1)
	if got.CurrentPhase != 2 {
		t.Errorf("CurrentPhase = %d, want 2", got.CurrentPhase)
	}
	if got.ConfigNotes != "some notes" {
		t.Errorf("ConfigNotes = %q, want %q", got.ConfigNotes, "some notes")
	}
}

func TestContextCancellation(t *testing.T) {
	s := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := s.EnsureState(ctx, 1, "en", "warm")
	if err == nil {
		t.Error("expected error with cancelled context, got nil")
	}
}
