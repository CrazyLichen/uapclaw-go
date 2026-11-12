package processcli

import (
	"os"
	"strings"
	"testing"
)

// ──────────────────────────── newPromptSession ────────────────────────────

// TestNewPromptSession_非TTY返回nil strings.Builder 不是终端，返回 nil。
// Python: prompt.py:88-106 isatty 双条件不满足即返回 None
func TestNewPromptSession_非TTY返回nil(t *testing.T) {
	if session := newPromptSession(strings.NewReader(""), &strings.Builder{}); session != nil {
		t.Error("非 TTY 应返回 nil，走逐行读 stdin 的降级路径")
	}
}

// TestNewPromptSession_非文件reader返回nil 非 *os.File 的 reader 视为非 TTY。
func TestNewPromptSession_非文件reader返回nil(t *testing.T) {
	if session := newPromptSession(strings.NewReader("x"), os.Stdout); session != nil {
		t.Error("非文件 reader 应返回 nil")
	}
}

// TestNewPromptSession_非文件writer返回nil 非 *os.File 的 writer 视为非 TTY。
func TestNewPromptSession_非文件writer返回nil(t *testing.T) {
	if session := newPromptSession(os.Stdin, &strings.Builder{}); session != nil {
		t.Error("非文件 writer 应返回 nil")
	}
}

// ──────────────────────────── readPrompt ────────────────────────────

// TestReadPrompt_读stdin一行 非 TTY 时逐行读 os.Stdin（重定向验证）。
// Python: prompt.py:123-131 read_prompt 降级到 input()
func TestReadPrompt_读stdin一行(t *testing.T) {
	original := os.Stdin
	defer func() { os.Stdin = original }()

	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	if _, err := writeEnd.WriteString("hello\n"); err != nil {
		t.Fatalf("写入管道: %v", err)
	}
	_ = writeEnd.Close()
	os.Stdin = readEnd

	got, readErr := readPrompt(nil, promptText)
	if readErr != nil {
		t.Fatalf("readPrompt: %v", readErr)
	}
	if got != "hello" {
		t.Errorf("got %q, 期望 hello", got)
	}
}

// TestReadPrompt_空stdin返回EOF 无输入时返回 io.EOF。
func TestReadPrompt_空stdin返回EOF(t *testing.T) {
	original := os.Stdin
	defer func() { os.Stdin = original }()

	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	_ = writeEnd.Close()
	os.Stdin = readEnd

	if _, readErr := readPrompt(nil, promptText); readErr == nil {
		t.Error("空 stdin 应返回 EOF")
	}
}

// ──────────────────────────── readPromptFrom 边界 ────────────────────────────


// TestReadPromptFrom_多行只读第一行 一次只读一条指令。
func TestReadPromptFrom_多行只读第一行(t *testing.T) {
	got, err := readPromptFrom(nil, promptText, strings.NewReader("first\nsecond\n"))
	if err != nil {
		t.Fatalf("readPromptFrom: %v", err)
	}
	if got != "first" {
		t.Errorf("got %q, 期望 first（只读第一行）", got)
	}
}

// TestReadPromptFrom_空白行返回空串 空行不算 EOF。
func TestReadPromptFrom_空白行返回空串(t *testing.T) {
	got, err := readPromptFrom(nil, promptText, strings.NewReader("   \n"))
	if err != nil {
		t.Fatalf("readPromptFrom: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, 期望空串", got)
	}
}

// ──────────────────────────── slashCompleter ────────────────────────────


// TestSlashCompleterText_参数段替换范围 "/mode ag" 只替换 "ag" 段。
func TestSlashCompleterText_参数段替换范围(t *testing.T) {
	_, start, end := slashCompleterText("/mode ag")
	if start != 6 || end != 8 {
		t.Errorf("替换范围 = %d..%d, 期望 6..8", start, end)
	}
}


// TestSlashCompleterText_中文前缀 中文输入不 panic 且返回空。
func TestSlashCompleterText_中文前缀(t *testing.T) {
	suggests, start, end := slashCompleterText("/中文")
	if len(suggests) != 0 {
		t.Errorf("中文前缀不应补全：%v", suggests)
	}
	if start != 0 || end != 3 {
		t.Errorf("替换范围 = %d..%d, 期望 0..3（rune 计数）", start, end)
	}
}
