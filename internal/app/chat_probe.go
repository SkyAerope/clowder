package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func (a *App) checkViaChat(ctx context.Context, baseURL, apiKey, model string, timeout time.Duration) (CheckProbeResult, error) {
	url := JoinURL(baseURL, "/v1/chat/completions")
	body := map[string]any{
		"model": model, "messages": []map[string]any{{"role": "user", "content": "回答yes"}},
		"max_tokens": 16, "stream": true,
	}
	up, err := json.Marshal(body)
	if err != nil {
		return CheckProbeResult{}, err
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, url, bytes.NewReader(up))
	if err != nil {
		return CheckProbeResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return CheckProbeResult{Status: "error"}, err
	}
	defer resp.Body.Close()

	status := checkStatus(resp.StatusCode)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
		if readErr != nil {
			return CheckProbeResult{Status: "error", StatusCode: resp.StatusCode}, fmt.Errorf("read check response: %w", readErr)
		}
		return CheckProbeResult{Status: status, StatusCode: resp.StatusCode, ResponseBody: string(responseBody)}, nil
	}
	return readCheckStream(resp.Body, start, resp.StatusCode, status)
}

func checkStatus(statusCode int) string {
	switch {
	case statusCode >= 200 && statusCode < 300:
		return "active"
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return "invalid"
	case statusCode == http.StatusTooManyRequests:
		return "rate_limited"
	default:
		return "error"
	}
}

func readCheckStream(body io.Reader, start time.Time, statusCode int, status string) (CheckProbeResult, error) {
	var ttftMS int64
	var modelResponse strings.Builder
	var responseBody strings.Builder
	scanner := bufio.NewScanner(io.LimitReader(body, 1<<20))
	for scanner.Scan() {
		data := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(scanner.Text()), "data:"))
		if data == "[DONE]" {
			break
		}
		if data == "" || !strings.HasPrefix(strings.TrimSpace(scanner.Text()), "data:") {
			continue
		}
		responseBody.WriteString(data)
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil || len(chunk.Choices) == 0 || chunk.Choices[0].Delta.Content == "" {
			continue
		}
		if modelResponse.Len() == 0 {
			ttftMS = time.Since(start).Milliseconds()
		}
		modelResponse.WriteString(chunk.Choices[0].Delta.Content)
	}
	if err := scanner.Err(); err != nil {
		return CheckProbeResult{Status: "error"}, fmt.Errorf("read check response: %w", err)
	}
	return CheckProbeResult{
		Status: status, ModelResponse: modelResponse.String(), TTFTMS: ttftMS,
		StatusCode: statusCode, ResponseBody: responseBody.String(),
	}, nil
}
