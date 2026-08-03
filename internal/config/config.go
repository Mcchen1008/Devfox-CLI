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
			BaseURL        string  `json:"base_url"`
			ModelName      string  `json:"model_name"`
			APIKey         string  `json:"api_key"`
			Temperature    float64 `json:"temperature"`
			MaxTokens      int     `json:"max_tokens"`
			Timeout        int     `json:"timeout"`
			EnableThinking bool    `json:"enable_thinking"`
			ThinkingBudget int     `json:"thinking_budget"`
			MaxRetries     int     `json:"max_retries"`
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
	if d.Temperature != 0 {
		cfg.Temperature = d.Temperature
	}
	if d.MaxTokens != 0 {
		cfg.MaxTokens = d.MaxTokens
	}
	if d.Timeout != 0 {
		cfg.Timeout = d.Timeout
	}
	cfg.EnableThinking = d.EnableThinking
	if d.ThinkingBudget != 0 {
		cfg.ThinkingBudget = d.ThinkingBudget
	}
	if d.MaxRetries != 0 {
		cfg.MaxRetries = d.MaxRetries
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
