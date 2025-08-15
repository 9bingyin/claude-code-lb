package types

import "time"

type UpstreamServer struct {
	URL       string    `json:"url"`
	Weight    int       `json:"weight"`
	Token     string    `json:"token"`
	DownUntil time.Time `json:"-"` // 不可用直到这个时间
}

type LoadBalancer struct {
	Type    string           `json:"type"` // "round_robin", "weighted_round_robin", "random"
	Servers []UpstreamServer `json:"servers"`
}

type Config struct {
	Port         string       `json:"port"`
	LoadBalancer LoadBalancer `json:"load_balancer"`
	Fallback     bool         `json:"fallback"`
	Auth         struct {
		Enabled     bool     `json:"enabled"`      // 是否启用鉴权
		AllowedKeys []string `json:"allowed_keys"` // 允许的 API Key 列表
	} `json:"auth"`
	CircuitBreaker struct {
		CooldownSeconds int `json:"cooldown_seconds"` // 标记为down后的冷却时间
	} `json:"circuit_breaker"`
}