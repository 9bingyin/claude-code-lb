# Claude Code 代理服务

这是一个用 Go 语言实现的高可用 Claude Code 请求转发服务，支持负载均衡、故障转移和健康检查功能。

## 功能特性

- **负载均衡**: 支持轮询、加权轮询、随机三种算法
- **故障转移**: 自动检测服务器故障并切换到可用服务器
- **健康检查**: 定期检查上游服务器状态，自动恢复可用服务器
- **流式响应**: 完全支持 Claude Code 的流式响应
- **配置热加载**: 通过配置文件管理所有设置
- **多Token支持**: 每个上游服务器可配置独立的 API Token

## 快速开始

### 1. 安装依赖

```bash
go mod download
```

### 2. 创建配置文件

复制示例配置文件：

```bash
cp config.example.json config.json
```

编辑 `config.json` 配置你的上游服务器：

```json
{
  "port": "3000",
  "algorithm": "weighted_round_robin",
  "servers": [
    {
      "url": "https://api.anthropic.com",
      "weight": 5,
      "token": "sk-your-primary-token"
    },
    {
      "url": "https://api.backup.com",
      "weight": 3,
      "token": "sk-your-backup-token"
    }
  ],
  "fallback": true,
  "auth": false,
  "cooldown": 60
}
```

### 3. 运行服务

```bash
go run main.go
```

服务将在指定端口启动，默认为 3000。

### 4. 配置 Claude Code

将 Claude Code 的请求指向你的代理服务：

```bash
export ANTHROPIC_API_URL="http://localhost:3000"
```

## 配置说明

### 基本配置

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| port | string | "3000" | 服务监听端口 |
| fallback | bool | false | 是否启用故障转移 |

### 负载均衡配置

```json
{
  "algorithm": "round_robin",
  "servers": [...]
}
```

#### 负载均衡算法

- **round_robin**: 轮询算法，平均分配请求
- **weighted_round_robin**: 加权轮询，根据权重分配请求
- **random**: 随机选择服务器

#### 服务器配置

```json
{
  "url": "https://api.example.com",
  "weight": 5,
  "token": "sk-your-token-here"
}
```

- **url**: 上游服务器地址
- **weight**: 权重（仅在加权轮询模式下生效）
- **token**: 该服务器使用的 API Token

### 健康检查配置

健康检查现在是内置功能，通过 `cooldown` 参数配置冷却时间（秒）：

```json
{
  "cooldown": 60
}
```

## API 端点

- `GET /health` - 服务健康状态和统计信息
- `ANY /v1/*` - 转发所有 Claude API 请求

### 健康检查响应示例

```json
{
  "status": "ok",
  "total_servers": 3,
  "available_servers": 2,
  "load_balancer": "weighted_round_robin",
  "fallback": true,
  "time": "2025-08-15T10:30:00Z"
}
```

## 工作原理

1. **请求接收**: 接收 Claude Code 发送的请求
2. **服务器选择**: 根据配置的负载均衡算法选择可用服务器
3. **请求转发**: 将请求转发到选中的上游服务器，使用对应的 Token
4. **故障处理**: 如果请求失败或返回 5xx 错误，标记服务器为不可用
5. **自动重试**: 如果启用 fallback，自动重试其他可用服务器
6. **健康恢复**: 定期检查不可用服务器，自动恢复可用状态

## 故障转移机制

当服务器出现以下情况时会被标记为不可用：
- 网络连接失败
- 返回 5xx 状态码
- 健康检查失败

启用 fallback 后，系统会：
- 自动切换到其他可用服务器
- 如果所有服务器都不可用，尝试所有服务器（最后的保险机制）
- 通过健康检查自动恢复故障服务器

## 环境变量

可以通过环境变量指定配置文件路径：

```bash
export CONFIG_FILE="/path/to/your/config.json"
```

## 安全建议

1. 在生产环境运行时设置：
   ```bash
   export GIN_MODE=release
   ```

2. 使用防火墙限制访问
3. 定期轮换 API Token
4. 监控服务器日志和健康状态

## 示例场景

### 场景 1: 主备模式
```json
{
  "algorithm": "weighted_round_robin",
  "servers": [
    {"url": "https://primary.com", "weight": 10, "token": "primary-token"},
    {"url": "https://backup.com", "weight": 1, "token": "backup-token"}
  ],
  "fallback": true
}
```

### 场景 2: 多服务器均衡
```json
{
  "algorithm": "round_robin",
  "servers": [
    {"url": "https://server1.com", "token": "token1"},
    {"url": "https://server2.com", "token": "token2"},
    {"url": "https://server3.com", "token": "token3"}
  ]
}
```

这样你就可以实现高可用的 Claude Code 代理服务，确保服务的稳定性和可靠性。