package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Config Devfox 配置（对应 C 版 devfox_config_t）
type Config struct {
	BaseURL        string
	ModelName      string
	APIKey         string
	Temperature    float64
	MaxTokens      int
	Timeout        int
	EnableThinking bool
	ThinkingBudget int
	MaxRetries     int
}

// Limits 工具调用限制配置（config/limits.json，独立配置文件）
type Limits struct {
	MaxToolRounds    int `json:"max_tool_rounds"`     // LLM 决策轮次上限
	MaxToolCallsTotal int `json:"max_tool_calls_total"` // 总体工具调用次数上限
	MaxSameToolCalls  int `json:"max_same_tool_calls"`  // 相同工具+相同参数连续调用上限
}

// LoadLimits 从 config/limits.json 加载；文件不存在时使用默认值
func LoadLimits(path string) (*Limits, error) {
	l := &Limits{
		MaxToolRounds:     64,
		MaxToolCallsTotal: 64,
		MaxSameToolCalls:  4,
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return l, nil // 无文件用默认
	}
	if err := json.Unmarshal(data, l); err != nil {
		return nil, fmt.Errorf("limits 配置解析失败: %v", err)
	}
	if l.MaxToolRounds <= 0 {
		l.MaxToolRounds = 64
	}
	if l.MaxToolCallsTotal <= 0 {
		l.MaxToolCallsTotal = 64
	}
	if l.MaxSameToolCalls <= 0 {
		l.MaxSameToolCalls = 4
	}
	return l, nil
}

// Load 从 config.json 的 "devfox" 段加载，支持环境变量覆盖：
// DEVFOX_API_KEY / DEVFOX_BASE_URL / DEVFOX_MODEL
func Load(path string) (*Config, error) {
	cfg := &Config{
		Temperature:    0.7,
		MaxTokens:      4096,
		Timeout:        120,
		ThinkingBudget: 2048,
		MaxRetries:     3,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("配置文件未找到: %s", path)
	}

	var raw struct {
		Devfox struct {
			BaseURL        string   `json:"base_url"`
			ModelName      string   `json:"model_name"`
			APIKey         string   `json:"api_key"`
			Temperature    *float64 `json:"temperature"`
			MaxTokens      *int     `json:"max_tokens"`
			Timeout        *int     `json:"timeout"`
			EnableThinking bool     `json:"enable_thinking"`
			ThinkingBudget *int     `json:"thinking_budget"`
			MaxRetries     *int     `json:"max_retries"`
		} `json:"devfox"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("配置文件解析失败: %v", err)
	}

	d := raw.Devfox
	if d.BaseURL == "" || d.ModelName == "" || d.APIKey == "" {
		return nil, fmt.Errorf("配置段 'devfox' 缺少必要字段 (base_url/model_name/api_key)")
	}

	cfg.BaseURL = d.BaseURL
	cfg.ModelName = d.ModelName
	cfg.APIKey = d.APIKey
	if d.Temperature != nil {
		cfg.Temperature = *d.Temperature
	}
	if d.MaxTokens != nil {
		cfg.MaxTokens = *d.MaxTokens
	}
	if d.Timeout != nil {
		cfg.Timeout = *d.Timeout
	}
	cfg.EnableThinking = d.EnableThinking
	if d.ThinkingBudget != nil {
		cfg.ThinkingBudget = *d.ThinkingBudget
	}
	if d.MaxRetries != nil {
		cfg.MaxRetries = *d.MaxRetries
	}

	// 环境变量覆盖（优先级最高）
	if v := os.Getenv("DEVFOX_API_KEY"); v != "" {
		cfg.APIKey = v
	}
	if v := os.Getenv("DEVFOX_BASE_URL"); v != "" {
		cfg.BaseURL = v
	}
	if v := os.Getenv("DEVFOX_MODEL"); v != "" {
		cfg.ModelName = v
	}

	// 去掉 base_url 尾部斜杠
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return cfg, nil
}
