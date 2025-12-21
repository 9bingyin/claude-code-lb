package types

// 负载均衡模式常量
const (
	ModeLoadBalance = "load_balance"
	ModeFallback    = "fallback"
)

// 负载均衡算法常量
const (
	AlgorithmRoundRobin         = "round_robin"
	AlgorithmWeightedRoundRobin = "weighted_round_robin"
	AlgorithmRandom             = "random"
)

// 请求体大小限制常量
const (
	// DefaultMaxRequestBodySize 默认最大请求体大小 (32 MB，与 Claude API 一致)
	DefaultMaxRequestBodySize = 32 * 1024 * 1024
	// MaxAllowedRequestBodySize 最大允许设置的请求体大小 (128 MB)
	MaxAllowedRequestBodySize = 128 * 1024 * 1024
)

// ValidModes 有效的模式列表
var ValidModes = []string{ModeLoadBalance, ModeFallback}

// ValidAlgorithms 有效的算法列表
var ValidAlgorithms = []string{AlgorithmRoundRobin, AlgorithmWeightedRoundRobin, AlgorithmRandom}
