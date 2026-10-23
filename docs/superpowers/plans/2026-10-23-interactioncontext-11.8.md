# 11.8 InteractionContext 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 PendingInteraction 追问上下文存储层（8 方法 + dataclass 三类默认值语义对齐），对齐 Python `jiuwenswarm/gateway/routing/interaction_context.py`（189 行）。

**Architecture:** 单文件纯函数存储层，放 `internal/swarm/gateway/routing/interaction_context.go`（与 Python 同包对齐，routing 包已有 11.7 SessionMap 先例）。无状态文件操作不加锁；每个方法动态调 `path.AgentInteractionsDir()` + mkdir（对齐 Python `_get_interactions_dir()` 每次调用语义）。消费方 11.12/11.16/11.21 ⤵️ 回填，本期零调用（先例：CryptoUtility set_crypto_provider 零调用死代码保留）。

**Tech Stack:** Go 标准库（encoding/json / path/filepath / os / time / strconv / strings）+ internal/common/logger + internal/common/utils/path。测试用 t.Setenv("UAPCLAW_DATA_DIR") + pathutil.ResetCache() 注入（memoryrpc 先例）。

**Spec:** `docs/superpowers/specs/2026-10-23-interactioncontext-11.8-design.md`

**Python 源锚点**（注释中引用，实施时勿重查）：`/home/opensource/jiuwenswarm-develop/jiuwenswarm/gateway/routing/interaction_context.py`
- L28 `_get_interactions_dir` / L34 `_filter_sensitive` / L39-54 dataclass 字段 / L56 `_path` / L59 `save` / L84 `remove` / L94 `load` / L106 `find_pending` / L133 `find_all_group_pending` / L149 `find_group_pending` / L154 `cleanup_expired` / L172 `build_resume_content`

**编译纪律**（项目规则 5）：编译前 `pgrep -f 'go (build|test)'` 检查残留进程，有则先 kill；同一时间只跑一个 go 编译进程；`export GOPROXY=https://goproxy.cn,direct`；不要 `go clean -cache`。

---

### Task 1: 测试脚手架 + Save/Remove（写失败测试 → 最小实现）

**Files:**
- Create: `internal/swarm/gateway/routing/interaction_context.go`
- Create: `internal/swarm/gateway/routing/interaction_context_test.go`

- [ ] **Step 1: 写失败测试（Save 落盘与敏感键过滤 + Remove）**

创建 `internal/swarm/gateway/routing/interaction_context_test.go`：

```go
package routing

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// setupInteractionsEnv 隔离 interactions 目录环境（memoryrpc 先例）
func setupInteractionsEnv(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmp)
	pathutil.ResetCache()
	t.Cleanup(pathutil.ResetCache)
	return tmp
}

// newTestPI 构造测试用完整 PendingInteraction（15 字段全填）
func newTestPI(id string) *PendingInteraction {
	return &PendingInteraction{
		InteractionID:    id,
		Mode:             "group",
		OriginChannelID:  "feishu_enterprise:app1",
		OriginSessionID:  "sess-1",
		OriginContent:    "帮我订会议室",
		OriginSenderName: "张三",
		OriginSenderID:   "ou-zhangsan",
		Question:         "需要哪个楼层？",
		TargetUserID:     "ou-zhangsan",
		TargetUserName:   "张三",
		OriginMetadata:   map[string]any{"api_key": "sk-secret", "Token": "tok", "AUTHORIZATION": "Bearer x", "keep": "v"},
		PrincipalID:      "ou-principal",
		Status:           "pending",
		CreatedAt:        1700000000.0,
		UpdatedAt:        1700000000.0,
	}
}

// TestSave_落盘与敏感键过滤 Save 写文件内容断言（api_key/Token/AUTHORIZATION 大小写三态剔除）
func TestSave_落盘与敏感键过滤(t *testing.T) {
	setupInteractionsEnv(t)

	pi := newTestPI("gpq_sess-1_ou-zhangsan")
	pi.Save()

	// 文件应存在
	fp := filepath.Join(pathutil.AgentInteractionsDir(), "gpq_sess-1_ou-zhangsan.json")
	raw, err := os.ReadFile(fp)
	if err != nil {
		t.Fatalf("save 后文件应存在: %v", err)
	}

	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("应为合法 JSON: %v", err)
	}

	// 敏感键剔除（小写比较三键）
	for _, k := range []string{"api_key", "Token", "AUTHORIZATION"} {
		if _, ok := data["origin_metadata"].(map[string]any)[k]; ok {
			t.Errorf("敏感键 %q 应被剔除", k)
		}
	}
	meta := data["origin_metadata"].(map[string]any)
	if meta["keep"] != "v" {
		t.Errorf("非敏感键 keep 应保留, 实际 %v", data["keep"])
	}

	// updated_at 被刷新为 now（>= 保存前时刻）
	updatedAt, _ := data["updated_at"].(float64)
	if updatedAt < time.Now().Unix()-2 {
		t.Errorf("updated_at 应刷新为当前时间, 实际 %v", updatedAt)
	}

	// 15 字段全量断言
	if data["interaction_id"] != "gpq_sess-1_ou-zhangsan" || data["mode"] != "group" ||
		data["status"] != "pending" || data["question"] != "需要哪个楼层？" {
		t.Errorf("核心字段不匹配: %+v", data)
	}
	if data["origin_metadata"].(map[string]any)["keep"] != "v" {
		t.Errorf("keep 应保留")
	}

	// created_at 零值时补 now（dataclass default_factory 对齐）
	pi2 := newTestPI("gpq_zero_time")
	pi2.CreatedAt = 0
	pi2.UpdatedAt = 0
	pi2.Save()
	raw2, _ := os.ReadFile(filepath.Join(pathutil.AgentInteractionsDir(), "gpq_zero_time.json"))
	var data2 map[string]any
	_ = json.Unmarshal(raw2, &data2)
	createdAt, _ := data2["created_at"].(float64)
	if createdAt < time.Now().Unix()-2 {
		t.Errorf("created_at 零值应补当前时间, 实际 %v", createdAt)
	}
}

// TestSave_失败仅Warn 目录路径被同名文件占据 → MkdirAll 失败不 panic
func TestSave_失败仅Warn(t *testing.T) {
	tmp := setupInteractionsEnv(t)
	// 用同名文件占据 interactions 目录路径
	dir := filepath.Join(tmp, "agent", "workspace", "interactions")
	_ = os.MkdirAll(filepath.Dir(dir), 0o755)
	if err := os.WriteFile(dir, []byte("occupied"), 0o644); err != nil {
		t.Fatalf("预置失败: %v", err)
	}

	pi := newTestPI("gpq_any")
	pi.Save() // 不 panic 即通过
}

// TestRemove_存在与不存在 存在删除 + 不存在静默
func TestRemove_存在与不存在(t *testing.T) {
	setupInteractionsEnv(t)

	pi := newTestPI("gpq_rm_1")
	pi.Save()
	pi.Remove()

	fp := filepath.Join(pathutil.AgentInteractionsDir(), "gpq_rm_1.json")
	if _, err := os.Stat(fp); !os.IsNotExist(err) {
		t.Errorf("remove 后文件应不存在")
	}

	// 不存在时静默（不 panic 不报错）
	pi2 := newTestPI("gpq_rm_missing")
	pi2.Remove()
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && \
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; \
export GOPROXY=https://goproxy.cn,direct && \
go test -tags "sqlite_fts5 test" -run 'TestSave_|TestRemove_' ./internal/swarm/gateway/routing/ -v 2>&1 | head -30
```

Expected: FAIL —— `undefined: PendingInteraction`（编译错误即失败）

- [ ] **Step 3: 最小实现（结构体 + 常量 + Save/Remove + 辅助函数）**

创建 `internal/swarm/gateway/routing/interaction_context.go`（按项目规范：结构体→枚举→常量→全局变量→导出函数→非导出函数分区，注释全中文）：

```go
package routing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PendingInteraction 统一的追问上下文，支持群聊追问和 DM 追问两种模式。
//
// 存储路径: {workspace_dir}/agent/workspace/interactions/{interaction_id}.json
// 文件名前缀: gpq_* 群聊追问, iact_* DM 追问
// TTL: 24 小时
//
// Python: jiuwenswarm/gateway/routing/interaction_context.py PendingInteraction (L39-54)
type PendingInteraction struct {
	// InteractionID 交互标识（gpq_{session}_{user} 或 iact_{channel}_{hex}）
	InteractionID string `json:"interaction_id"`
	// Mode 模式："dm" 私聊追问 / "group" 群聊追问
	Mode string `json:"mode"`
	// OriginChannelID 发起渠道 ID
	OriginChannelID string `json:"origin_channel_id"`
	// OriginSessionID 发起会话 ID
	OriginSessionID string `json:"origin_session_id"`
	// OriginContent 原始请求内容
	OriginContent string `json:"origin_content"`
	// OriginSenderName 原始发送者名称
	OriginSenderName string `json:"origin_sender_name"`
	// OriginSenderID 原始发送者 ID
	OriginSenderID string `json:"origin_sender_id"`
	// Question 追问内容
	Question string `json:"question"`
	// TargetUserID 被追问用户 ID
	TargetUserID string `json:"target_user_id"`
	// TargetUserName 被追问用户名称
	TargetUserName string `json:"target_user_name"`
	// OriginMetadata 原始 metadata（落盘前过滤敏感键）
	OriginMetadata map[string]any `json:"origin_metadata"`
	// PrincipalID 数字分身主体用户 ID
	PrincipalID string `json:"principal_id"`
	// Status 状态："pending" / "completed"
	Status string `json:"status"`
	// CreatedAt 创建时间戳（秒，float 对齐 Python time.time()）
	CreatedAt float64 `json:"created_at"`
	// UpdatedAt 更新时间戳（秒，float 对齐 Python time.time()）
	UpdatedAt float64 `json:"updated_at"`
}

// pendingInteractionAlias UnmarshalJSON 辅助：指针字段检测键存在性
// （对齐 Python cls(**{k: data[k] for k in data if k in fields}) 的必填/可选区分）
type pendingInteractionAlias struct {
	InteractionID    *string         `json:"interaction_id"`
	Mode             *string         `json:"mode"`
	OriginChannelID  *string         `json:"origin_channel_id"`
	OriginSessionID  *string         `json:"origin_session_id"`
	OriginContent    *string         `json:"origin_content"`
	OriginSenderName *string         `json:"origin_sender_name"`
	OriginSenderID   *string         `json:"origin_sender_id"`
	Question         *string         `json:"question"`
	TargetUserID     *string         `json:"target_user_id"`
	TargetUserName   *string         `json:"target_user_name"`
	OriginMetadata   map[string]any  `json:"origin_metadata"`
	PrincipalID      *string         `json:"principal_id"`
	Status           *string         `json:"status"`
	CreatedAt        *float64        `json:"created_at"`
	UpdatedAt        *float64        `json:"updated_at"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ModeDM 私聊追问模式
	ModeDM = "dm"
	// ModeGroup 群聊追问模式
	ModeGroup = "group"
	// StatusPending 待处理状态
	StatusPending = "pending"
	// StatusCompleted 已完成状态
	StatusCompleted = "completed"
	// DefaultTTL 默认存活时长：24 小时（秒）
	DefaultTTL = 86400.0
)

// ──────────────────────────── 全局变量 ────────────────────────────

// sensitiveKeys 敏感键集合（小写比较剔除）
// Python: _SENSITIVE_KEYS = frozenset({"api_key", "token", "authorization"}) (L24)
var sensitiveKeys = map[string]bool{
	"api_key":       true,
	"token":         true,
	"authorization": true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Save 将交互上下文落盘（防御式，失败仅 Warn）。
//
// Python: save (L59-82)：updated_at 刷新为当前时间 → 过滤敏感键 →
// json.dumps(ensure_ascii=False, indent=2) 写文件；异常仅 warning。
// Go 对齐差异：CreatedAt 零值补当前时间（对齐 dataclass default_factory 语义）。
func (pi *PendingInteraction) Save() {
	// Python: self.updated_at = time.time()（无条件刷新）
	now := float64(time.Now().UnixMilli()) / 1000.0
	pi.UpdatedAt = now
	// dataclass default_factory=time.time 的构造期默认值对齐：
	// 调用方（11.12）逐字段构造 struct 时若未填时间字段，此处补当前时间
	if pi.CreatedAt == 0 {
		pi.CreatedAt = now
	}

	data := map[string]any{
		"interaction_id":    pi.InteractionID,
		"mode":              pi.Mode,
		"origin_channel_id": pi.OriginChannelID,
		"origin_session_id": pi.OriginSessionID,
		"origin_content":    pi.OriginContent,
		"origin_sender_name": pi.OriginSenderName,
		"origin_sender_id":  pi.OriginSenderID,
		"question":          pi.Question,
		"target_user_id":    pi.TargetUserID,
		"target_user_name":  pi.TargetUserName,
		"origin_metadata":   filterSensitive(pi.OriginMetadata),
		"principal_id":      pi.PrincipalID,
		"status":            pi.Status,
		"created_at":        pi.CreatedAt,
		"updated_at":        pi.UpdatedAt,
	}

	// Python: json.dumps(data, ensure_ascii=False, indent=2) — 中文不转义
	// Go: json.Marshal 默认不转义非 ASCII，但会转义 HTML 字符（<>&），
	// SetEscapeHTML(false) 对齐 ensure_ascii=False 的完整语义
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(data); err != nil {
		logger.Warn(logComponent).
			Str("interaction_id", pi.InteractionID).
			Err(err).
			Msg("PendingInteraction save failed")
		return
	}
	// Encode 自带尾换行；Python write_text 不带 → TrimSpace 对齐
	content := bytes.TrimRight(buf.Bytes(), "\n")
	// 缩进 2 空格（对齐 indent=2）：Encode 无缩进能力，用 MarshalIndent 重排
	indented := indentJSON(content)
	if err := os.WriteFile(pi.path(), indented, 0o644); err != nil {
		logger.Warn(logComponent).
			Str("interaction_id", pi.InteractionID).
			Err(err).
			Msg("PendingInteraction save failed")
		return
	}
	logger.Info(logComponent).
		Str("interaction_id", pi.InteractionID).
		Str("mode", pi.Mode).
		Msg("PendingInteraction save")
}

// Remove 删除交互文件（防御式，失败仅 Warn）。
//
// Python: remove (L84-91)：存在才删；异常仅 warning。
func (pi *PendingInteraction) Remove() {
	fp := pi.path()
	if _, err := os.Stat(fp); err != nil {
		if os.IsNotExist(err) {
			return // Python: p.exists() 为 False 直接跳过
		}
		logger.Warn(logComponent).
			Str("interaction_id", pi.InteractionID).
			Err(err).
			Msg("PendingInteraction remove failed")
		return
	}
	if err := os.Remove(fp); err != nil {
		logger.Warn(logComponent).
			Str("interaction_id", pi.InteractionID).
			Err(err).
			Msg("PendingInteraction remove failed")
		return
	}
	logger.Info(logComponent).
		Str("interaction_id", pi.InteractionID).
		Msg("PendingInteraction remove")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// path 交互文件完整路径：{interactionsDir}/{interaction_id}.json
// Python: _path (L56-57)
func (pi *PendingInteraction) path() string {
	return filepath.Join(interactionsDir(), pi.InteractionID+".json")
}

// interactionsDir 交互目录（每次动态解析 + mkdir，对齐 Python _get_interactions_dir L28-31）
func interactionsDir() string {
	dir := path.AgentInteractionsDir()
	_ = os.MkdirAll(dir, 0o755) // Python: mkdir(parents=True, exist_ok=True) 不抛错
	return dir
}

// filterSensitive 过滤敏感键（小写比较）
// Python: _filter_sensitive (L34-35)
func filterSensitive(metadata map[string]any) map[string]any {
	result := make(map[string]any, len(metadata))
	for k, v := range metadata {
		if sensitiveKeys[strings.ToLower(k)] {
			continue
		}
		result[k] = v
	}
	return result
}

// indentJSON 为紧凑 JSON 加 2 空格缩进（对齐 Python indent=2）
func indentJSON(compact []byte) []byte {
	var buf bytes.Buffer
	if err := json.Indent(&buf, compact, "", "  "); err != nil {
		return compact // 缩进失败退回紧凑形式（写入仍有效）
	}
	return buf.Bytes()
}
```

**⚠️ 注意**：Save 里组装 map 手动列 15 键是**对齐 Python 显式 dict**（Python save() 也是显式列键）。`enc.Encode` 输出带尾换行，`json.Indent` 处理后无换行——与 Python `json.dumps`（无尾换行）一致。

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && \
export GOPROXY=https://goproxy.cn,direct && \
go test -tags "sqlite_fts5 test" -run 'TestSave_|TestRemove_' ./internal/swarm/gateway/routing/ -v 2>&1 | tail -15
```

Expected: 3 个测试 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/gateway/routing/interaction_context.go internal/swarm/gateway/routing/interaction_context_test.go
git commit -m "feat(11.8): PendingInteraction 结构体 + Save/Remove（敏感键过滤 + dataclass 时间默认值对齐）"
```

---

### Task 2: Load + UnmarshalJSON（必填检测 + 三类默认值）

**Files:**
- Modify: `internal/swarm/gateway/routing/interaction_context.go`（非导出函数区追加）
- Modify: `internal/swarm/gateway/routing/interaction_context_test.go`

- [ ] **Step 1: 写失败测试（Load 五场景）**

在 `interaction_context_test.go` 追加：

```go
// TestLoad_完整往返 Save→Load 15 字段逐一断言
func TestLoad_完整往返(t *testing.T) {
	setupInteractionsEnv(t)

	pi := newTestPI("gpq_round_1")
	pi.Save()

	loaded := LoadPendingInteraction("gpq_round_1")
	if loaded == nil {
		t.Fatal("load 应返回非 nil")
	}
	if loaded.InteractionID != pi.InteractionID || loaded.Mode != pi.Mode ||
		loaded.OriginChannelID != pi.OriginChannelID || loaded.OriginSessionID != pi.OriginSessionID ||
		loaded.OriginContent != pi.OriginContent || loaded.OriginSenderName != pi.OriginSenderName ||
		loaded.OriginSenderID != pi.OriginSenderID || loaded.Question != pi.Question ||
		loaded.TargetUserID != pi.TargetUserID || loaded.TargetUserName != pi.TargetUserName ||
		loaded.PrincipalID != pi.PrincipalID || loaded.Status != pi.Status ||
		loaded.CreatedAt != pi.CreatedAt {
		t.Errorf("字段往返不匹配:\n got %+v\nwant %+v", loaded, pi)
	}
	if _, ok := loaded.OriginMetadata["keep"]; !ok {
		t.Errorf("origin_metadata.keep 应保留")
	}
	// updated_at 被 Save 刷新为 now，往返后 >= 原值
	if loaded.UpdatedAt < pi.CreatedAt {
		t.Errorf("UpdatedAt 应 >= 原时间戳")
	}
}

// TestLoad_文件不存在与损坏 nil 返回 + 坏 JSON → nil
func TestLoad_文件不存在与损坏(t *testing.T) {
	setupInteractionsEnv(t)

	if got := LoadPendingInteraction("gpq_not_exist"); got != nil {
		t.Errorf("文件不存在应返回 nil, 实际 %v", got)
	}

	// 坏 JSON
	dir := pathutil.AgentInteractionsDir()
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "gpq_bad.json"), []byte("{not json"), 0o644)
	if got := LoadPendingInteraction("gpq_bad"); got != nil {
		t.Errorf("坏 JSON 应返回 nil, 实际 %v", got)
	}
}

// TestLoad_未知键忽略与缺失时间补now 手写 JSON 含未知字段、缺 created_at → now 附近
func TestLoad_未知键忽略与缺失时间补now(t *testing.T) {
	setupInteractionsEnv(t)

	raw := `{
  "interaction_id": "iact_ch_1",
  "mode": "dm",
  "origin_channel_id": "feishu_enterprise:app1",
  "origin_session_id": "sess-1",
  "origin_content": "订会议室",
  "origin_sender_name": "张三",
  "origin_sender_id": "ou-zhangsan",
  "question": "确认吗？",
  "target_user_id": "ou-principal",
  "unknown_extra_key": "ignored"
}`
	dir := pathutil.AgentInteractionsDir()
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "iact_ch_1.json"), []byte(raw), 0o644)

	pi := LoadPendingInteraction("iact_ch_1")
	if pi == nil {
		t.Fatal("应加载成功")
	}
	// 未知键被忽略（无报错）
	// 缺失可选字段用默认值
	if pi.TargetUserName != "" || pi.PrincipalID != "" {
		t.Errorf("缺失可选字段应为零值")
	}
	if pi.Status != "pending" {
		t.Errorf("缺 status 应默认 pending（dataclass default），实际 %q", pi.Status)
	}
	if pi.OriginMetadata == nil || len(pi.OriginMetadata) != 0 {
		t.Errorf("缺 origin_metadata 应为空 map（dataclass default_factory=dict）")
	}
	// 缺失时间字段补当前时间（default_factory=time.time）
	if pi.CreatedAt < time.Now().Unix()-2 {
		t.Errorf("缺 created_at 应补当前时间, 实际 %v", pi.CreatedAt)
	}
	if pi.UpdatedAt < time.Now().Unix()-2 {
		t.Errorf("缺 updated_at 应补当前时间, 实际 %v", pi.UpdatedAt)
	}
}

// TestLoad_必填字段缺失跳过 缺 question 的 JSON → nil（Python TypeError→except 对齐）
func TestLoad_必填字段缺失跳过(t *testing.T) {
	setupInteractionsEnv(t)

	raw := `{"interaction_id": "gpq_miss_q", "mode": "group", "origin_channel_id": "ch",
"origin_session_id": "s", "origin_content": "c", "origin_sender_name": "n",
"origin_sender_id": "u"}`
	dir := pathutil.AgentInteractionsDir()
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "gpq_miss_q.json"), []byte(raw), 0o644)

	if got := LoadPendingInteraction("gpq_miss_q"); got != nil {
		t.Errorf("必填字段缺失应返回 nil, 实际 %+v", got)
	}
}

// TestUnmarshalJSON_null元数据 JSON null → nil map（Python None）
func TestUnmarshalJSON_null元数据(t *testing.T) {
	var pi PendingInteraction
	raw := `{"interaction_id":"a","mode":"dm","origin_channel_id":"c","origin_session_id":"s",
"origin_content":"c","origin_sender_name":"n","origin_sender_id":"u","question":"q",
"target_user_id":"t","origin_metadata":null}`
	if err := json.Unmarshal([]byte(raw), &pi); err != nil {
		t.Fatalf("unmarshal 失败: %v", err)
	}
	if pi.OriginMetadata != nil {
		t.Errorf("JSON null 应解为 nil map")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && \
export GOPROXY=https://goproxy.cn,direct && \
go test -tags "sqlite_fts5 test" -run 'TestLoad_|TestUnmarshalJSON_' ./internal/swarm/gateway/routing/ -v 2>&1 | head -20
```

Expected: FAIL —— `undefined: LoadPendingInteraction`

- [ ] **Step 3: 实现 Load + UnmarshalJSON**

在 `interaction_context.go` 导出函数区追加（插在 Remove 之后）：

```go
// LoadPendingInteraction 按 ID 加载交互上下文；文件不存在/解析失败/必填字段缺失返回 nil。
//
// Python: load (L94-103)：文件不存在 → None；cls(**fields) 重建（必填缺失
// TypeError → except → None）；解析失败 warning + None。
func LoadPendingInteraction(interactionID string) *PendingInteraction {
	fp := filepath.Join(interactionsDir(), interactionID+".json")
	raw, err := os.ReadFile(fp)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Warn(logComponent).
				Str("interaction_id", interactionID).
				Err(err).
				Msg("PendingInteraction load failed")
		}
		return nil
	}
	pi, err := unmarshalPendingInteraction(raw)
	if err != nil {
		logger.Warn(logComponent).
			Str("interaction_id", interactionID).
			Err(err).
			Msg("PendingInteraction load failed")
		return nil
	}
	return pi
}

// UnmarshalJSON 对齐 Python dataclass 重建语义（spec §6 决策2 三类默认值）：
//   - 必填字段缺失 → error（Python TypeError → 文件跳过）
//   - 可选字段缺失 → dataclass 默认值：status→"pending"、origin_metadata→空 map、
//     created_at/updated_at→当前时间（default_factory=time.time 构造期求值）
//   - 未知键 → 忽略（Python k in dataclass_fields；Go json 天然忽略）
func (pi *PendingInteraction) UnmarshalJSON(data []byte) error {
	var alias pendingInteractionAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	// Python dataclass 必填字段（无默认值）：缺失 → TypeError
	for name, v := range map[string]*string{
		"interaction_id":    alias.InteractionID,
		"mode":              alias.Mode,
		"origin_channel_id": alias.OriginChannelID,
		"origin_session_id": alias.OriginSessionID,
		"origin_content":    alias.OriginContent,
		"origin_sender_name": alias.OriginSenderName,
		"origin_sender_id":  alias.OriginSenderID,
		"question":          alias.Question,
		"target_user_id":    alias.TargetUserID,
	} {
		if v == nil {
			return fmt.Errorf("missing required field %q", name)
		}
	}

	now := float64(time.Now().UnixMilli()) / 1000.0
	pi.InteractionID = *alias.InteractionID
	pi.Mode = *alias.Mode
	pi.OriginChannelID = *alias.OriginChannelID
	pi.OriginSessionID = *alias.OriginSessionID
	pi.OriginContent = *alias.OriginContent
	pi.OriginSenderName = *alias.OriginSenderName
	pi.OriginSenderID = *alias.OriginSenderID
	pi.Question = *alias.Question
	pi.TargetUserID = *alias.TargetUserID
	if alias.TargetUserName != nil {
		pi.TargetUserName = *alias.TargetUserName
	}
	pi.OriginMetadata = alias.OriginMetadata // JSON null → nil（Python None）
	if alias.PrincipalID != nil {
		pi.PrincipalID = *alias.PrincipalID
	}
	// 可选默认：status → "pending"（dataclass default）
	pi.Status = StatusPending
	if alias.Status != nil {
		pi.Status = *alias.Status
	}
	// 可选默认：时间 → 当前时间（default_factory=time.time）
	pi.CreatedAt = now
	if alias.CreatedAt != nil {
		pi.CreatedAt = *alias.CreatedAt
	}
	pi.UpdatedAt = now
	if alias.UpdatedAt != nil {
		pi.UpdatedAt = *alias.UpdatedAt
	}
	return nil
}
```

非导出函数区追加：

```go
// unmarshalPendingInteraction 解析字节流为 PendingInteraction（UnmarshalJSON 封装）
func unmarshalPendingInteraction(raw []byte) (*PendingInteraction, error) {
	var pi PendingInteraction
	if err := json.Unmarshal(raw, &pi); err != nil {
		return nil, err
	}
	return &pi, nil
}
```

**⚠️ 注意**：`fmt` 已在 Task 1 import（UnmarshalJSON 的 `fmt.Errorf` 直接可用）。

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && \
export GOPROXY=https://goproxy.cn,direct && \
go test -tags "sqlite_fts5 test" -run 'TestLoad_|TestUnmarshalJSON_' ./internal/swarm/gateway/routing/ -v 2>&1 | tail -12
```

Expected: 5 个测试 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/gateway/routing/interaction_context.go internal/swarm/gateway/routing/interaction_context_test.go
git commit -m "feat(11.8): LoadPendingInteraction + UnmarshalJSON（必填检测 + status/时间/空map 三类 dataclass 默认值）"
```

---

### Task 3: parseInteractionFile + FindGroupPending + FindAllGroupPending

**Files:**
- Modify: `internal/swarm/gateway/routing/interaction_context.go`
- Modify: `internal/swarm/gateway/routing/interaction_context_test.go`

- [ ] **Step 1: 写失败测试**

在 `interaction_context_test.go` 追加：

```go
// TestFindGroupPending_直达加载 ID 拼接正确 + 不做 status/TTL 过滤（completed 也返回）
func TestFindGroupPending_直达加载(t *testing.T) {
	setupInteractionsEnv(t)

	// completed 状态的文件（find_group_pending 不过滤）
	pi := newTestPI("gpq_sess-9_ou-user9")
	pi.Status = StatusCompleted
	pi.CreatedAt = 1.0 // 远古时间（不过滤 TTL）
	pi.Save()

	got := FindGroupPending("sess-9", "ou-user9")
	if got == nil {
		t.Fatal("completed/远古时间也应返回（不过滤 status/TTL）")
	}
	if got.InteractionID != "gpq_sess-9_ou-user9" {
		t.Errorf("ID 拼接应 gpq_{session}_{user}")
	}

	// 不存在 → nil
	if got2 := FindGroupPending("sess-none", "ou-none"); got2 != nil {
		t.Errorf("不存在应返回 nil")
	}
}

// TestFindAllGroupPending pending+未过期收集、completed/过期排除
func TestFindAllGroupPending(t *testing.T) {
	setupInteractionsEnv(t)

	// 应收集
	p1 := newTestPI("gpq_s1_u1")
	p1.Save()
	// completed → 排除
	p2 := newTestPI("gpq_s2_u2")
	p2.Status = StatusCompleted
	p2.Save()
	// 过期 → 排除
	p3 := newTestPI("gpq_s3_u3")
	p3.CreatedAt = time.Now().Unix() - DefaultTTL - 100
	p3.Save()
	// iact 前缀 → 不在 gpq_ glob 内，排除
	p4 := newTestPI("iact_ch_abcd")
	p4.Save()
	// 坏 JSON → 跳过
	dir := pathutil.AgentInteractionsDir()
	_ = os.WriteFile(filepath.Join(dir, "gpq_bad.json"), []byte("{not json"), 0o644)

	all := FindAllGroupPending()
	if len(all) != 1 {
		t.Fatalf("应只收集 1 个 pending 未过期 gpq, 实际 %d: %+v", len(all), all)
	}
	if all[0].InteractionID != "gpq_s1_u1" {
		t.Errorf("应收集 gpq_s1_u1, 实际 %s", all[0].InteractionID)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && \
export GOPROXY=https://goproxy.cn,direct && \
go test -tags "sqlite_fts5 test" -run 'TestFindGroupPending_|TestFindAllGroupPending' ./internal/swarm/gateway/routing/ -v 2>&1 | head -15
```

Expected: FAIL —— `undefined: FindGroupPending`

- [ ] **Step 3: 实现三个函数**

导出函数区追加：

```go
// FindGroupPending 查群聊追问（直达加载 gpq_{session}_{user}，不过滤 status/TTL）。
//
// Python: find_group_pending (L149-151)
func FindGroupPending(sessionID, userID string) *PendingInteraction {
	return LoadPendingInteraction(fmt.Sprintf("gpq_%s_%s", sessionID, userID))
}

// FindAllGroupPending 收集全部 pending 且未过期的群聊追问。
//
// Python: find_all_group_pending (L133-146)：gpq_* glob → 重建 dataclass →
// status=="pending" 且未过期才收集；解析失败 debug 跳过。
// 注：Python 全仓零消费方，对齐原则照实实现（spec §1）。
func FindAllGroupPending() []*PendingInteraction {
	var result []*PendingInteraction
	dir := interactionsDir()
	fps, _ := filepath.Glob(filepath.Join(dir, "gpq_*.json"))
	for _, fp := range fps {
		pi := parseInteractionFile(fp)
		if pi == nil {
			continue
		}
		if pi.Status == StatusPending && time.Now().Unix()-pi.CreatedAt <= DefaultTTL {
			result = append(result, pi)
		}
	}
	return result
}
```

非导出函数区追加：

```go
// parseInteractionFile 读文件并解析（失败 Debug 日志 + nil）。
//
// Python: find_pending/find_all_group_pending 内层 try 块的共享语义
// （读文件 → json.loads → cls(**fields)；except → logger.debug + continue）。
// 注意：与 load() 的 warning 级别不同——Python 扫描场景用 debug。
func parseInteractionFile(fp string) *PendingInteraction {
	raw, err := os.ReadFile(fp)
	if err != nil {
		logger.Debug(logComponent).
			Str("file", fp).
			Err(err).
			Msg("PendingInteraction parse interaction file failed")
		return nil
	}
	pi, err := unmarshalPendingInteraction(raw)
	if err != nil {
		logger.Debug(logComponent).
			Str("file", fp).
			Err(err).
			Msg("PendingInteraction parse interaction file failed")
		return nil
	}
	return pi
}
```

**⚠️ 注意**：`time` 已在 Task 1 import。`time.Now().Unix()` 是 int，`pi.CreatedAt` 是 float64——表达式 `time.Now().Unix()-pi.CreatedAt` 中 int 自动转 float64，无需显式转换。

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && \
export GOPROXY=https://goproxy.cn,direct && \
go test -tags "sqlite_fts5 test" -run 'TestFindGroupPending_|TestFindAllGroupPending' ./internal/swarm/gateway/routing/ -v 2>&1 | tail -8
```

Expected: 2 个测试 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/gateway/routing/interaction_context.go internal/swarm/gateway/routing/interaction_context_test.go
git commit -m "feat(11.8): FindGroupPending 直达加载 + FindAllGroupPending 收集 + parseInteractionFile"
```

---

### Task 4: FindPending 五层过滤链

**Files:**
- Modify: `internal/swarm/gateway/routing/interaction_context.go`
- Modify: `internal/swarm/gateway/routing/interaction_context_test.go`

- [ ] **Step 1: 写失败测试（五层过滤 + 尾随 cleanup）**

在 `interaction_context_test.go` 追加：

```go
// TestFindPending_五层过滤顺序 ①status ②TTL触发删除 ③principal ④target ⑤取最旧
func TestFindPending_五层过滤顺序(t *testing.T) {
	setupInteractionsEnv(t)

	channelID := "feishu_enterprise:app1"
	principal := "ou-principal"

	// ① 非 pending → 跳过
	a := newTestPI("iact_" + channelID + "_aaaa")
	a.Status = StatusCompleted
	a.PrincipalID = principal
	a.Save()

	// ② TTL 过期 → 触发 Remove + 跳过
	b := newTestPI("iact_" + channelID + "_bbbb")
	b.PrincipalID = principal
	b.CreatedAt = time.Now().Unix() - DefaultTTL - 100
	b.Save()

	// ③ principal 不匹配 → 跳过
	c := newTestPI("iact_" + channelID + "_cccc")
	c.PrincipalID = "ou-other"
	c.Save()

	// ④ target 不匹配 → 跳过
	d := newTestPI("iact_" + channelID + "_dddd")
	d.PrincipalID = principal
	d.TargetUserID = "ou-somebody-else"
	d.Save()

	// ⑤ 候选 1（较新）
	e := newTestPI("iact_" + channelID + "_eeee")
	e.PrincipalID = principal
	e.TargetUserID = principal
	e.CreatedAt = time.Now().Unix() - 10
	e.Save()
	// ⑤ 候选 2（较旧 → 应命中）
	f := newTestPI("gpq_sess-1_ou-zhangsan")
	f.PrincipalID = principal
	f.TargetUserID = principal
	f.CreatedAt = time.Now().Unix() - 50
	f.Save()

	got := FindPendingInteraction(channelID, principal)
	if got == nil {
		t.Fatal("应命中候选 2（最旧）")
	}
	if got.InteractionID != "gpq_sess-1_ou-zhangsan" {
		t.Errorf("应取最旧 gpq_sess-1_ou-zhangsan, 实际 %s", got.InteractionID)
	}

	// ② 的过期文件已被顺带删除
	fpB := filepath.Join(pathutil.AgentInteractionsDir(), "iact_"+channelID+"_bbbb.json")
	if _, err := os.Stat(fpB); !os.IsNotExist(err) {
		t.Errorf("过期文件应被 Remove")
	}
}

// TestFindPending_全部不匹配 全过滤后返回 nil
func TestFindPending_全部不匹配(t *testing.T) {
	setupInteractionsEnv(t)

	// 只有 principal 不匹配的文件
	c := newTestPI("iact_feishu_enterprise:app1_cccc")
	c.PrincipalID = "ou-other"
	c.Save()

	if got := FindPendingInteraction("feishu_enterprise:app1", "ou-principal"); got != nil {
		t.Errorf("全部不匹配应返回 nil")
	}
}

// TestFindPending_尾随cleanup FindPending 尾部无条件 CleanupExpired
func TestFindPending_尾随cleanup(t *testing.T) {
	setupInteractionsEnv(t)

	dir := pathutil.AgentInteractionsDir()
	_ = os.MkdirAll(dir, 0o755)

	// 预置坏 JSON gpq 文件（两前缀扫描 parse 失败跳过，不影响主流程）
	_ = os.WriteFile(filepath.Join(dir, "gpq_badjson.json"), []byte("{not json"), 0o644)
	// 预置缺 created_at 的完整 iact 文件 → 尾部清理当 0 → 恒过期 → 删除
	// （主扫描：UnmarshalJSON 补 now → 不会在过滤链中被判过期；尾部清理走裸 dict → 删除）
	raw := `{"interaction_id":"iact_ch_zzzz","mode":"dm","origin_channel_id":"ch",
"origin_session_id":"s","origin_content":"c","origin_sender_name":"n",
"origin_sender_id":"u","question":"q","target_user_id":"ou-principal",
"origin_metadata":{},"principal_id":"ou-principal","status":"pending"}`
	_ = os.WriteFile(filepath.Join(dir, "iact_ch_zzzz.json"), []byte(raw), 0o644)

	_ = FindPendingInteraction("ch", "ou-principal")

	// 坏 JSON 文件保留（两处解析都跳过）
	if _, err := os.Stat(filepath.Join(dir, "gpq_badjson.json")); err != nil {
		t.Errorf("坏 JSON 文件不应被删除（解析失败跳过）")
	}
	// 缺 created_at 的文件被尾部清理删除（裸 dict 语义 created_at=0 → 恒过期）
	if _, err := os.Stat(filepath.Join(dir, "iact_ch_zzzz.json")); !os.IsNotExist(err) {
		t.Errorf("缺 created_at 文件应被尾部 CleanupExpired 删除（get 默认 0 → 过期）")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && \
export GOPROXY=https://goproxy.cn,direct && \
go test -tags "sqlite_fts5 test" -run 'TestFindPending_' ./internal/swarm/gateway/routing/ -v 2>&1 | head -15
```

Expected: FAIL —— `undefined: FindPendingInteraction`

- [ ] **Step 3: 实现 FindPendingInteraction + CleanupExpired（依赖）**

导出函数区追加：

```go
// FindPendingInteraction 扫描 iact_{channel}_* 与 gpq_* 两前缀，按五层过滤取最旧。
//
// Python: find_pending (L106-130)，过滤顺序不可调换（执行顺序纪律）：
//  1. 解析失败 → debug 跳过
//  2. status != pending → 跳过
//  3. 过期 → 顺带 Remove + 跳过
//  4. principal_id 非空且不匹配 → 跳过
//  5. target_user_id 非空且不匹配 → 跳过
//  6. 取 created_at 最旧
// 尾部无条件 cleanup_expired。
func FindPendingInteraction(channelID, principalID string) *PendingInteraction {
	dir := interactionsDir()
	var oldest *PendingInteraction
	now := float64(time.Now().Unix())
	for _, prefix := range []string{"iact_" + channelID + "_", "gpq_"} {
		fps, _ := filepath.Glob(filepath.Join(dir, prefix+"*.json"))
		for _, fp := range fps {
			pi := parseInteractionFile(fp)
			if pi == nil {
				continue // 解析失败 debug 已记
			}
			if pi.Status != StatusPending {
				continue
			}
			if now-pi.CreatedAt > DefaultTTL {
				pi.Remove() // Python L118: 过期顺带删除
				continue
			}
			if pi.PrincipalID != "" && pi.PrincipalID != principalID {
				continue
			}
			if pi.TargetUserID != "" && pi.TargetUserID != principalID {
				continue
			}
			if oldest == nil || pi.CreatedAt < oldest.CreatedAt {
				oldest = pi
			}
		}
	}
	CleanupExpired(DefaultTTL) // Python L129: 尾部无条件清理
	return oldest
}

// CleanupExpired 清理过期交互文件（宽松解析：字符串时间戳容忍），返回删除数。
//
// Python: cleanup_expired (L154-170)：*.json glob → 裸 dict 取 created_at：
//   - 缺失 → 0 → 恒过期 → 删除（data.get("created_at", 0)）
//   - 字符串数字 → float() 转换成功 → 用
//   - 字符串非数字 → ValueError → except → 跳过不删
//   - JSON 解析失败 → debug 跳过
func CleanupExpired(ttl float64) int {
	dir := interactionsDir()
	fps, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	count := 0
	now := float64(time.Now().Unix())
	for _, fp := range fps {
		raw, err := os.ReadFile(fp)
		if err != nil {
			logger.Debug(logComponent).Str("file", fp).Err(err).
				Msg("PendingInteraction cleanup interaction file failed")
			continue
		}
		var data map[string]any
		if err := json.Unmarshal(raw, &data); err != nil {
			logger.Debug(logComponent).Str("file", fp).Err(err).
				Msg("PendingInteraction cleanup interaction file failed")
			continue
		}
		created, ok := extractCreatedAt(data)
		if !ok {
			continue // Python: float("abc") ValueError → except 跳过
		}
		if now-created > ttl {
			if err := os.Remove(fp); err == nil {
				count++
			}
		}
	}
	if count > 0 {
		logger.Info(logComponent).
			Int("count", count).
			Msg("PendingInteraction cleanup expired")
	}
	return count
}
```

非导出函数区追加：

```go
// extractCreatedAt 从裸 dict 提取 created_at（对齐 Python float(data.get("created_at", 0))）：
// 数字 → 直接用；字符串数字 → ParseFloat；字符串非数字 → !ok（跳过）；
// 缺失 → 0（恒过期语义）；JSON null → 0。
func extractCreatedAt(data map[string]any) (float64, bool) {
	v, exists := data["created_at"]
	if !exists || v == nil {
		return 0, true // Python: get 默认 0
	}
	switch val := v.(type) {
	case float64:
		return val, true
	case string:
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return 0, false // Python: float("abc") → ValueError → except 跳过
		}
		return f, true
	default:
		// Python: float(其他类型如 list/dict) → TypeError → except 跳过
		return 0, false
	}
}
```

**⚠️ 注意**：Task 4 实现时在 import 块追加 `"strconv"`（Task 1 的 import 初始不含）。

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && \
export GOPROXY=https://goproxy.cn,direct && \
go test -tags "sqlite_fts5 test" -run 'TestFindPending_' ./internal/swarm/gateway/routing/ -v 2>&1 | tail -8
```

Expected: 3 个测试 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/gateway/routing/interaction_context.go internal/swarm/gateway/routing/interaction_context_test.go
git commit -m "feat(11.8): FindPendingInteraction 五层过滤链 + CleanupExpired 宽松时间解析"
```

---

### Task 5: CleanupExpired 独立用例 + BuildResumeContent

**Files:**
- Modify: `internal/swarm/gateway/routing/interaction_context.go`
- Modify: `internal/swarm/gateway/routing/interaction_context_test.go`

- [ ] **Step 1: 写失败测试**

在 `interaction_context_test.go` 追加：

```go
// TestCleanupExpired 过期删/未过期留/坏文件跳过不删/返回计数
func TestCleanupExpired(t *testing.T) {
	setupInteractionsEnv(t)
	dir := pathutil.AgentInteractionsDir()
	_ = os.MkdirAll(dir, 0o755)

	writeFile := func(name, content string) {
		_ = os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
	}
	now := time.Now().Unix()

	// 过期（数字时间戳）→ 删
	writeFile("gpq_old1.json", fmt.Sprintf(`{"created_at": %d}`, now-DefaultTTL-100))
	// 未过期 → 留
	writeFile("gpq_new1.json", fmt.Sprintf(`{"created_at": %d}`, now-10))
	// 坏 JSON → 跳过不删
	writeFile("gpq_bad.json", "{not json")
	// 字符串时间戳可解析且过期 → 删
	writeFile("gpq_str1.json", `{"created_at": "1"}`)
	// 字符串时间戳无法解析 → 跳过不删
	writeFile("gpq_str2.json", `{"created_at": "abc"}`)
	// 缺 created_at → 当 0 → 恒过期 → 删
	writeFile("gpq_missing.json", `{"interaction_id": "x"}`)
	// 非数字非字符串类型（list）→ 跳过不删
	writeFile("gpq_list.json", `{"created_at": [1, 2]}`)

	count := CleanupExpired(DefaultTTL)

	if count != 3 {
		t.Errorf("应删除 3 个（gpq_old1/gpq_str1/gpq_missing）, 实际 %d", count)
	}
	for _, gone := range []string{"gpq_old1.json", "gpq_str1.json", "gpq_missing.json"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Errorf("%s 应被删除", gone)
		}
	}
	for _, keep := range []string{"gpq_new1.json", "gpq_bad.json", "gpq_str2.json", "gpq_list.json"} {
		if _, err := os.Stat(filepath.Join(dir, keep)); err != nil {
			t.Errorf("%s 应保留: %v", keep, err)
		}
	}
}

// TestBuildResumeContent_DM模板 逐字节断言中文模板（提示词一比一纪律）
func TestBuildResumeContent_DM模板(t *testing.T) {
	pi := newTestPI("iact_ch_1")
	pi.Mode = "dm"
	pi.OriginContent = "帮我订明天的会议室"
	pi.OriginSenderName = "张三"
	pi.Question = "需要哪个楼层？"
	answer := "三楼"

	want := "[任务恢复] 之前的群聊请求：帮我订明天的会议室（来自 张三）\n" +
		"你之前的追问：需要哪个楼层？\n" +
		"principal 的回答：三楼\n" +
		"请综合「原始请求」和「principal 的回答」中的所有信息继续完成任务，" +
		"原始请求中已明确提供的信息直接使用即可，不要再次追问。" +
		"并将结果回复到群聊。"

	if got := pi.BuildResumeContent(answer); got != want {
		t.Errorf("DM 模板不匹配:\n got: %q\nwant: %q", got, want)
	}
}

// TestBuildResumeContent_群聊模板 target_user_name 回退 "用户" 分支 + 显式名分支
func TestBuildResumeContent_群聊模板(t *testing.T) {
	// 显式名分支
	pi := newTestPI("gpq_s_u")
	pi.Mode = "group"
	pi.OriginContent = "订会议室"
	pi.OriginSenderName = "张三"
	pi.Question = "几点开始？"
	pi.TargetUserName = "李四"
	answer := "14点"

	want := "[任务恢复] 你正在处理的群聊请求：订会议室（来自 张三）\n" +
		"你之前追问了 李四：几点开始？\n" +
		"李四 的回答：14点\n" +
		"请综合「原始请求」和「用户的回答」中的所有信息来完成任务。" +
		"原始请求中已明确提供的信息（如时间、地点等）直接使用即可，不要再次追问或要求补充。" +
		"现在请立即执行任务（如设置提醒、创建日程等），不要只是确认或记录信息，并将执行结果回复到群聊。"

	if got := pi.BuildResumeContent(answer); got != want {
		t.Errorf("群聊模板（显式名）不匹配:\n got: %q\nwant: %q", got, want)
	}

	// 回退 "用户" 分支（TargetUserName 为空）
	pi2 := newTestPI("gpq_s_u2")
	pi2.Mode = "group"
	pi2.OriginContent = "订会议室"
	pi2.OriginSenderName = "张三"
	pi2.Question = "几点开始？"
	pi2.TargetUserName = ""

	got2 := pi2.BuildResumeContent(answer)
	if !strings.Contains(got2, "你之前追问了 用户：几点开始？") {
		t.Errorf("空名应回退 '用户':\n%q", got2)
	}
	if !strings.Contains(got2, "用户 的回答：14点") {
		t.Errorf("空名应回退 '用户'（回答段）:\n%q", got2)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && \
export GOPROXY=https://goproxy.cn,direct && \
go test -tags "sqlite_fts5 test" -run 'TestCleanupExpired$|TestBuildResumeContent_' ./internal/swarm/gateway/routing/ -v 2>&1 | head -15
```

Expected: `TestCleanupExpired` PASS（Task 4 已实现）；`TestBuildResumeContent_*` FAIL —— `undefined: BuildResumeContent`

- [ ] **Step 3: 实现 BuildResumeContent**

导出函数区追加（模板逐字复制 Python L172-189，**禁止翻译改写**）：

```go
// BuildResumeContent 构建任务恢复提示词（dm/group 两套模板，逐字对齐 Python）。
//
// Python: build_resume_content (L172-189)。中文提示词一比一复制，禁止改写。
func (pi *PendingInteraction) BuildResumeContent(answer string) string {
	if pi.Mode == ModeDM {
		return "[任务恢复] 之前的群聊请求：" + pi.OriginContent + "（来自 " + pi.OriginSenderName + "）\n" +
			"你之前的追问：" + pi.Question + "\n" +
			"principal 的回答：" + answer + "\n" +
			"请综合「原始请求」和「principal 的回答」中的所有信息继续完成任务，" +
			"原始请求中已明确提供的信息直接使用即可，不要再次追问。" +
			"并将结果回复到群聊。"
	}
	targetName := pi.TargetUserName
	if targetName == "" {
		targetName = "用户"
	}
	return "[任务恢复] 你正在处理的群聊请求：" + pi.OriginContent + "（来自 " + pi.OriginSenderName + "）\n" +
		"你之前追问了 " + targetName + "：" + pi.Question + "\n" +
		targetName + " 的回答：" + answer + "\n" +
		"请综合「原始请求」和「用户的回答」中的所有信息来完成任务。" +
		"原始请求中已明确提供的信息（如时间、地点等）直接使用即可，不要再次追问或要求补充。" +
		"现在请立即执行任务（如设置提醒、创建日程等），不要只是确认或记录信息，并将执行结果回复到群聊。"
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && \
export GOPROXY=https://goproxy.cn,direct && \
go test -tags "sqlite_fts5 test" -run 'TestCleanupExpired$|TestBuildResumeContent_' ./internal/swarm/gateway/routing/ -v 2>&1 | tail -8
```

Expected: 3 个测试 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/gateway/routing/interaction_context.go internal/swarm/gateway/routing/interaction_context_test.go
git commit -m "feat(11.8): BuildResumeContent 两套恢复模板逐字对齐 + CleanupExpired 边界用例"
```

---

### Task 6: doc.go 回填 + IMPLEMENTATION_PLAN.md 状态 + 全量验证

**Files:**
- Modify: `internal/swarm/gateway/routing/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md:738`

- [ ] **Step 1: 更新 doc.go**

`internal/swarm/gateway/routing/doc.go` 两处修改：

包概述段（SessionMap 段落后）追加一句：

```go
// PendingInteraction 统一追问上下文（群聊/DM 两模式），JSON 文件持久化 + 24h TTL
// （Python interaction_context.py；消费方 im_pipeline 11.12 / feishu 11.16 / wecom 11.21 ⤵️）。
```

文件目录树追加一行（session_map.go 之后）：

```
//	├── session_map.go      # SessionMap 企业通道会话映射（11.7）
//	└── interaction_context.go # PendingInteraction 追问上下文（11.8）
```

注意树形最后一项用 `└──`，原 session_map.go 行的 `├──` 保持不变。

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md 11.8 行**

`IMPLEMENTATION_PLAN.md` L738：

```markdown
| 11.8 | ✅ | InteractionContext | 交互上下文：PendingInteraction 15字段+8方法（Save/Remove/Load/FindPending五层过滤/FindAllGroupPending/FindGroupPending/CleanupExpired宽松解析/BuildResumeContent双模板）+dataclass三类默认值对齐（必填缺失跳过/status默认pending/时间补now）+敏感键过滤；⤵️ 消费方 11.12（im_pipeline）/11.16 飞书/11.21 企微 DM resume | `jiuwenswarm/gateway/routing/interaction_context.py` |
```

- [ ] **Step 3: 全量验证（单进程互斥 + 规则 5 编译纪律）**

```bash
cd /home/opensource/uapclaw-gateway && \
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; \
export GOPROXY=https://goproxy.cn,direct && \
go build ./... && \
go vet ./internal/swarm/gateway/routing/ && \
go test -tags "sqlite_fts5 test" -cover ./internal/swarm/gateway/routing/ 2>&1 | tail -5
```

Expected: build 通过；vet 干净；routing 包测试全绿，覆盖率 ≥85%（interaction_context.go 单文件用 `go tool cover -func` 核对）

- [ ] **Step 4: 单文件覆盖率核对（≥85% 目标）**

```bash
cd /home/opensource/uapclaw-gateway && \
export GOPROXY=https://goproxy.cn,direct && \
go test -tags "sqlite_fts5 test" -coverprofile=/tmp/ic11_8.out ./internal/swarm/gateway/routing/ && \
go tool cover -func=/tmp/ic11_8.out | grep interaction_context
```

Expected: `interaction_context.go` 各函数覆盖率大部分 100%，文件总覆盖率 ≥85%。若未达标：补用例覆盖未达分支（通常是 MkdirAll 失败、Glob 错误等防御分支），补后重跑。

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/gateway/routing/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(11.8): routing doc.go 条目回填 + IMPLEMENTATION_PLAN 11.8 ✅（⤵️ 消费方 11.12/11.16/11.21）"
```

---

## 验收清单（对照 spec §9）

- [ ] `go build ./...` 通过，零新 import 环（interaction_context.go 只 import path/logger/标准库）
- [ ] `go vet ./internal/swarm/gateway/routing/` 干净
- [ ] routing 包测试全绿；interaction_context.go 覆盖率 ≥85%
- [ ] 8 方法 Python 行号锚点齐全（L59/L84/L94/L106/L133/L149/L154/L172）
- [ ] forward_loop.go L113 与 message_handler.go L105-107 的 TODO(#11.12) **未被触碰**
- [ ] IMPLEMENTATION_PLAN.md 11.8 行 ✅ + ⤵️ 标注
- [ ] 提示词模板逐字（测试逐字节断言把关）
