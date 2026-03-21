package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog"

	"github.com/gosub/nudgent/log"
)

const (
	apiTimeoutSeconds   = 60
	apiMaxRetries       = 3
	apiRetryBackoffBase = 2
)

// Message is a single chat turn (user or assistant).
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Agent struct {
	apiKey string
	model  string
	client *http.Client
	log    zerolog.Logger
}

type Agenter interface {
	Chat(ctx context.Context, systemPrompt string, history []Message, userMessage string) (string, error)
}

var _ Agenter = (*Agent)(nil)

func New(apiKey, model string) *Agent {
	return &Agent{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{Timeout: apiTimeoutSeconds * time.Second},
		log:    log.Logger.With().Str("component", "agent").Logger(),
	}
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type chatChoice struct {
	Message Message `json:"message"`
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (a *Agent) Chat(ctx context.Context, systemPrompt string, history []Message, userMessage string) (string, error) {
	messages := []Message{
		{Role: "system", Content: systemPrompt},
	}
	messages = append(messages, history...)
	messages = append(messages, Message{Role: "user", Content: userMessage})

	body, err := json.Marshal(chatRequest{Model: a.model, Messages: messages})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < apiMaxRetries; attempt++ {
		if attempt > 0 {
			a.log.Warn().Int("attempt", attempt).Err(lastErr).Msg("retrying request")
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(attempt) * apiRetryBackoffBase * time.Second):
			}
		}

		resp, err := a.doRequest(ctx, body)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			continue
		}
		return resp, nil
	}

	a.log.Error().Int("attempts", apiMaxRetries).Err(lastErr).Msg("request failed after retries")
	return "", fmt.Errorf("request failed after retries: %w", lastErr)
}

func (a *Agent) doRequest(ctx context.Context, body []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)

	resp, err := a.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if chatResp.Error != nil {
		return "", fmt.Errorf("API error: %s", chatResp.Error.Message)
	}
	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}
	return chatResp.Choices[0].Message.Content, nil
}
