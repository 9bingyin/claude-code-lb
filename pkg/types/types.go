package types

import "time"

type UpstreamServer struct {
	URL       string    `json:"url"`
	Weight    int       `json:"weight"`
	Token     string    `json:"token"`
	DownUntil time.Time `json:"-"` // 不可用直到这个时间
}

// 配置结构
type Config struct {
	Port      string           `json:"port"`
	Algorithm string           `json:"algorithm"`   // "round_robin", "weighted_round_robin", "random"
	Servers   []UpstreamServer `json:"servers"`
	Fallback  bool             `json:"fallback"`
	Auth      bool             `json:"auth"`        // 是否启用鉴权
	AuthKeys  []string         `json:"auth_keys"`   // 允许的 API Key 列表
	Cooldown  int              `json:"cooldown"`    // 冷却时间（秒）
}