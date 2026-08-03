package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"

	"github.com/Mcchen1008/devfox-go/internal/config"
	"github.com/Mcchen1008/devfox-go/internal/history"
	"github.com/Mcchen1008/devfox-go/internal/skills"
	"github.com/Mcchen1008/devfox-go/internal/tools"
)

// ---------- 消息结构 ----------

type msg struct {
	Role         string         `json:"role"`
	Content      string         `json:"content,omitempty"`
	ToolCallID   string         `json:"tool_call_id,omitempty"`
	ToolCalls    json.RawMessage `json:"tool_calls,omitempty"`
}

// ---------- 工具定义 ----------

func appendToolDefs(mgr *skills.Manager) []map[string]any {
	defs := make([]map[string]any, 0, len(tools.BaseTools)+1)
	for i := range tools.BaseTools {
		defs = append(defs, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tools.BaseTools[i].Name,
				"description": tools.BaseTools[i].Description,
				"parameters":  json.RawMessage(tools.BaseTools[i].Parameters),
			},
		})
	}
	// use_skill 动态技能工具
	names := mgr.Names()
	defs = append(defs, map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        "use_skill",
			"description": "加载并使用一个技能。技能是一组预定义的专业指令/工作流，调用后会返回该技能的完整说明文档，请仔细阅读并按文档要求完成任务。",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"skill_name": map[string]any{
						"type":        "string",
						"description": "要使用的技能名称，可选值: " + names,
					},
				},
				"required": []string{"skill_name"},
			},
		},
	})
	return defs
}

// BuildPayload 构建 chat/completions 请求体（对应 C 版 llm_build_payload）
func BuildPayload(cfg *config.Config, mgr *skills.Manager, hist *history.History,
	sys, tctx, user string) ([]byte, error) {

	messages := []msg{
		{Role: "system", Content: sys},
		{Role: "system", Content: tctx},
	}
	for _, m := range hist.Get() {
		switch {
		case m.ToolCallID != "":
			messages = append(messages, msg{Role: "tool", ToolCallID: m.ToolCallID, Content: m.Content})
		case m.RawAssistant != "":
			messages = append(messages, msg{Role: "assistant", Content: m.Content, ToolCalls: json.RawMessage(m.RawAssistant)})
		default:
			messages = append(messages, msg{Role: m.Role, Content: m.Content})
		}
	}
	messages = append(messages, msg{Role: "user", Content: user})

	payload := map[string]any{
		"model":       cfg.ModelName,
		"messages":    messages,
		"temperature": cfg.Temperature,
		"max_tokens":  cfg.MaxTokens,
		"stream":      false,
		"tools":       appendToolDefs(mgr),
		"tool_choice": "auto",
	}
	if cfg.EnableThinking {
		payload["chat_template_kwargs"] = map[string]any{
			"enable_thinking":  true,
			"thinking_budget":  cfg.ThinkingBudget,
		}
	}
	return json.Marshal(payload)
}

// ---------- HTTP 请求（指数退避重试） ----------

// Chat 发送请求并返回响应体（对应 C 版 llm_chat + http_post_json）
func Chat(cfg *config.Config, payload []byte) (string, error) {
	url := cfg.BaseURL + "/chat/completions"

	var lastErr error
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		body, status, err := postOnce(cfg, url, payload)
		if err == nil {
			return body, nil
		}
		lastErr = err

		retryable := false
		if status == http.StatusTooManyRequests || status >= 500 {
			retryable = true
		}
		if status == 0 {
			retryable = true // 网络错误
		}
		if !retryable || attempt >= cfg.MaxRetries {
			return "", lastErr
		}

		// 指数退避 + 随机抖动，上限 30s
		backoff := 1 << attempt
		wait := float64(backoff) + rand.Float64()
		if wait > 30.0 {
			wait = 30.0
		}
		time.Sleep(time.Duration(wait * float64(time.Second)))
	}
	return "", fmt.Errorf("重试次数耗尽: %v", lastErr)
}

func postOnce(cfg *config.Config, url string, payload []byte) (string, int, error) {
	client := &http.Client{Timeout: time.Duration(cfg.Timeout) * time.Second}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("User-Agent", "devfox-go/beta-0.1")

	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("网络错误: %v", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp.StatusCode, fmt.Errorf("读取响应失败: %v", err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return string(data), resp.StatusCode, nil
	}
	body := string(data)
	if len(body) > 500 {
		body = body[:500]
	}
	return "", resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
}
