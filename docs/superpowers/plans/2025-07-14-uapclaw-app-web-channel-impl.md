# uapclaw app Web Channel 通信打通 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 打通前端界面与 Go 后端之间的 WebSocket 通信，`uapclaw app` 命令启动后，浏览器访问 `http://127.0.0.1:19000` 可加载前端、WS 连接可建立、所有 RPC 方法可调用返回 stub/真实响应。

**Architecture:** 单进程模式，chi HTTP 路由器同时提供静态文件服务（go:embed 嵌入前端 dist/）、WebSocket 端点（`/ws`）、文件操作 HTTP API（`/file-api/*`）。WebChannel 管理 WS 连接生命周期，RPCDispatcher 分发方法到 handler。config.get/set 全量对齐 Python ~65 字段读取/写入，其余 RPC 全量注册为 stub。

**Tech Stack:** Go 1.26, go-chi/chi/v5 (HTTP router), gorilla/websocket (WS), zerolog (日志), cobra (CLI), go:embed (静态资源)

---

## File Structure

```
internal/swarm/gateway/
├── doc.go                        # 包文档
├── app_gateway.go                # GatewayServer + chi router + 静态文件
├── embed.go                      # go:embed 前端 dist/
├── file_api.go                   # /file-api/* HTTP handlers
├── channel_manager/
│   ├── doc.go                    # 包文档
│   ├── base.go                   # BaseChannel + ChannelType + ChannelMetadata
│   ├── channel_manager.go        # ChannelManager 注册/分发
│   └── web/
│       ├── doc.go                # 包文档
│       ├── frame.go              # WsRequest/WsResponse/WsEvent + 编解码
│       ├── web_connect.go        # WebChannel + WS handler + connection.ack
│       └── web_handlers.go       # RPCDispatcher + 全量 RPC handlers
├── message_handler/
│   ├── doc.go                    # 包文档
│   └── message_handler.go        # MessageHandler 骨架
└── routing/
    ├── doc.go                    # 包文档
    └── agent_client.go           # AgentClient 骨架

internal/swarm/server/gateway_push/
├── doc.go                        # 包文档
├── transport.go                  # AgentTransport 接口
└── channel_transport.go          # ChannelTransport 实现

cmd/uapclaw/cmd.go                # 修改: 实现 app 子命令
```

**迁移:**
- `internal/swarm/channel/web/frontend/` → `internal/swarm/gateway/channel_manager/web/frontend/`

---

### Task 1: 添加 chi 依赖 + 提升 gorilla/websocket 为直接依赖

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: 添加 chi 依赖**

```bash
cd /home/opensource/uapclaw-gateway
export GOPROXY=https://goproxy.cn,direct
go get github.com/go-chi/chi/v5@latest
go get github.com/gorilla/websocket@v1.5.3
go mod tidy
```

- [ ] **Step 2: 验证依赖已添加**

```bash
grep 'go-chi/chi' go.mod
grep 'gorilla/websocket' go.mod
```

Expected: 两者都是 direct 依赖

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: 添加 chi/v5 + 提升 gorilla/websocket 为直接依赖"
```

---

### Task 2: 迁移前端目录

**Files:**
- Move: `internal/swarm/channel/web/frontend/` → `internal/swarm/gateway/channel_manager/web/frontend/`

- [ ] **Step 1: 创建目标目录结构**

```bash
mkdir -p internal/swarm/gateway/channel_manager/web
```

- [ ] **Step 2: 迁移 frontend 目录**

```bash
mv internal/swarm/channel/web/frontend internal/swarm/gateway/channel_manager/web/frontend
```

- [ ] **Step 3: 删除旧的 channel 目录（如果为空）**

```bash
rm -rf internal/swarm/channel
```

- [ ] **Step 4: 验证迁移结果**

```bash
ls internal/swarm/gateway/channel_manager/web/frontend/dist/index.html
```

Expected: index.html 存在

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "refactor: 迁移前端 frontend/ 到 gateway/channel_manager/web/frontend/"
```

---

### Task 3: 帧协议 — frame.go + 测试

**Files:**
- Create: `internal/swarm/gateway/channel_manager/web/frame.go`
- Create: `internal/swarm/gateway/channel_manager/web/frame_test.go`

- [ ] **Step 1: 创建 frame.go**

```go
package web

import (
	"encoding/json"
	"fmt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WsRequest 客户端→服务端请求帧
type WsRequest struct {
	// Type 帧类型，固定 "req"
	Type string `json:"type"`
	// ID 客户端生成的请求 ID
	ID string `json:"id"`
	// Method RPC 方法名
	Method string `json:"method"`
	// Params 方法参数
	Params map[string]any `json:"params,omitempty"`
}

// WsResponse 服务端→客户端响应帧
type WsResponse struct {
	// Type 帧类型，固定 "res"
	Type string `json:"type"`
	// ID 匹配请求 ID
	ID string `json:"id"`
	// OK 是否成功
	OK bool `json:"ok"`
	// Payload 响应数据
	Payload map[string]any `json:"payload,omitempty"`
	// Error 错误描述
	Error string `json:"error,omitempty"`
	// Code 错误码
	Code string `json:"code,omitempty"`
}

// WsEvent 服务端→客户端推送事件帧
type WsEvent struct {
	// Type 帧类型，固定 "event"
	Type string `json:"type"`
	// Event 事件名
	Event string `json:"event"`
	// Payload 事件数据
	Payload map[string]any `json:"payload"`
	// Seq 流式序号
	Seq int `json:"seq,omitempty"`
	// StreamID 流分组 ID
	StreamID string `json:"stream_id,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// WsErrBadRequest 请求格式错误
	WsErrBadRequest = "BAD_REQUEST"
	// WsErrMethodNotFound 方法未找到
	WsErrMethodNotFound = "METHOD_NOT_FOUND"
	// WsErrInternalError 内部错误
	WsErrInternalError = "INTERNAL_ERROR"
	// WsErrLLMError LLM 调用错误
	WsErrLLMError = "LLM_ERROR"
	// WsErrServiceUnavailable 服务不可用
	WsErrServiceUnavailable = "SERVICE_UNAVAILABLE"
	// WsErrNotFound 资源未找到
	WsErrNotFound = "NOT_FOUND"
	// WsErrAlreadyExists 资源已存在
	WsErrAlreadyExists = "ALREADY_EXISTS"
	// WsErrConflict 冲突
	WsErrConflict = "CONFLICT"
	// WsErrAgentUnavailable Agent 不可用
	WsErrAgentUnavailable = "AGENT_UNAVAILABLE"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseRequest 解析客户端请求帧
func ParseRequest(data []byte) (*WsRequest, error) {
	var req WsRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("JSON 解析失败: %w", err)
	}
	return &req, nil
}

// BuildResponse 构建成功响应帧
func BuildResponse(reqID string, ok bool, payload map[string]any) []byte {
	resp := WsResponse{
		Type:    "res",
		ID:      reqID,
		OK:      ok,
		Payload: payload,
	}
	data, _ := json.Marshal(resp)
	return data
}

// BuildErrorResponse 构建错误响应帧
func BuildErrorResponse(reqID, errMsg, code string) []byte {
	resp := WsResponse{
		Type:  "res",
		ID:    reqID,
		OK:    false,
		Error: errMsg,
		Code:  code,
	}
	data, _ := json.Marshal(resp)
	return data
}

// BuildEvent 构建推送事件帧
func BuildEvent(event string, payload map[string]any) []byte {
	evt := WsEvent{
		Type:    "event",
		Event:   event,
		Payload: payload,
	}
	data, _ := json.Marshal(evt)
	return data
}

// BuildStreamEvent 构建流式推送事件帧
func BuildStreamEvent(event string, payload map[string]any, seq int, streamID string) []byte {
	evt := WsEvent{
		Type:     "event",
		Event:    event,
		Payload:  payload,
		Seq:      seq,
		StreamID: streamID,
	}
	data, _ := json.Marshal(evt)
	return data
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 创建 frame_test.go**

```go
package web

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseRequest_正常解析 测试正常 JSON 解析
func TestParseRequest_正常解析(t *testing.T) {
	data := []byte(`{"type":"req","id":"req_123","method":"config.get","params":{}}`)
	req, err := ParseRequest(data)
	require.NoError(t, err)
	assert.Equal(t, "req", req.Type)
	assert.Equal(t, "req_123", req.ID)
	assert.Equal(t, "config.get", req.Method)
}

// TestParseRequest_畸形JSON 测试畸形 JSON
func TestParseRequest_畸形JSON(t *testing.T) {
	data := []byte(`{invalid`)
	_, err := ParseRequest(data)
	assert.Error(t, err)
}

// TestParseRequest_缺省params 测试 params 省略
func TestParseRequest_缺省params(t *testing.T) {
	data := []byte(`{"type":"req","id":"req_1","method":"chat.send"}`)
	req, err := ParseRequest(data)
	require.NoError(t, err)
	assert.Nil(t, req.Params)
}

// TestBuildResponse_成功 测试成功响应帧构建
func TestBuildResponse_成功(t *testing.T) {
	data := BuildResponse("req_1", true, map[string]any{"key": "val"})
	var resp WsResponse
	require.NoError(t, json.Unmarshal(data, &resp))
	assert.Equal(t, "res", resp.Type)
	assert.Equal(t, "req_1", resp.ID)
	assert.True(t, resp.OK)
	assert.Equal(t, "val", resp.Payload["key"])
}

// TestBuildResponse_失败 测试失败响应帧构建
func TestBuildResponse_失败(t *testing.T) {
	data := BuildResponse("req_1", false, nil)
	var resp WsResponse
	require.NoError(t, json.Unmarshal(data, &resp))
	assert.False(t, resp.OK)
}

// TestBuildErrorResponse 测试错误响应帧构建
func TestBuildErrorResponse(t *testing.T) {
	data := BuildErrorResponse("req_1", "内部错误", WsErrInternalError)
	var resp WsResponse
	require.NoError(t, json.Unmarshal(data, &resp))
	assert.Equal(t, "res", resp.Type)
	assert.False(t, resp.OK)
	assert.Equal(t, "内部错误", resp.Error)
	assert.Equal(t, WsErrInternalError, resp.Code)
}

// TestBuildEvent 测试事件帧构建
func TestBuildEvent(t *testing.T) {
	data := BuildEvent("connection.ack", map[string]any{"session_id": "sess_1"})
	var evt WsEvent
	require.NoError(t, json.Unmarshal(data, &evt))
	assert.Equal(t, "event", evt.Type)
	assert.Equal(t, "connection.ack", evt.Event)
	assert.Equal(t, "sess_1", evt.Payload["session_id"])
}

// TestBuildStreamEvent 测试流式事件帧构建
func TestBuildStreamEvent(t *testing.T) {
	data := BuildStreamEvent("chat.delta", map[string]any{"content": "hello"}, 1, "stream_1")
	var evt WsEvent
	require.NoError(t, json.Unmarshal(data, &evt))
	assert.Equal(t, 1, evt.Seq)
	assert.Equal(t, "stream_1", evt.StreamID)
}
```

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/channel_manager/web/ -run TestParseRequest -v
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/channel_manager/web/ -run TestBuild -v
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/swarm/gateway/channel_manager/web/frame.go internal/swarm/gateway/channel_manager/web/frame_test.go
git commit -m "feat(gateway): 添加帧协议类型定义和编解码函数"
```

---

### Task 4: BaseChannel 接口 — base.go + 测试

**Files:**
- Create: `internal/swarm/gateway/channel_manager/base.go`
- Create: `internal/swarm/gateway/channel_manager/base_test.go`

- [ ] **Step 1: 创建 base.go**

```go
package channel_manager

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ChannelMetadata 渠道元数据
type ChannelMetadata struct {
	// ChannelID 渠道标识，如 "web"
	ChannelID string
	// Source 来源，如 "websocket"
	Source string
	// UserID 用户 ID（可选）
	UserID string
	// Extra 额外信息
	Extra map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ChannelType 渠道类型枚举
type ChannelType int

const (
	// ChannelTypeACP ACP 渠道
	ChannelTypeACP ChannelType = iota
	// ChannelTypeWeb Web 渠道
	ChannelTypeWeb
	// ChannelTypeFeishu 飞书渠道
	ChannelTypeFeishu
	// ChannelTypeXiaoyi 小翼渠道
	ChannelTypeXiaoyi
	// ChannelTypeDingTalk 钉钉渠道
	ChannelTypeDingTalk
	// ChannelTypeTelegram Telegram 渠道
	ChannelTypeTelegram
	// ChannelTypeDiscord Discord 渠道
	ChannelTypeDiscord
	// ChannelTypeWhatsApp WhatsApp 渠道
	ChannelTypeWhatsApp
	// ChannelTypeWeCom 企业微信渠道
	ChannelTypeWeCom
	// ChannelTypeWeChat 微信渠道
	ChannelTypeWeChat
	// ChannelTypeCLI 命令行渠道
	ChannelTypeCLI
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// BaseChannel 渠道基础接口
// 对齐 Python channel_manager/base.py BaseChannel
type BaseChannel interface {
	// Name 渠道名称
	Name() string
	// Start 启动渠道
	Start(ctx context.Context) error
	// Stop 停止渠道
	Stop() error
	// Send 发送消息到渠道（出站，Agent→用户）
	Send(ctx context.Context, msg *schema.Message) error
	// IsRunning 是否运行中
	IsRunning() bool
}
```

- [ ] **Step 2: 创建 base_test.go**

```go
package channel_manager

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestChannelType_值 测试渠道类型枚举值
func TestChannelType_值(t *testing.T) {
	assert.Equal(t, 0, int(ChannelTypeACP))
	assert.Equal(t, 1, int(ChannelTypeWeb))
	assert.Equal(t, 2, int(ChannelTypeFeishu))
	assert.Equal(t, 10, int(ChannelTypeCLI))
}

// TestChannelMetadata_字段 测试渠道元数据字段
func TestChannelMetadata_字段(t *testing.T) {
	meta := ChannelMetadata{
		ChannelID: "web",
		Source:    "websocket",
		UserID:    "user1",
		Extra:     map[string]any{"key": "val"},
	}
	assert.Equal(t, "web", meta.ChannelID)
	assert.Equal(t, "websocket", meta.Source)
}
```

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/channel_manager/ -v
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/swarm/gateway/channel_manager/base.go internal/swarm/gateway/channel_manager/base_test.go
git commit -m "feat(gateway): 添加 BaseChannel 接口和 ChannelType 枚举"
```

---

### Task 5: ChannelManager — channel_manager.go + 测试

**Files:**
- Create: `internal/swarm/gateway/channel_manager/channel_manager.go`
- Create: `internal/swarm/gateway/channel_manager/channel_manager_test.go`

- [ ] **Step 1: 创建 channel_manager.go**

```go
package channel_manager

import (
	"context"
	"fmt"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ChannelManager 渠道管理器
// 对齐 Python channel_manager/channel_manager.py
type ChannelManager struct {
	// channels 已注册的渠道，key 为渠道 ID
	channels map[string]BaseChannel
	// mu 读写锁
	mu sync.RWMutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const logComponent = logger.ComponentGateway

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewChannelManager 创建渠道管理器
func NewChannelManager() *ChannelManager {
	return &ChannelManager{
		channels: make(map[string]BaseChannel),
	}
}

// RegisterChannel 注册渠道
func (cm *ChannelManager) RegisterChannel(channel BaseChannel) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	name := channel.Name()
	if _, exists := cm.channels[name]; exists {
		return fmt.Errorf("渠道 %s 已注册", name)
	}
	cm.channels[name] = channel
	logger.Info(logComponent).Str("channel_id", name).Msg("渠道已注册")
	return nil
}

// UnregisterChannel 注销渠道
func (cm *ChannelManager) UnregisterChannel(channelID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if _, exists := cm.channels[channelID]; !exists {
		return fmt.Errorf("渠道 %s 未注册", channelID)
	}
	delete(cm.channels, channelID)
	logger.Info(logComponent).Str("channel_id", channelID).Msg("渠道已注销")
	return nil
}

// GetEnabledChannels 获取启用的渠道列表（channel.get RPC 使用）
func (cm *ChannelManager) GetEnabledChannels() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	result := make([]string, 0, len(cm.channels))
	for id := range cm.channels {
		result = append(result, id)
	}
	return result
}

// GetChannel 获取指定渠道
func (cm *ChannelManager) GetChannel(channelID string) (BaseChannel, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	ch, ok := cm.channels[channelID]
	return ch, ok
}

// BroadcastToChannels 广播消息到所有渠道（出站，Agent→用户）
func (cm *ChannelManager) BroadcastToChannels(ctx context.Context, msg *schema.Message) error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	var lastErr error
	for _, ch := range cm.channels {
		if err := ch.Send(ctx, msg); err != nil {
			logger.Error(logComponent).Err(err).Str("channel_id", ch.Name()).Msg("广播消息失败")
			lastErr = err
		}
	}
	return lastErr
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 创建 channel_manager_test.go**

```go
package channel_manager

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// stubChannel 用于测试的 stub 渠道
type stubChannel struct {
	name    string
	running bool
}

func (c *stubChannel) Name() string                                                { return c.name }
func (c *stubChannel) Start(_ context.Context) error                               { c.running = true; return nil }
func (c *stubChannel) Stop() error                                                 { c.running = false; return nil }
func (c *stubChannel) Send(_ context.Context, _ *schema.Message) error             { return nil }
func (c *stubChannel) IsRunning() bool                                             { return c.running }

// TestNewChannelManager 测试创建渠道管理器
func TestNewChannelManager(t *testing.T) {
	cm := NewChannelManager()
	assert.NotNil(t, cm)
	assert.Empty(t, cm.GetEnabledChannels())
}

// TestChannelManager_RegisterChannel 测试注册渠道
func TestChannelManager_RegisterChannel(t *testing.T) {
	cm := NewChannelManager()
	err := cm.RegisterChannel(&stubChannel{name: "web"})
	require.NoError(t, err)
	assert.Contains(t, cm.GetEnabledChannels(), "web")
}

// TestChannelManager_RegisterChannel_重复注册 测试重复注册返回错误
func TestChannelManager_RegisterChannel_重复注册(t *testing.T) {
	cm := NewChannelManager()
	_ = cm.RegisterChannel(&stubChannel{name: "web"})
	err := cm.RegisterChannel(&stubChannel{name: "web"})
	assert.Error(t, err)
}

// TestChannelManager_UnregisterChannel 测试注销渠道
func TestChannelManager_UnregisterChannel(t *testing.T) {
	cm := NewChannelManager()
	_ = cm.RegisterChannel(&stubChannel{name: "web"})
	err := cm.UnregisterChannel("web")
	require.NoError(t, err)
	assert.NotContains(t, cm.GetEnabledChannels(), "web")
}

// TestChannelManager_UnregisterChannel_未注册 测试注销未注册渠道返回错误
func TestChannelManager_UnregisterChannel_未注册(t *testing.T) {
	cm := NewChannelManager()
	err := cm.UnregisterChannel("web")
	assert.Error(t, err)
}

// TestChannelManager_GetChannel 测试获取指定渠道
func TestChannelManager_GetChannel(t *testing.T) {
	cm := NewChannelManager()
	_ = cm.RegisterChannel(&stubChannel{name: "web"})
	ch, ok := cm.GetChannel("web")
	assert.True(t, ok)
	assert.Equal(t, "web", ch.Name())

	_, ok = cm.GetChannel("feishu")
	assert.False(t, ok)
}
```

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/channel_manager/ -v
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/swarm/gateway/channel_manager/channel_manager.go internal/swarm/gateway/channel_manager/channel_manager_test.go
git commit -m "feat(gateway): 添加 ChannelManager 渠道管理器"
```

---

### Task 6: RPC 分发器 + 全量 Handlers — web_handlers.go + 测试

**Files:**
- Create: `internal/swarm/gateway/channel_manager/web/web_handlers.go`
- Create: `internal/swarm/gateway/channel_manager/web/web_handlers_test.go`

这是最大的单个文件，包含 RPCDispatcher + configEnvMap + 全量 RPC handler 注册。

- [ ] **Step 1: 创建 web_handlers.go**

内容要点：
- `RPCHandlerFunc` 签名：`func(ctx context.Context, params map[string]any, sessionID string) (map[string]any, error)`
- `RPCDispatcher` 结构体 + `Register` / `Dispatch` 方法
- `configEnvMap` 全量 47 项映射（对齐 Python `_CONFIG_SET_ENV_MAP`）
- `NewAppRPCHandlers` 函数：创建 dispatcher，注册所有方法
  - 本地实现：`config.get`（读 env + config.yaml）、`config.set`（写 .env + config.yaml + os.Setenv）、`models.list`、`channel.get`、`session.list`/`session.create`/`session.delete`
  - chat 类：`chat.send`/`chat.resume`/`chat.interrupt`/`chat.user_answer` — 返回 ack + goroutine 延时 500ms 发模拟 event（`// [模拟事件]` 标注）
  - 转发方法 stub：`initialize`、`session.switch`、`acp.tool_response`、`team.*`、`history.get`、`browser.start`、`skills.*`、`plugins.*`、`extensions.*`、`agents.*`、`schedule.*`
  - 其他 stub：`locale.*`、`heartbeat.*`、`updater.*`、`hooks.*`、`permissions.*`、`memory.*`、`channel.<platform>.*`、`cron.*`、`harness.*`
- `configGet` handler 实现：遍历 `configEnvMap` 读 `os.Getenv` + 从 config.yaml 读补充字段
- `configSet` handler 实现：映射参数→环境变量名→`os.Setenv` + 写 .env + 写 config.yaml + stub reload 通知
- `sessionList`/`sessionCreate`/`sessionDelete` handler 实现：文件系统操作 `~/.uapclaw/agent/sessions/`
- `makeSessionID` 函数：`sess_{hex_timestamp}_{6_random_hex}`

- [ ] **Step 2: 创建 web_handlers_test.go**

测试要点：
- `TestRPCDispatcher_Register` / `TestRPCDispatcher_Dispatch` / `TestRPCDispatcher_MethodNotFound`
- `TestConfigGet`：验证返回的 payload 包含 `configEnvMap` 中所有键
- `TestConfigSet`：验证 `.env` 文件被写入
- `TestSessionList`：验证空目录返回空列表
- `TestSessionCreate`：验证目录和 metadata.json 被创建
- `TestSessionDelete`：验证目录被删除
- `TestMakeSessionID`：验证格式 `sess_*

_******`

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/channel_manager/web/ -v
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/swarm/gateway/channel_manager/web/web_handlers.go internal/swarm/gateway/channel_manager/web/web_handlers_test.go
git commit -m "feat(gateway): 添加 RPC 分发器和全量 RPC handlers（对齐 Python）"
```

---

### Task 7: WebChannel — web_connect.go + 测试

**Files:**
- Create: `internal/swarm/gateway/channel_manager/web/web_connect.go`
- Create: `internal/swarm/gateway/channel_manager/web/web_connect_test.go`

- [ ] **Step 1: 创建 web_connect.go**

内容要点：
- `WebChannelConfig` 结构体：`Enabled`、`Host`（默认 127.0.0.1）、`Port`（默认 19000）、`Path`（默认 /ws）、`AllowFrom`
- `WebChannel` 结构体：实现 `BaseChannel` 接口，持有 `config`、`clients`（map[*websocket.Conn]bool）、`clientsMu`、`dispatcher`、`upgrader`、`running`
- `NewWebChannel` 构造函数：创建 `RPCDispatcher`，注册 handlers，配置 `websocket.Upgrader`（ReadLimit 8MB，CheckOrigin 使用 `wsorigin.GorillaCheckOrigin()`）
- `HandleWebSocket` 方法：HTTP handler，升级 WS → 加入 clients → 发 `connection.ack` → 读消息循环 → 清理
- `connection.ack` 事件 payload：`{session_id, mode: "BUILD", tools: [], protocol_version: "1.0"}`
- 消息处理循环：读 raw → `ParseRequest` → 验证 type/id/method → 生成 sessionID → `dispatcher.Dispatch` → 写 response
- `Start`/`Stop`/`IsRunning`/`Name`/`Send` 方法实现
- `Send` 方法：将 `schema.Message` 转为 WsEvent/WsResponse 帧广播到所有 clients

- [ ] **Step 2: 创建 web_connect_test.go**

测试要点：
- `TestNewWebChannel`：验证默认配置
- `TestWebChannel_Name`：验证返回 "web"
- `TestWebChannel_IsRunning`：验证初始状态为 false
- `TestMakeSessionID`：验证格式和唯一性

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/channel_manager/web/ -v
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/swarm/gateway/channel_manager/web/web_connect.go internal/swarm/gateway/channel_manager/web/web_connect_test.go
git commit -m "feat(gateway): 添加 WebChannel WebSocket 服务端和连接管理"
```

---

### Task 8: Transport 抽象 — transport.go + channel_transport.go + 测试

**Files:**
- Create: `internal/swarm/server/gateway_push/transport.go`
- Create: `internal/swarm/server/gateway_push/channel_transport.go`
- Create: `internal/swarm/server/gateway_push/channel_transport_test.go`

- [ ] **Step 1: 创建 transport.go**

```go
package gateway_push

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// AgentTransport Gateway → AgentServer 的传输抽象
//
// 单进程模式（uapclaw app/chat/serve/acp）：
//   Gateway → ChannelTransport → AgentServer（Go channel，零网络开销）
//
// 跨进程模式（uapclaw gateway 独立部署）：
//   Gateway → WebSocketTransport → AgentServer（WebSocket + E2A 协议）
type AgentTransport interface {
	// Send 发送 E2A 请求信封到 AgentServer
	Send(ctx context.Context, envelope *e2a.E2AEnvelope) error
	// Recv 接收 AgentServer 的响应流
	Recv() (<-chan *e2a.E2AResponse, error)
	// Close 关闭传输连接
	Close() error
}
```

- [ ] **Step 2: 创建 channel_transport.go**

```go
package gateway_push

import (
	"context"
	"sync/atomic"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ChannelTransport 进程内 Go channel 传输
type ChannelTransport struct {
	// reqCh 请求通道
	reqCh chan *e2a.E2AEnvelope
	// respCh 响应通道
	respCh chan *e2a.E2AResponse
	// closed 关闭标志
	closed atomic.Bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const logComponent = logger.ComponentGateway

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewChannelTransport 创建进程内 Go channel 传输
func NewChannelTransport() *ChannelTransport {
	return &ChannelTransport{
		reqCh:  make(chan *e2a.E2AEnvelope, 256),
		respCh: make(chan *e2a.E2AResponse, 256),
	}
}

// Send 发送 E2A 请求信封
func (t *ChannelTransport) Send(ctx context.Context, envelope *e2a.E2AEnvelope) error {
	select {
	case t.reqCh <- envelope:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Recv 接收响应流
func (t *ChannelTransport) Recv() (<-chan *e2a.E2AResponse, error) {
	return t.respCh, nil
}

// Close 关闭传输连接
func (t *ChannelTransport) Close() error {
	if t.closed.CompareAndSwap(false, true) {
		close(t.reqCh)
		close(t.respCh)
	}
	return nil
}

// RequestCh 暴露请求通道，供 AgentServer 端消费
func (t *ChannelTransport) RequestCh() <-chan *e2a.E2AEnvelope {
	return t.reqCh
}

// SendResponse 供 AgentServer 端发送响应
func (t *ChannelTransport) SendResponse(resp *e2a.E2AResponse) {
	if !t.closed.Load() {
		t.respCh <- resp
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 3: 创建 channel_transport_test.go**

```go
package gateway_push

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
)

// TestNewChannelTransport 测试创建 ChannelTransport
func TestNewChannelTransport(t *testing.T) {
	ct := NewChannelTransport()
	assert.NotNil(t, ct)
}

// TestChannelTransport_SendRecv 测试发送和接收
func TestChannelTransport_SendRecv(t *testing.T) {
	ct := NewChannelTransport()
	defer ct.Close()

	env := e2a.NewE2AEnvelope()
	env.RequestID = "test-1"

	err := ct.Send(context.Background(), env)
	require.NoError(t, err)

	// AgentServer 端消费
	received := <-ct.RequestCh()
	assert.Equal(t, "test-1", received.RequestID)
}

// TestChannelTransport_SendResponse 测试 AgentServer 端发送响应
func TestChannelTransport_SendResponse(t *testing.T) {
	ct := NewChannelTransport()
	defer ct.Close()

	resp := e2a.NewE2AResponse()
	resp.RequestID = "test-1"
	ct.SendResponse(resp)

	// Gateway 端消费
	ch, err := ct.Recv()
	require.NoError(t, err)
	received := <-ch
	assert.Equal(t, "test-1", received.RequestID)
}

// TestChannelTransport_Send_Context取消 测试上下文取消
func TestChannelTransport_Send_Context取消(t *testing.T) {
	ct := NewChannelTransport()
	defer ct.Close()

	// 填满通道
	for i := 0; i < 256; i++ {
		_ = ct.Send(context.Background(), e2a.NewE2AEnvelope())
	}

	// 超时上下文
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := ct.Send(ctx, e2a.NewE2AEnvelope())
	assert.Error(t, err)
}

// TestChannelTransport_Close 测试关闭
func TestChannelTransport_Close(t *testing.T) {
	ct := NewChannelTransport()
	err := ct.Close()
	require.NoError(t, err)
	assert.True(t, ct.closed.Load())

	// 重复关闭不 panic
	err = ct.Close()
	require.NoError(t, err)
}
```

- [ ] **Step 4: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/gateway_push/ -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/server/gateway_push/
git commit -m "feat(gateway): 添加 AgentTransport 接口和 ChannelTransport 实现"
```

---

### Task 9: 骨架 — MessageHandler + AgentClient + doc.go

**Files:**
- Create: `internal/swarm/gateway/message_handler/message_handler.go`
- Create: `internal/swarm/gateway/routing/agent_client.go`
- Create: `internal/swarm/gateway/doc.go`
- Create: `internal/swarm/gateway/channel_manager/doc.go`
- Create: `internal/swarm/gateway/channel_manager/web/doc.go`
- Create: `internal/swarm/gateway/message_handler/doc.go`
- Create: `internal/swarm/gateway/routing/doc.go`
- Create: `internal/swarm/server/gateway_push/doc.go`

- [ ] **Step 1: 创建所有骨架文件和 doc.go**

message_handler.go — 空结构体 + Start/Stop 骨架方法
agent_client.go — 空结构体 + stub 方法
doc.go 文件 — 按项目规范写包文档（中文注释 + 文件目录树）

- [ ] **Step 2: 验证编译**

```bash
cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/gateway/...
cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/gateway_push/...
```

Expected: 无编译错误

- [ ] **Step 3: Commit**

```bash
git add internal/swarm/gateway/ internal/swarm/server/gateway_push/
git commit -m "feat(gateway): 添加 MessageHandler/AgentClient 骨架和 doc.go"
```

---

### Task 10: file_api — file_api.go + 测试

**Files:**
- Create: `internal/swarm/gateway/file_api.go`
- Create: `internal/swarm/gateway/file_api_test.go`

- [ ] **Step 1: 创建 file_api.go**

内容要点：
- `handleFileContentGet`：读取文件内容（query: path, encoding），路径安全检查，编码处理
- `handleFileContentPost`：写入 Markdown 文件（body: {path, content}），路径安全检查，仅允许 .md/.mdx
- `handleListFiles`：列目录内容（query: dir），排序（目录优先）
- `handleListMarkdown`：列 Markdown 文件（query: dir）
- `handleWsDebugConfigGet`/`handleWsDebugConfigPost`：WS 调试配置读写
- `handleRebuildAgentData`：重建 Agent 数据（stub）
- `isPathUnderAllowedRoot`：路径安全检查函数，禁止 `../` 穿越
- 工作区路径从 `workspace.AgentWorkspaceDir()` 获取

- [ ] **Step 2: 创建 file_api_test.go**

测试要点：
- `TestHandleFileContentGet_正常读取`
- `TestHandleFileContentGet_路径穿越`
- `TestHandleFileContentPost_仅允许Markdown`
- `TestHandleListFiles_正常`
- `TestHandleListMarkdown_正常`
- 使用 `httptest.NewServer` + `t.TempDir()`

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/ -run TestHandle -v
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/swarm/gateway/file_api.go internal/swarm/gateway/file_api_test.go
git commit -m "feat(gateway): 添加 /file-api/* HTTP 路由处理"
```

---

### Task 11: Gateway 组装 — app_gateway.go + embed.go + 测试

**Files:**
- Create: `internal/swarm/gateway/app_gateway.go`
- Create: `internal/swarm/gateway/embed.go`
- Create: `internal/swarm/gateway/app_gateway_test.go`

- [ ] **Step 1: 创建 embed.go**

```go
package gateway

import "embed"

//go:embed channel_manager/web/frontend/dist
var frontendDist embed.FS
```

- [ ] **Step 2: 创建 app_gateway.go**

内容要点：
- `GatewayServer` 结构体：`config`、`router`（*chi.Mux）、`webChannel`、`channelMgr`、`httpServer`（*http.Server）
- `NewGatewayServer`：创建 WebChannel + ChannelManager + 组装 chi router
- `setupRouter`：中间件（requestID、recover、zerolog）+ `/ws` 路由 + `/file-api/*` 路由 + SPA 静态文件
- `Start`：启动 HTTP 服务器（阻塞）
- `Stop`：优雅关闭（Shutdown with timeout）
- `spaHandler`：SPA fallback，非文件路径返回 index.html
- chi 中间件函数：`requestIDMiddleware`、`recoverMiddleware`、`zeroLogMiddleware`

- [ ] **Step 3: 创建 app_gateway_test.go**

测试要点：
- `TestNewGatewayServer`：验证创建不报错
- `TestGatewayServer_Router`：验证路由已注册（用 `httptest.NewServer` 测试 404/200）

- [ ] **Step 4: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/ -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/gateway/app_gateway.go internal/swarm/gateway/embed.go internal/swarm/gateway/app_gateway_test.go
git commit -m "feat(gateway): 添加 GatewayServer（chi router + 静态文件 + WS 路由）"
```

---

### Task 12: cmd/uapclaw app 命令

**Files:**
- Modify: `cmd/uapclaw/cmd.go`

- [ ] **Step 1: 修改 cmd.go 中 newAppCmd 函数**

将现有的 placeholder `Run` 替换为实际实现：
- 保留 `PreRunE: makeDotenvPreRunE()` hook
- `RunE` 中：加载 config → 创建 `GatewayServer` → `gw.Start(ctx)`
- 添加 `--host`、`--port` flags

- [ ] **Step 2: 验证编译**

```bash
cd /home/opensource/uapclaw-gateway && go build ./cmd/uapclaw/
```

Expected: 无编译错误

- [ ] **Step 3: Commit**

```bash
git add cmd/uapclaw/cmd.go
git commit -m "feat(cmd): 实现 uapclaw app 命令（启动 Gateway + 静态文件服务）"
```

---

### Task 13: 集成测试 — WebSocket 连接生命周期

**Files:**
- Create: `internal/swarm/gateway/channel_manager/web/web_connect_integration_test.go`

- [ ] **Step 1: 创建集成测试**

```go
//go:build integration

package web

// TestWebSocket_连接生命周期 测试真实 WS 连接 + connection.ack
// TestWebSocket_RPC请求响应 测试真实 WS 发 req → 收 res
// TestWebSocket_未注册方法 测试 METHOD_NOT_FOUND 错误
```

使用 `httptest.NewServer` + `gorilla/websocket` 客户端连接，验证：
1. 连接成功
2. 收到 `connection.ack` event
3. 发送 `config.get` req → 收到 res
4. 发送不存在方法 → 收到 METHOD_NOT_FOUND

- [ ] **Step 2: 运行集成测试**

```bash
cd /home/opensource/uapclaw-gateway && go test -tags=integration ./internal/swarm/gateway/channel_manager/web/ -v
```

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/swarm/gateway/channel_manager/web/web_connect_integration_test.go
git commit -m "test(gateway): 添加 WebSocket 集成测试"
```

---

### Task 14: 全量编译验证 + 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 全量编译**

```bash
cd /home/opensource/uapclaw-gateway && go build ./...
```

Expected: 无编译错误

- [ ] **Step 2: 运行全量单元测试**

```bash
cd /home/opensource/uapclaw-gateway && go test -cover ./internal/swarm/gateway/... ./internal/swarm/server/gateway_push/... -v
```

Expected: PASS，覆盖率 ≥ 85%

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md 章节状态**

按实现计划章节对应表更新状态：
- 11.1 ☐→✅
- 11.2 ☐→✅
- 11.3 ☐→🔄（骨架）
- 11.5 ☐→🔄（骨架）
- 11.9 ☐→🔄（WebChannel 部分）
- 11.14 ☐→✅
- 10.3.21 ☐→✅
- 12.7 ☐→🔄

- [ ] **Step 4: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新实现计划章节状态（Web Channel 通信打通完成）"
```
