# Claude Code Load Balancer

高性能的 Claude API 负载均衡器，支持多种分发策略、故障转移和健康检查功能。

[![Build](https://github.com/9bingyin/claude-code-lb/actions/workflows/build.yml/badge.svg)](https://github.com/9bingyin/claude-code-lb/actions/workflows/build.yml) [![Docker](https://github.com/9bingyin/claude-code-lb/actions/workflows/docker.yml/badge.svg)](https://github.com/9bingyin/claude-code-lb/actions/workflows/docker.yml)

## 功能特性

- **双工作模式**: 负载均衡模式 + 故障转移模式
- **多种算法**: 轮询、加权轮询、随机分配
- **故障转移**: 自动检测故障并切换到备用服务器
- **健康检查**: 被动健康检查，自动恢复故障服务器
- **身份验证**: 支持客户端 API 密钥认证
- **高性能**: 支持流式响应，低延迟代理
- **统计监控**: 实时请求统计和服务器状态监控
- **灵活配置**: 支持环境变量和配置文件

## 快速开始

### 方式 1: 从 GitHub Actions 下载构建产物

1. **访问 Actions 页面**: 前往 [https://github.com/9bingyin/claude-code-lb/actions](https://github.com/9bingyin/claude-code-lb/actions)
2. **选择 `Build` 工作流**: 在左侧选择 "Build" 工作流。
3. **下载构建产物**: 从列表中选择一个最近成功运行的工作流，在页面底部的 "Artifacts" 部分下载适用于您系统的压缩包。

2. **创建配置文件**
   ```bash
   curl -o config.json https://raw.githubusercontent.com/9bingyin/claude-code-lb/main/config.example.json
   ```

3. **编辑配置并运行**
   ```bash
   # 解压下载的产物后，为其添加执行权限并移动到 PATH
   chmod +x claude-code-lb-linux-amd64
   mv claude-code-lb-linux-amd64 /usr/local/bin/claude-code-lb
   
   # 编辑配置
   vim config.json
   
   # 启动服务
   claude-code-lb
   ```

### 方式 2: 使用 Docker

```bash
# 拉取镜像
docker pull ghcr.io/9bingyin/claude-code-lb:dev

# 运行容器
# 请确保当前目录下有 config.json 文件
docker run -d \
  --name claude-code-lb \
  -p 3000:3000 \
  -v $(pwd):/data \
  ghcr.io/9bingyin/claude-code-lb:dev
```

### 方式 3: 从源码构建

```bash
# 克隆项目
git clone https://github.com/9bingyin/claude-code-lb.git
cd claude-code-lb

# 安装依赖
go mod download

# 构建二进制文件
# 这会在当前目录生成一个名为 claude-code-lb 的可执行文件
go build -o claude-code-lb .

# 复制配置文件
cp config.example.json config.json

# 编辑配置
vim config.json

# 运行服务
./claude-code-lb
```

## 配置文档

### 基础配置

#### `port` (字符串)
- **说明**: 服务器监听端口
- **默认值**: `"3000"`
- **示例**: `"3000"`, `"8080"`

#### `mode` (字符串)
- **说明**: 工作模式，决定服务器选择策略
- **可选值**:
  - `"load_balance"`: 负载均衡模式，按算法分配请求到所有健康服务器
  - `"fallback"`: 故障转移模式，严格按优先级选择服务器
- **默认值**: `"load_balance"`

#### `algorithm` (字符串)
- **说明**: 负载均衡算法 (仅在 `load_balance` 模式下有效)
- **可选值**:
  - `"round_robin"`: 轮询算法，依次轮流选择服务器
  - `"weighted_round_robin"`: 加权轮询算法，根据权重分配流量
  - `"random"`: 随机算法，随机选择服务器
- **默认值**: `"round_robin"`

### 服务器配置

#### `servers` (数组)

每个服务器对象包含以下字段：

##### `url` (字符串, 必填)
- **说明**: 上游服务器URL
- **示例**: `"https://api.anthropic.com"`, `"http://localhost:8080"`

##### `weight` (数字)
- **说明**: 
  - 负载均衡模式：权重，数值越大分配流量越多
  - 故障转移模式：用于自动计算优先级
- **默认值**: `1`
- **示例**: `5`, `3`, `1`

##### `priority` (数字)
- **说明**: 优先级 (仅在故障转移模式下有效)
- **规则**: 数字越小优先级越高，1为最高优先级
- **特殊值**: `0` 表示根据 `weight` 自动计算优先级
- **默认值**: `0`
- **示例**: `1`, `2`, `3`

##### `token` (字符串, 可选)
- **说明**: 访问上游服务器的API令牌
- **建议**: 强烈推荐设置以提高安全性
- **示例**: `"sk-your-token-here"`

##### `balance_check` (字符串, 可选)
- **说明**: 用于检查服务器账户余额的 shell 命令。该命令的输出必须是一个纯数字。
- **功能**: 如果命令输出的余额小于或等于 `balance_threshold`，服务器将被自动标记为不可用。
- **依赖**: `Dockerfile` 中已包含 `curl`, `jq`, `bash` 等常用工具。
- **示例**: `"curl -s -H 'Authorization: Bearer sk-token' https://api.example.com/v1/balance | jq .balance"`

##### `balance_check_interval` (数字, 可选)
- **说明**: 余额检查命令的执行间隔时间（秒）。
- **默认值**: `300` (5分钟)
- **示例**: `180`

##### `balance_threshold` (数字, 可选)
- **说明**: 余额阈值。当余额小于或等于此值时，服务器将被禁用。
- **默认值**: `0`
- **示例**: `10.0`

### 故障处理

#### `cooldown` (数字)
- **说明**: 服务器冷却时间 (秒)
- **功能**: 服务器故障后的等待时间，支持动态退避
- **动态退避**: 失败次数越多，冷却时间越长 (最大10分钟)
- **默认值**: `60`

#### `fallback` (布尔值)
- **说明**: 向后兼容字段 (已废弃，建议使用 `mode`)
- **规则**: `true` 等同于 `mode="fallback"`
- **默认值**: `false`

### 身份验证

#### `auth` (布尔值)
- **说明**: 是否启用客户端身份验证
- **规则**:
  - `true`: 要求客户端提供有效的API密钥
  - `false`: 允许任何客户端访问
- **默认值**: `false`

#### `auth_keys` (字符串数组)
- **说明**: 允许的客户端API密钥列表
- **前提**: 仅在 `auth=true` 时有效，此时为必填字段
- **使用**: 客户端需要在请求头提供 `Authorization: Bearer <key>`

## 配置示例

### 负载均衡模式
适合高可用分布式场景：

```json
{
  "port": "3000",
  "mode": "load_balance",
  "algorithm": "weighted_round_robin",
  "debug": false,
  "servers": [
    {
      "url": "https://api.primary-server.com",
      "weight": 5,
      "token": "sk-primary-token-here",
      "balance_check": "curl -s -H 'Authorization: Bearer sk-primary-token-here' https://api.primary-server.com/balance",
      "balance_check_interval": 180,
      "balance_threshold": 10.0
    },
    {
      "url": "https://api.secondary-server.com",
      "weight": 3,
      "token": "sk-secondary-token-here",
      "balance_check": "curl -s -H 'Authorization: Bearer sk-secondary-token-here' https://api.secondary-server.com/v1/account/balance | jq -r '.balance'",
      "balance_check_interval": 600,
      "balance_threshold": 5.0
    },
    {
      "url": "https://api.backup-server.com",
      "weight": 1,
      "token": "sk-backup-token-here"
    }
  ],
  "auth": false,
  "cooldown": 60
}
```

### 故障转移模式
适合主备切换场景：

```json
{
  "port": "3000",
  "mode": "fallback",
  "servers": [
    {
      "url": "https://primary.com",
      "priority": 1,
      "token": "sk-primary-token"
    },
    {
      "url": "https://backup.com",
      "priority": 2, 
      "token": "sk-backup-token"
    },
    {
      "url": "https://emergency.com",
      "priority": 3,
      "token": "sk-emergency-token"
    }
  ],
  "auth": true,
  "auth_keys": [
    "sk-client-key-1",
    "sk-client-key-2"
  ],
  "cooldown": 30
}
```

## 使用方法

### 命令行选项

```bash
claude-code-lb [选项]

选项:
  -version      显示版本信息
  -help         显示帮助信息
  -health-check 执行健康检查

环境变量:
  CONFIG_FILE   配置文件路径 (默认: config.json)
```

### 配置 Claude Code

设置环境变量将 Claude Code 请求指向代理服务器：

```bash
export ANTHROPIC_API_URL="http://localhost:3000"
export ANTHROPIC_AUTH_TOKEN="sk-xxx"
```

## 开发和构建

### 本地开发

```bash
# 安装依赖
go mod download

# 运行测试
go test ./...

# 本地运行
go run main.go
```

### 构建二进制

```bash
# 构建当前平台
go build -o claude-code-lb .

# 交叉编译
GOOS=linux GOARCH=amd64 go build -o claude-code-lb-linux-amd64 .
GOOS=darwin GOARCH=arm64 go build -o claude-code-lb-darwin-arm64 .
```

### Docker 构建

```bash
# 构建镜像
docker build -t claude-code-lb .

# 多架构构建
docker buildx build --platform linux/amd64,linux/arm64 -t claude-code-lb .
```

## 监控和日志

服务提供详细的日志输出和统计信息：

- **启动日志**: 显示配置信息和服务器状态
- **请求日志**: 记录每个代理请求的详细信息
- **错误日志**: 记录故障服务器和错误信息
- **统计报告**: 定期输出请求统计和响应时间