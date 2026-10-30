# 10.4.6 ACP CLI Smoke 入口（uapclaw acp-chat）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增 `uapclaw acp-chat <profile> [message]` 一次性冒烟命令，一比一复刻 `jiuwenswarm/acp/cli.py`（connect → chat 一次 → close），并关闭 `10.4.3` 的悬空回填点。

**Architecture:** cmd 层新增一个 cobra 子命令文件；profile 解析复用 `acpchat` 包（导出 `LoadProfile`/`ClientSpec`/`BuildClientSpec`）；轻量自举（logger.Setup + dotenv 非覆盖 + config.Load，不加载 extensions/crypto/workspace）；退出码用 typed error + `exitCodeFor` 落到 `os.Exit`；响应文本直出 stdout，诊断走 logger。

**Tech Stack:** Go 1.2x、cobra、zerolog（`internal/common/logger`）、`internal/swarm/acp`（AcpStdioClient）、`internal/swarm/agents/harness/common/tools/acp_chat`、`gopkg.in/yaml.v3`（经 config 包）。

**Spec:** `docs/superpowers/specs/2026-10-31-acp-cli-smoke-10.4.6-design.md`

---

## 通用约定（每个任务都适用）

```bash
export GOPROXY=https://goproxy.cn,direct
# 编译前确认单进程（规范 规则5）
pgrep -f 'go (build|test)' || true
```

测试统一带构建标签（与 CI 一致）：

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./cmd/uapclaw/ -run TestXxx -v
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/tools/acp_chat/ -run TestXxx -v
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/common/dotenv/ -run TestXxx -v
```

**提交纪律（重要）**：工作区存在他人在途改动（约 35 文件）。每次提交**只显式 `git add` 本任务涉及的文件**，禁止 `git add -A` / `git commit -a`。提交前 `git status -s` 逐文件核对。

---

### Task 1: 导出 acpchat 的 profile 解析（LoadProfile / ClientSpec / BuildClientSpec）

**Files:**
- Modify: `internal/swarm/agents/harness/common/tools/acp_chat/client_cache.go`
- Modify: `internal/swarm/agents/harness/common/tools/acp_chat/tool_test.go`
- Modify: `internal/swarm/agents/harness/common/tools/acp_chat/doc.go`

**行为零变更**，只改可见性与命名。

- [ ] **Step 1: 改类型声明为导出**

`client_cache.go` 中把：

```go
// clientSpec 子进程启动规格（Python: _get_or_create_client 内局部解析结果）。
type clientSpec struct {
	// command 启动命令（trim 后非空）
	command string
	// args 命令参数（元素经 str() 转换）
	args []string
	// env 子进程环境（nil = 继承）
	env map[string]any
	// cwd 子进程工作目录（空 = 继承）
	cwd string
}
```

改为：

```go
// ClientSpec 子进程启动规格（Python: _get_or_create_client 内局部解析结果）。
//
// 导出给 cmd 层 acp-chat 冒烟入口（10.4.6）复用严格解析逻辑。
type ClientSpec struct {
	// Command 启动命令（trim 后非空）
	Command string
	// Args 命令参数（元素经 str() 转换）
	Args []string
	// Env 子进程环境（nil = 继承）
	Env map[string]any
	// Cwd 子进程工作目录（空 = 继承）
	Cwd string
}
```

- [ ] **Step 2: 改两个函数为导出并更新字段名**

`loadProfile` → `LoadProfile`（函数体不变，仅签名首字母大写与注释补一句「导出给 10.4.6 冒烟入口复用」）。

`buildClientSpec` → `BuildClientSpec`，函数体里 4 处结构体字面量改为导出字段：

```go
	// env：非映射 → 报错（构造在 NewAcpStdioClient 内完成）
	if rawEnv, ok := spec["env"]; ok && rawEnv != nil {
		if _, isMap := rawEnv.(map[string]any); !isMap {
			return nil, fmt.Errorf("acp_agents.env must be a map of strings")
		}
		envMap, _ := rawEnv.(map[string]any)
		return &ClientSpec{Command: command, Args: args, Env: envMap, Cwd: cwd}, nil
	}
	return &ClientSpec{Command: command, Args: args, Env: nil, Cwd: cwd}, nil
```

签名改为 `func BuildClientSpec(spec map[string]any) (*ClientSpec, error)`。

- [ ] **Step 3: 更新 `getOrCreateClient` 内的调用点**

```go
	spec, err := LoadProfile(cfg, profile)
	if err != nil {
		return nil, err
	}
	...
	cs, err := BuildClientSpec(spec)
	if err != nil {
		return nil, err
	}

	client := acp.NewAcpStdioClient(cs.Command, cs.Args, cs.Cwd, cs.Env)
```

- [ ] **Step 4: 更新 `tool_test.go` 的调用点与字段名**

全文件替换（共 12 处 `buildClientSpec`、6 处 `loadProfile`，字段 `cs.args`/`cs.cwd`/`cs.env`）：

```bash
cd /home/opensource/uapclaw-gateway/internal/swarm/agents/harness/common/tools/acp_chat
sed -i 's/\bbuildClientSpec(/BuildClientSpec(/g; s/\bloadProfile(/LoadProfile(/g' tool_test.go
sed -i 's/cs\.args/cs.Args/g; s/cs\.cwd/cs.Cwd/g; s/cs\.env/cs.Env/g; s/cs\.command/cs.Command/g' tool_test.go
grep -n "buildClientSpec\|loadProfile\|cs\.\(args\|cwd\|env\|command\)" tool_test.go
```

期望：`grep` 无输出。

- [ ] **Step 5: 更新 `doc.go` 的包说明**

在包功能段末尾追加一句：

```go
// 导出面（供 cmd 层 acp-chat 冒烟入口 10.4.6 复用）：
// LoadProfile / ClientSpec / BuildClientSpec——与工具路径共用同一套严格校验，
// 故冒烟入口对 args/env 类型错误的容忍度低于 Python cli.py（有意偏差，见该节 spec §6）。
```

- [ ] **Step 6: 跑包内测试确认零回归**

```bash
cd /home/opensource/uapclaw-gateway
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/tools/acp_chat/ -cover
```

期望：`ok`，覆盖率 ≥ 90%（改名前为 92.9%）。

- [ ] **Step 7: Commit**

```bash
git add internal/swarm/agents/harness/common/tools/acp_chat/client_cache.go \
        internal/swarm/agents/harness/common/tools/acp_chat/tool_test.go \
        internal/swarm/agents/harness/common/tools/acp_chat/doc.go
git commit -m "refactor(acpchat): 导出 LoadProfile/ClientSpec/BuildClientSpec 供 10.4.6 复用"
```

---

### Task 2: 新增 `dotenv.LoadNonOverride`

**Files:**
- Modify: `internal/common/dotenv/dotenv.go`
- Test: `internal/common/dotenv/dotenv_test.go`

Python `cli.py` 用 `load_dotenv(..., override=False)`，现有 `dotenv.Load` 是 `override=True`。

- [ ] **Step 1: 写失败测试**

先确认 `dotenv_test.go` 顶部 import 含 `os` 与 `path/filepath`（缺则补）：

```go
import (
	"os"
	"path/filepath"
	"testing"
)
```

在 `dotenv_test.go` 追加：

```go
// TestLoadNonOverride_不覆盖已有变量 验证非覆盖语义（Python override=False）。
func TestLoadNonOverride_不覆盖已有变量(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("KEEP=from_file\nNEW=added\n"), 0o600); err != nil {
		t.Fatalf("写临时 .env 失败: %v", err)
	}

	t.Setenv("KEEP", "from_shell")
	os.Unsetenv("NEW")

	if err := LoadNonOverride(path); err != nil {
		t.Fatalf("LoadNonOverride 失败: %v", err)
	}

	if got := os.Getenv("KEEP"); got != "from_shell" {
		t.Errorf("KEEP 期望保留 shell 值 from_shell, 实际 %q", got)
	}
	if got := os.Getenv("NEW"); got != "added" {
		t.Errorf("NEW 期望 added, 实际 %q", got)
	}
}

// TestLoadNonOverride_空值也覆盖 验证 shell 侧为空字符串时按未设置处理。
func TestLoadNonOverride_空值也覆盖(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("EMPTY=from_file\n"), 0o600); err != nil {
		t.Fatalf("写临时 .env 失败: %v", err)
	}

	t.Setenv("EMPTY", "")

	if err := LoadNonOverride(path); err != nil {
		t.Fatalf("LoadNonOverride 失败: %v", err)
	}
	if got := os.Getenv("EMPTY"); got != "from_file" {
		t.Errorf("EMPTY 期望 from_file, 实际 %q", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/common/dotenv/ -run TestLoadNonOverride -v
```

期望：编译失败 `undefined: LoadNonOverride`。

- [ ] **Step 3: 实现**

在 `dotenv.go` 的导出函数区，`Load` 之后追加：

```go
// LoadNonOverride 加载 .env 文件但**不覆盖**已存在的环境变量。
//
// 与 Load 的区别仅在于写入前检查变量是否已存在（含空字符串视为未设置，
// 与 python-dotenv override=False 的 `if key not in os.environ` 存在性判定有细微差别：
// 这里把空字符串也补写，便于 shell 里 `export K=` 占位后仍由 .env 提供值）。
// Python: dotenv.load_dotenv(path, override=False)
func LoadNonOverride(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取 .env 文件失败: %w", err)
	}

	kvs := Parse(string(data))
	for k, v := range kvs {
		if _, exists := os.LookupEnv(k); exists && os.Getenv(k) != "" {
			continue
		}
		if err := os.Setenv(k, v); err != nil {
			logger.Error(logComponent).Str("key", k).Err(err).Msg("failed to set env var")
			return fmt.Errorf("设置环境变量 %s 失败: %w", k, err)
		}
	}

	return nil
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/common/dotenv/ -cover
```

期望：`ok`，覆盖率 ≥ 90%。

- [ ] **Step 5: Commit**

```bash
git add internal/common/dotenv/dotenv.go internal/common/dotenv/dotenv_test.go
git commit -m "feat(dotenv): 新增 LoadNonOverride 对齐 Python load_dotenv(override=False)"
```

---

### Task 3: 退出码基础设施（acpChatExitError + exitCodeFor + Execute 改造）

**Files:**
- Create: `cmd/uapclaw/acp_chat_cmd.go`（本任务只放结构体/常量/退出码函数）
- Test: `cmd/uapclaw/acp_chat_cmd_test.go`
- Modify: `cmd/uapclaw/main.go`

- [ ] **Step 1: 写失败测试**

新建 `cmd/uapclaw/acp_chat_cmd_test.go`：

```go
package main

// 10.4.6 ACP CLI 冒烟入口测试。

import (
	"errors"
	"fmt"
	"testing"
)

// TestExitCodeFor 退出码解析全分支（0/1/2/130/未知/包装）。
func TestExitCodeFor(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"配置错", &acpChatExitError{code: 2}, 2},
		{"会话错", &acpChatExitError{code: 1}, 1},
		{"中断", &acpChatExitError{code: 130}, 130},
		{"包装后仍可取码", fmt.Errorf("wrap: %w", &acpChatExitError{code: 2}), 2},
		{"普通错误回落 1", errors.New("boom"), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitCodeFor(tc.err); got != tc.want {
				t.Errorf("exitCodeFor = %d, want %d", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./cmd/uapclaw/ -run TestExitCodeFor -v
```

期望：编译失败 `undefined: acpChatExitError`。

- [ ] **Step 3: 创建 `acp_chat_cmd.go` 骨架**

```go
package main

// acp_chat_cmd.go 提供 uapclaw acp-chat 子命令（10.4.6）。
// Python: jiuwenswarm/acp/cli.py（90 行，一次性冒烟：connect → chat 一次 → close）。
// 定位：外部 ACP 子代理出口链的旁路冒烟入口——不进 Gateway/AgentServer/LLM/工具调度，
// 不产生 E2A 消息与会话历史（详见 docs/superpowers/specs/2026-10-31-acp-cli-smoke-10.4.6-design.md）。

import (
	"errors"
	"fmt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// acpChatExitError 带进程退出码的语义错误（Execute 消费，映射为 os.Exit 码）。
type acpChatExitError struct {
	// code 进程退出码
	code int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// exitCodeOK 成功
	exitCodeOK = 0
	// exitCodeSession 会话异常（connect/chat 失败）
	exitCodeSession = 1
	// exitCodeConfig 配置错误（profile 缺失/规格非法/参数个数错）
	exitCodeConfig = 2
	// exitCodeInterrupted 被信号中断（对齐 Python KeyboardInterrupt → 130）
	exitCodeInterrupted = 130
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// Error 实现 error 接口。
func (e *acpChatExitError) Error() string {
	return fmt.Sprintf("acp-chat exit code %d", e.code)
}

// exitCodeFor 从 error 解析进程退出码（nil/未知 → 0/1）。
func exitCodeFor(err error) int {
	if err == nil {
		return exitCodeOK
	}
	var exitErr *acpChatExitError
	if errors.As(err, &exitErr) && exitErr != nil {
		return exitErr.code
	}
	return exitCodeSession
}
```

- [ ] **Step 4: 改 `main.go` 的 Execute**

```go
// Execute 执行根命令
func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(exitCodeFor(err))
	}
}
```

- [ ] **Step 5: 跑测试确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./cmd/uapclaw/ -run TestExitCodeFor -v
```

期望：`PASS`。

- [ ] **Step 6: Commit**

```bash
git add cmd/uapclaw/acp_chat_cmd.go cmd/uapclaw/acp_chat_cmd_test.go cmd/uapclaw/main.go
git commit -m "feat(cli): acp-chat 退出码基础设施（typed error + exitCodeFor）"
```

---

### Task 4: 配置加载（轻量自举）

**Files:**
- Modify: `cmd/uapclaw/acp_chat_cmd.go`
- Test: `cmd/uapclaw/acp_chat_cmd_test.go`

- [ ] **Step 1: 写失败测试**

追加到 `acp_chat_cmd_test.go`（import 需补 `os`、`path/filepath`、`context`）：

```go
// TestLoadAcpChatConfigFrom 从显式路径加载 config.yaml（不碰默认工作区）。
func TestLoadAcpChatConfigFrom(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	content := "acp_agents:\n  codex:\n    command: codex\n    args:\n      - exec\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("写临时 config.yaml 失败: %v", err)
	}

	raw, err := loadAcpChatConfigFrom(cfgPath, "")
	if err != nil {
		t.Fatalf("loadAcpChatConfigFrom 失败: %v", err)
	}
	agents, ok := raw["acp_agents"].(map[string]any)
	if !ok {
		t.Fatalf("acp_agents 类型不符: %#v", raw["acp_agents"])
	}
	if _, ok := agents["codex"]; !ok {
		t.Errorf("未解析出 codex profile: %#v", agents)
	}
}

// TestLoadAcpChatConfigFrom_文件缺失不报错 缺文件 → 空 map（对齐 config.Load 语义）。
func TestLoadAcpChatConfigFrom_文件缺失不报错(t *testing.T) {
	raw, err := loadAcpChatConfigFrom(filepath.Join(t.TempDir(), "nope.yaml"), "")
	if err != nil {
		t.Fatalf("期望不报错, 实际 %v", err)
	}
	if len(raw) != 0 {
		t.Errorf("期望空配置, 实际 %#v", raw)
	}
}

// TestLoadAcpChatConfigFrom_加载dotenv envFile 指向真实文件时写入环境变量。
func TestLoadAcpChatConfigFrom_加载dotenv(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("ACP_CHAT_TEST_KEY=xyz\n"), 0o600); err != nil {
		t.Fatalf("写临时 .env 失败: %v", err)
	}
	os.Unsetenv("ACP_CHAT_TEST_KEY")
	t.Cleanup(func() { os.Unsetenv("ACP_CHAT_TEST_KEY") })

	if _, err := loadAcpChatConfigFrom(filepath.Join(dir, "nope.yaml"), envFile); err != nil {
		t.Fatalf("loadAcpChatConfigFrom 失败: %v", err)
	}
	if got := os.Getenv("ACP_CHAT_TEST_KEY"); got != "xyz" {
		t.Errorf("dotenv 未生效, ACP_CHAT_TEST_KEY = %q", got)
	}
}

// TestAcpChatSkipDotenv 只认 "1"（对齐 Python strip()=="1"）。
func TestAcpChatSkipDotenv(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"1", true},
		{" 1 ", true},
		{"0", false},
		{"", false},
		{"true", false},
	}
	for _, tc := range cases {
		getenv := func(string) string { return tc.val }
		if got := acpChatSkipDotenv(getenv); got != tc.want {
			t.Errorf("acpChatSkipDotenv(%q) = %v, want %v", tc.val, got, tc.want)
		}
	}
	if acpChatSkipDotenv(nil) {
		t.Error("getenv nil 应返回 false")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./cmd/uapclaw/ -run "TestLoadAcpChatConfigFrom|TestAcpChatSkipDotenv" -v
```

期望：编译失败 `undefined: loadAcpChatConfigFrom`。

- [ ] **Step 3: 实现**

`acp_chat_cmd.go` 的 import 补：

```go
import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/crypto"
	"github.com/uapclaw/uapclaw-go/internal/common/dotenv"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)
```

常量区追加：

```go
	// envSkipDotenv 跳过 .env 加载的开关（品牌映射自 Python JIUWENCLAW_SKIP_DOTENV）
	envSkipDotenv = "UAPCLAW_SKIP_DOTENV"
	// logComponentAcpChat 冒烟入口日志组件（工具层归属 AgentServer，对齐 acpchat 包）
	logComponentAcpChat = logger.ComponentAgentServer
```

非导出函数区追加：

```go
// acpChatSkipDotenv 是否跳过 .env 加载（对齐 Python：strip() 后等于 "1"）。
func acpChatSkipDotenv(getenv func(string) string) bool {
	if getenv == nil {
		return false
	}
	return strings.TrimSpace(getenv(envSkipDotenv)) == "1"
}

// loadAcpChatConfig 生产配置加载：logger.Setup → 可选 dotenv → config.Load。
// Python: main() 的 load_dotenv 段 + _get_spec 里的 get_config()。
func loadAcpChatConfig(_ context.Context) (map[string]any, error) {
	// Go 的 logger 未 Setup 时是 Nop（静默），冒烟工具必须初始化才有诊断输出
	if err := logger.Setup(logger.WithConfigFile()); err != nil {
		fmt.Fprintf(os.Stderr, "初始化日志系统失败: %v\n", err)
	}

	envFile := ""
	if !acpChatSkipDotenv(os.Getenv) {
		envFile = workspace.EnvFile()
	}
	return loadAcpChatConfigFrom("", envFile)
}

// loadAcpChatConfigFrom 从指定路径加载配置（测试注入点）。
// cfgPath 空 → 默认 config.yaml；envFile 空 → 不加载 .env。
// Python: get_config() = yaml.safe_load + resolve_env_vars + _normalize_config。
func loadAcpChatConfigFrom(cfgPath, envFile string) (map[string]any, error) {
	if envFile != "" {
		// 非覆盖加载：shell 已导出的变量优先（Python override=False）
		if err := dotenv.LoadNonOverride(envFile); err != nil {
			logger.Debug(logComponentAcpChat).Err(err).Str("env_file", envFile).
				Msg("[acp-chat] skipping dotenv load")
		}
	}

	cfg, err := config.New(cfgPath,
		config.WithNormalize(config.NormalizeConfig),
		config.WithDecrypt(crypto.NewDecryptFunc()))
	if err != nil {
		return nil, err
	}
	return cfg.Load()
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./cmd/uapclaw/ -run "TestLoadAcpChatConfigFrom|TestAcpChatSkipDotenv" -v
```

期望：全部 `PASS`。

- [ ] **Step 5: Commit**

```bash
git add cmd/uapclaw/acp_chat_cmd.go cmd/uapclaw/acp_chat_cmd_test.go
git commit -m "feat(cli): acp-chat 轻量自举（logger.Setup + dotenv 非覆盖 + config.Load）"
```

---

### Task 5: runAcpChat 主体 + 依赖注入

**Files:**
- Modify: `cmd/uapclaw/acp_chat_cmd.go`
- Test: `cmd/uapclaw/acp_chat_cmd_test.go`

- [ ] **Step 1: 写失败测试**

追加到 `acp_chat_cmd_test.go`（import 补 `bytes`、`io`、`time`、`acp` 包）：

```go
// fakeAcpChatDeps 构造注入依赖；profile 规格固定为 command=sh 的合法值。
func fakeAcpChatDeps(t *testing.T, raw map[string]any, connectErr, chatErr error, out string) (acpChatDeps, *bytes.Buffer) {
	t.Helper()
	buf := new(bytes.Buffer)
	return acpChatDeps{
		loadConfig: func(context.Context) (map[string]any, error) { return raw, nil },
		connect:    func(context.Context, *acp.AcpStdioClient) error { return connectErr },
		chat:       func(context.Context, *acp.AcpStdioClient, string) (string, error) { return out, chatErr },
		stdout:     buf,
		getenv:     func(string) string { return "" },
	}, buf
}

// validAcpAgentsRaw 合法 profile 原始配置。
func validAcpAgentsRaw() map[string]any {
	return map[string]any{
		"acp_agents": map[string]any{
			"codex": map[string]any{
				"command": "sh",
				"args":    []any{"-c", "cat"},
			},
		},
	}
}

// TestRunAcpChat_退出码矩阵 覆盖解析错/连接错/对话错/成功/空响应。
func TestRunAcpChat_退出码矩阵(t *testing.T) {
	ctx := context.Background()

	t.Run("配置加载失败", func(t *testing.T) {
		deps, _ := fakeAcpChatDeps(t, nil, nil, nil, "")
		deps.loadConfig = func(context.Context) (map[string]any, error) {
			return nil, errors.New("boom")
		}
		if got := runAcpChat(ctx, deps, "codex", "hi"); got != exitCodeConfig {
			t.Errorf("码 = %d, want %d", got, exitCodeConfig)
		}
	})

	t.Run("无acp_agents", func(t *testing.T) {
		deps, _ := fakeAcpChatDeps(t, map[string]any{}, nil, nil, "")
		if got := runAcpChat(ctx, deps, "codex", "hi"); got != exitCodeConfig {
			t.Errorf("码 = %d, want %d", got, exitCodeConfig)
		}
	})

	t.Run("未知profile", func(t *testing.T) {
		deps, _ := fakeAcpChatDeps(t, validAcpAgentsRaw(), nil, nil, "")
		if got := runAcpChat(ctx, deps, "nope", "hi"); got != exitCodeConfig {
			t.Errorf("码 = %d, want %d", got, exitCodeConfig)
		}
	})

	t.Run("command为空", func(t *testing.T) {
		raw := map[string]any{"acp_agents": map[string]any{"codex": map[string]any{"command": "  "}}}
		deps, _ := fakeAcpChatDeps(t, raw, nil, nil, "")
		if got := runAcpChat(ctx, deps, "codex", "hi"); got != exitCodeConfig {
			t.Errorf("码 = %d, want %d", got, exitCodeConfig)
		}
	})

	t.Run("args非列表_严格", func(t *testing.T) {
		raw := map[string]any{"acp_agents": map[string]any{
			"codex": map[string]any{"command": "sh", "args": "not-a-list"}}}
		deps, _ := fakeAcpChatDeps(t, raw, nil, nil, "")
		if got := runAcpChat(ctx, deps, "codex", "hi"); got != exitCodeConfig {
			t.Errorf("严格解析应返回 %d, 实际 %d", exitCodeConfig, got)
		}
	})

	t.Run("连接失败", func(t *testing.T) {
		deps, buf := fakeAcpChatDeps(t, validAcpAgentsRaw(), errors.New("connect boom"), nil, "")
		if got := runAcpChat(ctx, deps, "codex", "hi"); got != exitCodeSession {
			t.Errorf("码 = %d, want %d", got, exitCodeSession)
		}
		if buf.Len() != 0 {
			t.Errorf("失败不应写 stdout, 实际 %q", buf.String())
		}
	})

	t.Run("对话失败", func(t *testing.T) {
		deps, _ := fakeAcpChatDeps(t, validAcpAgentsRaw(), nil, errors.New("chat boom"), "")
		if got := runAcpChat(ctx, deps, "codex", "hi"); got != exitCodeSession {
			t.Errorf("码 = %d, want %d", got, exitCodeSession)
		}
	})

	t.Run("成功", func(t *testing.T) {
		deps, buf := fakeAcpChatDeps(t, validAcpAgentsRaw(), nil, nil, "hello there")
		if got := runAcpChat(ctx, deps, "codex", "hi"); got != exitCodeOK {
			t.Errorf("码 = %d, want %d", got, exitCodeOK)
		}
		if buf.String() != "hello there\n" {
			t.Errorf("stdout = %q", buf.String())
		}
	})

	t.Run("空响应占位", func(t *testing.T) {
		deps, buf := fakeAcpChatDeps(t, validAcpAgentsRaw(), nil, nil, "")
		if got := runAcpChat(ctx, deps, "codex", "hi"); got != exitCodeOK {
			t.Errorf("码 = %d, want %d", got, exitCodeOK)
		}
		if buf.String() != "(empty response)\n" {
			t.Errorf("stdout = %q", buf.String())
		}
	})

	t.Run("信号取消", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(context.Background())
		cancel()
		deps, _ := fakeAcpChatDeps(t, validAcpAgentsRaw(), nil, context.Canceled, "")
		if got := runAcpChat(cancelCtx, deps, "codex", "hi"); got != exitCodeInterrupted {
			t.Errorf("码 = %d, want %d", got, exitCodeInterrupted)
		}
	})
}

// TestAcpChatCloseTimeout 关闭超时解析（非法/缺省回落 30s，0 保留）。
func TestAcpChatCloseTimeout(t *testing.T) {
	cases := []struct {
		val  string
		want time.Duration
	}{
		{"", 30 * time.Second},
		{"5", 5 * time.Second},
		{"2.5", 2500 * time.Millisecond},
		{"abc", 30 * time.Second},
		{"-1", 0},
	}
	for _, tc := range cases {
		getenv := func(string) string { return tc.val }
		if got := acpChatCloseTimeout(getenv); got != tc.want {
			t.Errorf("acpChatCloseTimeout(%q) = %v, want %v", tc.val, got, tc.want)
		}
	}
	if got := acpChatCloseTimeout(nil); got != 30*time.Second {
		t.Errorf("getenv nil = %v, want 30s", got)
	}
}

// blockingCloser 关闭阻塞的桩，用于超时用例。
type blockingCloser struct{ release chan struct{} }

// Close 阻塞直到 release 被关闭。
func (b *blockingCloser) Close() error {
	<-b.release
	return nil
}

// errorCloser 关闭返回错误的桩。
type errorCloser struct{}

// Close 恒返回错误。
func (e *errorCloser) Close() error { return errors.New("close boom") }

// TestCloseAcpChatClientWithTimeout 超时与错误都不致命。
func TestCloseAcpChatClientWithTimeout(t *testing.T) {
	blocker := &blockingCloser{release: make(chan struct{})}
	start := time.Now()
	closeAcpChatClientWithTimeout(blocker, 20*time.Millisecond)
	close(blocker.release)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("超时未生效, 耗时 %v", elapsed)
	}

	// 错误路径不 panic、不阻塞
	closeAcpChatClientWithTimeout(&errorCloser{}, time.Second)
	// nil 安全
	closeAcpChatClientWithTimeout(nil, time.Second)
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./cmd/uapclaw/ -run "TestRunAcpChat|TestAcpChatCloseTimeout|TestCloseAcpChatClientWithTimeout" -v
```

期望：编译失败 `undefined: runAcpChat` / `undefined: acpChatDeps`。

- [ ] **Step 3: 实现**

`acp_chat_cmd.go` 结构体区追加：

```go
// acpChatDeps 冒烟入口依赖注入点（生产用 defaultAcpChatDeps，测试替换）。
type acpChatDeps struct {
	// loadConfig 读取 config base 映射
	loadConfig func(context.Context) (map[string]any, error)
	// connect 建立 ACP 会话
	connect func(context.Context, *acp.AcpStdioClient) error
	// chat 发送一轮消息并取回文本
	chat func(context.Context, *acp.AcpStdioClient, string) (string, error)
	// stdout 响应文本输出目标
	stdout io.Writer
	// getenv 环境变量读取
	getenv func(string) string
}
```

常量区追加：

```go
	// envCloseTimeout 关闭超时环境变量（对齐 Python ACP_CLI_CLOSE_TIMEOUT_S）
	envCloseTimeout = "ACP_CLI_CLOSE_TIMEOUT_S"
	// defaultCloseTimeoutS 关闭超时默认秒数（Python 默认 30）
	defaultCloseTimeoutS = 30
	// emptyResponse 空响应占位（对齐 Python cli.py 字面，勿用工具侧文案）
	emptyResponse = "(empty response)"
	// defaultAcpChatMessage 缺省消息（Python argparse default="Hello"）
	defaultAcpChatMessage = "Hello"
```

import 补 `"io"`、`"strconv"`、`"time"`、`"github.com/uapclaw/uapclaw-go/internal/swarm/acp"`、`acpchat "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools/acp_chat"`。

非导出函数区追加：

```go
// defaultAcpChatDeps 生产依赖。
func defaultAcpChatDeps() acpChatDeps {
	return acpChatDeps{
		loadConfig: loadAcpChatConfig,
		connect: func(ctx context.Context, c *acp.AcpStdioClient) error {
			return c.Connect(ctx)
		},
		chat: func(ctx context.Context, c *acp.AcpStdioClient, msg string) (string, error) {
			return c.Chat(ctx, msg)
		},
		stdout: os.Stdout,
		getenv: os.Getenv,
	}
}

// acpChatCloseTimeout 关闭超时（env ACP_CLI_CLOSE_TIMEOUT_S，默认 30s；非法值回落默认）。
// 偏差：Python float() 解析失败会抛 ValueError，这里回落默认值（防御性）。
func acpChatCloseTimeout(getenv func(string) string) time.Duration {
	if getenv != nil {
		if raw := strings.TrimSpace(getenv(envCloseTimeout)); raw != "" {
			if secs, err := strconv.ParseFloat(raw, 64); err == nil {
				if secs < 0 {
					secs = 0
				}
				return time.Duration(secs * float64(time.Second))
			}
		}
	}
	return defaultCloseTimeoutS * time.Second
}

// closeAcpChatClientWithTimeout 限时等待关闭（对齐 Python wait_for(shield(close()))）。
// 超时/错误仅 Warn，不影响退出码。
func closeAcpChatClientWithTimeout(closer interface{ Close() error }, timeout time.Duration) {
	if closer == nil {
		return
	}
	done := make(chan error, 1)
	go func() { done <- closer.Close() }()
	select {
	case err := <-done:
		if err != nil {
			logger.Warn(logComponentAcpChat).Err(err).Msg("[acp-chat] failed to close ACP client")
		}
	case <-time.After(timeout):
		logger.Warn(logComponentAcpChat).Str("timeout", timeout.String()).
			Msg("[acp-chat] close ACP client timed out")
	}
}

// acpChatFailureCode 失败分支退出码：信号取消 → 130，否则 1（并记 Error 日志）。
func acpChatFailureCode(ctx context.Context, stage string, err error) int {
	if ctx.Err() != nil {
		logger.Warn(logComponentAcpChat).Str("stage", stage).Err(err).
			Msg("[acp-chat] interrupted by signal")
		return exitCodeInterrupted
	}
	logger.Error(logComponentAcpChat).Str("stage", stage).Err(err).
		Msg("[acp-chat] ACP session failed")
	return exitCodeSession
}

// runAcpChat 冒烟主体（对齐 Python _run_once）：返回进程退出码，不上抛语义错误。
func runAcpChat(ctx context.Context, deps acpChatDeps, profile, message string) int {
	raw, err := deps.loadConfig(ctx)
	if err != nil {
		logger.Error(logComponentAcpChat).Err(err).Msg("[acp-chat] load config failed")
		return exitCodeConfig
	}

	spec, err := acpchat.LoadProfile(raw, profile)
	if err != nil {
		logger.Error(logComponentAcpChat).Str("profile", profile).Err(err).
			Msg("[acp-chat] profile lookup failed")
		return exitCodeConfig
	}

	cs, err := acpchat.BuildClientSpec(spec)
	if err != nil {
		logger.Error(logComponentAcpChat).Str("profile", profile).Err(err).
			Msg("[acp-chat] invalid profile spec")
		return exitCodeConfig
	}

	client := acp.NewAcpStdioClient(cs.Command, cs.Args, cs.Cwd, cs.Env)
	// 对齐 Python finally：无论如何都尝试关闭，且不改退出码
	defer closeAcpChatClientWithTimeout(client, acpChatCloseTimeout(deps.getenv))

	if err := deps.connect(ctx, client); err != nil {
		return acpChatFailureCode(ctx, "connect", err)
	}

	out, err := deps.chat(ctx, client, message)
	if err != nil {
		return acpChatFailureCode(ctx, "chat", err)
	}

	if out == "" {
		out = emptyResponse
	}
	fmt.Fprintln(deps.stdout, out)
	return exitCodeOK
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./cmd/uapclaw/ -run "TestRunAcpChat|TestAcpChatCloseTimeout|TestCloseAcpChatClientWithTimeout" -v
```

期望：全部 `PASS`（含 `args非列表_严格` 返回 2）。

- [ ] **Step 5: Commit**

```bash
git add cmd/uapclaw/acp_chat_cmd.go cmd/uapclaw/acp_chat_cmd_test.go
git commit -m "feat(cli): acp-chat 执行主体（严格解析 + 一次性 chat + 限时关闭 + 130 中断）"
```

---

### Task 6: 子命令接线与注册

**Files:**
- Modify: `cmd/uapclaw/acp_chat_cmd.go`
- Modify: `cmd/uapclaw/cmd.go`
- Modify: `cmd/uapclaw/doc.go`
- Modify: `cmd/uapclaw/main.go`（包注释）
- Modify: `cmd/uapclaw/main_test.go`（`TestHelpFlag` 子命令清单）

- [ ] **Step 1: 写失败测试**

追加到 `acp_chat_cmd_test.go`：

```go
// TestNewAcpChatCmd_参数校验 参数个数错 → 码 2 的 typed error，且不打印 usage。
func TestNewAcpChatCmd_参数校验(t *testing.T) {
	cmd := newAcpChatCmd()
	if cmd.Use != "acp-chat <profile> [message]" {
		t.Errorf("Use = %q", cmd.Use)
	}
	if !cmd.SilenceUsage || !cmd.SilenceErrors {
		t.Error("应设置 SilenceUsage/SilenceErrors")
	}

	for _, args := range [][]string{nil, {"a", "b", "c"}} {
		err := cmd.RunE(cmd, args)
		var exitErr *acpChatExitError
		if !errors.As(err, &exitErr) || exitErr.code != exitCodeConfig {
			t.Errorf("args=%v 期望码 %d, 实际 %v", args, exitCodeConfig, err)
		}
	}
}

// TestNewAcpChatCmd_已注册 根命令包含 acp-chat。
func TestNewAcpChatCmd_已注册(t *testing.T) {
	root := newRootCmd()
	found := false
	for _, c := range root.Commands() {
		if c.Name() == "acp-chat" {
			found = true
			break
		}
	}
	if !found {
		t.Error("根命令未注册 acp-chat")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./cmd/uapclaw/ -run "TestNewAcpChatCmd" -v
```

期望：编译失败 `undefined: newAcpChatCmd`。

- [ ] **Step 3: 实现子命令**

`acp_chat_cmd.go` 非导出函数区追加：

```go
// newAcpChatCmd 创建 acp-chat 子命令（一次性冒烟，对齐 Python console script jiuwenswarm-acp-chat）。
func newAcpChatCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "acp-chat <profile> [message]",
		Short: "外部 ACP 子代理冒烟入口（一次性 connect→chat→close）",
		Long: `对 config.yaml 的 acp_agents.<profile> 做一次冒烟：spawn 子进程 → ACP 握手 →
发送一条消息 → 打印回复 → 关闭。不进 Gateway/AgentServer，不产生会话历史。

profile 规格与 acp_chat 工具共用严格校验（args 必须是列表、env 必须是映射）。

退出码：0 成功 / 2 配置错 / 1 会话异常 / 130 中断。`,
		// 冒烟工具不该打 usage 与 Error: 前缀（对齐 Python argparse 的输出观感）
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			if len(args) == 0 || len(args) > 2 {
				fmt.Fprintln(os.Stderr, "usage: uapclaw acp-chat <profile> [message]")
				return &acpChatExitError{code: exitCodeConfig}
			}

			message := defaultAcpChatMessage
			if len(args) == 2 {
				message = args[1]
			}

			code := runAcpChat(ctx, defaultAcpChatDeps(), args[0], message)
			if code == exitCodeOK {
				return nil
			}
			return &acpChatExitError{code: code}
		},
	}
	return cmd
}
```

import 补 `"github.com/spf13/cobra"`、`"os/signal"`、`"syscall"`。

- [ ] **Step 4: 注册到根命令**

`cmd.go` 的 `rootCmd.AddCommand(...)` 里，`newAcpCmd(),` 之后追加 `newAcpChatCmd(),`：

```go
	rootCmd.AddCommand(
		newChatCmd(),
		newServeCmd(),
		newAppCmd(),
		newAgentServerCmd(),
		newGatewayCmd(),
		newWebCmd(),
		newInitCmd(),
		newAcpCmd(),
		newAcpChatCmd(),
		newSpawnChildCmd(),
	)
```

- [ ] **Step 5: 更新 `TestHelpFlag` 期望清单**

`main_test.go` 中：

```go
	expectedCommands := []string{"chat", "serve", "app", "agentserver", "gateway", "web", "init", "acp", "acp-chat"}
```

- [ ] **Step 6: 更新包文档**

`cmd/uapclaw/doc.go` 子命令清单在 `acp` 行后追加：

```go
//   - acp-chat: 外部 ACP 子代理冒烟入口（一次性 connect→chat→close，10.4.6）
```

文件目录段把 `acp_cmd.go` 行改为两行：

```go
//	├── acp_cmd.go       # ACP 子命令实现：runAcpCmd / resolveAcpGatewayURL / bootstrapInprocServers
//	├── acp_chat_cmd.go  # acp-chat 冒烟入口：runAcpChat / acpChatDeps / exitCodeFor（10.4.6）
```

`main.go` 包注释的子命令清单同样在 `- acp:` 行后追加：

```go
//   - acp-chat: 外部 ACP 子代理冒烟入口（一次性 connect→chat→close）
```

- [ ] **Step 7: 跑测试确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./cmd/uapclaw/ -run "TestNewAcpChatCmd|TestHelpFlag" -v
```

期望：`PASS`。

- [ ] **Step 8: Commit**

```bash
git add cmd/uapclaw/acp_chat_cmd.go cmd/uapclaw/acp_chat_cmd_test.go \
        cmd/uapclaw/cmd.go cmd/uapclaw/doc.go cmd/uapclaw/main.go cmd/uapclaw/main_test.go
git commit -m "feat(cli): 注册 uapclaw acp-chat 子命令并补文档"
```

---

### Task 7: 假对端 e2e（真 AcpStdioClient）

**Files:**
- Modify: `cmd/uapclaw/acp_chat_cmd_test.go`

复用 `internal/swarm/acp/testdata/acp_peer.sh`（`initialize`/`session/new`/`session/prompt` → `peer-reply`）。

- [ ] **Step 1: 写测试**

```go
// TestRunAcpChat_E2E假对端 真客户端 + testdata 假 ACP 对端（Windows 跳过，脚本依赖 sh）。
func TestRunAcpChat_E2E假对端(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("acp_peer.sh 依赖 sh")
	}

	// cmd/uapclaw → ../../internal/swarm/acp/testdata/acp_peer.sh
	peer := "../../internal/swarm/acp/testdata/acp_peer.sh"
	if _, err := os.Stat(peer); err != nil {
		t.Fatalf("假对端脚本缺失: %v", err)
	}

	raw := map[string]any{
		"acp_agents": map[string]any{
			"peer": map[string]any{
				"command": "sh",
				"args":    []any{"-c", "sh " + peer},
			},
		},
	}

	buf := new(bytes.Buffer)
	deps := defaultAcpChatDeps()
	deps.loadConfig = func(context.Context) (map[string]any, error) { return raw, nil }
	deps.stdout = buf
	deps.getenv = func(string) string { return "" }

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if code := runAcpChat(ctx, deps, "peer", "hi"); code != exitCodeOK {
		t.Fatalf("码 = %d, want %d", code, exitCodeOK)
	}
	if !strings.Contains(buf.String(), "peer-reply") {
		t.Errorf("stdout = %q, 期望含 peer-reply", buf.String())
	}
}
```

import 补 `"runtime"`、`"strings"`。跑完 Task 7 后，`acp_chat_cmd_test.go` 的 import 块应为（顺序按 gofmt 归并）：

```go
import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/swarm/acp"
)
```

- [ ] **Step 2: 跑测试**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./cmd/uapclaw/ -run "TestRunAcpChat_E2E假对端" -v
```

期望：`PASS`。若失败先看子进程 stderr（`acp_peer.sh` 需 `sh`/`sed` 可用）。

- [ ] **Step 3: Commit**

```bash
git add cmd/uapclaw/acp_chat_cmd_test.go
git commit -m "test(cli): acp-chat 假对端 e2e（真实 AcpStdioClient 走通 connect→chat→close）"
```

---

### Task 8: 回填文档（PLAN + spec 偏差追加）

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`
- Modify: `docs/superpowers/specs/2026-10-31-acp-cli-smoke-10.4.6-design.md`

- [ ] **Step 1: 改 10.4.6 行**

把：

```
| 10.4.6 | ☐ | ACP CLI Smoke 入口 | jiuwenswarm-acp-chat 对应（cli.py 90 行 REPL）；实现留后（D4 拍板，11.25 只占行——Go 侧 acp 子命令已落地，本行余 Python REPL 对等形态） | `jiuwenswarm/acp/cli.py` |
```

改为：

```
| 10.4.6 | ✅ | ACP CLI Smoke 入口 | ✅ `uapclaw acp-chat <profile> [message]` 已落地 `cmd/uapclaw/acp_chat_cmd.go`：一次性冒烟（connect→chat→close，message 缺省 Hello，**非 REPL**，已订正 11.25 D4 的误读）；轻量自举（logger.Setup+dotenv 非覆盖+config.Load，不加载 extensions/crypto/workspace）；profile 解析复用 acpchat 严格校验（args/env 类型错比 Python 更严，有意偏差）；typed error + exitCodeFor 落 0/1/2/130；响应文本直出 stdout；ACP_CLI_CLOSE_TIMEOUT_S=30 限时关闭；假对端 e2e 覆盖 | `jiuwenswarm/acp/cli.py` |
```

- [ ] **Step 2: 改 10.4.3 行（关闭悬空回填点）**

把该行描述里的：

```
剩 cli.py REPL 入口 ⤵️ 10.4.6
```

替换为：

```
cli.py 一次性冒烟入口已随 10.4.6 落地（`uapclaw acp-chat`，非 REPL）
```

- [ ] **Step 3: spec 偏差表补两条**

在 spec §6 表格末尾追加：

```markdown
| 关闭超时非法值 | `float()` 解析失败抛 ValueError | 回落默认 30s 并 Warn | 防御性偏差 |
| close 错误文案 | `Failed to close ACP client: …` | 英文 Msg（日志规范） | 有意偏差 |
```

- [ ] **Step 4: 校验 PLAN 无残留 ⤵️ 指向 10.4.6**

```bash
cd /home/opensource/uapclaw-gateway
grep -n "⤵️ 10.4.6\|REPL 入口" IMPLEMENTATION_PLAN.md
```

期望：无输出。

- [ ] **Step 5: Commit**

```bash
git add IMPLEMENTATION_PLAN.md docs/superpowers/specs/2026-10-31-acp-cli-smoke-10.4.6-design.md
git commit -m "docs(plan): 10.4.6 标记完成并回填 10.4.3 悬空点（订正 cli.py REPL 误读）"
```

---

### Task 9: 全量回归与覆盖率核对

**Files:**
- 无代码改动（只跑验证）

- [ ] **Step 1: 单进程确认**

```bash
pgrep -f 'go (build|test)' || echo "无残留 go 进程"
```

- [ ] **Step 2: 受影响包覆盖率**

```bash
cd /home/opensource/uapclaw-gateway
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -cover \
  ./cmd/uapclaw/ \
  ./internal/common/dotenv/ \
  ./internal/swarm/agents/harness/common/tools/acp_chat/
```

期望：三包 `ok`，`cmd/uapclaw` ≥ 85%、`dotenv` ≥ 90%、`acp_chat` ≥ 90%。

- [ ] **Step 3: 全量回归**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./... 2>&1 | tail -40
```

期望：无 `FAIL`。若出现与本改动无关的既有失败，记录包名并在提交信息中注明（不要顺手改）。

- [ ] **Step 4: staticcheck / vet**

```bash
go vet -tags "sqlite_fts5 test" ./cmd/uapclaw/ ./internal/common/dotenv/ ./internal/swarm/agents/harness/common/tools/acp_chat/
```

期望：无输出。

- [ ] **Step 5: 手工冒烟（可选，需真实 ACP CLI）**

```bash
go run ./cmd/uapclaw acp-chat <profile> "hello" ; echo "exit=$?"
go run ./cmd/uapclaw acp-chat 不存在的profile ; echo "exit=$?"   # 期望 exit=2
```

- [ ] **Step 6: 确认无遗漏改动**

```bash
git status -s
```

期望：只剩他人在途改动，本任务文件均已提交。

---

## 自查（Self-Review 结果）

**Spec 覆盖**：§1 定位/作用 → Task 6 的 Long 文案 + Task 8 文档；§2 五项拍板 → D1 范围（全计划）、D2 Task 1、D3 Task 4、D4 Task 3/5、D5 Task 5；§3 事实基线 → Task 5 严格解析与空响应文案、Task 2 dotenv、Task 3 退出码；§4 文件清单/执行流/注入 → Task 3-6；§5 退出码与输出 → Task 3/5；§6 偏差表 → Task 8 Step 3；§7 测试 13 例 → Task 3/4/5/6/7（`TestExitCodeFor`、`TestLoadAcpChatConfigFrom`、`TestAcpChatSkipDotenv`、`TestRunAcpChat_退出码矩阵`、`TestAcpChatCloseTimeout`、`TestCloseAcpChatClientWithTimeout`、`TestNewAcpChatCmd_*`、`TestRunAcpChat_E2E假对端`）；§8 回填 → Task 8；§9 风险 → 通用约定 + Task 9 Step 6。

**命名一致性**：`acpChatDeps`/`runAcpChat`/`loadAcpChatConfig`/`loadAcpChatConfigFrom`/`acpChatSkipDotenv`/`acpChatCloseTimeout`/`closeAcpChatClientWithTimeout`/`acpChatFailureCode`/`exitCodeFor`/`acpChatExitError.code`/`LoadProfile`/`BuildClientSpec`/`ClientSpec.{Command,Args,Env,Cwd}` 全程一致。

**已知实施注意**：`config.New("")` 在缺文件时返回空 map（Step 已被 `TestLoadAcpChatConfigFrom_文件缺失不报错` 锁定）；`logger.Setup` 有全局 once 语义，测试里多次调用安全。
