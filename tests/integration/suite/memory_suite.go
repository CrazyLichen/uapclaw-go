//go:build integration

// Package suite 提供集成测试基础套件。
package suite

import (
	"os"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemorySuite 包含 Memory 模块集成测试的公共基础设施。
//
// 继承 RunnerSuite 全部能力，额外提供：
//   - InMemoryKVStore 实例（线程安全，无需外部依赖）
//   - MockEmbeddingProvider 实例（MD5-hash 确定性 128 维向量，无需外部 API）
//   - 临时目录（用于 SQLite 数据库文件、记忆文件等）
type MemorySuite struct {
	RunnerSuite
	// KVStore 内存 KV 存储，用于 LongTermMemory 测试
	KVStore *kv.InMemoryKVStore
	// MockEmbedding 确定性 Mock 嵌入向量提供者
	MockEmbedding *lite.MockEmbeddingProvider
	// TempDir 临时测试目录
	TempDir string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetupSuite 初始化 Memory 测试环境。
func (s *MemorySuite) SetupSuite() {
	s.RunnerSuite.SetupSuite()

	s.KVStore = kv.NewInMemoryKVStore()
	s.MockEmbedding = lite.NewMockEmbeddingProvider()

	dir, err := os.MkdirTemp("", "memory_test_*")
	if err != nil {
		s.T().Fatalf("创建临时目录失败: %v", err)
	}
	s.TempDir = dir
}

// TearDownSuite 清理 Memory 测试环境。
func (s *MemorySuite) TearDownSuite() {
	if s.TempDir != "" {
		_ = os.RemoveAll(s.TempDir)
	}
	s.KVStore = nil
	s.MockEmbedding = nil

	s.RunnerSuite.TearDownSuite()
}
