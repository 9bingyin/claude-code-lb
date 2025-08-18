package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"claude-code-lb/internal/auth"
	"claude-code-lb/internal/balance"
	"claude-code-lb/internal/config"
	"claude-code-lb/internal/health"
	"claude-code-lb/internal/logger"
	"claude-code-lb/internal/proxy"
	"claude-code-lb/internal/stats"

	"github.com/gin-gonic/gin"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	// 解析命令行参数
	var showVersion = flag.Bool("version", false, "Show version information")
	var showHelp = flag.Bool("help", false, "Show help information")
	flag.Parse()

	if *showVersion {
		fmt.Printf("Claude Code Load Balancer\n")
		fmt.Printf("Version: %s\n", version)
		fmt.Printf("Commit: %s\n", commit)
		fmt.Printf("Built: %s\n", date)
		os.Exit(0)
	}

	if *showHelp {
		fmt.Printf("Claude Code Load Balancer - A high-performance load balancer for Claude API endpoints\n\n")
		fmt.Printf("Usage:\n")
		fmt.Printf("  claude-code-lb [options]\n\n")
		fmt.Printf("Options:\n")
		flag.PrintDefaults()
		fmt.Printf("\nEnvironment Variables:\n")
		fmt.Printf("  CONFIG_FILE    Configuration file path (default: config.json)\n")
		os.Exit(0)
	}

	// 加载配置
	cfg := config.Load()

	// 创建负载均衡器
	balancer := balance.New(cfg)

	// 创建统计报告器
	statsReporter := stats.New()

	// 创建健康检查器
	healthChecker := health.NewChecker(cfg, balancer)

	// 设置 Gin 为发布模式，关闭调试日志
	gin.SetMode(gin.ReleaseMode)

	// 创建不带默认中间件的 Gin 引擎
	r := gin.New()

	// 添加我们自己的日志中间件
	r.Use(statsReporter.GinLoggerMiddleware())

	// 添加恢复中间件
	r.Use(gin.Recovery())

	// 设置信任的代理，如果不需要获取真实IP可以设置为空
	r.SetTrustedProxies([]string{})

	// 健康检查路由
	r.GET("/health", health.Handler(cfg, balancer))

	// 在需要鉴权的路由上应用鉴权中间件和代理处理
	r.Any("/v1/*path", auth.Middleware(cfg), proxy.Handler(balancer, statsReporter))

	// 启动被动健康检查（自动恢复冷却期过期的服务器）
	go healthChecker.PassiveHealthCheck()

	// 启动统计报告器
	go statsReporter.StartReporter()

	port := cfg.Port
	if port == "" {
		port = "3000"
	}

	log.Printf("%s==================== Claude Code Proxy ====================%s", logger.ColorBold, logger.ColorReset)
	logger.Info("STARTUP", "Version: %s (commit: %s, built: %s)", version, commit, date)
	logger.Info("STARTUP", "Starting server on port %s", port)
	logger.Info("STARTUP", "Load balancer: %s (%d servers)", cfg.Algorithm, len(cfg.Servers))
	logger.Info("STARTUP", "Fallback: %t | Circuit breaker: %ds", cfg.Fallback, cfg.Cooldown)
	logger.Info("STARTUP", "Health check: passive (auto-recovery after cooldown)")
	logger.Info("STARTUP", "Authentication: %t", cfg.Auth)
	if cfg.Auth {
		logger.Info("STARTUP", "  Allowed keys: %d", len(cfg.AuthKeys))
	}
	log.Printf("%s==========================================================%s", logger.ColorBold, logger.ColorReset)
	log.Fatal(r.Run(":" + port))
}
