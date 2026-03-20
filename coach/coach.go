package coach

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"nudgent/log"
)

const (
	apiTimeoutSeconds   = 60
	apiMaxRetries       = 3
	apiRetryBackoffBase = 2
)

var logger = log.Logger.With().Str("component", "coach").Logger()

type Coach struct {
	apiKey string
	model  string
	client *http.Client
}

type Coacher interface {
	Chat(ctx context.Context, systemPrompt string, history []map[string]string, userMessage string) (string, error)
}

var _ Coacher = (*Coach)(nil)

func New(apiKey, model string) *Coach {
	return &Coach{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{Timeout: apiTimeoutSeconds * time.Second},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatChoice struct {
	Message chatMessage `json:"message"`
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *Coach) Chat(ctx context.Context, systemPrompt string, history []map[string]string, userMessage string) (string, error) {
	messages := []chatMessage{
		{Role: "system", Content: systemPrompt},
	}

	for _, h := range history {
		role := h["role"]
		if role != "user" && role != "assistant" {
			role = "user"
		}
		messages = append(messages, chatMessage{
			Role:    role,
			Content: h["content"],
		})
	}

	messages = append(messages, chatMessage{
		Role:    "user",
		Content: userMessage,
	})

	reqBody := chatRequest{
		Model:    c.model,
		Messages: messages,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < apiMaxRetries; attempt++ {
		if attempt > 0 {
			logger.Warn().Int("attempt", attempt).Err(lastErr).Msg("retrying chat request")
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(attempt) * apiRetryBackoffBase * time.Second):
			}
		}

		resp, err := c.doRequest(ctx, body)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			continue
		}

		return resp, nil
	}

	logger.Error().Int("attempts", apiMaxRetries).Err(lastErr).Msg("chat request failed after retries")
	return "", fmt.Errorf("chat failed after retries: %w", lastErr)
}

func (c *Coach) doRequest(ctx context.Context, body []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
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
