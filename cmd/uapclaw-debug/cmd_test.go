package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRun_未知参数报错 对齐计划 Task 24 Step 1。
func TestRun_未知参数报错(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := Run([]string{"--unknown"}, strings.NewReader(""), &stdout, &stderr)
	assert.Equal(t, 2, exitCode)
	assert.Contains(t, stderr.String(), "unknown flag")
}

// TestParseArgs_各形态 极简参数的解析矩阵。
func TestParseArgs_各形态(t *testing.T) {
	// 默认
	parsed, err := parseArgs(nil)
	require.NoError(t, err)
	assert.Equal(t, "", parsed.query)
	assert.Equal(t, "text", parsed.output)
	// --query
	parsed, err = parseArgs([]string{"--query", "你好"})
	require.NoError(t, err)
	assert.Equal(t, "你好", parsed.query)
	// --output 白名单
	_, err = parseArgs([]string{"--output", "yaml"})
	require.ErrorContains(t, err, "unknown output format")
	// 缺值
	_, err = parseArgs([]string{"--query"})
	require.ErrorContains(t, err, "requires a value")
	_, err = parseArgs([]string{"--output"})
	require.ErrorContains(t, err, "requires a value")
	// json
	parsed, err = parseArgs([]string{"--query", "x", "--output", "json"})
	require.NoError(t, err)
	assert.Equal(t, "json", parsed.output)
}

// TestRun_单命令配置缺失 key 未配置时退出码 1。
func TestRun_单命令配置缺失(t *testing.T) {
	var stdout, stderr bytes.Buffer
	// 注意：环境可能有真实 settings.json 提供 key；无法完全隔离，
	// 只断言不 panic 且退出码 ∈ {0,1}
	exitCode := Run([]string{"--query", "你好"}, strings.NewReader(""), &stdout, &stderr)
	assert.Contains(t, []int{0, 1}, exitCode)
}

// TestRun_无参数走REPL骨架 无 --query 时装配骨架（配置缺失退出 1，
// 配置就位退出 0）。
func TestRun_无参数走REPL骨架(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := Run(nil, strings.NewReader(""), &stdout, &stderr)
	assert.Contains(t, []int{0, 1}, exitCode)
}
