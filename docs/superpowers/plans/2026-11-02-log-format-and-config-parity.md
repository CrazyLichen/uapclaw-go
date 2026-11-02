# 日志格式改造与 config.yaml 对齐 Python 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把日志输出统一为 `[时间][级别][component][文件:行号] 描述 key=value`，并把 `resources/config.yaml` 从 31 行骨架补全为与 Go 实现真实匹配的完整模板。

**Architecture:** 日志侧复用 `zerolog.ConsoleWriter` 的定制能力，在 `sanitizerWriter` 内侧插入文本格式化层（脱敏仍在 JSON 上完成，规则零变化）；配置侧按「先写防漂移测试、再逐段补模板」的顺序推进，用测试锁住「模板里的键必须是代码真读的键」。

**Tech Stack:** Go 1.22、zerolog v1.35.1、yaml.v3、lumberjack v2

**设计依据：** `docs/superpowers/specs/2026-11-02-log-format-and-config-parity-design.md`

---

## 文件结构

| 文件 | 职责 |
|---|---|
| `internal/common/logger/format.go`（新建） | 纯渲染层：把 zerolog 的 JSON 事件渲染成目标文本行，不含任何 IO 策略 |
| `internal/common/logger/logger.go`（改） | 装配：调用方采集（`.Caller()`）、writer 链插入格式化层 |
| `internal/common/logger/format_test.go`（新建） | 渲染函数的表驱动测试 |
| `internal/common/logger/logger_test.go`（改） | 端到端形态、脱敏、单行、NO_COLOR、caller 准确性 |
| `internal/common/config/config_yaml_parity_test.go`（新建） | 防漂移：模板键存在性 + 值与代码默认值一致 + 可解析 |
| `resources/config.yaml`（重写） | 出厂配置模板 |

---

### Task 1: 渲染函数 `format.go`

**Files:**
- Create: `internal/common/logger/format_test.go`
- Create: `internal/common/logger/format.go`

- [ ] **Step 1: 写失败的测试**

创建 `internal/common/logger/format_test.go`：

```go
package logger

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestFormatTimestamp 覆盖时间戳渲染的输入形态与非法值兜底
func TestFormatTimestamp(t *testing.T) {
	want := "[" + time.UnixMilli(1762000000123).Local().Format(timestampLayout) + "]"
	cases := []struct {
		name  string
		input any
		want  string
	}{
		{name: "json.Number", input: json.Number("1762000000123"), want: want},
		{name: "float64", input: float64(1762000000123), want: want},
		{name: "int64", input: int64(1762000000123), want: want},
		{name: "零值毫秒", input: json.Number("0"), want: "[" + time.UnixMilli(0).Local().Format(timestampLayout) + "]"},
		{name: "负值毫秒", input: int64(-1000), want: "[" + time.UnixMilli(-1000).Local().Format(timestampLayout) + "]"},
		{name: "RFC3339字符串", input: "2026-11-02T10:31:20.123Z", want: "[" + time.Date(2026, 11, 2, 10, 31, 20, 123000000, time.UTC).Local().Format(timestampLayout) + "]"},
		{name: "非法字符串", input: "not-a-time", want: ""},
		{name: "不支持的类型", input: true, want: ""},
		{name: "nil", input: nil, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatTimestamp(tc.input); got != tc.want {
				t.Errorf("formatTimestamp(%v) = %q, 期望 %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestFormatLevel 覆盖级别大写化、未知级别与着色开关
func TestFormatLevel(t *testing.T) {
	cases := []struct {
		name      string
		input     any
		withColor bool
		want      string
	}{
		{name: "info不带色", input: "info", withColor: false, want: "[INFO]"},
		{name: "warn不带色", input: "warn", withColor: false, want: "[WARN]"},
		{name: "debug不带色", input: "debug", withColor: false, want: "[DEBUG]"},
		{name: "error不带色", input: "error", withColor: false, want: "[ERROR]"},
		{name: "fatal不带色", input: "fatal", withColor: false, want: "[FATAL]"},
		{name: "未知级别", input: "custom", withColor: false, want: "[CUSTOM]"},
		{name: "info带色", input: "info", withColor: true, want: "\x1b[32m[INFO]\x1b[0m"},
		{name: "未知级别带色不着色", input: "custom", withColor: true, want: "[CUSTOM]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatLevel(tc.input, tc.withColor); got != tc.want {
				t.Errorf("formatLevel(%v,%v) = %q, 期望 %q", tc.input, tc.withColor, got, tc.want)
			}
		})
	}
}

// TestFormatCaller 覆盖绝对路径裁剪与降级
func TestFormatCaller(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  string
	}{
		{name: "绝对路径", input: "/home/u/uap-claw-go/internal/common/config/loader.go:88", want: "[config/loader.go:88]"},
		{name: "末两段", input: "internal/common/logger/logger.go:12", want: "[logger/logger.go:12]"},
		{name: "单段路径", input: "main.go:7", want: "[main.go:7]"},
		{name: "无行号", input: "/a/b/pkg/file.go", want: "[pkg/file.go]"},
		{name: "Windows分隔符", input: `C:\repo\internal\gateway\app.go:55`, want: "[gateway/app.go:55]"},
		{name: "空串", input: "", want: ""},
		{name: "非字符串", input: 42, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatCaller(tc.input); got != tc.want {
				t.Errorf("formatCaller(%v) = %q, 期望 %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestFormatMessage 覆盖控制字符转义，保证一行一条
func TestFormatMessage(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  string
	}{
		{name: "普通消息", input: "hello world", want: "hello world"},
		{name: "换行转义", input: "a\nb", want: `a\nb`},
		{name: "回车转义", input: "a\rb", want: `a\rb`},
		{name: "制表转义", input: "a\tb", want: `a\tb`},
		{name: "多控制符混合", input: "a\nb\tc\r", want: `a\nb\tc\r`},
		{name: "非字符串", input: 7, want: "7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatMessage(tc.input); got != tc.want {
				t.Errorf("formatMessage(%v) = %q, 期望 %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestFormatPartValue 覆盖 component 栏渲染
func TestFormatPartValue(t *testing.T) {
	if got := formatPartValue("common", "component"); got != "[common]" {
		t.Errorf("字符串值渲染错误：%q", got)
	}
	if got := formatPartValue(nil, "component"); got != "" {
		t.Errorf("nil 应渲染为空串，实际 %q", got)
	}
	if got := formatPartValue(3, "component"); got != "[3]" {
		t.Errorf("非字符串值渲染错误：%q", got)
	}
}

// TestNewTextWriter_定制项 验证 PartsOrder 与字段排除装配正确
func TestNewTextWriter_定制项(t *testing.T) {
	w := newTextWriter(&strings.Builder{}, true)
	wantParts := []string{"time", "level", "component", "caller", "message"}
	if fmt.Sprint(w.PartsOrder) != fmt.Sprint(wantParts) {
		t.Errorf("PartsOrder = %v, 期望 %v", w.PartsOrder, wantParts)
	}
	if len(w.FieldsExclude) != 1 || w.FieldsExclude[0] != "component" {
		t.Errorf("FieldsExclude = %v, 期望 [component]", w.FieldsExclude)
	}
	if !w.NoColor {
		t.Error("noColor=true 时应禁用着色")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/common/logger -run 'TestFormat|TestNewTextWriter' -v`
Expected: 编译失败，`undefined: formatTimestamp`（函数尚不存在）

- [ ] **Step 3: 写最小实现**

创建 `internal/common/logger/format.go`（注意本项目要求：注释中文、导出函数在前非导出在后、分区注释）：

```go
package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────
const (
	// timestampLayout 日志时间戳布局（本地时区，毫秒精度）
	timestampLayout = "2006-01-02 15:04:05.000"
	// ansiReset ANSI 颜色重置序列
	ansiReset = "\x1b[0m"
)

// ──────────────────────────── 全局变量 ────────────────────────────
var (
	// levelColors 级别→ANSI 前景色；未列出的级别不着色
	levelColors = map[string]string{
		"DEBUG": "\x1b[36m",
		"INFO":  "\x1b[32m",
		"WARN":  "\x1b[33m",
		"ERROR": "\x1b[31m",
		"FATAL": "\x1b[35m",
	}
	// messageEscaper 消息正文控制字符转义器，保证一行一条
	messageEscaper = strings.NewReplacer("\n", `\n`, "\r", `\r`, "\t", `\t`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTextWriter 构建文本格式化 writer。
//
// noColor 为 true 时不着色（写文件时使用）；NO_COLOR 环境变量非空时强制不着色。
// 输出形态：[时间] [级别] [component] [文件:行号] 描述 key=value
func newTextWriter(out io.Writer, noColor bool) zerolog.ConsoleWriter {
	useColor := !noColor && os.Getenv("NO_COLOR") == ""
	return zerolog.ConsoleWriter{
		Out:     out,
		NoColor: !useColor,
		PartsOrder: []string{
			zerolog.TimestampFieldName,
			zerolog.LevelFieldName,
			"component",
			zerolog.CallerFieldName,
			zerolog.MessageFieldName,
		},
		// component 已出现在 PartsOrder 中，必须排除在尾部字段之外，否则重复输出 component=xxx
		FieldsExclude:         []string{"component"},
		FormatTimestamp:       formatTimestamp,
		FormatLevel:           func(i any) string { return formatLevel(i, useColor) },
		FormatPartValueByName: formatPartValue,
		FormatCaller:          formatCaller,
		FormatMessage:         formatMessage,
	}
}

// formatTimestamp 把 Unix 毫秒（或 RFC3339 字符串）渲染为 [YYYY-MM-DD HH:MM:SS.mmm]。
// 无法解析时返回空串，ConsoleWriter 会跳过该栏而不是输出半截括号。
func formatTimestamp(v any) string {
	var ts time.Time
	switch val := v.(type) {
	case json.Number:
		ms, err := val.Int64()
		if err != nil {
			return ""
		}
		ts = time.UnixMilli(ms)
	case float64:
		ts = time.UnixMilli(int64(val))
	case int64:
		ts = time.UnixMilli(val)
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, val)
		if err != nil {
			return ""
		}
		ts = parsed
	default:
		return ""
	}
	return "[" + ts.Local().Format(timestampLayout) + "]"
}

// formatLevel 把级别渲染为大写方括号形式，withColor 为 true 时按级别着色。
func formatLevel(v any, withColor bool) string {
	name := strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", v)))
	if name == "" {
		name = "UNKNOWN"
	}
	text := "[" + name + "]"
	if !withColor {
		return text
	}
	code, ok := levelColors[name]
	if !ok {
		return text
	}
	return code + text + ansiReset
}

// formatPartValue 渲染 PartsOrder 中的非内建部件（当前仅 component）。
func formatPartValue(v any, _ string) string {
	if v == nil {
		return ""
	}
	return "[" + fmt.Sprintf("%v", v) + "]"
}

// formatCaller 把调用方路径裁剪为末两段：[config/loader.go:88]。
// 路径段不足两段时保留原值；空值返回空串。
func formatCaller(v any) string {
	s, ok := v.(string)
	if !ok || s == "" {
		return ""
	}
	// 统一分隔符，兼容 Windows 构建
	normalized := strings.ReplaceAll(s, `\`, "/")
	file, line := normalized, ""
	if idx := strings.LastIndex(normalized, ":"); idx > 0 {
		file, line = normalized[:idx], normalized[idx:]
	}
	if parts := strings.Split(file, "/"); len(parts) >= 2 {
		file = parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return "[" + file + line + "]"
}

// formatMessage 渲染消息正文，转义控制字符保证一行一条。
func formatMessage(v any) string {
	return messageEscaper.Replace(fmt.Sprintf("%v", v))
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/common/logger -run 'TestFormat|TestNewTextWriter' -v`
Expected: 全部 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/common/logger/format.go internal/common/logger/format_test.go
git commit -m "feat(logger): 新增文本格式化层（时间/级别/component/caller/消息转义）"
```

---

### Task 2: 接线到 `logger.go` 并断言整行形态

**Files:**
- Modify: `internal/common/logger/logger.go:171-179`（控制台 writer）、`internal/common/logger/logger.go:435-461`（文件与组装）
- Modify: `internal/common/logger/logger_test.go`（新增端到端用例）

- [ ] **Step 1: 写失败的测试**

在 `internal/common/logger/logger_test.go` 末尾追加：

```go
// TestLogLine_整行形态 断言日志行符合 [时间] [INFO] [component] [file:line] msg k=v
func TestLogLine_整行形态(t *testing.T) {
	dir := t.TempDir()
	resetGlobal()
	if err := Setup(WithOutputDir(dir)); err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}
	defer Close()

	_, _, callLine, _ := runtime.Caller(0)
	Info(ComponentCommon).Str("key", "val").Msg("format probe")

	data, err := os.ReadFile(filepath.Join(dir, "common.log"))
	if err != nil {
		t.Fatalf("读取 common.log 失败: %v", err)
	}
	// 只挑出本次探针那一行，避免 Setup 期间其他日志干扰
	var line string
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.Contains(l, "format probe") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("未找到探针日志行，文件内容: %q", string(data))
	}

	re := regexp.MustCompile(`^\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3}\] \[INFO\] \[common\] \[logger/logger_test\.go:(\d+)\] format probe key=val$`)
	m := re.FindStringSubmatch(line)
	if m == nil {
		t.Fatalf("日志行不符合目标形态: %q", line)
	}
	// caller 行号必须落在本函数的 Msg 调用附近（不写死行号，避免改测试即翻红）
	gotLine, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("行号解析失败: %v", err)
	}
	// Info(...).Msg(...) 比 runtime.Caller(0) 晚 1~10 行
	if gotLine < callLine+1 || gotLine > callLine+10 {
		t.Errorf("caller 行号 = %d，期望落在 (%d, %d] 区间", gotLine, callLine+1, callLine+10)
	}
}
```

并在该测试文件 import 块补 `"regexp"`、`"runtime"`、`"strconv"`（若已存在则不重复）。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/common/logger -run TestLogLine_整行形态 -v`
Expected: FAIL，形如 `日志行不符合目标形态: "{\"level\":\"info\",...}"`（当前仍是 JSON）

- [ ] **Step 3: 改 `logger.go` 三处**

**(a) 控制台 writer**（`logger.go` 约 171-174 行）把

```go
		l.consoleWriter = zerolog.ConsoleWriter{Out: os.Stderr}
		sanitizedConsoleWriter := NewSanitizerWriter(&l.consoleWriter, l.sanitizer)
```

改为

```go
		l.consoleWriter = newTextWriter(os.Stderr, false)
		sanitizedConsoleWriter := NewSanitizerWriter(&l.consoleWriter, l.sanitizer)
```

**(b) full.log writer**（约 165-169 行）把

```go
		l.fullRotationWriter = NewRotatingWriter(
			filepath.Join(l.outputDir, "full.log"), l.rotationCfg)
		fullWriter := NewMutexWriter(l.fullRotationWriter)
		sanitizedFullWriter := NewSanitizerWriter(fullWriter, l.sanitizer)
```

改为

```go
		l.fullRotationWriter = NewRotatingWriter(
			filepath.Join(l.outputDir, "full.log"), l.rotationCfg)
		fullWriter := NewMutexWriter(l.fullRotationWriter)
		// 脱敏在 JSON 上完成，其后渲染为文本行
		sanitizedFullWriter := NewSanitizerWriter(newTextWriter(fullWriter, true), l.sanitizer)
```

**(c) 组件文件 writer + 调用方采集**（`createComponentLogger` 内）

把

```go
	compWriter := NewMutexWriter(compRotationWriter)
	sanitizedCompWriter := NewSanitizerWriter(compWriter, l.sanitizer)
```

改为

```go
	compWriter := NewMutexWriter(compRotationWriter)
	sanitizedCompWriter := NewSanitizerWriter(newTextWriter(compWriter, true), l.sanitizer)
```

把

```go
	zl := zerolog.New(multiWriter).
		Level(level.ToZerologLevel()).
		With().
		Timestamp().
		Str("component", comp.String()).
		Logger()
```

改为

```go
	// Caller() 采集业务调用点：logger.Info(comp) 返回 event 后立即返回，
	// Msg() 仍在业务帧执行，因此 zerolog 默认跳帧数即可定位到调用点
	zl := zerolog.New(multiWriter).
		Level(level.ToZerologLevel()).
		With().
		Timestamp().
		Caller().
		Str("component", comp.String()).
		Logger()
```

以及 `getOrCreateAgentServerWriter` 内：

```go
	agentServerWriter := NewMutexWriter(agentServerRotationWriter)
	return NewSanitizerWriter(agentServerWriter, l.sanitizer)
```

改为

```go
	agentServerWriter := NewMutexWriter(agentServerRotationWriter)
	return NewSanitizerWriter(newTextWriter(agentServerWriter, true), l.sanitizer)
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/common/logger -v`
Expected: 全部 PASS（含既有用例）

- [ ] **Step 5: 提交**

```bash
git add internal/common/logger/logger.go internal/common/logger/logger_test.go
git commit -m "feat(logger): 控制台与日志文件统一文本格式并采集调用点"
```

---

### Task 3: 脱敏、单行、NO_COLOR 三项回归

**Files:**
- Modify: `internal/common/logger/logger_test.go`

- [ ] **Step 1: 写三个测试**

追加到 `logger_test.go`：

```go
// TestLogLine_文本形态仍脱敏 验证换成文本格式后敏感字段仍被掩码
func TestLogLine_文本形态仍脱敏(t *testing.T) {
	dir := t.TempDir()
	resetGlobal()
	if err := Setup(WithOutputDir(dir)); err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}
	defer Close()

	Info(ComponentCommon).Str("password", "secret123").Msg("auth probe")

	data, _ := os.ReadFile(filepath.Join(dir, "common.log"))
	content := string(data)
	if strings.Contains(content, "secret123") {
		t.Errorf("敏感值未脱敏: %q", content)
	}
	if !strings.Contains(content, "******") {
		t.Errorf("未出现掩码: %q", content)
	}
}

// TestLogLine_换行被转义 验证消息里的换行不破坏「一行一条」
func TestLogLine_换行被转义(t *testing.T) {
	dir := t.TempDir()
	resetGlobal()
	if err := Setup(WithOutputDir(dir)); err != nil {
		t.Fatalf("Setup 失败: %v", err)
	}
	defer Close()

	Info(ComponentCommon).Msg("line1\nline2")

	data, _ := os.ReadFile(filepath.Join(dir, "common.log"))
	// 逐行扫描：包含 line1 的行必须恰好一条，且内容里的换行是字面量
	var hits int
	var hitLine string
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.Contains(l, "line1") {
			hits++
			hitLine = l
		}
	}
	if hits != 1 {
		t.Fatalf("期望恰好 1 行包含 line1，实际 %d 行: %q", hits, string(data))
	}
	if !strings.Contains(hitLine, `line1\nline2`) {
		t.Errorf("换行未转义为字面量: %q", hitLine)
	}
}

// TestConsole_NoColor 验证 NO_COLOR 环境变量关闭控制台着色
func TestConsole_NoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建管道失败: %v", err)
	}
	os.Stderr = w

	dir := t.TempDir()
	resetGlobal()
	if err := Setup(WithOutputDir(dir)); err != nil {
		os.Stderr = origStderr
		t.Fatalf("Setup 失败: %v", err)
	}
	Info(ComponentCommon).Msg("color probe")
	Close()

	_ = w.Close()
	os.Stderr = origStderr

	out, _ := io.ReadAll(r)
	if strings.Contains(string(out), "\x1b[") {
		t.Errorf("NO_COLOR 下仍有着色序列: %q", string(out))
	}
	if !strings.Contains(string(out), "[INFO]") {
		t.Errorf("控制台未输出级别栏: %q", string(out))
	}
}
```

import 块补 `"io"`（若已存在则不重复）。

- [ ] **Step 2: 运行测试**

Run: `go test ./internal/common/logger -run 'TestLogLine_文本形态仍脱敏|TestLogLine_换行被转义|TestConsole_NoColor' -v`
Expected: 全 PASS。若 `TestConsole_NoColor` 失败，检查 `newTextWriter` 是否在 Setup 时读 `NO_COLOR`（`t.Setenv` 必须在 `Setup` 之前调用）。

- [ ] **Step 3: 提交**

```bash
git add internal/common/logger/logger_test.go
git commit -m "test(logger): 补文本形态下的脱敏、单行与 NO_COLOR 回归"
```

---

### Task 4: 删除 `logging.format` 死键

**Files:**
- Modify: `internal/common/config/sections.go:40-41`
- Modify: `internal/common/config/testdata/config.yaml:12`、`internal/common/config/testdata/template.yaml:12`
- Modify: `internal/common/config/sections_test.go:87-88`、`:122`、`:128`、`:151-152`
- Modify: `internal/common/logger/logger_test.go:551`

- [ ] **Step 1: 删字段**

`sections.go` 删除两行：

```go
	// Format 输出格式：json / text
	Format string `yaml:"format"`
```

- [ ] **Step 2: 删测试数据与断言**

`testdata/config.yaml` 与 `testdata/template.yaml` 各删一行 `  format: "json"`。

`sections_test.go` 三处改动：
- `if logging.Format != "json" { t.Errorf(...) }` 整块删除
- `"logging": map[string]any{"level": "info", "format": "json"},` → `"logging": map[string]any{"level": "info"},`
- `Format: "text",` 删除；对应的 `if logging.Format != "text" {...}` 整块删除

`logger_test.go` 第 551 行附近 yaml 字面量里的 `  format: "json"` 删除。

- [ ] **Step 3: 编译并跑测试**

Run: `go test ./internal/common/config ./internal/common/logger -v`
Expected: 全 PASS；若出现 `unknown field "format"` 说明还有遗漏引用

- [ ] **Step 4: 全仓确认无残留**

Run: `grep -rn "logging.Format\|format: \"json\"" --include=*.go --include=*.yaml .`
Expected: 无输出（`resources/config.yaml` 尚在第 7~10 步重写，此时允许仍有该行）

- [ ] **Step 5: 提交**

```bash
git add internal/common/config/sections.go internal/common/config/sections_test.go internal/common/config/testdata internal/common/logger/logger_test.go
git commit -m "refactor(config): 删除 logging.format 死键（Python 模板本无此键）"
```

---

### Task 5: 更新 `doc.go` 文件目录

**Files:**
- Modify: `internal/common/logger/doc.go`

- [ ] **Step 1: 补条目**

在 `doc.go` 的文件目录树中，`├── logger.go` 之后插入一行：

```
//	├── format.go        # 文本格式化层（时间/级别/caller/消息转义 → [时间][级别][component][文件:行号] 描述 k=v）
```

- [ ] **Step 2: 验证包文档可读**

Run: `go doc ./internal/common/logger`
Expected: 输出包注释，文件目录含 `format.go` 条目

- [ ] **Step 3: 提交**

```bash
git add internal/common/logger/doc.go
git commit -m "docs(logger): doc.go 补 format.go 条目"
```

---

### Task 6: 防漂移测试（先红）

**Files:**
- Create: `internal/common/config/config_yaml_parity_test.go`

**说明：** 本任务先写好测试并让它失败，随后 Task 7~10 逐段补模板直到转绿。测试里的键清单就是**模板必须包含的键**，也是「键必须是代码真读的键」的执行依据。

- [ ] **Step 1: 写测试**

创建 `internal/common/config/config_yaml_parity_test.go`：

```go
package config

import (
	"path/filepath"
	"reflect"
	"testing"
)

// resourcesConfigPath 出厂配置模板相对本包测试文件的路径
func resourcesConfigPath(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "..", "resources", "config.yaml")
}

// requiredKeyPaths 出厂模板必须包含的键路径（Go 生产代码真实读取的键）
var requiredKeyPaths = []string{
	"preferred_language",
	"logging.level",
	"logging.console_level",
	"logging.common",
	"logging.gateway",
	"logging.channel",
	"logging.agent_server",
	"logging.permissions",
	"logging.agent_core",
	"logging.full",
	"memory.mode",
	"memory.engine",
	"memory.external.provider",
	"memory.external.user_id",
	"memory.external.scope_id",
	"memory.external.mem0.user_id",
	"memory.external.mem0.agent_id",
	"models.defaults",
	"embed.embed_api_key",
	"embed.embed_base_url",
	"embed.embed_model",
	"react.agent_name",
	"react.max_iterations",
	"react.enable_task_loop",
	"react.enable_task_planning",
	"react.skill_mode",
	"react.subagents.general_agent.enabled",
	"react.subagents.code_agent.enabled",
	"react.subagents.research_agent.enabled",
	"gateway.host",
	"gateway.port",
	"gateway.session_map_scope",
	"heartbeat.every",
	"heartbeat.target",
	"heartbeat.active_hours.start",
	"heartbeat.active_hours.end",
	"channels.web.send_file_allowed",
	"permissions.enabled",
	"permissions.schema",
	"permissions.permission_mode",
	"team.team_name",
	"team.lifecycle",
	"team.teammate_mode",
	"team.spawn_mode",
	"team.transport.type",
	"team.storage.type",
	"modes.agent.fast.memory.enabled",
	"modes.agent.fast.memory.is_proactive",
	"modes.agent.plan.memory.enabled",
	"modes.agent.plan.memory.is_proactive",
	"modes.team.memory.enabled",
	"mcp.servers",
	"hooks.disable_all_hooks",
	"extensions.extension_dirs",
	"browser.chrome_path",
	"acp_agents",
	"agents",
}

// TestResourcesConfigYAML_键路径齐全 断言模板包含全部被代码读取的键
func TestResourcesConfigYAML_键路径齐全(t *testing.T) {
	cfg, err := New(resourcesConfigPath(t))
	if err != nil {
		t.Fatalf("加载出厂模板失败: %v", err)
	}
	for _, key := range requiredKeyPaths {
		if cfg.Get(key) == nil {
			t.Errorf("出厂模板缺少键：%s", key)
		}
	}
}

// TestResourcesConfigYAML_关键值 断言模板值与已裁决的出厂值一致
func TestResourcesConfigYAML_关键值(t *testing.T) {
	cfg, err := New(resourcesConfigPath(t))
	if err != nil {
		t.Fatalf("加载出厂模板失败: %v", err)
	}
	cases := []struct {
		key  string
		want any
	}{
		{"heartbeat.every", 3600},
		{"react.max_iterations", 100},
		{"react.enable_task_loop", true},
		{"react.subagents.general_agent.enabled", true},
		{"modes.agent.fast.memory.enabled", true},
		{"modes.agent.fast.memory.is_proactive", false},
		{"modes.agent.plan.memory.enabled", true},
		{"modes.agent.plan.memory.is_proactive", true},
		{"modes.team.memory.enabled", false},
		{"channels.web.send_file_allowed", true},
		{"team.team_name", "uapclaw_team"},
		{"memory.external.mem0.user_id", "uapclawswarm-user"},
		{"memory.external.mem0.agent_id", "uapclawswarm"},
		{"memory.mode", "local"},
		{"memory.engine", "builtin"},
	}
	for _, tc := range cases {
		got := cfg.Get(tc.key)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s = %v (%T)，期望 %v (%T)", tc.key, got, got, tc.want, tc.want)
		}
	}
}

// TestResourcesConfigYAML_不含死键 断言已删除的死段不会回流
func TestResourcesConfigYAML_不含死键(t *testing.T) {
	cfg, err := New(resourcesConfigPath(t))
	if err != nil {
		t.Fatalf("加载出厂模板失败: %v", err)
	}
	for _, dead := range []string{"server", "workspace", "logging.format", "telemetry"} {
		if cfg.Get(dead) != nil {
			t.Errorf("出厂模板不应包含死段/未实现段：%s", dead)
		}
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/common/config -run TestResourcesConfigYAML -v`
Expected: FAIL，大量 `出厂模板缺少键` 报告（当前模板只有 3 段）

- [ ] **Step 3: 提交（红态测试先入库）**

```bash
git add internal/common/config/config_yaml_parity_test.go
git commit -m "test(config): 新增出厂模板防漂移测试（键路径/关键值/死键）"
```

---

### Task 7: 模板第一段 —— 头部、logging、gateway、heartbeat

**Files:**
- Modify: `resources/config.yaml`（整体重写开始）

- [ ] **Step 1: 整体重写为下列内容**

```yaml
# UapClaw 出厂配置模板
#
# 约定：
#   1. 每个键的注释标出「环境变量覆盖名 / 代码默认值 / 默认值来源」，
#      便于确认该键是否真的生效、默认行为是什么。
#   2. ${VAR:-default} 形式由代码在读取时展开，保留占位不要替换成示例值。
#   3. 未在本文件出现的段意味着当前版本未实现（见文件末尾说明）。

# 前端显示语言：zh（中文）/ en（英文）。代码默认：zh（team/config_loader.go:93-96）
preferred_language: zh

# 日志配置
logging:
  # 基础级别：debug/info/warn/error/fatal。环境变量 LOG_LEVEL 仅覆盖控制台级别
  # 代码默认：info（logger/levels.go:133-137）
  level: info
  # 控制台级别。代码默认：跟随 level（logger/levels.go:151-153）
  console_level: info
  # 各通道独立级别（未配置时跟随 level）
  # common.log：基础设施层（config/workspace/utils 等）。代码默认：跟随 level
  common: info
  # gateway.log。代码默认：跟随 level
  gateway: info
  # channel.log。代码默认：跟随 level
  channel: info
  # agent_server.log（permissions 也会写入此文件）。代码默认：跟随 level
  agent_server: info
  # permissions.log。代码默认：跟随 level
  permissions: info
  # agent_core.log。代码默认：跟随 level
  agent_core: info
  # full.log 汇总文件。代码默认：跟随 level
  full: info

# Gateway 配置（Gateway 与 AgentServer 共用）
gateway:
  # 监听地址。环境变量 UAPCLAW_GATEWAY_HOST 优先。
  # 代码默认：127.0.0.1（swarm/gateway/app_gateway.go:773-788）
  host: 127.0.0.1
  # 监听端口。环境变量 UAPCLAW_GATEWAY_PORT 优先。
  # 代码默认：19000（swarm/gateway/app_gateway.go:790-812）
  port: 19000
  # SessionMap 作用域：per_chat_bot（同 chat+bot 共享会话）/ per_chat_bot_user（按用户分会话）
  # 代码默认：per_chat_bot（swarm/gateway/routing/session_map.go:74-80）
  session_map_scope: per_chat_bot

# 心跳配置
heartbeat:
  # 心跳间隔（秒）。环境变量：无。代码默认：60（swarm/gateway/app_gateway.go:216-222）
  every: 3600
  # 心跳结果回传通道（如 web）。代码默认：web（swarm/gateway/app_gateway.go:245-255）
  target: web
  # 心跳生效时段（本地时间），start <= now < end 时生效；留空表示全天生效
  # 代码默认：不限制（swarm/gateway/app_gateway.go:206-212）
  active_hours:
    start: "08:00"
    end: "22:00"
```

**注意：** `gateway.host/port` 必须留在顶层 `gateway:` 段（代码读取点是 `cfg.Get("gateway.host")`）；不要写到 `server.gateway` 下。

- [ ] **Step 2: 跑对应断言**

Run: `go test ./internal/common/config -run TestResourcesConfigYAML -v`
Expected: 仍有失败（memory/models/react 等段尚未写入），但 `logging.*`、`gateway.*`、`heartbeat.*`、`preferred_language` 相关缺键报错应消失

- [ ] **Step 3: 提交**

```bash
git add resources/config.yaml
git commit -m "feat(config): 出厂模板补全头部、logging、gateway、heartbeat 段"
```

---

### Task 8: 模板第二段 —— memory、embed、models

**Files:**
- Modify: `resources/config.yaml`（在 `heartbeat` 段后追加）

**取值来源：** 结构参照 Python 模板 `jiuwenswarm/resources/config.yaml:16-114`（memory）、`:350-353`（embed）、`:116-169`（models）；键集合以 Go 读取点为准（`agents/harness/common/memory/config.go:141-158`、`external_memory_config.go:70-93`、`common/config/default_models.go:35-193`）。

- [ ] **Step 1: 追加下列段**

```yaml
# 记忆系统配置
#
# 两个正交开关：
#   engine — 挂载哪套 rail：builtin（内置）/ external（外接）/ both / none
#   mode   — 内置记忆存储位置：local | cloud，不影响外接
# 内置 rail 生效条件：engine ∈ {builtin,both} AND mode=local AND modes.agent.<mode>.memory.enabled=true
# 外接 rail 生效条件：engine ∈ {external,both} AND external.provider 非空
memory:
  # 内置记忆存储位置。环境变量 MEMORY_MODE。代码默认：local（memory/config.go:313-324）
  mode: local
  # 记忆引擎总开关。环境变量 MEMORY_ENGINE。代码默认：builtin（memory/config.go:60-84）
  engine: builtin
  # 禁止记忆的内容定义；enabled=true 时按 patterns 过滤
  # 代码默认：disabled（memory/forbidden.go:96-110）
  forbidden_memory_definition:
    enabled: false
    patterns: []
  # 外接记忆
  external:
    # 选哪家 provider，留空等于禁用外接。环境变量 MEMORY_EXTERNAL_PROVIDER
    provider: ""
    # 数据隔离标识（所有 provider 共用）。环境变量 MEMORY_USER_ID
    # 代码默认：__default__（memory/external_memory_builder.go:36-46）
    user_id: __default__
    # 环境变量 MEMORY_SCOPE_ID。代码默认：__default__
    scope_id: __default__
    # 本地长期记忆（provider=openjiuwen 时生效），依赖顶层 embed 段提供 embedding
    openjiuwen:
      kv_type: shelve
      kv_path: ""
      vector_type: chroma
      vector_persist_dir: ""
      db_type: sqlite
      db_path: ""
    # 云端事实抽取（provider=mem0 时生效），api_key 必填
    mem0:
      # 环境变量 MEM0_API_KEY。代码默认：空（未配置即不可用）
      api_key: ""
      # 环境变量 MEM0_USER_ID。代码默认：uapclawswarm-user（external_memory_builder.go:19-22）
      user_id: uapclawswarm-user
      # 环境变量 MEM0_AGENT_ID。代码默认：uapclawswarm
      agent_id: uapclawswarm
      # 搜索结果重排序（更准但更慢）。代码默认：true
      rerank: true
    # 字节上下文数据库（provider=openviking 时生效）
    openviking:
      endpoint: "http://127.0.0.1:1933"
      api_key: ""
      account: root
      user: default

# Embedding 配置（内置记忆与 CodingMemoryRail 依赖）。
# 缺失时 MemoryRail 不创建并打 "no embed config" 告警（adapter/deep_adapter_rails.go:713-718）
embed:
  # 环境变量 EMBED_API_KEY。代码默认：空
  embed_api_key: ""
  # 环境变量 EMBED_API_BASE。代码默认：空
  embed_base_url: ""
  # 环境变量 EMBED_MODEL。代码默认：空（部分链路兜底 text-embedding-v3，adapter/code_adapter.go:1029）
  embed_model: ""

# 模型配置
# 优先级：models.defaults（列表）> models.default（旧格式单对象）> 环境变量兜底
# （common/config/default_models.go:36-70）。本模板走 defaults 列表，值全部取自环境变量，
# 因此行为等价于环境变量兜底；请勿把 ${...} 替换成具体模型名，否则会覆盖环境变量链路。
models:
  defaults:
    - model_client_config:
        # 环境变量 API_BASE
        api_base: ${API_BASE}
        # 环境变量 API_KEY
        api_key: ${API_KEY}
        # 环境变量 MODEL_NAME
        model_name: ${MODEL_NAME}
        # 环境变量 MODEL_PROVIDER
        client_provider: ${MODEL_PROVIDER}
        # 单次请求超时（秒）。代码默认：1800（default_models.go:181）
        timeout: 1800
        # 是否校验服务端证书。代码默认：false（default_models.go:182）
        verify_ssl: false
        # 附加请求头（JSON 字符串或映射）
        custom_headers: {}
      model_config_obj:
        # 采样温度。代码默认：0.95（default_models.go:187）
        temperature: 0.95
      # 该条目是否为默认模型
      is_default: true
```

- [ ] **Step 2: 跑断言**

Run: `go test ./internal/common/config -run TestResourcesConfigYAML -v`
Expected: `memory.*`、`embed.*`、`models.*` 缺键报错消失

- [ ] **Step 3: 提交**

```bash
git add resources/config.yaml
git commit -m "feat(config): 出厂模板补全 memory、embed、models 段"
```

---

### Task 9: 模板第三段 —— react、agents、acp_agents、modes、team

**Files:**
- Modify: `resources/config.yaml`

**取值来源：** 结构参照 Python 模板 `:249-338`（react）、`:216-227`（agents）、`:171-177`（acp_agents）、`:818-917`（modes）、`:769-796`（team）；键集合以 Go 读取点为准（`adapter/deep_adapter.go:481/575/748/2031/2657-2676`、`adapter/deep_adapter_config.go:175-178/835`、`team/config_loader.go:81-110/417-570`、`memory/config.go:288-300`）。

- [ ] **Step 1: 追加下列段**

```yaml
# ReAct / 主 Agent 配置
react:
  # Agent 名称。代码默认：main_agent（adapter/deep_adapter.go:355-360）
  agent_name: main_agent
  # 任务循环总开关。代码默认：true（adapter/deep_adapter.go:2657-2663）
  # 注意：为 true 时内层 ReAct 上限被置为 MaxInt（deep_agent.go:1548/1739），max_iterations 对主 agent 不生效
  enable_task_loop: true
  # 任务规划开关（独立于 task_loop）：为 true 才注册 todo_create/todo_list/todo_get/todo_modify
  # 代码默认：true（adapter/deep_adapter.go:2668-2676）
  enable_task_planning: true
  # ReAct 最大迭代次数。代码默认：15（adapter/deep_adapter.go:575）
  max_iterations: 100
  # 模型名；留空则用 models 段默认条目
  model_name: ""
  # 技能模式：all / 指定列表
  skill_mode: all
  # 子代理开关：仅显式 enabled: true 才启用（adapter/deep_adapter_config.go:175-178）
  subagents:
    general_agent:
      enabled: true
    code_agent:
      enabled: false
    research_agent:
      enabled: false
  # 输出分块（字符数）
  answer_chunk_size: 500
  stream_chunk_threshold: 50
  stream_character_threshold: 2000
  # 上下文工程配置
  context_engine_config:
    # 是否释放 KV 缓存
    enable_kv_cache_release: false

# 预定义 Agent 列表（agent 名 → 模型/技能/工作区/迭代上限）
# 代码默认：max_iterations=200、completion_timeout=600、stable_base=true（team/config_loader.go:20-26）
agents: {}

# ACP 子代理配置（profile → command/args/cwd/env）
# 代码读取：agents/harness/common/tools/acp_chat/client_cache.go:69
# 未配置任何 profile 时 ACP 工具不可用（无代码默认）
acp_agents: {}

# 模式配置：agent（fast/plan）/ code / team
modes:
  agent:
    # 快速模式
    fast:
      # 内置记忆开关；生效还需 memory.engine/mode 放行且 embed 段齐全
      # 代码默认：false（memory/config.go:296-300）
      memory:
        enabled: true
        # 主动记忆：true=模型自行静默记录；false=仅在用户明确要求时记录
        # 代码默认：false（memory/config.go:303-310）
        is_proactive: false
    # 规划模式
    plan:
      memory:
        enabled: true
        # 规划模式默认启用主动记忆（prompts/sections/memory.go:268-277）
        is_proactive: true
  code:
    # 动态 Rails：出现在列表中的 = 启用
    rails: []
    # 可用工具列表；留空表示不额外限制
    tools: []
  # 团队模式
  team:
    # 团队级共享记忆（跨成员读写同一份 TEAM_MEMORY.md）
    # 代码默认：false（关闭时成员各自使用独立 MemoryRail）
    memory:
      enabled: false

# 团队配置
team:
  # 团队标识，作为运行时身份键（TeamRuntimeManager 按 team_name + session_id 取活跃团队）
  # 代码默认：team（team/config_loader.go:80-83）
  team_name: uapclaw_team
  # 生命周期：persistent / ephemeral。代码默认：persistent（team/config_loader.go:86）
  lifecycle: persistent
  # 成员模式：build_mode / ...。代码默认：build_mode（team/config_loader.go:87）
  teammate_mode: build_mode
  # 派生方式：inprocess / ...。代码默认：inprocess（team/config_loader.go:88）
  spawn_mode: inprocess
  # 是否启用 HITT（human-in-the-loop）。代码默认：true（team/config_loader.go:89）
  enable_hitt: true
  # 成员定义（agent 名 → 规格）
  agents: {}
  # 传输方式。代码默认：inprocess（team/config_loader.go:502-518）
  transport:
    type: inprocess
  # 存储方式。代码默认：sqlite（team/config_loader.go:113-155）
  storage:
    type: sqlite
  # 工作区
  workspace:
    enabled: true
```

- [ ] **Step 2: 跑断言**

Run: `go test ./internal/common/config -run TestResourcesConfigYAML -v`
Expected: `react.*`、`modes.*`、`team.*` 缺键报错消失；若 `modes.code.rails` 报类型不符（`[]` 解析为 `[]any`），按测试实际输出调整断言方式（保留 `rails: []`）

- [ ] **Step 3: 提交**

```bash
git add resources/config.yaml
git commit -m "feat(config): 出厂模板补全 react、agents、acp_agents、modes、team 段"
```

---

### Task 10: 模板第四段 —— channels、permissions、hooks、mcp、browser、extensions + 未实现段说明

**Files:**
- Modify: `resources/config.yaml`

**取值来源：** 结构参照 Python 模板 `:393-515`（channels）、`:527-680`（permissions）、`:733-767`（hooks）、`:798-816`（mcp）、`:682-693`（browser）、`:709-712`（extensions）；Go 读取点：`common/config/normalize.go:65-79`、`adapter/deep_adapter_config.go:603-607`、`security/tiered_policy.go:135-393`、`common/hooks/config.go:133`、`adapter/deep_adapter_mcp.go:144-186`、`adapter/deep_adapter_config.go:890`、`cmd/uapclaw/cmd.go:60`。

- [ ] **Step 1: 追加下列段**

```yaml
# 通道配置
channels:
  # Web 通道
  web:
    # 允许模型向用户发送文件。代码默认：false；归一化在 channels 存在时补 true
    # （common/config/normalize.go:65-79）
    # 注意：web 下 send_file_allowed 缺键或显式为 null 时，整个 channels.web 会被替换为
    # {send_file_allowed: true}，本段其他键会一并丢失，因此本段键必须写全
    send_file_allowed: true
  # ACP 通道
  acp:
    # 是否启用。代码默认：false
    enabled: false
    # 通道标识
    channel_id: ""
    # 默认会话 ID
    default_session_id: ""

# 权限配置
permissions:
  # 总开关；关闭时权限 rail 不注册。代码默认：false（adapter/deep_adapter_rails.go:806-809）
  enabled: false
  # 策略类型。代码默认：tiered_policy
  schema: tiered_policy
  # 权限模式：normal / ...。代码默认：normal
  permission_mode: normal
  # 工具级默认策略（工具名 → allow/ask/deny）
  tools: {}
  # 规则列表
  rules: []
  # 外部目录访问策略
  external_directory:
    enabled: false
  # 拒绝时给模型的提示
  deny_guidance_message: ""

# Hooks 配置
hooks:
  # 全局禁用所有 hook。代码默认：false（common/hooks/config.go:133-140）
  disable_all_hooks: false

# MCP 服务配置（servers[].{name,enabled,transport,command,args,cwd,env,url,headers,timeout_s}）
# enabled 缺省为 true（adapter/deep_adapter_mcp.go:186）
mcp:
  servers: []

# 浏览器配置
browser:
  # Chrome 可执行文件路径（可为字符串或平台→路径的映射）
  # 代码默认：空（由浏览器 runtime 自行探测，adapter/deep_adapter_config.go:890）
  chrome_path: ""

# 扩展配置
extensions:
  # 扩展目录列表，多个用 ';' 分隔。代码默认：空（cmd/uapclaw/cmd.go:60）
  extension_dirs: ""

# ──────────────────────────────────────────────────────────────
# 以下段在当前版本尚未实现，配置后不会生效，故不提供键：
#   telemetry        —— 可观测性未接入（observability 包不读 YAML）
#   task_memory      —— 任务记忆服务未接入配置面
#   tools            —— 工具总开关未实现
#   email_settings   —— 仅 Web UI 环境变量映射，不读该段
#   updater          —— 自动更新为桩实现
# 待对应领域实现后在此补充。
# ──────────────────────────────────────────────────────────────
```

- [ ] **Step 2: 跑全量断言**

Run: `go test ./internal/common/config -run TestResourcesConfigYAML -v`
Expected: 三个用例全部 PASS

- [ ] **Step 3: 提交**

```bash
git add resources/config.yaml
git commit -m "feat(config): 出厂模板补全 channels、permissions、hooks、mcp 等段并标注未实现段"
```

---

### Task 11: 修 D-1 —— `resolveEnableTaskLoop` 漏接 `skill_create`

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go:2655-2664`
- Modify: `internal/swarm/server/adapter/deep_adapter_config_test.go`

- [ ] **Step 1: 写失败的测试**

追加到 `deep_adapter_config_test.go`：

```go
// TestResolveEnableTaskLoop_SkillCreate覆盖 验证 skill_create=true 时强制启用 task loop
func TestResolveEnableTaskLoop_SkillCreate覆盖(t *testing.T) {
	d := &DeepAdapter{}
	cases := []struct {
		name       string
		config     map[string]any
		configBase map[string]any
		want       bool
	}{
		{
			name:       "skill_create=true且用户关闭则强制开启",
			config:     map[string]any{"enable_task_loop": false},
			configBase: map[string]any{"evolution": map[string]any{"skill_create": true}},
			want:       true,
		},
		{
			name:       "skill_create=true且缺键默认开启",
			config:     map[string]any{},
			configBase: map[string]any{"evolution": map[string]any{"skill_create": true}},
			want:       true,
		},
		{
			name:       "skill_create=false则尊重用户关闭",
			config:     map[string]any{"enable_task_loop": false},
			configBase: map[string]any{"evolution": map[string]any{"skill_create": false}},
			want:       false,
		},
		{
			name:       "无skill_create配置则尊重用户关闭",
			config:     map[string]any{"enable_task_loop": false},
			configBase: map[string]any{},
			want:       false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := d.resolveEnableTaskLoop(tc.config, tc.configBase); got != tc.want {
				t.Errorf("resolveEnableTaskLoop = %v，期望 %v", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/swarm/server/adapter -run TestResolveEnableTaskLoop_SkillCreate覆盖 -v`
Expected: FAIL，第一条用例得到 `false`，期望 `true`

- [ ] **Step 3: 改实现**

把 `deep_adapter.go` 的 `resolveEnableTaskLoop` 替换为：

```go
// resolveEnableTaskLoop 解析是否启用任务循环。
// Python: _resolve_enable_task_loop(config, config_base)
//
// skill_create=True 时强制启用：SkillCreateRail 依赖 AFTER_TASK_ITERATION 事件与
// enqueue_follow_up()，单轮模式下无法工作（Python interface_deep.py:2215-2248）。
func (d *DeepAdapter) resolveEnableTaskLoop(config map[string]any, configBase map[string]any) bool {
	configured := true
	if v, ok := config["enable_task_loop"]; ok {
		if b, ok := v.(bool); ok {
			configured = b
		}
	}
	if team.GetSkillCreateEnabled(configBase) {
		if !configured {
			logger.Warn(logComponent).
				Bool("configured_value", configured).
				Msg("skill_create requires enable_task_loop, overriding user config")
		}
		return true
	}
	return configured
}
```

并在 `deep_adapter.go` 的 import 块加入（该包其他文件已引用此包，不会形成循环依赖）：

```go
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/team"
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/swarm/server/adapter -run TestResolveEnableTaskLoop -v`
Expected: 全 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/adapter/deep_adapter.go internal/swarm/server/adapter/deep_adapter_config_test.go
git commit -m "fix(adapter): resolveEnableTaskLoop 补上 skill_create 强制覆盖（对齐 Python）"
```

---

### Task 12: 全量验证与冒烟

**Files:** 无改动（仅验证）

- [ ] **Step 1: 编译与静态检查**

```bash
go build ./... && gofmt -l internal/ cmd/ && go vet ./internal/common/logger ./internal/common/config
```
Expected: 无输出（`gofmt -l` 列出文件即为格式不合格）

- [ ] **Step 2: 目标包测试与覆盖率**

```bash
go test ./internal/common/logger ./internal/common/config ./internal/swarm/server/adapter -cover
```
Expected: 全 PASS；`internal/common/logger` 覆盖率 ≥85%

- [ ] **Step 3: 全量回归**

```bash
go test ./... 2>&1 | tail -30
```
Expected: 无 FAIL 包

- [ ] **Step 4: 本地冒烟**

```bash
make build
UAPCLAW_DATA_DIR=/tmp/uapclaw-smoke ./bin/uapclaw --help >/dev/null 2>/tmp/uapclaw-smoke.log || true
tail -3 /tmp/uapclaw-smoke.log
```
Expected: stderr 里的日志行形如 `[2026-11-02 10:31:20.123] [INFO] [common] [xxx/yyy.go:NN] ...`，不再是 JSON 或 `10:31AM INF`

- [ ] **Step 5: 核对出厂模板被实际加载**

```bash
grep -c "代码默认" resources/config.yaml
```
Expected: 输出 ≥ 40（每个键都有默认值标注）

- [ ] **Step 6: 提交（如有格式修正）**

```bash
git add -A
git commit -m "chore: 日志格式与配置模板改造的收尾格式修正"
```

---

## 自检清单（执行者每完成一个 Task 勾选）

- [ ] 所有新增/修改的 Go 注释为中文，导出在前非导出在后，分区注释完整
- [ ] 新增文件已登记到对应 `doc.go` 的文件目录
- [ ] 每个 Task 的测试从「先失败」开始（TDD）
- [ ] `logging.format` 在全仓已彻底消失
- [ ] `resources/config.yaml` 中不存在 `telemetry` / `task_memory` / `tools` / `email_settings` / `updater` 段的键
