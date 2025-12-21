package types

import "time"

// HTTPClientConfig HTTP客户端配置
type HTTPClientConfig struct {
	DialTimeout           int `json:"dial_timeout"`             // 连接超时（秒），默认10
	KeepAlive             int `json:"keep_alive"`               // TCP keep-alive间隔（秒），默认30
	MaxIdleConns          int `json:"max_idle_conns"`           // 最大空闲连接数，默认100
	MaxIdleConnsPerHost   int `json:"max_idle_conns_per_host"`  // 每个host最大空闲连接数，默认20
	IdleConnTimeout       int `json:"idle_conn_timeout"`        // 空闲连接超时（秒），默认120
	ResponseHeaderTimeout int `json:"response_header_timeout"`  // 响应头超时（秒），默认30
	TLSHandshakeTimeout   int `json:"tls_handshake_timeout"`    // TLS握手超时（秒），默认10
	WriteBufferSize       int `json:"write_buffer_size"`        // 写缓冲区大小（字节），默认4096
	ReadBufferSize        int `json:"read_buffer_size"`         // 读缓冲区大小（字节），默认4096
}

type UpstreamServer struct {
	URL                  string    `json:"url"`
	Weight               int       `json:"weight"`
	Priority             int       `json:"priority"`       // fallback模式下的优先级，数字越小优先级越高
	Token                string    `json:"token"`
	FailThreshold        int       `json:"fail_threshold"` // 连续失败多少次才标记离线，默认1
	BalanceCheck         string    `json:"balance_check"`          // 余额查询命令（可选）
	BalanceCheckInterval int       `json:"balance_check_interval"` // 余额查询间隔（秒，可选）
	BalanceThreshold     float64   `json:"balance_threshold"`      // 余额阈值，小于等于此值标记为不可用（可选，默认0）
	DownUntil            time.Time `json:"-"`                      // 不可用直到这个时间
}

// 配置结构
type Config struct {
	Port       string           `json:"port"`
	Mode       string           `json:"mode"`       // "load_balance" 或 "fallback"
	Algorithm  string           `json:"algorithm"`  // "round_robin", "weighted_round_robin", "random"
	Servers    []UpstreamServer `json:"servers"`
	Fallback   bool             `json:"fallback"`   // 向后兼容字段
	Auth       bool             `json:"auth"`       // 是否启用鉴权
	AuthKeys   []string         `json:"auth_keys"`  // 允许的 API Key 列表
	Cooldown   int              `json:"cooldown"`   // 冷却时间（秒）
	Debug      bool             `json:"debug"`      // 是否启用调试模式
	MaxRetries         int              `json:"max_retries"`           // 最大重试次数，默认2
	MaxRequestBodySize int64            `json:"max_request_body_size"` // 最大请求体大小（字节），默认32MB
	HTTPClient         HTTPClientConfig `json:"http_client"`           // HTTP客户端配置
}

// Claude API 响应结构（用于解析 usage 信息）
type ClaudeUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

type ClaudeResponse struct {
	Model string      `json:"model"`
	Usage ClaudeUsage `json:"usage"`
}
