package selector

import (
	"fmt"

	"claude-code-lb/pkg/types"
)

// CreateSelector 创建服务器选择器的工厂函数
func CreateSelector(config types.Config) (ServerSelector, error) {
	switch config.Mode {
	case types.ModeLoadBalance:
		return NewLoadBalancer(config), nil
	case types.ModeFallback:
		return NewFallbackSelector(config), nil
	default:
		// 向后兼容：如果没有设置mode，根据fallback字段判断
		if config.Fallback {
			return NewFallbackSelector(config), nil
		}
		return NewLoadBalancer(config), nil
	}
}

// GetSelectorType 获取选择器类型描述
func GetSelectorType(config types.Config) string {
	switch config.Mode {
	case types.ModeLoadBalance:
		return fmt.Sprintf("Load Balancer (%s)", config.Algorithm)
	case types.ModeFallback:
		return "Fallback Selector"
	default:
		if config.Fallback {
			return "Fallback Selector (Legacy)"
		}
		return fmt.Sprintf("Load Balancer (%s, Legacy)", config.Algorithm)
	}
}
