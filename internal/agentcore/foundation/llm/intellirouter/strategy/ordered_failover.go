package strategy

import (
	intelliroutercore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OrderedFailover 顺序故障转移策略。
// ← strategy/ordered_failover.py:11 OrderedFailoverStrategy
//
// Always choose the first deployment in the given (already availability-
// filtered) order. Availability filtering is owned by the router; state is
// the single source of truth.（py:13-15 原文）
type OrderedFailover struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOrderedFailover 构造。← py:17 strict_fallback_errors = True
func NewOrderedFailover() *OrderedFailover { return &OrderedFailover{} }

// SelectDeployment 按已过滤顺序取首个；空返回 nil。← py:18-24
func (s *OrderedFailover) SelectDeployment(deployments []*intelliroutercore.Deployment, _ *intelliroutercore.RoutingContext) *intelliroutercore.Deployment {
	if len(deployments) == 0 {
		return nil
	}
	return deployments[0]
}

// OnSuccess 无状态更新。← py:26
func (s *OrderedFailover) OnSuccess(_ *intelliroutercore.Deployment, _ float64, _ int) {}

// OnFailure 无状态更新。← py:28
func (s *OrderedFailover) OnFailure(_ *intelliroutercore.Deployment, _ error) {}

// StrictFallbackErrors ← py:17 strict_fallback_errors = True
func (s *OrderedFailover) StrictFallbackErrors() bool { return true }

// FallbackToFirstOnEmptySelection 类属性未定义 → getattr 默认 True（router:243）。
func (s *OrderedFailover) FallbackToFirstOnEmptySelection() bool { return true }
