package types

import "time"

type UpstreamServer struct {
	URL       string    `json:"url"`
	Weight    int       `json:"weight"`
	Priority  int       `json:"priority"` // fallback模式下的优先级，数字越小优先级越高
	Token     string    `json:"token"`
	DownUntil time.Time `json:"-"` // 不可用直到这个时间
}

// 配置结构
type Config struct {
	Port      string           `json:"port"`
	Mode      string           `json:"mode"`      // "load_balance" 或 "fallback"
	Algorithm string           `json:"algorithm"` // "round_robin", "weighted_round_robin", "random"
	Servers   []UpstreamServer `json:"servers"`
	Fallback  bool             `json:"fallback"`  // 向后兼容字段
	Auth      bool             `json:"auth"`      // 是否启用鉴权
	AuthKeys  []string         `json:"auth_keys"` // 允许的 API Key 列表
	Cooldown  int              `json:"cooldown"`  // 冷却时间（秒）
	Debug     bool             `json:"debug"`     // 是否启用调试模式
}

// Claude API 响应结构（用于解析 usage 信息）
type ClaudeUsage struct {
	InputTokens             int `json:"input_tokens"`
	OutputTokens            int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens    int `json:"cache_read_input_tokens"`
}

type ClaudeResponse struct {
	Model string      `json:"model"`
	Usage ClaudeUsage `json:"usage"`
}
