# model-fallback-chain

**CLIProxyAPI (CPA) 插件** — 多链路模型降级，带熔断器和内容异常检测。

当首选模型不可用（429/502/503/504/超时/内容异常/连续失败）时，自动切换到备用模型，保证请求不中断。

---

## 目录

- [工作原理](#工作原理)
- [安装](#安装)
- [快速开始](#快速开始)
- [配置详解](#配置详解)
- [链路匹配规则](#链路匹配规则)
- [降级触发条件](#降级触发条件)
- [熔断器](#熔断器)
- [构建](#构建)
- [架构](#架构)
- [注意事项](#注意事项)

---

## 工作原理

```
客户端请求 → ModelRouter 拦截 → 匹配降级链路（chain）
  → Executor 按顺序尝试 chain 中的后端（backend）：
     backend 1 失败（429/超时/异常）→ 记录惩罚 → 尝试下一个
     backend 2 失败（502）         → 记录惩罚 → 尝试下一个
     backend 3 成功                 → 返回响应
  → 全部失败：返回最后一个错误
```

插件通过 `host.model.execute` / `host.model.execute_stream` 回调将请求转发给 CPA 内置的 provider，所以每个 backend 必须是 CPA 已注册的 provider+model 组合。

---

## 安装

### 方式一：从 GitHub Actions 下载预编译产物

1. 前往 [releases/Actions 页面](https://github.com/zcr268/CPA-model-fallback-chain/actions)
2. 下载对应平台的 zip 包（如 `plugin-linux-amd64.zip`）
3. 解压得到 `.so`（Linux）/ `.dylib`（macOS）/ `.dll`（Windows）文件
4. **重命名**：去掉平台后缀 → `model-fallback-chain.so`（详见[文件命名规则](#文件命名规则)）
5. 放入 CPA 的 plugins 目录

### 方式二：从源码构建

```bash
git clone https://github.com/zcr268/CPA-model-fallback-chain.git
cd model-fallback-chain
# 需要 Go 1.26+，且 CLIProxyAPI 作为同级目录（go.mod 中的 replace 指令）
make build
# 产物：bin/model-fallback-chain.so / .dylib / .dll
```

### 文件命名规则

**关键**：`.so` 文件名必须为 `model-fallback-chain.so`（不带平台后缀）。

CPA 的 `pluginFileFromPath` 会从文件名中解析 plugin ID。如果文件名为 `model-fallback-chain-linux-amd64.so`，CPA 会将整个 `model-fallback-chain-linux-amd64` 当作 plugin ID，与配置中的 `model-fallback-chain` 不匹配，导致插件被跳过。

```bash
# ❌ 错误（GHA 产物原文件名）
model-fallback-chain-linux-amd64.so

# ✅ 正确（重命名后）
model-fallback-chain.so
```

---

## 快速开始

以下是一个完整的可运行配置示例（基于 OmniRoute 作为上游 provider，已验证通过）：

```yaml
# CPA config.yaml

host: "127.0.0.1"
port: 18400

api-keys:
  - "test-key-12345"

# ── 插件配置 ──
plugins:
  enabled: true
  dir: "plugins"
  configs:
    model-fallback-chain:
      enabled: true
      priority: 1
      default_timeout_seconds: 30
      penalty_cooldown_seconds: 60
      max_penalty_failures: 3
      check_content_anomaly: false    # 流式场景建议设为 false（见注意事项）
      chains:
        # 链路1：premium 模型降级链
        - name: "premium-fallback"
          match:
            models:
              - "premium-model"
            source_formats:
              - "chat-completions"
          backends:
            - provider: "openai-compatible-omniroute"
              model: "premium-model"      # ← 使用 alias
            - provider: "openai-compatible-omniroute"
              model: "fallback-model"     # ← 使用 alias

        # 链路2：兜底链路（匹配所有未命中的请求）
        - name: "catch-all"
          match: {}
          backends:
            - provider: "openai-compatible-omniroute"
              model: "fallback-model"

# ── 上游 provider 配置 ──
openai-compatibility:
  - name: "omniroute"
    base-url: "http://your-omniroute:20128/v1"
    api-key-entries:
      - api-key: "sk-your-key"
    models:
      - name: "cw/claude-sonnet-4-6"    # 上游真实模型名
        alias: "premium-model"           # 客户端可见的别名
      - name: "minimax-m3"
        alias: "fallback-model"
```

### 验证

```bash
# 启动 CPA
./cli-proxy-api --config config.yaml

# 检查插件是否加载（日志应显示）
# pluginhost: plugin loaded plugin_id=model-fallback-chain
# pluginhost: plugin registered plugin_id=model-fallback-chain version=0.1.0

# 发送请求
curl -X POST http://127.0.0.1:18400/v1/chat/completions \
  -H "Authorization: Bearer test-key-12345" \
  -H "Content-Type: application/json" \
  -d '{"model":"premium-model","messages":[{"role":"user","content":"Hi"}],"max_tokens":10}'
```

---

## 配置详解

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `enabled` | bool | `false` | 是否启用插件 |
| `priority` | int | `0` | 插件优先级（数字越大越先执行） |
| `default_timeout_seconds` | int | `0` | 每个 backend 的超时秒数（0=不超时） |
| `penalty_cooldown_seconds` | int | `60` | 熔断后 backend 的冷却秒数 |
| `max_penalty_failures` | int | `3` | 连续失败多少次后熔断 |
| `check_content_anomaly` | bool | `true` | 是否启用内容异常检测 |
| `chains` | array | `[]` | 降级链路列表 |

### 链路配置

| 字段 | 类型 | 说明 |
|------|------|------|
| `name` | string | 链路名称（日志中标识） |
| `match` | object | 匹配规则（见下） |
| `backends` | array | 按顺序尝试的后端列表 |

### 后端配置

| 字段 | 类型 | 说明 |
|------|------|------|
| `provider` | string | CPA 中的 provider 名称 |
| `model` | string | **CPA alias**（客户端可见名，非上游原始名） |

---

## 链路匹配规则

| 字段 | 类型 | 说明 |
|------|------|------|
| `models` | `[]string` | 要匹配的模型名。支持 `*` 后缀做前缀匹配。空=匹配所有 |
| `source_formats` | `[]string` | 要匹配的请求协议：`chat-completions`、`claude`、`gemini`、`openai-responses`。空=匹配所有 |

第一个匹配的 chain 获胜。`match: {}`（空对象）匹配所有请求，通常用作兜底链路。

---

## 降级触发条件

一个 backend 被视为失败并尝试下一个 backend 的条件：

1. **HTTP 可重试状态码**：429、502、503、504
2. **超时**：设置了 `default_timeout_seconds` 且 backend 未在时间内响应
3. **内容异常**（`check_content_anomaly: true` 时）：
   - 空响应体
   - 非流式响应不是有效 JSON
   - 响应体包含 `{"error": ...}`
   - 空 `choices` 数组（OpenAI 格式）
   - `{"type": "error"}`（Anthropic 格式）
   - SSE 流中没有包含实质内容的 `data:` 行
4. **Go 级错误**：`host.model.execute` 回调返回 error（如模型未注册）

---

## 熔断器

- 连续失败 `max_penalty_failures` 次（默认 3）后，backend 被标记为 **penalized**
- penalized 期间（`penalty_cooldown_seconds`，默认 60 秒），该 backend 被跳过
- 冷却结束后自动恢复参与
- 一旦 backend 成功，其所有惩罚状态被清除

---

## 构建

### 依赖

- Go 1.26+
- CLIProxyAPI 源码（作为同级目录，用于 go.mod replace 指令）

```
workspace/
├── CLIProxyAPI/           # CPA 源码
└── model-fallback-chain/ # 本插件
```

### 编译

```bash
make build          # 当前平台
make linux          # Linux amd64
make darwin         # macOS arm64
make windows        # Windows amd64
make all            # 全部平台
```

### CI/CD

项目已配置 GitHub Actions（`.github/workflows/build.yml`），push 到 master 后自动在 4 个平台编译：
- linux/amd64 ✅
- linux/arm64 ✅
- darwin/arm64 ✅
- windows/amd64 ✅

产物上传到 GHA run 的 Artifacts，可直接下载。

---

## 架构

```
main.go        — C FFI 胶水层：插件初始化、RPC 分发、host 回调
config.go      — YAML 配置解析、归一化、热加载
router.go      — ModelRouter：请求 → 链路匹配
executor.go    — Executor：通过 host.model.execute 回调执行降级链
health.go      — 熔断器：惩罚追踪、冷却、自动恢复
anomaly.go     — 内容异常检测（非流式 + 流式）
util.go        — 字符串工具函数
```

### 支持的请求格式

Executor 声明支持：`chat-completions`、`claude`、`gemini`、`openai-responses`

可拦截来自 OpenAI 兼容客户端、Claude Code、Gemini 客户端、OpenAI Responses API 客户端的请求。

---

## 注意事项

### 1. backend model 字段使用 alias

`backends[].model` 必须使用 CPA `openai-compatibility` 配置中的 **alias**（客户端可见名），而非上游原始模型名。CPA 的模型注册表以 alias 为 key，`host.model.execute` 内部将 alias 解析为上游名。直接使用上游名会报 `"unknown provider for model"` 错误。

```yaml
# openai-compatibility 配置
openai-compatibility:
  - name: "omniroute"
    models:
      - name: "cw/claude-sonnet-4-6"   # 上游名（不要在 chain backend 里用这个）
        alias: "premium-model"          # ← 在 chain backend model 字段用这个

# chain backend 配置
chains:
  - backends:
      - provider: "openai-compatible-omniroute"
        model: "premium-model"           # ✅ 用 alias
```

### 2. 流式内容异常检测

流式响应中，某些 provider（如 minimax）的首个 chunk 可能只包含 `reasoning_content` 而没有标准 SSE `data:` 帧格式，导致内容异常检测误报。如果遇到 `"content anomaly detected"` 错误，将 `check_content_anomaly` 设为 `false`。

### 3. provider 命名

`backends[].provider` 的值要与 CPA 内部注册的 provider 名一致。可以通过 CPA 启动日志查看：

```
[debug] Registered client openai-compatibility:omniroute:xxx from provider openai-compatible-omniroute
```

这里的 `openai-compatible-omniroute` 就是 provider 名。

### 4. .so 文件名

GHA 产物文件名含平台后缀（如 `model-fallback-chain-linux-amd64.so`），放入 CPA plugins 目录前必须重命名为 `model-fallback-chain.so`，否则 CPA 解析的 plugin ID 与配置 key 不匹配。

---

## 许可证

MIT
