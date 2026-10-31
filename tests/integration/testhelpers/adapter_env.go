//go:build integration

package testhelpers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AdapterTestEnv CodeAdapter 集成测试的隔离环境。
//
// 隔离三件套（先例 tests/integration/swarm/server/adapter/code_adapter_coding_memory_test.go）：
//  1. UAPCLAW_DATA_DIR → t.TempDir()，先 pathutil.ResetCache() 再做任何路径调用
//  2. 创建 config/ 目录（模拟已初始化，防 getResolvedPaths 回退 ResourcesDir）
//  3. 创建 agent/workspace/（WriteFile 不建父目录）
//
// MockLLM 注册必须先于写 config.yaml（YAML 中 client_provider 引用此注册名）。
//
// 本文件与同包其他助手不同，额外带 //go:build integration 标签：
// 它依赖 mockllm（同为 integration 标签），不带标签会破坏无标签构建。
type AdapterTestEnv struct {
	// DataDir 每测试隔离的 UAPCLAW_DATA_DIR
	DataDir string
	// Provider MockLLM 在 ClientRegistry 的注册名（config.yaml 引用）
	Provider string
	// envBackup 环境变量备份（Cleanup 恢复）
	envBackup [][3]string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAdapterTestEnv 构建隔离环境，并从 t.Cleanup 注册收尾。
//
// prefix 用于生成唯一 provider 名，避免同包多个套件顺序执行时相互覆盖。
func NewAdapterTestEnv(t *testing.T, mock *mockllm.MockModelClient, prefix string) *AdapterTestEnv {
	t.Helper()

	env := &AdapterTestEnv{
		Provider: fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano()),
	}
	model_clients.GetClientRegistry().Register(env.Provider, "llm", mock.Factory())

	env.DataDir = t.TempDir()
	env.Setenv(t, "UAPCLAW_DATA_DIR", env.DataDir)
	pathutil.ResetCache()
	// 持久化检查点器单例重置：同进程内连续 CreateInstance 且各用 TempDir 隔离，
	// 首次绑定的 SQLite 路径随目录清理失效（PreRun 前置读写 kv_store 报 disk I/O error）。
	adapter.ResetPersistentCheckpointer()

	if err := os.MkdirAll(filepath.Join(env.DataDir, "config"), 0o755); err != nil {
		t.Fatalf("创建 config 目录失败: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(env.DataDir, "agent", "workspace"), 0o755); err != nil {
		t.Fatalf("创建 agent/workspace 目录失败: %v", err)
	}

	t.Cleanup(func() {
		_ = model_clients.GetClientRegistry().Unregister(env.Provider, "llm")
		// ResourceMgr 释放（对齐 Python Runner.stop() 语义）：
		// rail 注册逻辑含 get_tool 查重，命中时跳过 add_tool，后续测试会拿到
		// 绑定已删除 TempDir 的工具实例。
		if rm := runner.GetResourceMgr(); rm != nil {
			_ = rm.Release(context.Background())
		}
		env.restoreEnv()
		pathutil.ResetCache()
	})
	return env
}

// Setenv 记录原值并设置环境变量（Cleanup 时恢复）。
func (e *AdapterTestEnv) Setenv(t *testing.T, key, val string) {
	t.Helper()
	orig, had := os.LookupEnv(key)
	e.envBackup = append(e.envBackup, [3]string{key, orig, fmt.Sprintf("%t", had)})
	if err := os.Setenv(key, val); err != nil {
		t.Fatalf("设置环境变量 %s 失败: %v", key, err)
	}
}

// WriteConfigYAML 写入标准 config/config.yaml（models + 空 modes.code + react 骨架）。
//
// reactExtra 为追加到 react: 段末尾的原始 YAML 片段（调用方负责 2 空格缩进），例如：
//
//	"  max_iterations: 8\n"
//	"  subagents:\n    explore_agent:\n      max_iterations: 5\n"
//
// modes.code.tools/rails 置空，避免拉起无关工具与护栏。
func (e *AdapterTestEnv) WriteConfigYAML(t *testing.T, reactExtra string) {
	t.Helper()
	yamlContent := fmt.Sprintf(`models:
  defaults:
    - model_client_config:
        model_name: mock-model
        client_provider: %s
        api_key: mock-api-key
        api_base: http://127.0.0.1:1
        client_id: %s
      is_default: true

modes:
  code:
    tools: []
    rails: []

react:
  agent_name: main_agent
%s`, e.Provider, e.Provider, reactExtra)

	if err := os.WriteFile(filepath.Join(e.DataDir, "config", "config.yaml"), []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("写 config.yaml 失败: %v", err)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// restoreEnv 恢复环境变量。
func (e *AdapterTestEnv) restoreEnv() {
	for _, item := range e.envBackup {
		if item[2] == "false" {
			_ = os.Unsetenv(item[0])
			continue
		}
		_ = os.Setenv(item[0], item[1])
	}
	e.envBackup = nil
}
