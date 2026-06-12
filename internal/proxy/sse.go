// Package proxy 实现 OpenAI-compatible HTTP 代理：重试链、SSE 透传、TTFT、规则引擎驱动的故障转移。
package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// sseEvent 一条 SSE data 行的解析结果。
type sseChunk struct {
	raw  []byte // 原始行（含 "data: " 前缀与结尾空行），透传用
	data []byte // JSON 部分；[DONE] 时为 nil
	done bool
}

// sseReader 从上游响应体逐条读 SSE 事件。
type sseReader struct {
	r *bufio.Reader
}

func newSSEReader(r io.Reader) *sseReader {
	return &sseReader{r: bufio.NewReaderSize(r, 64*1024)}
}

// next 读下一条事件；io.EOF 表示流自然结束。
func (s *sseReader) next() (*sseChunk, error) {
	var raw bytes.Buffer
	var data []byte
	for {
		line, err := s.r.ReadBytes('\n')
		raw.Write(line)
		if len(line) > 0 {
			trimmed := bytes.TrimRight(line, "\r\n")
			if len(trimmed) == 0 && data != nil {
				// 空行 = 事件边界
				break
			}
			if bytes.HasPrefix(trimmed, []byte("data:")) {
				d := bytes.TrimSpace(trimmed[5:])
				if bytes.Equal(d, []byte("[DONE]")) {
					return &sseChunk{raw: raw.Bytes(), done: true}, nil
				}
				data = append(data, d...)
			}
		}
		if err != nil {
			if data != nil {
				break // 残余数据也算一个事件
			}
			return nil, err
		}
	}
	return &sseChunk{raw: raw.Bytes(), data: data}, nil
}

// newModelRewriter 返回一个就地重写 sseChunk 中 model 字段的函数：
// 把上游原生模型名替换为 canonical 名，避免向客户端泄漏上游信息。
func newModelRewriter(native, canonical string) func(*sseChunk) {
	if native == "" || canonical == "" || native == canonical {
		return func(*sseChunk) {}
	}
	patterns := [][2][]byte{
		{[]byte(`"model":"` + native + `"`), []byte(`"model":"` + canonical + `"`)},
		{[]byte(`"model": "` + native + `"`), []byte(`"model": "` + canonical + `"`)},
	}
	return func(c *sseChunk) {
		if c.data == nil {
			return
		}
		for _, p := range patterns {
			if bytes.Contains(c.data, p[0]) {
				c.data = bytes.Replace(c.data, p[0], p[1], 1)
				raw := make([]byte, 0, len(c.data)+8)
				raw = append(raw, []byte("data: ")...)
				raw = append(raw, c.data...)
				raw = append(raw, '\n', '\n')
				c.raw = raw
				return
			}
		}
	}
}

// aggregator 把流式 SSE 聚合为一个非流式 chat.completion 响应。
type aggregator struct {
	id        string
	model     string
	created   int64
	role      string
	content   strings.Builder
	reasoning strings.Builder
	finish    string
	usage     json.RawMessage
}

type chunkPayload struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Created int64  `json:"created"`
	Choices []struct {
		Delta struct {
			Role             string `json:"role"`
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage json.RawMessage `json:"usage"`
}

func (a *aggregator) feed(data []byte) {
	var c chunkPayload
	if err := json.Unmarshal(data, &c); err != nil {
		return // 容忍无法解析的 chunk
	}
	if c.ID != "" {
		a.id = c.ID
	}
	if c.Model != "" {
		a.model = c.Model
	}
	if c.Created != 0 {
		a.created = c.Created
	}
	if len(c.Usage) > 0 && string(c.Usage) != "null" {
		a.usage = c.Usage
	}
	if len(c.Choices) > 0 {
		ch := c.Choices[0]
		if ch.Delta.Role != "" {
			a.role = ch.Delta.Role
		}
		a.content.WriteString(ch.Delta.Content)
		a.reasoning.WriteString(ch.Delta.ReasoningContent)
		if ch.FinishReason != nil && *ch.FinishReason != "" {
			a.finish = *ch.FinishReason
		}
	}
}

// result 生成非流式响应 JSON。requestModel = 客户端请求的 canonical 名（对外不暴露 native 名）。
func (a *aggregator) result(requestModel string) []byte {
	role := a.role
	if role == "" {
		role = "assistant"
	}
	finish := a.finish
	if finish == "" {
		finish = "stop"
	}
	msg := map[string]any{"role": role, "content": a.content.String()}
	if a.reasoning.Len() > 0 {
		msg["reasoning_content"] = a.reasoning.String()
	}
	resp := map[string]any{
		"id":      a.id,
		"object":  "chat.completion",
		"created": a.created,
		"model":   requestModel,
		"choices": []map[string]any{{
			"index":         0,
			"message":       msg,
			"finish_reason": finish,
		}},
	}
	if len(a.usage) > 0 {
		resp["usage"] = a.usage
	}
	b, _ := json.Marshal(resp)
	return b
}

// errorJSON 生成 OpenAI 风格错误体。
func errorJSON(message, typ string) []byte {
	b, _ := json.Marshal(map[string]any{
		"error": map[string]any{"message": message, "type": typ},
	})
	return b
}

// sseErrorEvent 流中途断开时发给客户端的 error event。
func sseErrorEvent(message string) []byte {
	return []byte(fmt.Sprintf("data: %s\n\n", errorJSON(message, "upstream_error")))
}
