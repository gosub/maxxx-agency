package agent

import (
	"encoding/json"
	"fmt"
)

type ActionType string

const (
	ActionAddTask        ActionType = "add_task"
	ActionUpdateTask     ActionType = "update_task"
	ActionCompleteTask   ActionType = "complete_task"
	ActionDeleteTask     ActionType = "delete_task"
	ActionUpdateSchedule ActionType = "update_schedule"
)

type Action struct {
	Type        ActionType `json:"type"`
	Description string     `json:"description,omitempty"` // add_task, update_task
	ID          int64      `json:"id,omitempty"`          // update_task, complete_task, delete_task
	NextNudgeAt string     `json:"next_nudge_at,omitempty"` // add_task, update_task
	Schedule    string     `json:"schedule,omitempty"`    // update_schedule
}

type Response struct {
	Reply   string   `json:"reply"`
	Actions []Action `json:"actions"`
}

func ParseResponse(raw string) (*Response, error) {
	var r Response
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &r, nil
}
