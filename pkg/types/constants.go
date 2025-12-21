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

// ValidModes 有效的模式列表
var ValidModes = []string{ModeLoadBalance, ModeFallback}

// ValidAlgorithms 有效的算法列表
var ValidAlgorithms = []string{AlgorithmRoundRobin, AlgorithmWeightedRoundRobin, AlgorithmRandom}
