package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
)

// TestRuntime_小结果inline 验证阈值内不落盘。
func TestRuntime_小结果inline(t *testing.T) {
	require.NoError(t, schema.SetLanguage(schema.LanguageCN))
	dir := t.TempDir()
	rec := &injectionRecorder{}
	rt := NewAsyncToolRuntime(func(_ context.Context, text string) error {
		rec.add(text)
		return nil
	}, WithSpillThreshold(1000), WithOutputDirResolver(func() string { return dir }))

	rt.Launch("s1", func(context.Context) (any, error) { return strings.Repeat("x", 500), nil },
		tools.AsyncToolLaunchOptions{ToolName: "demo"})
	waitStatus(t, rt, "s1", AsyncToolStatusCompleted)
	injected := waitInjected(t, rec, 1)
	assert.Empty(t, rt.Get("s1").OutputFile)
	assert.Contains(t, injected[0], strings.Repeat("x", 500))
	entries, _ := os.ReadDir(dir)
	assert.Empty(t, entries)
}

// TestRuntime_大结果spill 验证超阈值落盘 + 摘要注入。
func TestRuntime_大结果spill(t *testing.T) {
	require.NoError(t, schema.SetLanguage(schema.LanguageCN))
	dir := t.TempDir()
	rec := &injectionRecorder{}
	rt := NewAsyncToolRuntime(func(_ context.Context, text string) error {
		rec.add(text)
		return nil
	}, WithSpillThreshold(1000), WithOutputDirResolver(func() string { return dir }))

	payload := strings.Repeat("y", 5000)
	rt.Launch("s2", func(context.Context) (any, error) { return payload, nil },
		tools.AsyncToolLaunchOptions{ToolName: "demo"})
	waitStatus(t, rt, "s2", AsyncToolStatusCompleted)
	injected := waitInjected(t, rec, 1)

	record := rt.Get("s2")
	require.NotEmpty(t, record.OutputFile)
	data, err := os.ReadFile(record.OutputFile)
	require.NoError(t, err)
	assert.Equal(t, payload, string(data))
	assert.Contains(t, injected[0], record.OutputFile)
	assert.Less(t, len(injected[0]), 5000)
	assert.Equal(t, "s2.output", filepath.Base(record.OutputFile))
}

// TestRuntime_CJK摘要按rune切 验证不会截断半个汉字。
func TestRuntime_CJK摘要按rune切(t *testing.T) {
	require.NoError(t, schema.SetLanguage(schema.LanguageCN))
	dir := t.TempDir()
	rec := &injectionRecorder{}
	rt := NewAsyncToolRuntime(func(_ context.Context, text string) error {
		rec.add(text)
		return nil
	}, WithSpillThreshold(10), WithOutputDirResolver(func() string { return dir }))

	rt.Launch("cjk", func(context.Context) (any, error) { return strings.Repeat("汉", 2000), nil },
		tools.AsyncToolLaunchOptions{ToolName: "demo"})
	waitStatus(t, rt, "cjk", AsyncToolStatusCompleted)
	injected := waitInjected(t, rec, 1)
	assert.Contains(t, injected[0], strings.Repeat("汉", spillSummaryChars))
}

// TestRuntime_无resolver全量inline 验证 resolver 缺失时大结果不落盘。
func TestRuntime_无resolver全量inline(t *testing.T) {
	rec := &injectionRecorder{}
	rt := NewAsyncToolRuntime(func(_ context.Context, text string) error {
		rec.add(text)
		return nil
	}, WithSpillThreshold(10))

	payload := strings.Repeat("z", 5000)
	rt.Launch("s3", func(context.Context) (any, error) { return payload, nil },
		tools.AsyncToolLaunchOptions{ToolName: "demo"})
	waitStatus(t, rt, "s3", AsyncToolStatusCompleted)
	injected := waitInjected(t, rec, 1)
	assert.Empty(t, rt.Get("s3").OutputFile)
	assert.Contains(t, injected[0], payload)
}

// TestRuntime_spill失败降级inline 验证目录不可创建时结果仍全量 inline。
func TestRuntime_spill失败降级inline(t *testing.T) {
	rec := &injectionRecorder{}
	dir := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(dir, []byte("x"), 0o644))
	rt := NewAsyncToolRuntime(func(_ context.Context, text string) error {
		rec.add(text)
		return nil
	}, WithSpillThreshold(10), WithOutputDirResolver(func() string { return dir }))

	payload := strings.Repeat("q", 100)
	rt.Launch("s4", func(context.Context) (any, error) { return payload, nil },
		tools.AsyncToolLaunchOptions{ToolName: "demo"})
	waitStatus(t, rt, "s4", AsyncToolStatusCompleted)
	injected := waitInjected(t, rec, 1)
	assert.Empty(t, rt.Get("s4").OutputFile)
	assert.Contains(t, injected[0], payload)
}

// TestRuntime_spill写失败降级inline 验证目标文件写失败时结果仍全量 inline。
func TestRuntime_spill写失败降级inline(t *testing.T) {
	rec := &injectionRecorder{}
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "s5.output"), 0o755))
	rt := NewAsyncToolRuntime(func(_ context.Context, text string) error {
		rec.add(text)
		return nil
	}, WithSpillThreshold(10), WithOutputDirResolver(func() string { return dir }))

	payload := strings.Repeat("w", 100)
	rt.Launch("s5", func(context.Context) (any, error) { return payload, nil },
		tools.AsyncToolLaunchOptions{ToolName: "demo"})
	waitStatus(t, rt, "s5", AsyncToolStatusCompleted)
	injected := waitInjected(t, rec, 1)
	assert.Empty(t, rt.Get("s5").OutputFile)
	assert.Contains(t, injected[0], payload)
}
