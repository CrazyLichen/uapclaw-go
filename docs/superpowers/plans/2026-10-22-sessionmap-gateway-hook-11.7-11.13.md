# 11.7 SessionMap + 11.13 Gateway Hook 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 SessionMap（企业 IM 稳定身份→轮转 agent session_id 映射，checkpoint 持久化）与 GatewayHookHandler（4 事件网关钩子），完成 MessageHandler 4 处 TODO 回填与 HookExecutor 跨域迁移。

**Architecture:** HookExecutor 从 `server/hooks` 迁到 `common/hooks`（破 gateway→server 禁令，user_hook_rail 留守）；`gateway/routing` 新增 SessionMap；`gateway/hooks` 新增 GatewayHookHandler；MessageHandler 集成 4 个回填点；NewGatewayServer 装配 RegisterConfig。设计 spec：`docs/superpowers/specs/2026-10-22-sessionmap-gateway-hook-11.7-11.13-design.md`（7 项决策 D1-D7 已拍板）。

**Tech Stack:** Go 1.26 / 标准库（encoding/json、crypto/rand、sync、os/exec 由 executor 既有实现携带） / testify（现有测试惯例）

**测试命令惯例（Makefile）：**
```bash
# 单包测试（开发循环用）
CGO_ENABLED=1 go test -v -tags "sqlite_fts5 test" ./internal/<path>/...
# 全量覆盖率（任务收尾用）
CGO_ENABLED=1 go test -v -tags "sqlite_fts5 test" -coverprofile=coverage.out ./...
```
注意规则：同一时间只允许一个 go test 进程；编译前 `pgrep -f 'go (build|test)'` 检查残留。

**源码排列约定（项目规范）：** 每文件按 结构体→枚举→常量→全局变量→导出函数→非导出函数 顺序，分节注释 `// ──── 结构体 ────` 等；所有注释中文。

---

### Task 1: HookExecutor 迁移 server/hooks → common/hooks

**Files:**
- Create: `internal/common/hooks/executor.go`（从 `internal/swarm/server/hooks/executor.go` 整体迁移 + 调整）
- Create: `internal/common/hooks/executor_test.go`（迁移自 `internal/swarm/server/hooks/executor_test.go`）
- Create: `internal/common/hooks/executor_llm_test.go`（迁移，保留 `//go:build llm`）
- Delete: `internal/swarm/server/hooks/executor.go`、`executor_test.go`、`executor_llm_test.go`
- Modify: `internal/swarm/server/hooks/user_hook_rail.go`（import 改 common/hooks 的 executor 符号）
- Modify: `internal/swarm/server/hooks/doc.go`（去掉 executor 条目）
- Modify: `internal/common/hooks/doc.go`（增 executor 条目）
- Modify: `internal/swarm/server/agent_server.go:14,113`（RegisterConfig 改 import）
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go:28,202`、`internal/swarm/server/adapter/code_adapter.go:39,1018`（如引用 executor 符号则改）

**背景（给零上下文工程师）：** Go 项目规则禁止 `gateway/` import `server/`（规则 6.5，因 Gateway 与 AgentServer 是两个可独立部署的服务）。Python 里 Gateway 侧的 GatewayHookHandler 直接 `from jiuwenswarm.server.hooks.executor import HookExecutor`——Go 做不到，所以把 executor（无 server 域内依赖，仅依赖 agentcore/*+common/*）下沉到共享包 `common/hooks`。这是 transport 包提取（commit 9ba8a1b1）之后的第三例同类破环迁移。

- [ ] **Step 1.1: 迁移 executor.go 到 common/hooks**

```bash
git mv internal/swarm/server/hooks/executor.go internal/common/hooks/executor.go
git mv internal/swarm/server/hooks/executor_test.go internal/common/hooks/executor_test.go
git mv internal/swarm/server/hooks/executor_llm_test.go internal/common/hooks/executor_llm_test.go
```

迁移后修改 `internal/common/hooks/executor.go` 顶部包声明与文件头注释：

```go
package hooks

// 本文件从 server/hooks 迁入（11.13），供 Gateway 与 AgentServer 共享。
// 迁移原因：Python GatewayHookHandler 直接 import server 侧 executor，
// Go 规则 6.5 禁止 gateway→server，故下沉共享位置。
```

关键调整点：
1. **logComponent 冲突**：`server/hooks/executor.go:58` 定义 `logComponent = logger.ComponentAgentServer`，而 `common/hooks/config.go:61` 已定义 `logComponent = logger.ComponentCommon`——同包冲突。删除 executor.go 中的 logComponent 定义（常量块中该行+注释），executor 内日志调用统一用 `config.go` 已有的 `logger.ComponentCommon`。这符合规则 3.2 的组件选择（common/hooks 属基础设施层）。
2. **注释更新**：`RegisterConfig` 的注释"由 AgentServer 启动时通过 RegisterConfig 注册"改为"由 AgentServer 或 GatewayServer 启动时注册（谁先启动谁注册，幂等）"。

- [ ] **Step 1.2: user_hook_rail.go 改用共享 executor**

`internal/swarm/server/hooks/user_hook_rail.go`：executor 符号（`HookExecutor`/`HookResult`/`HookOutcome*`/`NewHookExecutor`）已不在本包。检查现有 import——文件已 import `hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"`（第 11 行）。

将 `user_hook_rail.go:25` 的 `executor *HookExecutor` 改为 `executor *hookscfg.HookExecutor`；`:40` 的 `NewHookExecutor()` 改为 `hookscfg.NewHookExecutor()`；`HookResult`/`HookOutcome*` 引用全部加 `hookscfg.` 前缀。

- [ ] **Step 1.3: server 侧 import 点修改**

`internal/swarm/server/agent_server.go`：
```go
// 第 14 行改为：
hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
// 第 113 行改为：
hookscfg.RegisterConfig(s.config)
```
注释同步：`// 注册全局 Config，供 HookExecutor.queryLLM 运行时读取（executor 已迁 common/hooks）`

`internal/swarm/server/adapter/deep_adapter_rails.go:28` 与 `code_adapter.go:39`：这两个文件只用了 `serverhooks.NewUserHookRail`（UserHookRail 仍在 server/hooks），import 不变。若文件里另有 executor 符号引用（grep 确认过没有），一并加 `hookscfg` import。

- [ ] **Step 1.4: 更新两个 doc.go**

`internal/common/hooks/doc.go` 文件目录段改为：
```
//	hooks/
//	├── doc.go              # 包文档
//	├── types.go            # HookType 枚举 + HookEvent 常量 + AgentRailEvents/GatewayEvents 分组
//	├── config.go           # CommandHookConfig/PromptHookConfig/HookMatcher/HooksConfig/LoadHooksConfig
//	├── executor.go         # HookExecutor + HookResult + RegisterConfig + ParseCommandOutput + ExtractJSONFromResponse
//	└── executor_llm_test.go 等测试文件
```
包功能概述补一句："11.13 起 HookExecutor 迁入本包，Gateway 与 AgentServer 共享（对齐 Python gateway/hooks/handler.py 对 server/hooks/executor.py 的直接引用，规避 Go gateway→server 禁令）。"

`internal/swarm/server/hooks/doc.go` 改为：
```go
// Package hooks（server/hooks）提供 UserHookRail，对齐 Python jiuwenswarm/server/hooks/user_hook_rail.py。
//
// HookExecutor 已迁至 common/hooks（11.13，供 Gateway/AgentServer 共享）。
//
// 文件目录：
//
//	hooks/
//	├── doc.go            # 包文档
//	└── user_hook_rail.go # UserHookRail(embed DeepAgentRail) 4 个钩子方法
//
// 对应 Python 代码：jiuwenswarm/server/hooks/user_hook_rail.py
package hooks
```

- [ ] **Step 1.5: 运行测试验证迁移**

```bash
pgrep -f 'go (build|test)' || true
CGO_ENABLED=1 go test -v -tags "sqlite_fts5 test" ./internal/common/hooks/... ./internal/swarm/server/hooks/... ./internal/swarm/server/...
```
Expected: 全部 PASS（executor_test 迁移后原测试全绿；server/hooks 只剩 user_hook_rail_test）。

- [ ] **Step 1.6: 编译全仓确认无残留引用**

```bash
go build ./...
```
Expected: 编译通过（若有文件残留引用旧位置 `server/hooks` 的 executor 符号会在此暴露）。

- [ ] **Step 1.7: Commit**

```bash
git add -A
git commit -m "refactor(hooks): HookExecutor 迁移 server/hooks → common/hooks（11.13 前置）

- gateway 侧需要复用 executor，规则 6.5 禁止 gateway→server，下沉共享位置
- user_hook_rail 留守 server/hooks，改用 hookscfg.* 符号
- agent_server.go RegisterConfig 改经 common/hooks
- 第三例破环迁移（transport/schema-events 之后）"
```

---

### Task 2: SessionMap 组件（gateway/routing/session_map.go）

**Files:**
- Create: `internal/swarm/gateway/routing/session_map.go`
- Create: `internal/swarm/gateway/routing/session_map_test.go`
- Modify: `internal/swarm/gateway/routing/doc.go`（文件目录增条目）

**背景：** Python `jiuwenswarm/gateway/routing/session_map.py`（109 行）。核心：`SessionMapScope` 枚举（per_chat_bot 默认 / per_chat_bot_user）+ `load_session_map_scope()`（config `gateway.session_map_scope` 读一次）+ `SessionMap`（map 持久化到 `{workspace}/agent/.checkpoint/session_map.json`，rotate 语义）。Go 需补 `sync.Mutex`（Python asyncio 单线程无锁）。

- [ ] **Step 2.1: 写失败测试（session_map_test.go 骨干用例）**

```go
package routing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
)

// newSessionMapAt 测试专用构造：注入 storePath（不污染公共 API）
func newSessionMapAt(storePath string, scope SessionMapScope) *SessionMap {
	m := &SessionMap{
		scope:     scope,
		storePath: storePath,
		mapping:   make(map[string]string),
	}
	m.load()
	return m
}

// TestMakeKey 两 scope 的键格式
func TestMakeKey(t *testing.T) {
	cases := []struct {
		scope  SessionMapScope
		want   string
	}{
		{SessionMapScopePerChatBot, "feishu_enterprise::oc-123::bot-001"},
		{SessionMapScopePerChatBotUser, "feishu_enterprise::oc-123::bot-001::user-9"},
	}
	for _, c := range cases {
		got := makeKey(c.scope, "feishu_enterprise", "oc-123", "bot-001", "user-9")
		if got != c.want {
			t.Errorf("makeKey(scope=%d) = %q, want %q", c.scope, got, c.want)
		}
	}
}

// TestMakeSessionID 格式：key + "::" + hex时间戳 + "::" + 6位hex
func TestMakeSessionID(t *testing.T) {
	sid := makeSessionID(SessionMapScopePerChatBot, "feishu_enterprise", "oc-123", "bot-001", "user-9")
	parts := strings.Split(sid, "::")
	if len(parts) != 5 {
		t.Fatalf("PerChatBot session_id 应 5 段，实际 %d 段: %q", len(parts), sid)
	}
	if parts[0] != "feishu_enterprise" || parts[1] != "oc-123" || parts[2] != "bot-001" {
		t.Errorf("前三段应为身份键，实际 %q", sid)
	}
	if len(parts[3]) == 0 {
		t.Errorf("时间戳段非空")
	}
	if len(parts[4]) != 6 {
		t.Errorf("随机后缀应 6 个 hex 字符，实际 %q", parts[4])
	}

	sid6 := makeSessionID(SessionMapScopePerChatBotUser, "p", "c", "b", "u")
	if got := strings.Split(sid6, "::"); len(got) != 6 {
		t.Errorf("PerChatBotUser session_id 应 6 段，实际 %d", len(got))
	}
}

// TestGetSessionID_非轮转幂等
func TestGetSessionID_非轮转幂等(t *testing.T) {
	dir := t.TempDir()
	m := newSessionMapAt(filepath.Join(dir, "session_map.json"), SessionMapScopePerChatBot)
	first := m.GetSessionID("p", "c", "b", "u", false)
	second := m.GetSessionID("p", "c", "b", "u", false)
	if first == "" {
		t.Fatal("首次应生成非空 sid")
	}
	if first != second {
		t.Errorf("非轮转两次应返回同值: %q vs %q", first, second)
	}
}

// TestGetSessionID_轮转生成新值且写盘
func TestGetSessionID_轮转生成新值且写盘(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "session_map.json")
	m := newSessionMapAt(store, SessionMapScopePerChatBot)
	old := m.GetSessionID("p", "c", "b", "u", false)
	fresh := m.GetSessionID("p", "c", "b", "u", true)
	if old == fresh {
		t.Fatal("轮转应生成新值")
	}
	// 写盘验证
	data, err := os.ReadFile(store)
	if err != nil {
		t.Fatalf("轮转后应写盘: %v", err)
	}
	var persisted map[string]string
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("持久化内容应为合法 JSON: %v", err)
	}
	if persisted[makeKey(SessionMapScopePerChatBot, "p", "c", "b", "u")] != fresh {
		t.Errorf("盘上映射应更新为新值")
	}
	if !strings.Contains(string(data), "\n  ") {
		t.Errorf("应使用 indent=2 缩进（对齐 json.dump indent=2）")
	}
}

// TestGetSessionID_跨实例恢复（checkpoint 持久化语义）
func TestGetSessionID_跨实例恢复(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "session_map.json")
	m1 := newSessionMapAt(store, SessionMapScopePerChatBot)
	sid := m1.GetSessionID("p", "c", "b", "u", false)

	m2 := newSessionMapAt(store, SessionMapScopePerChatBot)
	if got := m2.GetSessionID("p", "c", "b", "u", false); got != sid {
		t.Errorf("重启（新实例）后应恢复同 sid: %q vs %q", got, sid)
	}
}

// TestGetSessionID_存在相同sid幂等分支（Python L105-106：existing==sid 直接返回不写盘）
// 说明：makeSessionID 含时间戳+随机后缀，自然构造 existing==sid 不可行；
// 该分支的可达性依赖同毫秒+同随机（理论幂等），Go 保留对齐。
// 测试策略：同包测试直接构造内部状态——预置 mapping[key]=V 后调用 rotate=true，
// 验证新值 != V 时写盘；再验证非轮转返回 == V（分支 2 的回归）。
// existing==sid 分支本身由代码审查保证（无法注入 makeSessionID 输出），
// 在 spec §8 偏差记录中说明。
func TestGetSessionID_存在相同sid幂等分支(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "session_map.json")
	m := newSessionMapAt(store, SessionMapScopePerChatBot)
	key := makeKey(SessionMapScopePerChatBot, "p", "c", "b", "u")

	first := m.GetSessionID("p", "c", "b", "u", false)
	if m.mapping[key] != first {
		t.Fatalf("首次生成后 mapping 应记录返回值")
	}
	// 非轮转回归：返回既有值
	if got := m.GetSessionID("p", "c", "b", "u", false); got != first {
		t.Errorf("非轮转应返回既有值 %q，实际 %q", first, got)
	}
}

// TestLoad_损坏JSON降级
func TestLoad_损坏JSON降级(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "session_map.json")
	_ = os.WriteFile(store, []byte("{not json"), 0o644)
	m := newSessionMapAt(store, SessionMapScopePerChatBot)
	if len(m.mapping) != 0 {
		t.Errorf("损坏 JSON 应降级为空映射，实际 %d 项", len(m.mapping))
	}
	// 降级后仍可正常生成
	if m.GetSessionID("p", "c", "b", "u", false) == "" {
		t.Error("降级后应仍能生成新 sid")
	}
}

// TestLoad_过滤非法值
func TestLoad_过滤非法值(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "session_map.json")
	// 值非 string / 空串 / 键非 string 的都应剔除（Python isinstance(v, str) and v）
	content := `{"good::k1::b": "sid-1", "bad_int::k::b": 123, "bad_empty::k::b": ""}`
	_ = os.WriteFile(store, []byte(content), 0o644)
	m := newSessionMapAt(store, SessionMapScopePerChatBot)
	if len(m.mapping) != 1 {
		t.Fatalf("应只保留 1 项合法值，实际 %d", len(m.mapping))
	}
	if m.mapping["good::k1::b"] != "sid-1" {
		t.Errorf("合法项应保留")
	}
}
```

- [ ] **Step 2.2: 运行测试确认失败**

```bash
CGO_ENABLED=1 go test -v -tags "sqlite_fts5 test" ./internal/swarm/gateway/routing/ -run "TestMakeKey|TestMakeSessionID|TestGetSessionID|TestLoad_" 2>&1 | head -20
```
Expected: FAIL（`undefined: SessionMapScope` 等编译错误）。

- [ ] **Step 2.3: 实现 session_map.go**

```go
package routing

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionMap 会话映射：稳定身份（provider/chat/bot[/user]）→ 轮转 agent session_id。
// 持久化到 {workspace}/agent/.checkpoint/session_map.json。
// Python: jiuwenswarm/gateway/routing/session_map.py SessionMap
type SessionMap struct {
	// scope 键作用域（构造时确定，不热重载）
	scope SessionMapScope
	// storePath 持久化文件路径
	storePath string
	// mapping 身份键 → agent session_id
	mapping map[string]string
	// mu 并发保护（Python asyncio 单线程无锁，Go 补齐）
	mu sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// SessionMapScope 会话映射键作用域枚举
// Python: SessionMapScope(str, Enum)
type SessionMapScope int

const (
	// SessionMapScopePerChatBot 同 (provider, chat, bot) 共享会话（默认）
	SessionMapScopePerChatBot SessionMapScope = iota
	// SessionMapScopePerChatBotUser 同 (provider, chat, bot, user) 一个会话
	SessionMapScopePerChatBotUser
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sessionMapScopePerChatBotStr 配置值 "per_chat_bot"
	sessionMapScopePerChatBotStr = "per_chat_bot"
	// sessionMapScopePerChatBotUserStr 配置值 "per_chat_bot_user"
	sessionMapScopePerChatBotUserStr = "per_chat_bot_user"
	// sessionMapFileName 持久化文件名
	sessionMapFileName = "session_map.json"

	// logComponent 日志组件标识
	logComponent = logger.ComponentGateway
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// LoadSessionMapScope 读 config gateway.session_map_scope（构造时读一次，不热重载）。
//
// Python: load_session_map_scope (L21-33)：
// 全局读通道（getGlobalConfig → cfg.Load() 重读盘，等价 Python get_config()）；
// 未知值 / 未注册 / 读失败 → Warn + 默认 PerChatBot。
func LoadSessionMapScope() SessionMapScope {
	raw := sessionMapScopePerChatBotStr
	if cfg := hookscfg.GetGlobalConfigForScope(); cfg != nil {
		configBase, err := cfg.Load()
		if err == nil {
			if gateway, ok := configBase["gateway"].(map[string]any); ok {
				if v, ok := gateway["session_map_scope"].(string); ok && v != "" {
					raw = v
				}
			}
		} else {
			logger.Warn(logComponent).
				Err(err).
				Str("default", sessionMapScopePerChatBotStr).
				Msg("SessionMap scope load failed, using default")
			return SessionMapScopePerChatBot
		}
	}
	raw = strings.TrimSpace(strings.ToLower(raw))
	switch raw {
	case sessionMapScopePerChatBotStr:
		return SessionMapScopePerChatBot
	case sessionMapScopePerChatBotUserStr:
		return SessionMapScopePerChatBotUser
	default:
		logger.Warn(logComponent).
			Str("value", raw).
			Str("default", sessionMapScopePerChatBotStr).
			Msg("Unknown gateway.session_map_scope, using default")
		return SessionMapScopePerChatBot
	}
}

// NewSessionMap 创建会话映射。
//
// scope 为 nil 时走 LoadSessionMapScope() 读 config。
// Python: SessionMap(scope=None) (L65-69)
func NewSessionMap(scope *SessionMapScope) *SessionMap {
	s := SessionMapScopePerChatBot
	if scope != nil {
		s = *scope
	} else {
		s = LoadSessionMapScope()
	}
	return &SessionMap{
		scope:     s,
		storePath: filepath.Join(path.CheckpointDir(), sessionMapFileName),
		mapping:   make(map[string]string),
	}
}

// GetSessionID 获取（或生成）身份键对应的 agent session_id。
//
// rotate=false 且已有映射 → 返回既有值；否则生成新值写盘。
// Python: get_session_id (L90-109)。
func (m *SessionMap) GetSessionID(provider, chatID, botID, userID string, rotate bool) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := makeKey(m.scope, provider, chatID, botID, userID)
	existing, ok := m.mapping[key]
	if ok && !rotate {
		return existing
	}

	sid := makeSessionID(m.scope, provider, chatID, botID, userID)
	if existing == sid {
		// Python L105-106：同毫秒+同随机的理论幂等分支，直接返回不写盘
		return sid
	}
	m.mapping[key] = sid
	m.save()
	return sid
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// makeKey 生成身份键。
//
// Python: _make_key (L36-45)：
// PerChatBot: "provider::chatID::botID"；PerChatBotUser 追加 "::userID"
func makeKey(scope SessionMapScope, provider, chatID, botID, userID string) string {
	if scope == SessionMapScopePerChatBot {
		return fmt.Sprintf("%s::%s::%s", provider, chatID, botID)
	}
	return fmt.Sprintf("%s::%s::%s::%s", provider, chatID, botID, userID)
}

// makeSessionID 生成新的 agent session_id。
//
// Python: _make_session_id (L48-59)：key 结构 + "::" + hex(ms时间戳) + "::" + 6位随机hex
func makeSessionID(scope SessionMapScope, provider, chatID, botID, userID string) string {
	ts := fmt.Sprintf("%x", time.Now().UnixMilli())
	suffix := generateRandomHex(3)
	if scope == SessionMapScopePerChatBot {
		return fmt.Sprintf("%s::%s::%s::%s::%s", provider, chatID, botID, ts, suffix)
	}
	return fmt.Sprintf("%s::%s::%s::%s::%s::%s", provider, chatID, botID, userID, ts, suffix)
}

// load 从盘加载映射（防御式，失败仅 Warn）。
//
// Python: _load (L71-80)：文件不存在静默；非 dict 跳过；
// 值必须为非空 string（Python isinstance(v, str) and v）
func (m *SessionMap) load() {
	data, err := os.ReadFile(m.storePath)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Warn(logComponent).Err(err).Msg("SessionMap load failed")
		}
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		logger.Warn(logComponent).Err(err).Msg("SessionMap load failed")
		return
	}
	for k, v := range parsed {
		if s, ok := v.(string); ok && s != "" {
			m.mapping[k] = s
		}
	}
}

// save 将映射写盘（防御式，失败仅 Warn）。
//
// Python: _save (L82-88)：mkdir parents + json.dump(indent=2, ensure_ascii=False)
func (m *SessionMap) save() {
	if err := os.MkdirAll(filepath.Dir(m.storePath), 0o755); err != nil {
		logger.Warn(logComponent).Err(err).Msg("SessionMap save failed")
		return
	}
	data, err := json.MarshalIndent(m.mapping, "", "  ")
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("SessionMap save failed")
		return
	}
	if err := os.WriteFile(m.storePath, data, 0o644); err != nil {
		logger.Warn(logComponent).Err(err).Msg("SessionMap save failed")
	}
}

// generateRandomHex 生成 n 字节的随机 hex（2n 字符）。
// Python: secrets.token_hex(n)；crypto/rand 失败降级时间戳切片
func generateRandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())[:n*2]
	}
	return hex.EncodeToString(b)
}
```

**⚠️ 关键依赖**：`LoadSessionMapScope` 需要 `common/hooks` 暴露 `getGlobalConfig`——它是 executor.go 的私有函数。方案：在 `common/hooks` 增加**导出包装**（Task 1 完成后 executor.go 已在本包）：

```go
// executor.go 追加（导出函数区）：
// GetGlobalConfigForScope 返回全局 Config（供 SessionMapScope 等配置读取方使用）。
// 未注册时返回 nil。
func GetGlobalConfigForScope() *config.Config {
	return getGlobalConfig()
}
```

- [ ] **Step 2.4: 运行测试确认通过**

```bash
CGO_ENABLED=1 go test -v -tags "sqlite_fts5 test" ./internal/swarm/gateway/routing/ -run "TestMakeKey|TestMakeSessionID|TestGetSessionID|TestLoad_"
```
Expected: PASS（7 个用例）。

- [ ] **Step 2.5: 补 LoadSessionMapScope 测试（含 config 注册路径）**

`session_map_test.go` 追加：

```go
// TestLoadSessionMapScope_未注册config降级默认
func TestLoadSessionMapScope_未注册config降级默认(t *testing.T) {
	hookscfg.ResetGlobalConfigForTest()
	defer hookscfg.ResetGlobalConfigForTest()
	if got := LoadSessionMapScope(); got != SessionMapScopePerChatBot {
		t.Errorf("未注册 config 应返回默认 scope，实际 %d", got)
	}
}

// TestLoadSessionMapScope_未知值降级默认
func TestLoadSessionMapScope_未知值降级默认(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	content := "gateway:\n  session_map_scope: per_chat_bat_typo\n"
	_ = os.WriteFile(cfgFile, []byte(content), 0o644)

	cfg, err := config.New(cfgFile)
	if err != nil {
		t.Fatalf("config.New 失败: %v", err)
	}
	hookscfg.RegisterConfig(cfg)
	defer hookscfg.ResetGlobalConfigForTest()

	if got := LoadSessionMapScope(); got != SessionMapScopePerChatBot {
		t.Errorf("未知值应降级默认，实际 %d", got)
	}
}

// TestLoadSessionMapScope_读取per_chat_bot_user
func TestLoadSessionMapScope_读取per_chat_bot_user(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	content := "gateway:\n  session_map_scope: PER_CHAT_BOT_USER\n" // 大写验证 lower 归一
	_ = os.WriteFile(cfgFile, []byte(content), 0o644)

	cfg, err := config.New(cfgFile)
	if err != nil {
		t.Fatalf("config.New 失败: %v", err)
	}
	hookscfg.RegisterConfig(cfg)
	defer hookscfg.ResetGlobalConfigForTest()

	if got := LoadSessionMapScope(); got != SessionMapScopePerChatBotUser {
		t.Errorf("应读取 per_chat_bot_user（大小写无关），实际 %d", got)
	}
}
```

`common/hooks/executor.go` 追加测试辅助（**跨包测试需要——ResetGlobalConfigForTest 必须导出**，与 GetGlobalConfigForScope 一并在此步骤加入）：

```go
// ResetGlobalConfigForTest 清空全局 Config 注册（仅供测试隔离使用）。
func ResetGlobalConfigForTest() {
	globalCfgMu.Lock()
	globalCfg = nil
	globalCfgMu.Unlock()
}
```

（测试文件头已含 config/hookscfg import，见 Step 2.1。）

- [ ] **Step 2.6: 运行全部 routing 测试**

```bash
CGO_ENABLED=1 go test -v -tags "sqlite_fts5 test" ./internal/swarm/gateway/routing/
```
Expected: PASS（含既有 agent_client/route_binding 测试）。

- [ ] **Step 2.7: 更新 routing/doc.go 文件目录**

文件目录段插入：
```
//	routing/
//	├── doc.go              # 包文档
//	├── agent_client.go     # AgentClient 完整实现
//	├── route_binding.go    # RouteBinding 多路径 WS 路由绑定
//	└── session_map.go      # SessionMap 企业通道会话映射（11.7）
```
包概述补："SessionMap 将企业 IM 稳定身份映射为轮转 agent session_id（checkpoint 持久化，Python session_map.py）。"

- [ ] **Step 2.8: Commit**

```bash
git add -A
git commit -m "feat(routing): 11.7 SessionMap 会话映射组件

- SessionMapScope 枚举 + LoadSessionMapScope（gateway.session_map_scope，未知值/未注册降级默认+Warn）
- makeKey/makeSessionID（:: 分隔 + hex 时间戳 + 6位随机 hex）
- SessionMap：rotate 语义 + checkpoint 持久化（load 过滤/防御式 save）
- sync.Mutex 并发补齐；GetGlobalConfigForScope/ResetGlobalConfigForTest 导出包装"
```

---

### Task 3: GatewayHookHandler（gateway/hooks/handler.go）

**Files:**
- Create: `internal/swarm/gateway/hooks/doc.go`
- Create: `internal/swarm/gateway/hooks/handler.go`
- Create: `internal/swarm/gateway/hooks/handler_test.go`

**背景：** Python `jiuwenswarm/gateway/hooks/handler.py`（127 行）。4 个事件方法（SessionStart/UserPromptSubmit/SessionEnd/Notification），共同结构：`config.match(event, query)` → 逐项 setdefault timeout=10 → `executor.run_all`（失败 Warn 不传播）。SessionStart 有 activeSessions 去重；SessionEnd 先 discard。**同步执行**（保证 session 生命周期顺序），异步性由调用方 TriggerSessionStartHook 的 goroutine 提供。

- [ ] **Step 3.1: 写失败测试（handler_test.go）**

```go
package hooks

import (
	"context"
	"sync"
	"testing"

	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
)

// newHandlerWithEvents 构造带自定义 events 配置的 handler
func newHandlerWithEvents(events map[string][]hookscfg.HookMatcher) *GatewayHookHandler {
	return NewGatewayHookHandler(&hookscfg.HooksConfig{
		Events: events,
	})
}

// TestOnSessionStart_activeSessions去重
func TestOnSessionStart_activeSessions去重(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	h := NewGatewayHookHandler(&hookscfg.HooksConfig{
		Events: map[string][]hookscfg.HookMatcher{
			hookscfg.HookEventSessionStart: {{
				Matcher: "*",
				Hooks:   []map[string]any{{"type": "command", "command": "echo ok"}},
			}},
		},
	})
	// 用 hook 执行次数间接验证：去重时第二次调用不执行 hook
	h.mu.Lock()
	h.executor = countingExecutor(t, &calls, &mu)
	h.mu.Unlock()

	h.OnSessionStart(context.Background(), "sess-1", "startup")
	h.OnSessionStart(context.Background(), "sess-1", "startup")
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("同 session 二次 SessionStart 应去重，实际执行 %d 次", calls)
	}
}

// TestOnSessionEnd_移除activeSessions后可再次start
func TestOnSessionEnd_移除activeSessions后可再次start(t *testing.T) {
	h := newHandlerWithEvents(map[string][]hookscfg.HookMatcher{})
	h.mu.Lock()
	h.activeSessions["sess-1"] = true
	h.mu.Unlock()

	h.OnSessionEnd(context.Background(), "sess-1", "clear")

	h.mu.Lock()
	_, still := h.activeSessions["sess-1"]
	h.mu.Unlock()
	if still {
		t.Error("SessionEnd 应从 activeSessions 移除")
	}
}

// TestOnUserPromptSubmit_timeoutSetdefault
func TestOnUserPromptSubmit_timeoutSetdefault(t *testing.T) {
	hooks := []map[string]any{
		{"type": "command", "command": "echo ok"},               // 无 timeout → 补 10
		{"type": "command", "command": "echo ok", "timeout": 5}, // 有 → 不覆盖
	}
	h := NewGatewayHookHandler(&hookscfg.HooksConfig{
		Events: map[string][]hookscfg.HookMatcher{
			hookscfg.HookEventUserPromptSubmit: {{Matcher: "*", Hooks: hooks}},
		},
	})
	h.OnUserPromptSubmit(context.Background(), "sess-1", "hello")

	// setdefault 就地修改共享 map（Python 语义）
	if v, _ := hooks[0]["timeout"].(int); v != 10 {
		t.Errorf("无 timeout 应补 10，实际 %v", hooks[0]["timeout"])
	}
	if v, _ := hooks[1]["timeout"].(int); v != 5 {
		t.Errorf("已有 timeout=5 不应覆盖，实际 %v", hooks[1]["timeout"])
	}
}

// TestOnNotification_matchQuery差异
func TestOnNotification_matchQuery差异(t *testing.T) {
	// matcher 精确 "permission"：notification_type 不匹配时不执行
	executed := false
	h := NewGatewayHookHandler(&hookscfg.HooksConfig{
		Events: map[string][]hookscfg.HookMatcher{
			hookscfg.HookEventNotification: {{
				Matcher: "permission",
				Hooks:   []map[string]any{{"type": "command", "command": "echo ok"}},
			}},
		},
	})
	h.mu.Lock()
	h.executor = boolExecutor(&executed)
	h.mu.Unlock()

	h.OnNotification(context.Background(), "cron", "job done", "sess-1")
	if executed {
		t.Error("notification_type=cron 不匹配 matcher=permission，不应执行")
	}
	h.OnNotification(context.Background(), "permission", "need approve", "sess-1")
	if !executed {
		t.Error("notification_type=permission 匹配，应执行")
	}
}

// TestDisableAllHooks_短路
func TestDisableAllHooks_短路(t *testing.T) {
	executed := false
	h := NewGatewayHookHandler(&hookscfg.HooksConfig{
		DisableAllHooks: true,
		Events: map[string][]hookscfg.HookMatcher{
			hookscfg.HookEventUserPromptSubmit: {{
				Matcher: "*",
				Hooks:   []map[string]any{{"type": "command", "command": "echo ok"}},
			}},
		},
	})
	h.mu.Lock()
	h.executor = boolExecutor(&executed)
	h.mu.Unlock()

	h.OnUserPromptSubmit(context.Background(), "sess-1", "hello")
	if executed {
		t.Error("DisableAllHooks 应短路")
	}
}
```

测试辅助（同文件，**依赖 D7 具体类型——通过可替换字段注入**，为让 executor 可替换，`executor` 字段设计为 `*hookscfg.HookExecutor` 不可替换……修正：D7 已拍板具体类型+recover 测试，**不做 mock 注入**。上面测试里的 countingExecutor/boolExecutor 方案改为：直接用真实 executor 跑 echo 命令验证执行与否）：

修正后的测试辅助：

```go
// 验证 hook 是否执行：用 hook 配置里的 command 落盘副作用判断
// （真实 executor 跑 echo > 文件，比 mock 更贴近 D7 决策）
```

实际采用方案：**activeSessions 去重测试改为验证行为而非执行次数**——第二次调用后 activeSessions 仍包含 sess-1 且函数提前 return（通过代码路径保证）；**match-query 差异测试改为验证 timeout setdefault 副作用**（matcher 不匹配时 Match 返回空 → 不进入 setdefault 循环 → hooks map 不被修改）。测试改写为断言 hooks 配置的修改与否，executor 真实执行 echo（无副作用断言）。

```go
// TestOnNotification_matchQuery差异（重写：matcher 不匹配 → 配置未被 setdefault）
func TestOnNotification_matchQuery差异(t *testing.T) {
	hooks := []map[string]any{{"type": "command", "command": "echo ok"}}
	h := NewGatewayHookHandler(&hookscfg.HooksConfig{
		Events: map[string][]hookscfg.HookMatcher{
			hookscfg.HookEventNotification: {{
				Matcher: "permission",
				Hooks:   hooks,
			}},
		},
	})
	h.OnNotification(context.Background(), "cron", "job done", "sess-1")
	if _, touched := hooks[0]["timeout"]; touched {
		t.Error("notification_type=cron 不匹配 matcher=permission，不应触发 setdefault")
	}
	h.OnNotification(context.Background(), "permission", "need approve", "sess-1")
	if v, _ := hooks[0]["timeout"].(int); v != 10 {
		t.Errorf("匹配时应补 timeout=10，实际 %v", hooks[0]["timeout"])
	}
}

// TestOnSessionStart_activeSessions去重（重写：二次调用不触发第二次 setdefault）
func TestOnSessionStart_activeSessions去重(t *testing.T) {
	hooks := []map[string]any{{"type": "command", "command": "echo ok"}}
	h := NewGatewayHookHandler(&hookscfg.HooksConfig{
		Events: map[string][]hookscfg.HookMatcher{
			hookscfg.HookEventSessionStart: {{
				Matcher: "*",
				Hooks:   hooks,
			}},
		},
	})
	h.OnSessionStart(context.Background(), "sess-1", "startup")
	if v, _ := hooks[0]["timeout"].(int); v != 10 {
		t.Fatalf("首次应补 timeout=10")
	}
	// 复位 timeout 验证第二次不再进入 setdefault（去重提前 return）
	delete(hooks[0], "timeout")
	h.OnSessionStart(context.Background(), "sess-1", "startup")
	if _, touched := hooks[0]["timeout"]; touched {
		t.Error("同 session 二次 SessionStart 应去重（不应再次 setdefault）")
	}
}
```

- [ ] **Step 3.2: 运行测试确认失败**

```bash
CGO_ENABLED=1 go test -v -tags "sqlite_fts5 test" ./internal/swarm/gateway/hooks/ 2>&1 | head -10
```
Expected: FAIL（包不存在 / undefined: GatewayHookHandler）。

- [ ] **Step 3.3: 实现 handler.go 与 doc.go**

`internal/swarm/gateway/hooks/handler.go`：

```go
// Package hooks 提供 Gateway 层钩子处理器。
//
// GatewayHookHandler 在 Gateway 层执行 session / 生命周期类 hooks：
// 同步串行执行（保证 session 生命周期顺序）、超时默认 10s、失败永不阻塞用户请求。
//
// 文件目录：
//
//	hooks/
//	├── doc.go     # 包文档
//	└── handler.go # GatewayHookHandler 4 事件方法
//
// 对应 Python 代码：jiuwenswarm/gateway/hooks/handler.py
package hooks

import (
	"context"
	"os"
	"sync"

	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GatewayHookHandler Gateway 层钩子处理器
// Python: GatewayHookHandler (handler.py L16-127)
type GatewayHookHandler struct {
	// config hooks 配置聚合
	config *hookscfg.HooksConfig
	// executor hook 执行器（共享自 common/hooks）
	executor *hookscfg.HookExecutor
	// gatewayTimeout 网关层 hook 超时（秒），短于 AgentServer 30s 避免阻塞转发
	gatewayTimeout int
	// mu 保护 activeSessions
	mu sync.Mutex
	// activeSessions 活跃会话去重集
	activeSessions map[string]bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentGateway

	// defaultGatewayTimeout 默认网关 hook 超时 10 秒（对齐 Python _gateway_timeout）
	defaultGatewayTimeout = 10
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGatewayHookHandler 创建 Gateway 层钩子处理器。
// Python: GatewayHookHandler(hooks_config) (L25-29)
func NewGatewayHookHandler(hooksConfig *hookscfg.HooksConfig) *GatewayHookHandler {
	return &GatewayHookHandler{
		config:         hooksConfig,
		executor:       hookscfg.NewHookExecutor(),
		gatewayTimeout: defaultGatewayTimeout,
		activeSessions: make(map[string]bool),
	}
}

// OnSessionStart 会话开始时触发 SessionStart hooks。
//
// Python: on_session_start (L31-57)：activeSessions 去重；
// match(query=source)；失败仅 Warn 不传播。
func (h *GatewayHookHandler) OnSessionStart(ctx context.Context, sessionID, source string) {
	h.mu.Lock()
	if h.activeSessions[sessionID] {
		h.mu.Unlock()
		return
	}
	h.activeSessions[sessionID] = true
	h.mu.Unlock()

	hookConfigs := h.config.Match(hookscfg.HookEventSessionStart, source)
	if len(hookConfigs) == 0 {
		return
	}
	applyGatewayTimeout(hookConfigs, h.gatewayTimeout)

	cwd, _ := os.Getwd() // 失败降级空串（Python Path.cwd() 不会失败，Go 需防御）
	defer recoverHookPanic("SessionStart")
	h.executor.RunAll(ctx, hookConfigs, map[string]any{
		"event":      "SessionStart",
		"source":     source,
		"session_id": sessionID,
		"cwd":        cwd,
	}, sessionID)
}

// OnUserPromptSubmit 用户提交消息时触发 UserPromptSubmit hooks。
//
// Python: on_user_prompt_submit (L59-78)：match 无 query（默认空串）。
func (h *GatewayHookHandler) OnUserPromptSubmit(ctx context.Context, sessionID, prompt string) {
	hookConfigs := h.config.Match(hookscfg.HookEventUserPromptSubmit, "")
	if len(hookConfigs) == 0 {
		return
	}
	applyGatewayTimeout(hookConfigs, h.gatewayTimeout)

	defer recoverHookPanic("UserPromptSubmit")
	h.executor.RunAll(ctx, hookConfigs, map[string]any{
		"event":      "UserPromptSubmit",
		"prompt":     prompt,
		"session_id": sessionID,
	}, sessionID)
}

// OnSessionEnd 会话结束时触发 SessionEnd hooks。
//
// Python: on_session_end (L80-103)：先 discard activeSessions，match(query=reason)。
func (h *GatewayHookHandler) OnSessionEnd(ctx context.Context, sessionID, reason string) {
	h.mu.Lock()
	delete(h.activeSessions, sessionID)
	h.mu.Unlock()

	hookConfigs := h.config.Match(hookscfg.HookEventSessionEnd, reason)
	if len(hookConfigs) == 0 {
		return
	}
	applyGatewayTimeout(hookConfigs, h.gatewayTimeout)

	defer recoverHookPanic("SessionEnd")
	h.executor.RunAll(ctx, hookConfigs, map[string]any{
		"event":      "SessionEnd",
		"reason":     reason,
		"session_id": sessionID,
	}, sessionID)
}

// OnNotification 通知发送时触发 Notification hooks。
//
// Python: on_notification (L105-127)：match(query=notification_type)。
func (h *GatewayHookHandler) OnNotification(ctx context.Context, notificationType, message, sessionID string) {
	hookConfigs := h.config.Match(hookscfg.HookEventNotification, notificationType)
	if len(hookConfigs) == 0 {
		return
	}
	applyGatewayTimeout(hookConfigs, h.gatewayTimeout)

	defer recoverHookPanic("Notification")
	h.executor.RunAll(ctx, hookConfigs, map[string]any{
		"event":             "Notification",
		"notification_type": notificationType,
		"message":           message,
		"session_id":        sessionID,
	}, sessionID)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// applyGatewayTimeout 逐项补默认超时（Python cfg.setdefault("timeout", 10)）。
//
// 就地修改共享 map（Python 同样就地改 Match 返回的 dict 引用，行为一致）。
func applyGatewayTimeout(hookConfigs []map[string]any, timeout int) {
	for i := range hookConfigs {
		if _, ok := hookConfigs[i]["timeout"]; !ok {
			hookConfigs[i]["timeout"] = timeout
		}
	}
}

// recoverHookPanic hook 执行 panic 兜底（对齐 Python try/except + logger.warning）。
func recoverHookPanic(event string) {
	if r := recover(); r != nil {
		logger.Warn(logComponent).
			Str("event", event).
			Any("panic", r).
			Msg("GatewayHookHandler hook failed")
	}
}
```

注：`defer recoverHookPanic("SessionStart")` 需要参数求值时立即求值（recover 在 defer 函数体内调用才有效——`recoverHookPanic` 内部调用 `recover()`，作为 defer 的函数调用合法）。

`doc.go` 内容已并入 handler.go 包注释（单文件包，按项目惯例 doc.go 独立）：创建 `internal/swarm/gateway/hooks/doc.go`，把上述包注释移入 doc.go，handler.go 只留 `package hooks` + import + 实现。

- [ ] **Step 3.4: 运行测试确认通过**

```bash
CGO_ENABLED=1 go test -v -tags "sqlite_fts5 test" ./internal/swarm/gateway/hooks/
```
Expected: PASS（5 个用例：去重/移除/setdefault/match-query/DisableAllHooks）。

- [ ] **Step 3.5: Commit**

```bash
git add -A
git commit -m "feat(gateway/hooks): 11.13 GatewayHookHandler 四事件网关钩子

- OnSessionStart（activeSessions 去重 + match query=source）
- OnUserPromptSubmit（match 默认空串 query）
- OnSessionEnd（先 discard + match query=reason）
- OnNotification（match query=notification_type）
- applyGatewayTimeout setdefault 10s + recoverHookPanic 兜底（对齐 Python try/except）"
```

---

### Task 4: MessageHandler 集成——字段与构造装配

**Files:**
- Modify: `internal/swarm/gateway/message_handler/message_handler.go`（新增字段 + 构造 + TriggerSessionStartHook）
- Modify: `internal/swarm/gateway/message_handler/channel_state.go`（新增 extractIdentityTuple/channelIDMatchesSessionMapTypes + sessionMapChannelTypes）

- [ ] **Step 4.1: 写失败测试（message_handler_test.go 追加）**

```go
// TestNewMessageHandler_sessionMap与HookHandler装配 测试构造注入
func TestNewMessageHandler_sessionMap与HookHandler装配(t *testing.T) {
	transport := transport.NewChannelTransport()
	agentClient := routing.NewAgentClient(transport)
	mh := NewMessageHandler(agentClient)

	assert.NotNil(t, mh.sessionMap, "sessionMap 应在构造时装配")
	assert.NotNil(t, mh.gatewayHookHandler, "gatewayHookHandler 应在构造时装配（LoadHooksConfig 不会失败——空 config 返回空配置）")
}

// TestExtractIdentityTuple 四元组提取
func TestExtractIdentityTuple(t *testing.T) {
	// 全齐
	msg := schema.NewReqMessage("feishu_enterprise", "s",
		schema.ReqMethodChatSend, json.RawMessage(`{}`),
		schema.WithProvider("feishu_enterprise"),
		schema.WithChatID("oc-1"),
		schema.WithBotID("bot-1"),
		schema.WithUserID("u-1"))
	p, c, b, u, ok := extractIdentityTuple(msg)
	assert.True(t, ok)
	assert.Equal(t, "feishu_enterprise", p)
	assert.Equal(t, "oc-1", c)
	assert.Equal(t, "bot-1", b)
	assert.Equal(t, "u-1", u)

	// 缺 user_id
	msg2 := schema.NewReqMessage("feishu_enterprise", "s",
		schema.ReqMethodChatSend, json.RawMessage(`{}`),
		schema.WithProvider("feishu_enterprise"),
		schema.WithChatID("oc-1"),
		schema.WithBotID("bot-1"))
	_, _, _, _, ok2 := extractIdentityTuple(msg2)
	assert.False(t, ok2, "缺任一字段应 ok=false")

	// 空白字符串按缺失处理（Python strip 语义）
	msg3 := schema.NewReqMessage("feishu_enterprise", "s",
		schema.ReqMethodChatSend, json.RawMessage(`{}`),
		schema.WithProvider("  "),
		schema.WithChatID("oc-1"),
		schema.WithBotID("bot-1"),
		schema.WithUserID("u-1"))
	_, _, _, _, ok3 := extractIdentityTuple(msg3)
	assert.False(t, ok3, "空白 provider 应按缺失处理")
}

// TestChannelIDMatchesSessionMapTypes 通道族匹配
func TestChannelIDMatchesSessionMapTypes(t *testing.T) {
	cases := []struct {
		cid  string
		want bool
	}{
		{"feishu_enterprise", true},
		{"feishu_enterprise:bot-2", true},
		{"feishu", false},
		{"web", false},
		{"", false},
		{"feishu_enterpriseX", false}, // 前缀必须带冒号
	}
	for _, c := range cases {
		if got := channelIDMatchesSessionMapTypes(c.cid); got != c.want {
			t.Errorf("channelIDMatchesSessionMapTypes(%q) = %v, want %v", c.cid, got, c.want)
		}
	}
}
```

- [ ] **Step 4.2: 运行确认失败**

```bash
CGO_ENABLED=1 go test -v -tags "sqlite_fts5 test" ./internal/swarm/gateway/message_handler/ -run "TestNewMessageHandler_sessionMap|TestExtractIdentityTuple|TestChannelIDMatchesSessionMapTypes" 2>&1 | head -8
```
Expected: FAIL（undefined: extractIdentityTuple 等）。

- [ ] **Step 4.3: 实现 channel_state.go 辅助函数**

`channel_state.go` 修改（非导出函数区追加；init() 的受控渠道集合后追加 sessionMap 集合）：

```go
// （init 函数内追加）
	// 构建企业通道会话映射族集合
	// Python: _session_map_channel_types = frozenset({"feishu_enterprise"})
	sessionMapChannelTypes = map[string]bool{
		"feishu_enterprise": true,
	}
```

全局变量区追加：
```go
// sessionMapChannelTypes 使用 SessionMap 的通道族集合（精确或 base: 前缀匹配）
var sessionMapChannelTypes map[string]bool
```

非导出函数区追加：
```go
// extractIdentityTuple 提取消息身份四元组。
//
// Python: _extract_identity_tuple (L321-329)：
// 四字段 TrimSpace 后任一为空返回 ok=false（对齐 Python all(identity_parts)）
func extractIdentityTuple(msg *schema.Message) (provider, chatID, botID, userID string, ok bool) {
	provider = strings.TrimSpace(msg.Provider)
	chatID = strings.TrimSpace(msg.ChatID)
	botID = strings.TrimSpace(msg.BotID)
	userID = strings.TrimSpace(msg.UserID)
	if provider == "" || chatID == "" || botID == "" || userID == "" {
		return "", "", "", "", false
	}
	return provider, chatID, botID, userID, true
}

// channelIDMatchesSessionMapTypes channel_id 是否属于 SessionMap 通道族。
//
// Python: _channel_id_matches_session_map_types (L331-337)：
// 精确匹配或 "base:" 前缀
func channelIDMatchesSessionMapTypes(channelID string) bool {
	cid := strings.TrimSpace(channelID)
	for base := range sessionMapChannelTypes {
		if cid == base || strings.HasPrefix(cid, base+":") {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4.4: 实现 message_handler.go 字段与构造**

字段区追加（`updateChannelInConfig` 字段后）：
```go
	// sessionMap 企业通道会话映射（对齐 Python _session_map，11.7）
	sessionMap *routing.SessionMap
	// gatewayHookHandler Gateway 层钩子处理器（对齐 Python _gateway_hook_handler，11.13；装配失败为 nil）
	gatewayHookHandler *hooksgw.GatewayHookHandler
```

import 增加：
```go
	hooksgw "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/hooks"
	// routing 已在 import（agentClient 字段使用）
```

`NewMessageHandler` 构造体尾部追加：
```go
	// 11.7：企业通道会话映射（scope 从 config 读一次）
	// Python: self._session_map = SessionMap() (L155)
	m := &MessageHandler{ /* 既有字段 */ }
	m.sessionMap = routing.NewSessionMap(nil)

	// 11.13：Gateway hooks handler 装配（失败 Warn + nil，对齐 Python L163-170）
	// Python: load_hooks_config(get_config()) + GatewayHookHandler(config)
	m.gatewayHookHandler = m.initGatewayHookHandler()
	return m
```

新增非导出函数（放 message_handler.go 非导出区）：
```go
// initGatewayHookHandler 装配 Gateway hooks handler。
//
// Python: L163-170 try/except——LoadHooksConfig 失败时 Warn + 返回 nil。
func (mh *MessageHandler) initGatewayHookHandler() *hooksgw.GatewayHookHandler {
	defer func() {
		if r := recover(); r != nil {
			logger.Warn(logComponent).
				Any("panic", r).
				Msg("Failed to init GatewayHookHandler")
		}
	}()
	cfg := hookscfg.GetGlobalConfigForScope()
	if cfg == nil {
		logger.Warn(logComponent).
			Msg("Failed to init GatewayHookHandler: global config not registered")
		return nil
	}
	configBase, err := cfg.Load()
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Msg("Failed to init GatewayHookHandler")
		return nil
	}
	hooksCfg := hookscfg.LoadHooksConfig(configBase)
	return hooksgw.NewGatewayHookHandler(hooksCfg)
}
```

注意：构造函数改造为两段式（字面量 + 追加装配），因为 initGatewayHookHandler 需在 struct 创建后调用（方法）。同时 NewMessageHandler 里创建 sessionMap 会触发 LoadSessionMapScope → cfg.Load()（若 RegisterConfig 已装配）。

新增导出方法（导出函数区）：
```go
// TriggerSessionStartHook 供 Channel 层调用，触发 SessionStart hook。
//
// Python: trigger_session_start_hook (L172-177)：asyncio.create_task fire-and-forget。
func (mh *MessageHandler) TriggerSessionStartHook(sessionID, source string) {
	if mh.gatewayHookHandler == nil {
		return
	}
	go mh.gatewayHookHandler.OnSessionStart(context.Background(), sessionID, source)
}
```

message_handler.go import 需补 `hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"`（若未引入）与 `"context"`（TriggerSessionStartHook 用）。

- [ ] **Step 4.5: 运行测试确认通过**

```bash
CGO_ENABLED=1 go test -v -tags "sqlite_fts5 test" ./internal/swarm/gateway/message_handler/ -run "TestNewMessageHandler|TestExtractIdentityTuple|TestChannelIDMatchesSessionMapTypes"
```
Expected: PASS。注意：测试进程未调 RegisterConfig 时 GetGlobalConfigForScope()==nil → gatewayHookHandler 为 nil（Warn 日志）——**修正 Step 4.1 测试断言**：`assert.Nil` 或改为注册后断言非 nil：

```go
	// 未注册全局 config 时 handler 为 nil（对齐 Python try/except 降级）
	assert.Nil(t, mh.gatewayHookHandler)
	assert.NotNil(t, mh.sessionMap)
```

（构造装配完整路径在 Task 6 的 NewGatewayServer 测试中覆盖 RegisterConfig 后非 nil。）

- [ ] **Step 4.6: Commit**

```bash
git add -A
git commit -m "feat(message_handler): 11.7+11.13 字段装配与身份提取

- sessionMap/gatewayHookHandler 字段 + 构造装配（对齐 Python L155/L163-170）
- initGatewayHookHandler：config 未注册/加载失败 Warn + nil
- TriggerSessionStartHook 导出（对齐 trigger_session_start_hook，goroutine fire-and-forget）
- extractIdentityTuple（四元组 TrimSpace 全非空）+ channelIDMatchesSessionMapTypes（精确/base: 前缀）"
```

---

### Task 5: 回填点 ①② —— GetOrCreateChannelState + ApplyChannelState

**Files:**
- Modify: `internal/swarm/gateway/message_handler/channel_state.go:92-151`
- Modify: `internal/swarm/gateway/message_handler/channel_state_test.go`（追加用例）

**⚠️ 陷阱（spec §6.3）**：ApplyChannelState 的 SessionMap 分支是**无条件覆盖** `state.SessionID` + `msg.SessionID`（Python L1156-1157）；非 SessionMap 分支才是 `elif`（state 有值才写 msg）。

- [ ] **Step 5.1: 写失败测试（channel_state_test.go 追加）**

```go
// TestApplyChannelState_sessionMap分支 企业通道身份齐备走 SessionMap
func TestApplyChannelState_sessionMap分支(t *testing.T) {
	mh := createTestMessageHandler() // 既有 helper：transport+agentClient 构造
	// SessionMap 用测试注入（避免写真实 checkpoint 目录）
	mh.sessionMap = newTestSessionMap(t)

	msg := schema.NewReqMessage("feishu_enterprise", "",
		schema.ReqMethodChatSend, json.RawMessage(`{}`),
		schema.WithProvider("feishu_enterprise"),
		schema.WithChatID("oc-9"),
		schema.WithBotID("bot-1"),
		schema.WithUserID("u-1"))

	mh.ApplyChannelState(msg)

	if !strings.Contains(msg.SessionID, "feishu_enterprise::oc-9::bot-1::") {
		t.Errorf("SessionMap 分支应注入 session-map 风格 sid，实际 %q", msg.SessionID)
	}
	// state 同步刷新（无条件覆盖，spec 陷阱点）
	state := mh.GetOrCreateChannelState(msg)
	if state.SessionID != msg.SessionID {
		t.Errorf("state.SessionID 应与 msg.SessionID 同步: %q vs %q", state.SessionID, msg.SessionID)
	}
	// 二次应用幂等（非轮转返回同值）
	first := msg.SessionID
	mh.ApplyChannelState(msg)
	if msg.SessionID != first {
		t.Errorf("二次 Apply 应幂等: %q vs %q", msg.SessionID, first)
	}
}

// TestApplyChannelState_身份缺失走elif 企业通道但缺 user_id → config/state 路径
func TestApplyChannelState_身份缺失走elif(t *testing.T) {
	mh := createTestMessageHandler()
	mh.sessionMap = newTestSessionMap(t)

	msg := schema.NewReqMessage("feishu_enterprise", "existing-sid",
		schema.ReqMethodChatSend, json.RawMessage(`{}`),
		schema.WithProvider("feishu_enterprise"),
		schema.WithChatID("oc-9"),
		schema.WithBotID("bot-1")) // 缺 UserID

	mh.ApplyChannelState(msg)
	if msg.SessionID != "existing-sid" {
		t.Errorf("身份缺失应走 elif 分支保留入站 sid，实际 %q", msg.SessionID)
	}
}

// TestApplyChannelState_非企业通道不触发SessionMap
func TestApplyChannelState_非企业通道不触发SessionMap(t *testing.T) {
	mh := createTestMessageHandler()
	mh.sessionMap = newTestSessionMap(t)

	msg := schema.NewReqMessage("feishu_test", "",
		schema.ReqMethodChatSend, json.RawMessage(`{}`),
		schema.WithProvider("feishu"),
		schema.WithChatID("oc-9"),
		schema.WithBotID("bot-1"),
		schema.WithUserID("u-1"))

	mh.ApplyChannelState(msg)
	if strings.Contains(msg.SessionID, "::") {
		t.Errorf("非企业通道不应走 SessionMap，实际 sid %q", msg.SessionID)
	}
}

// TestGetOrCreateChannelState_首次创建sessionMap覆盖 首建时 SessionMap 覆盖 config 默认值
func TestGetOrCreateChannelState_首次创建sessionMap覆盖(t *testing.T) {
	mh := createTestMessageHandler()
	mh.sessionMap = newTestSessionMap(t)

	msg := schema.NewReqMessage("feishu_enterprise", "",
		schema.ReqMethodChatSend, json.RawMessage(`{}`),
		schema.WithProvider("feishu_enterprise"),
		schema.WithChatID("oc-9"),
		schema.WithBotID("bot-1"),
		schema.WithUserID("u-1"))

	state := mh.GetOrCreateChannelState(msg)
	if !strings.Contains(state.SessionID, "feishu_enterprise::oc-9::bot-1::") {
		t.Errorf("首次创建应被 SessionMap 覆盖 config 默认，实际 %q", state.SessionID)
	}
}

// newTestSessionMap 测试用 SessionMap（t.TempDir 隔离）
func newTestSessionMap(t *testing.T) *routing.SessionMap {
	return routing.NewSessionMapAtForTest(filepath.Join(t.TempDir(), "session_map.json"), routing.SessionMapScopePerChatBot)
}
```

⚠️ `NewSessionMapAtForTest` 需要在 routing 包**导出**一个测试构造（message_handler 包是不同包，无法访问 newSessionMapAt 私有构造）。在 `session_map.go` 追加：

```go
// NewSessionMapAtForTest 指定存储路径构造（供跨包测试使用，生产代码勿用）。
func NewSessionMapAtForTest(storePath string, scope SessionMapScope) *SessionMap {
	return newSessionMapAt(storePath, scope)
}
```

（`newSessionMapAt` 为 Step 2.1 测试文件里定义的私有构造——**移到 session_map.go 非导出区**，测试文件与实现文件同包共用。）

- [ ] **Step 5.2: 运行确认失败**

```bash
CGO_ENABLED=1 go test -v -tags "sqlite_fts5 test" ./internal/swarm/gateway/message_handler/ -run "TestApplyChannelState_sessionMap|TestGetOrCreateChannelState_首次" 2>&1 | head -8
```
Expected: FAIL（NewSessionMapAtForTest undefined + 逻辑未实现）。

- [ ] **Step 5.3: 实现回填点 ①②**

`channel_state.go` `GetOrCreateChannelState`（L126-151）修改——TODO 行删除，创建默认 state 后追加：

```go
func (mh *MessageHandler) GetOrCreateChannelState(msg *schema.Message) *ChannelControlState {
	ch := msg.ChannelID
	key := getChannelStateKey(ch, msg.SessionID)

	mh.statesMu.RLock()
	state, exists := mh.channelStates[key]
	mh.statesMu.RUnlock()

	if exists {
		return state
	}

	// 创建默认状态（从 config 读取）
	state = mh.getChannelDefaultState(ch)

	// 11.7：企业通道身份齐备时，SessionMap 覆盖默认 session_id
	// Python: _get_or_create_channel_state (L295-297)
	if p, c, b, u, ok := extractIdentityTuple(msg); ok && channelIDMatchesSessionMapTypes(ch) {
		state.SessionID = mh.sessionMap.GetSessionID(p, c, b, u, false)
	}

	mh.statesMu.Lock()
	// 双重检查，防止并发创建
	if existing, ok := mh.channelStates[key]; ok {
		mh.statesMu.Unlock()
		return existing
	}
	mh.channelStates[key] = state
	mh.statesMu.Unlock()

	return state
}
```

`ApplyChannelState`（L92-119）修改——`if state.SessionID != ""` 块替换为双分支：

```go
func (mh *MessageHandler) ApplyChannelState(msg *schema.Message) {
	channelType := mh.resolveControlChannelType(msg)
	if !controlChannelTypes[string(channelType)] {
		return
	}
	state := mh.GetOrCreateChannelState(msg)

	// 11.7：企业通道族走 SessionMap（无条件刷新 state + msg）；其它受控通道按 state（elif）
	// Python: _apply_channel_state (L1151-1159)
	if p, c, b, u, ok := extractIdentityTuple(msg); ok && channelIDMatchesSessionMapTypes(msg.ChannelID) {
		sid := mh.sessionMap.GetSessionID(p, c, b, u, false)
		state.SessionID = sid
		msg.SessionID = sid
	} else if state.SessionID != "" {
		msg.SessionID = state.SessionID
	}

	// 将 mode 写入 params（既有逻辑不动）
	// ……（原 params setdefault 代码保持原样）
}
```

- [ ] **Step 5.4: 运行测试确认通过**

```bash
CGO_ENABLED=1 go test -v -tags "sqlite_fts5 test" ./internal/swarm/gateway/message_handler/ -run "TestApplyChannelState|TestGetOrCreateChannelState"
```
Expected: PASS（含既有受控渠道/setdefault 用例不回归）。

- [ ] **Step 5.5: Commit**

```bash
git add -A
git commit -m "feat(message_handler): 回填 11.7 集成点①②（GetOrCreateChannelState + ApplyChannelState）

- 首建时 SessionMap 覆盖 config 默认 session_id（对齐 Python L295-297）
- ApplyChannelState 双分支：企业通道无条件刷新 + 非 SessionMap elif（对齐 L1151-1159 陷阱点）
- NewSessionMapAtForTest 跨包测试构造"
```

---

### Task 6: 回填点 ③ —— handleChannelControl 对齐 Python 结构

**Files:**
- Modify: `internal/swarm/gateway/message_handler/slash_cmd.go:62-122`
- Modify: `internal/swarm/gateway/message_handler/slash_cmd_test.go`（适配签名变化 + 新用例）

**背景（D5）**：Python 把"改状态"写在 `_handle_channel_control` 函数体（L652-667），收尾甩异步；Go 现在是一锅端在 `newSessionCancelAndNotice`。重构为 Python 结构。

- [ ] **Step 6.1: 写失败测试（slash_cmd_test.go 追加）**

```go
// TestHandleChannelControl_newSession企业通道SessionMap轮转
func TestHandleChannelControl_newSession企业通道SessionMap轮转(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	mh.sessionMap = newTestSessionMap(t)

	go func() { for range mh.robotMessages {} }()

	msg := schema.NewReqMessage("feishu_enterprise", "sess-old",
		schema.ReqMethodChatSend, json.RawMessage(`{"content":"/new_session"}`),
		schema.WithProvider("feishu_enterprise"),
		schema.WithChatID("oc-9"),
		schema.WithBotID("bot-1"),
		schema.WithUserID("u-1"))

	handled := mh.handleChannelControl(msg)
	assert.True(t, handled)

	state := mh.GetOrCreateChannelState(msg)
	if !strings.Contains(state.SessionID, "feishu_enterprise::oc-9::bot-1::") {
		t.Errorf("企业通道 /new_session 应经 SessionMap 轮转，实际 %q", state.SessionID)
	}
}

// TestHandleChannelControl_newSession非企业通道走GenerateChannelSessionID
func TestHandleChannelControl_newSession非企业通道(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	mh.sessionMap = newTestSessionMap(t)

	go func() { for range mh.robotMessages {} }()

	msg := schema.NewReqMessage("feishu_test", "sess-old",
		schema.ReqMethodChatSend, json.RawMessage(`{"content":"/new_session"}`))

	handled := mh.handleChannelControl(msg)
	assert.True(t, handled)

	state := mh.GetOrCreateChannelState(msg)
	if !strings.HasPrefix(state.SessionID, "feishu_test_") {
		t.Errorf("非企业通道应走 GenerateChannelSessionID（channelID_ 前缀），实际 %q", state.SessionID)
	}
}
```

既有测试 `TestNewSessionCancelAndNotice`（slash_cmd_test.go:159-177）**签名适配**：`newSessionCancelAndNotice(msg, parsed)` → `newSessionCancelAndNotice(msg, oldSID, newSID)`。

- [ ] **Step 6.2: 运行确认失败**

```bash
CGO_ENABLED=1 go test -v -tags "sqlite_fts5 test" ./internal/swarm/gateway/message_handler/ -run "TestHandleChannelControl_newSession" 2>&1 | head -8
```
Expected: FAIL（企业通道用例：sid 未含 `::`；现逻辑一律 GenerateChannelSessionID）。

- [ ] **Step 6.3: 重构 handleChannelControl + newSessionCancelAndNotice**

`slash_cmd.go`：`handleChannelControl` 的 switch 前追加 state 获取（对齐 Python L652），NewSessionOK 分支展开（对齐 L654-680）：

```go
	// 获取当前会话的状态（使用复合键）
	// Python: state = self._get_or_create_channel_state(msg) (L652)
	state := mh.GetOrCreateChannelState(msg)

	// 按 action 分发
	switch parsed.Action {
	case command_parser.ActionNewSessionOK:
		// 11.7+11.13：SessionMap 轮转 + state 更新 + SessionStart hook
		// Python: L654-667（全部在入口函数体内，收尾甩子函数）
		oldSID := state.SessionID
		var newSID string
		if p, c, b, u, ok := extractIdentityTuple(msg); ok && channelIDMatchesSessionMapTypes(msg.ChannelID) {
			newSID = mh.sessionMap.GetSessionID(p, c, b, u, true) // rotate=true
		} else {
			newSID = GenerateChannelSessionID(string(channelType))
		}
		state.SessionID = newSID
		// 触发 SessionStart hook（fire-and-forget）
		mh.TriggerSessionStartHook(newSID, string(channelType))
		// 收尾：异步取消旧会话 + 发通知
		mh.newSessionCancelAndNotice(msg, oldSID, newSID)
	case command_parser.ActionNewSessionBad:
		// …（其余分支不动）
```

`newSessionCancelAndNotice` 瘦身为收尾函数：

```go
// newSessionCancelAndNotice /new_session 收尾：异步取消旧会话 + 发变更通知。
//
// Python: _new_session_cancel_and_notice (L575-591)。
// 状态更新（SessionMap 轮转/state.SessionID/hook）已上提至 handleChannelControl（D5 对齐）。
func (mh *MessageHandler) newSessionCancelAndNotice(msg *schema.Message, oldSID, newSID string) {
	// 异步取消旧会话（静默模式：publishInterruptResult=false）
	if oldSID != "" {
		go mh.CancelAgentWorkForSession(context.Background(), msg, oldSID, false)
	}
	mh.sendChannelNotice(msg, map[string]any{"content": fmt.Sprintf("[收到 CLI 指令], session_id 已变更为 %s", newSID)})
}
```

注意：`channelType` 变量在 switch 前已由 `mh.resolveControlChannelType(msg)` 赋值（L56 现有代码），NewSessionOK 分支直接复用。`TODO(#11.7+#11.13)` 行删除。

- [ ] **Step 6.4: 运行测试确认通过（含签名适配的既有用例）**

```bash
CGO_ENABLED=1 go test -v -tags "sqlite_fts5 test" ./internal/swarm/gateway/message_handler/ -run "TestHandleChannelControl|TestNewSessionCancelAndNotice"
```
Expected: PASS。

- [ ] **Step 6.5: Commit**

```bash
git add -A
git commit -m "feat(message_handler): 回填 11.7+11.13 集成点③（handleChannelControl 对齐 Python 结构）

- state 获取上提 switch 前（对齐 Python L652）
- /new_session：SessionMap rotate=true 轮转 + state 更新 + TriggerSessionStartHook（对齐 L654-667）
- newSessionCancelAndNotice 瘦身为收尾函数（取消旧会话+通知）
- TODO(#11.7+#11.13) 清除（D5 决策）"
```

---

### Task 7: 回填点 ④ —— forwardLoop 步骤3 UserPromptSubmit

**Files:**
- Modify: `internal/swarm/gateway/message_handler/forward_loop.go:97-99`
- Modify: `internal/swarm/gateway/message_handler/forward_loop_test.go`（追加用例）

- [ ] **Step 7.1: 写失败测试（forward_loop_test.go 追加）**

```go
// TestForwardLoop_步骤3UserPromptSubmitHook 触发 UserPromptSubmit（nil handler 跳过不 panic）
func TestForwardLoop_步骤3UserPromptSubmitHook(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	// nil handler（未注册全局 config 的默认构造路径）不 panic 即可
	msg := schema.NewReqMessage("feishu_test", "sess-1",
		schema.ReqMethodChatSend, json.RawMessage(`{"content":"hi"}`))

	// 直接驱动被测片段等价物：ApplyChannelState + 步骤3 调用
	mh.ApplyChannelState(msg)
	// 步骤3 的调用在 forwardLoop 内部；本用例验证 nil-guard 不 panic：
	mh.forwardLoopStep3UserPromptSubmit(context.Background(), msg)

	// 非 nil handler：配置一个 UserPromptSubmit hook（command echo）验证执行
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	hookYAML := "hooks:\n  events:\n    UserPromptSubmit:\n      - matcher: \"*\"\n        hooks:\n          - type: command\n            command: echo ok\n"
	_ = os.WriteFile(cfgFile, []byte(hookYAML), 0o644)
	cfg, err := config.New(cfgFile)
	if err != nil {
		t.Fatalf("config.New: %v", err)
	}
	hookscfg.RegisterConfig(cfg)
	defer hookscfg.ResetGlobalConfigForTest()

	hooksCfg := hookscfg.LoadHooksConfig(mustLoadConfigBase(t, cfg))
	mh.gatewayHookHandler = hooksgw.NewGatewayHookHandler(hooksCfg)
	mh.forwardLoopStep3UserPromptSubmit(context.Background(), msg) // 不 panic 即通过
}
```

⚠️ 若 forward_loop_test.go 缺 import：`os`、`path/filepath`、`config`、`hookscfg`、`hooksgw`。

`mustLoadConfigBase` helper（无则新增）：
```go
func mustLoadConfigBase(t *testing.T, cfg *config.Config) map[string]any {
	t.Helper()
	base, err := cfg.Load()
	if err != nil {
		t.Fatalf("cfg.Load: %v", err)
	}
	return base
}
```

- [ ] **Step 7.2: 运行确认失败**

```bash
CGO_ENABLED=1 go test -v -tags "sqlite_fts5 test" ./internal/swarm/gateway/message_handler/ -run "TestForwardLoop_步骤3" 2>&1 | head -6
```
Expected: FAIL（`forwardLoopStep3UserPromptSubmit` undefined）。

- [ ] **Step 7.3: 实现（forward_loop.go）**

步骤3 TODO 块替换为：

```go
				// 步骤3: Gateway hook: UserPromptSubmit（11.13 回填）
				// Python: L2188-2197（失败仅 Debug 日志，不阻塞转发）
				mh.forwardLoopStep3UserPromptSubmit(ctx, msg)
```

新增非导出方法（forward_loop.go 非导出区）：

```go
// forwardLoopStep3UserPromptSubmit 步骤3：触发 UserPromptSubmit hooks。
//
// Python: _forward_loop 内 L2188-2197：
// params 解析非 map 时空 map；prompt 取 params["query"] 或 params["content"]；
// handler 为 nil 跳过；异常仅 Debug 日志。
func (mh *MessageHandler) forwardLoopStep3UserPromptSubmit(ctx context.Context, msg *schema.Message) {
	if mh.gatewayHookHandler == nil {
		return
	}
	var paramsMap map[string]any
	if len(msg.Params) > 0 {
		if err := json.Unmarshal(msg.Params, &paramsMap); err != nil {
			paramsMap = map[string]any{}
		}
	}
	promptText := ""
	if v, ok := paramsMap["query"].(string); ok && v != "" {
		promptText = v
	} else if v, ok := paramsMap["content"].(string); ok {
		promptText = v
	}
	// Python: str(params.get("query") or params.get("content") or "")
	defer func() {
		if r := recover(); r != nil {
			logger.Debug(logComponent).
				Any("panic", r).
				Msg("Gateway hook UserPromptSubmit failed")
		}
	}()
	mh.gatewayHookHandler.OnUserPromptSubmit(ctx, msg.SessionID, promptText)
}
```

（forward_loop.go 需已有 `encoding/json` 与 `logger` import——按现有文件头核对，缺则补。）

- [ ] **Step 7.4: 运行测试确认通过**

```bash
CGO_ENABLED=1 go test -v -tags "sqlite_fts5 test" ./internal/swarm/gateway/message_handler/ -run "TestForwardLoop_步骤3"
```
Expected: PASS。

- [ ] **Step 7.5: Commit**

```bash
git add -A
git commit -m "feat(message_handler): 回填 11.13 集成点④（forwardLoop 步骤3 UserPromptSubmit）

- forwardLoopStep3UserPromptSubmit：params 解析 + query/content 提取 + nil-guard + Debug 兜底
- 对齐 Python _forward_loop L2188-2197，TODO(#11.13) 清除"
```

---

### Task 8: NewGatewayServer 装配 RegisterConfig

**Files:**
- Modify: `internal/swarm/gateway/app_gateway.go:71-84`
- Modify: `internal/swarm/gateway/app_gateway_test.go`（追加用例）

- [ ] **Step 8.1: 写失败测试**

```go
// TestNewGatewayServer_装配全局Config RegisterConfig 幂等装配
func TestNewGatewayServer_装配全局Config(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(cfgFile, []byte("gateway:\n  session_map_scope: per_chat_bot_user\n"), 0o644)

	hookscfg.ResetGlobalConfigForTest()
	defer hookscfg.ResetGlobalConfigForTest()

	cfg, err := config.New(cfgFile)
	if err != nil {
		t.Fatalf("config.New: %v", err)
	}

	transport := transport.NewChannelTransport()
	agentClient := routing.NewAgentClient(transport)

	s, err := NewGatewayServer(cfg, agentClient)
	if err != nil {
		t.Fatalf("NewGatewayServer: %v", err)
	}
	assert.NotNil(t, s)

	// 全局 config 已注册（scope 可读到 per_chat_bot_user）
	if got := hookscfg.GetGlobalConfigForScope(); got == nil {
		t.Fatal("NewGatewayServer 应装配全局 Config")
	}
	if got := routing.LoadSessionMapScope(); got != routing.SessionMapScopePerChatBotUser {
		t.Errorf("装配后 scope 应读到 per_chat_bot_user，实际 %d", got)
	}
	// MessageHandler 的 hookHandler 非 nil
	if s.msgHandler == nil || s.msgHandler.gatewayHookHandler == nil {
		t.Error("装配后 MessageHandler.gatewayHookHandler 应非 nil")
	}
}
```

⚠️ app_gateway_test.go 需补 import：`hookscfg`、`routing`（若无）、`transport`（既有）。

- [ ] **Step 8.2: 运行确认失败**

```bash
CGO_ENABLED=1 go test -v -tags "sqlite_fts5 test" ./internal/swarm/gateway/ -run "TestNewGatewayServer_装配全局Config" 2>&1 | head -6
```
Expected: FAIL（GetGlobalConfigForScope()==nil）。

- [ ] **Step 8.3: 实现（app_gateway.go）**

`NewGatewayServer` 步骤1 之前插入：

```go
	// 11.7+11.13：注册全局 Config（幂等，对齐 Python get_config() 全局可用性）
	// Python: 独立 Gateway 进程不启动 AgentServer，全局函数随时可读；
	// Go 由 NewGatewayServer 注册，12.7 单体路径由 AgentServer 注册，谁先启动谁注册
	hookscfg.RegisterConfig(cfg)
```

import 增加：`hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"`。

- [ ] **Step 8.4: 运行测试确认通过**

```bash
CGO_ENABLED=1 go test -v -tags "sqlite_fts5 test" ./internal/swarm/gateway/ -run "TestNewGatewayServer"
```
Expected: PASS（含既有用例）。

- [ ] **Step 8.5: Commit**

```bash
git add -A
git commit -m "feat(gateway): NewGatewayServer 装配全局 Config（D4）

- hookscfg.RegisterConfig(cfg) 幂等注册，独立 Gateway 进程（12.9）可读 scope/hooks 配置
- 对齐 Python get_config() 全局可用性；12.7 单体路径由 AgentServer 注册不冲突"
```

---

### Task 9: doc.go 同步 + IMPLEMENTATION_PLAN.md 回填

**Files:**
- Modify: `internal/swarm/gateway/message_handler/doc.go`（文件目录与概述更新）
- Modify: `IMPLEMENTATION_PLAN.md`（11.7/11.13/10.3.23.3 三行回填）

- [ ] **Step 9.1: 更新 message_handler/doc.go**

文件目录段 `channel_state.go` 条目补 SessionMap 说明，`slash_cmd.go` 条目补 Python 结构说明：

```
//	message_handler/
//	├── doc.go              # 包文档
//	├── message_handler.go  # MessageHandler 核心结构（双 channel、transport、state、SessionMap/hookHandler 装配）
//	├── forward_loop.go     # 入站转发循环（forwardLoop 11步骤含步骤3 UserPromptSubmit hook + processStream）
//	├── outbound.go         # 出站 push 消费 + handleAgentServerPush + cron push
//	├── channel_state.go    # ChannelControlState + ApplyChannelState（SessionMap 双分支）+ 身份四元组提取
//	├── convert.go          # E2AResponse→Message 转换函数
//	├── cancel.go           # 取消/中断逻辑
//	├── slash_cmd.go        # Slash 命令处理（handleChannelControl Python 结构 + SessionMap 轮转 + SessionStart hook）
//	├── at_file.go          # @file 引用解析
//	├── evolution.go        # Evolution 状态机
//	├── dispatch.go         # 派发辅助
//	├── config_inject.go    # SetOutboundPipeline 注入
//	├── disconnect.go       # 断连取消
//	└── command_parser/     # Slash 命令解析器子包
```

- [ ] **Step 9.2: 回填 IMPLEMENTATION_PLAN.md**

11.7 行（L736）：
```markdown
| 11.7 | ✅ | SessionMap | scope 枚举+LoadSessionMapScope（未知值/未注册降级默认）+ makeKey/makeSessionID（::分隔+hex时间戳+6位hex）+ checkpoint 持久化（load 过滤/防御 save）+ MessageHandler 集成：首建覆盖/ApplyChannelState 无条件刷新双分支（对齐 L1151-1159 陷阱）/new_session rotate 轮转 + TriggerSessionStartHook；sync.Mutex 并发补齐 | `jiuwenswarm/gateway/routing/session_map.py` |
```

11.13 行（L742）：
```markdown
| 11.13 | ✅ | Gateway Hook | GatewayHookHandler 4 事件方法（SessionStart 去重/UserPromptSubmit 空 query/SessionEnd 先 discard/Notification type query）+ activeSessions + 10s setdefault + recover 兜底；HookExecutor 迁 server/hooks→common/hooks（第三例破环，user_hook_rail 留守）；forward_loop 步骤3 回填；NewGatewayServer 装配 RegisterConfig | `jiuwenswarm/gateway/hooks/` |
```

10.3.23.3 行（L683）已 ✅，产出列补注：
```markdown
`executor.py`（11.13 起对应物迁至 internal/common/hooks/executor.go 供 Gateway/AgentServer 共享）
```

- [ ] **Step 9.3: 全仓测试 + 覆盖率**

```bash
pgrep -f 'go (build|test)' || true
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -coverprofile=coverage.out ./internal/common/hooks/... ./internal/swarm/gateway/... ./internal/swarm/server/...
go tool cover -func=coverage.out | grep -E "session_map|handler\.go|executor\.go|channel_state|slash_cmd|forward_loop" 
```
Expected: 新增文件覆盖率 ≥85%；受影响包全部 PASS。

- [ ] **Step 9.4: Commit**

```bash
git add -A
git commit -m "docs(plan): 11.7+11.13 实现完成回填（IMPLEMENTATION_PLAN + doc.go 同步）"
```

---

## 验收清单（对齐 spec）

- [ ] gateway 域 0 个 import server（`grep -rn "uapclaw-go/internal/swarm/server" internal/swarm/gateway --include="*.go" | grep -v _test | wc -l` == 0）
- [ ] 代码内 TODO(#11.7)/TODO(#11.7+#11.13)/TODO(#11.13) 全部清除（grep 为空）
- [ ] TODO(#11.12) 等其它章节回填标记未受影响
- [ ] session_map.json 语义：rotate=false 幂等 / rotate=true 新值 / 跨实例恢复 / 损坏降级
- [ ] GatewayHookHandler：4 事件 query 差异 / setdefault / 去重 / DisableAllHooks 短路
- [ ] 全量测试绿 + 新增包覆盖率 ≥85%
