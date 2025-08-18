# 配置文档

本文档详细说明了 Claude Code Load Balancer 的所有配置选项。

## 配置文件结构

### 基础配置

#### `port` (字符串)
- **说明**: 服务器监听端口
- **默认值**: `"3000"`
- **示例**: `"3000"`, `"8080"`

### 模式选择

#### `mode` (字符串)
- **说明**: 工作模式，决定服务器选择策略
- **可选值**:
  - `"load_balance"`: 负载均衡模式，按算法分配请求到所有健康服务器
  - `"fallback"`: 故障转移模式，严格按优先级选择服务器
- **默认值**: `"load_balance"` (如果未设置且 `fallback=true`，则为 `"fallback"`)

#### `algorithm` (字符串)
- **说明**: 负载均衡算法 (仅在 `load_balance` 模式下有效)
- **可选值**:
  - `"round_robin"`: 轮询算法，依次轮流选择服务器
  - `"weighted_round_robin"`: 加权轮询算法，根据权重分配流量
  - `"random"`: 随机算法，随机选择服务器
- **默认值**: `"round_robin"`

### 服务器配置

#### `servers` (数组)
服务器列表，每个服务器对象包含以下字段：

##### `url` (字符串, 必填)
- **说明**: 上游服务器URL
- **示例**: `"https://api.anthropic.com"`, `"http://localhost:8080"`

##### `weight` (数字)
- **说明**: 
  - 在 `load_balance` 模式下：用于加权算法，数值越大分配的流量越多
  - 在 `fallback` 模式下：用于自动计算优先级 (如果未设置 priority)
- **默认值**: `1`
- **示例**: `5`, `3`, `1`

##### `priority` (数字)
- **说明**: 优先级 (仅在 `fallback` 模式下有效)
- **规则**: 数字越小优先级越高，1为最高优先级
- **特殊值**: `0` 表示根据 `weight` 自动计算优先级
- **默认值**: `0` (自动根据权重计算)
- **示例**: `1`, `2`, `3`

##### `token` (字符串, 可选)
- **说明**: 访问上游服务器的API令牌
- **建议**: 强烈建议设置，提高安全性
- **示例**: `"sk-your-token-here"`

### 故障处理

#### `fallback` (布尔值)
- **说明**: 向后兼容的fallback开关 (已废弃，建议使用 `mode` 字段)
- **规则**: 
  - `true`: 等同于 `mode="fallback"`
  - `false`: 等同于 `mode="load_balance"`
- **默认值**: `false`

#### `cooldown` (数字)
- **说明**: 服务器冷却时间 (秒)
- **功能**: 服务器被标记为不可用后，需要等待多长时间才能重新尝试
- **动态退避**: 失败次数越多，实际冷却时间越长 (最大10分钟)
- **默认值**: `60`
- **示例**: `30`, `60`, `300`

### 身份验证

#### `auth` (布尔值)
- **说明**: 是否启用客户端身份验证
- **规则**:
  - `true`: 要求客户端提供有效的API密钥
  - `false`: 允许任何客户端访问
- **默认值**: `false`

#### `auth_keys` (字符串数组)
- **说明**: 允许的客户端API密钥列表
- **前提条件**: 仅在 `auth=true` 时生效，此时为必填字段
- **使用方式**: 客户端需要在请求头中提供 `Authorization: Bearer <key>`
- **示例**: `["sk-key-1", "sk-key-2"]`

## 配置示例

### 负载均衡模式示例
适合高可用分布式场景：

```json
{
  "port": "3000",
  "mode": "load_balance",
  "algorithm": "weighted_round_robin",
  "servers": [
    {
      "url": "https://api1.com",
      "weight": 5,
      "token": "sk-token-1"
    },
    {
      "url": "https://api2.com", 
      "weight": 3,
      "token": "sk-token-2"
    },
    {
      "url": "https://api3.com",
      "weight": 1,
      "token": "sk-token-3"
    }
  ],
  "auth": false,
  "cooldown": 60
}
```

### 故障转移模式示例
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

## 环境变量

### `CONFIG_FILE`
- **说明**: 指定配置文件路径
- **默认值**: `"config.json"`
- **使用方��**: `CONFIG_FILE=/path/to/config.json ./claude-code-lb`

## 工作模式对比

| 特性 | 负载均衡模式 | 故障转移模式 |
|------|-------------|-------------|
| 服务器选择策略 | 按算法分配到所有健康服务器 | 严格按优先级选择 |
| 流量分配 | 平衡分配 | 集中到最高优先级服务器 |
| 适用场景 | 高并发、性能优化 | 主备切换、灾难恢复 |
| 算法支持 | 轮询、加权轮询、随机 | 优先级排序 |
| 配置复杂度 | 中等 (需要调整权重) | 简单 (设置优先级) |