package store

import (
	"context"
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

// --- Tasks ---

func TestAddTask(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	task, err := s.AddTask(ctx, 1, "Call dentist — before end of March", false)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if task.ID == 0 {
		t.Error("expected non-zero task ID")
	}
	if task.Description != "Call dentist — before end of March" {
		t.Errorf("Description = %q", task.Description)
	}
}

func TestAddTask_Recurring(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	task, err := s.AddTask(ctx, 1, "Daily standup", true)
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if !task.Recurring {
		t.Error("expected Recurring = true")
	}
	tasks, _ := s.GetTasks(ctx, 1)
	if !tasks[0].Recurring {
		t.Error("expected Recurring = true after GetTasks")
	}
}

func TestGetTasks_Empty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	tasks, err := s.GetTasks(ctx, 1)
	if err != nil {
		t.Fatalf("GetTasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("len(tasks) = %d, want 0", len(tasks))
	}
}

func TestGetTasks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, _ = s.AddTask(ctx, 1, "Task A", false)
	_, _ = s.AddTask(ctx, 1, "Task B", false)

	tasks, err := s.GetTasks(ctx, 1)
	if err != nil {
		t.Fatalf("GetTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("len(tasks) = %d, want 2", len(tasks))
	}
	if tasks[0].Description != "Task A" {
		t.Errorf("tasks[0].Description = %q", tasks[0].Description)
	}
}

func TestGetTasks_ExcludesCompleted(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	task, _ := s.AddTask(ctx, 1, "Task A", false)
	_, _ = s.AddTask(ctx, 1, "Task B", false)
	_ = s.CompleteTask(ctx, task.ID)

	tasks, err := s.GetTasks(ctx, 1)
	if err != nil {
		t.Fatalf("GetTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d, want 1", len(tasks))
	}
	if tasks[0].Description != "Task B" {
		t.Errorf("tasks[0].Description = %q, want Task B", tasks[0].Description)
	}
}

func TestGetTasks_IsolatedByUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, _ = s.AddTask(ctx, 1, "User 1 task", false)
	_, _ = s.AddTask(ctx, 2, "User 2 task", false)

	tasks, _ := s.GetTasks(ctx, 1)
	if len(tasks) != 1 || tasks[0].Description != "User 1 task" {
		t.Errorf("user isolation failed: %v", tasks)
	}
}

func TestUpdateTask(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	task, _ := s.AddTask(ctx, 1, "Old description", false)
	if err := s.UpdateTask(ctx, task.ID, "New description"); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}

	tasks, _ := s.GetTasks(ctx, 1)
	if tasks[0].Description != "New description" {
		t.Errorf("Description = %q, want %q", tasks[0].Description, "New description")
	}
}

func TestSetNextNudgeAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	task, _ := s.AddTask(ctx, 1, "Task", false)
	if err := s.SetNextNudgeAt(ctx, task.ID, "2026-03-21T09:00:00"); err != nil {
		t.Fatalf("SetNextNudgeAt: %v", err)
	}

	tasks, _ := s.GetTasks(ctx, 1)
	if tasks[0].NextNudgeAt != "2026-03-21T09:00:00" {
		t.Errorf("NextNudgeAt = %q", tasks[0].NextNudgeAt)
	}
}

func TestSetRecurring(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	task, _ := s.AddTask(ctx, 1, "Task", false)
	if err := s.SetRecurring(ctx, task.ID, true); err != nil {
		t.Fatalf("SetRecurring: %v", err)
	}

	tasks, _ := s.GetTasks(ctx, 1)
	if !tasks[0].Recurring {
		t.Error("expected Recurring = true after SetRecurring")
	}
}

func TestCompleteTask(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	task, _ := s.AddTask(ctx, 1, "Task", false)
	if err := s.CompleteTask(ctx, task.ID); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	tasks, _ := s.GetTasks(ctx, 1)
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks after complete, got %d", len(tasks))
	}
}

func TestCompleteTask_LogsEvent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	task, _ := s.AddTask(ctx, 1, "Gym", false)
	_ = s.CompleteTask(ctx, task.ID)

	events, err := s.GetEventsForPeriod(ctx, 1, "completed", "2000-01-01T00:00:00", "2999-12-31T23:59:59")
	if err != nil {
		t.Fatalf("GetEventsForPeriod: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 completed event, got %d", len(events))
	}
	if events[0].Description != "Gym" {
		t.Errorf("event description = %q, want Gym", events[0].Description)
	}
}

func TestDeleteTask(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	task, _ := s.AddTask(ctx, 1, "Task", false)
	if err := s.DeleteTask(ctx, task.ID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	tasks, _ := s.GetTasks(ctx, 1)
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks after delete, got %d", len(tasks))
	}
}

func TestGetDueTasks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	t1, _ := s.AddTask(ctx, 1, "Overdue task", false)
	_ = s.SetNextNudgeAt(ctx, t1.ID, "2026-03-20T08:00:00")
	t2, _ := s.AddTask(ctx, 1, "Future task", false)
	_ = s.SetNextNudgeAt(ctx, t2.ID, "2026-03-25T09:00:00")
	_, _ = s.AddTask(ctx, 1, "No nudge set", false)

	due, err := s.GetDueTasks(ctx, 1, "2026-03-20T10:00:00")
	if err != nil {
		t.Fatalf("GetDueTasks: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("len(due) = %d, want 1", len(due))
	}
	if due[0].Description != "Overdue task" {
		t.Errorf("due[0].Description = %q", due[0].Description)
	}
}

func TestGetDueTasks_ExcludesCompleted(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	task, _ := s.AddTask(ctx, 1, "Done task", false)
	_ = s.SetNextNudgeAt(ctx, task.ID, "2026-03-20T08:00:00")
	_ = s.CompleteTask(ctx, task.ID)

	due, _ := s.GetDueTasks(ctx, 1, "2026-03-20T10:00:00")
	if len(due) != 0 {
		t.Errorf("expected 0 due tasks, got %d", len(due))
	}
}

func TestGetTasksForPeriod(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	t1, _ := s.AddTask(ctx, 1, "Morning task", false)
	_ = s.SetNextNudgeAt(ctx, t1.ID, "2026-03-22T09:00:00")
	t2, _ := s.AddTask(ctx, 1, "Evening task", false)
	_ = s.SetNextNudgeAt(ctx, t2.ID, "2026-03-22T18:00:00")
	t3, _ := s.AddTask(ctx, 1, "Tomorrow task", false)
	_ = s.SetNextNudgeAt(ctx, t3.ID, "2026-03-23T09:00:00")

	tasks, err := s.GetTasksForPeriod(ctx, 1, "2026-03-22T00:00:00", "2026-03-22T23:59:59")
	if err != nil {
		t.Fatalf("GetTasksForPeriod: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("len(tasks) = %d, want 2", len(tasks))
	}
}

func TestGetEventsForPeriod(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	task, _ := s.AddTask(ctx, 1, "Gym", false)
	_ = s.CompleteTask(ctx, task.ID)

	events, err := s.GetEventsForPeriod(ctx, 1, "completed", "2000-01-01T00:00:00", "2999-12-31T23:59:59")
	if err != nil {
		t.Fatalf("GetEventsForPeriod: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].EventType != "completed" {
		t.Errorf("EventType = %q", events[0].EventType)
	}
}

// --- Prefs ---

func TestEnsurePrefs_CreatesNew(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p, err := s.EnsurePrefs(ctx, 1, "en", 30)
	if err != nil {
		t.Fatalf("EnsurePrefs: %v", err)
	}
	if p.Language != "en" {
		t.Errorf("Language = %q, want en", p.Language)
	}
	if p.NudgeIntervalM != 30 {
		t.Errorf("NudgeIntervalM = %d, want 30", p.NudgeIntervalM)
	}
}

func TestEnsurePrefs_ReturnsExisting(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, _ = s.EnsurePrefs(ctx, 1, "en", 30)
	p, err := s.EnsurePrefs(ctx, 1, "it", 60)
	if err != nil {
		t.Fatalf("EnsurePrefs: %v", err)
	}
	if p.Language != "en" {
		t.Errorf("Language = %q, want en (should not overwrite)", p.Language)
	}
}

func TestGetPrefs_NonExistent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p, err := s.GetPrefs(ctx, 999)
	if err != nil {
		t.Fatalf("GetPrefs: %v", err)
	}
	if p != nil {
		t.Errorf("expected nil prefs for unknown user, got %+v", p)
	}
}

func TestSetLanguage(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, _ = s.EnsurePrefs(ctx, 1, "en", 30)
	if err := s.SetLanguage(ctx, 1, "it"); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}

	p, _ := s.GetPrefs(ctx, 1)
	if p.Language != "it" {
		t.Errorf("Language = %q, want it", p.Language)
	}
}

func TestSetSchedule(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, _ = s.EnsurePrefs(ctx, 1, "en", 30)
	if err := s.SetSchedule(ctx, 1, "weekdays 9-13 and 15-19"); err != nil {
		t.Fatalf("SetSchedule: %v", err)
	}

	p, _ := s.GetPrefs(ctx, 1)
	if p.Schedule != "weekdays 9-13 and 15-19" {
		t.Errorf("Schedule = %q", p.Schedule)
	}
}

func TestSetLastWakeupAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, _ = s.EnsurePrefs(ctx, 1, "en", 30)
	if err := s.SetLastWakeupAt(ctx, 1, "2026-03-20T09:00:00"); err != nil {
		t.Fatalf("SetLastWakeupAt: %v", err)
	}

	p, _ := s.GetPrefs(ctx, 1)
	if p.LastWakeupAt != "2026-03-20T09:00:00" {
		t.Errorf("LastWakeupAt = %q", p.LastWakeupAt)
	}
}

func TestContextCancellation(t *testing.T) {
	s := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.AddTask(ctx, 1, "Task", false)
	if err == nil {
		t.Error("expected error with cancelled context, got nil")
	}
}
