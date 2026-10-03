//go:build integration

package suite

import (
	"context"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseIntegrationSuite 集成测试基础套件。
// 所有集成测试的起点，等价于 Python conftest.py 中的 mock_llm fixture。
//
// 提供：
//   - 5 分钟超时的 Context（TearDownSuite 自动 Cancel）
//   - MockModelClient 实例（可直接预设响应）
//   - 嵌入 suite.Suite 获得完整断言方法
type BaseIntegrationSuite struct {
	suite.Suite
	// Ctx 测试用上下文，5 分钟超时
	Ctx context.Context
	// Cancel 取消函数，TearDownSuite 中调用
	Cancel context.CancelFunc
	// MockLLM Mock 模型客户端，所有 LLM 调用走此实例
	MockLLM *mockllm.MockModelClient
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetupSuite 初始化基础测试环境。
// 子 Suite 必须在自身 SetupSuite 开头调用此方法：
//
//	func (s *MySuite) SetupSuite() {
//	    s.BaseIntegrationSuite.SetupSuite()
//	    // ... 子 Suite 特有初始化
//	}
func (s *BaseIntegrationSuite) SetupSuite() {
	s.Ctx, s.Cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	s.MockLLM = mockllm.NewMockModelClient()
}

// TearDownSuite 清理基础测试环境。
// 子 Suite 必须在自身 TearDownSuite 结尾调用此方法：
//
//	func (s *MySuite) TearDownSuite() {
//	    // ... 子 Suite 特有清理
//	    s.BaseIntegrationSuite.TearDownSuite()
//	}
func (s *BaseIntegrationSuite) TearDownSuite() {
	if s.Cancel != nil {
		s.Cancel()
	}
}
