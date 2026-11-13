package strategy

import (
	intelliroutercore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TagFiltered 严格 fallback-tag 过滤路由策略。
// ← strategy/tag_filtered.py:12 TagFilteredStrategy
//
// Filter deployments by fallback_tag, then delegate selection.
// Deployments passed in are already availability-filtered by the router
// (state is the single source of truth).
//
// “model_description“ is intentionally not used for routing. It is carried
// on Deployment for future cluster-level model understanding.（py:17-22 原文）
type TagFiltered struct {
	// FallbackTag 固定 fallback 标签 ← py:28 self.fallback_tag（**导出契约**：
	// 上游 intelli_router_errors.pinned_route_tag 直接读 strategy.fallback_tag）
	FallbackTag *string
	// fallbackStrategy 委托的选择策略 ← py:29 self.fallback_strategy
	fallbackStrategy Strategy
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTagFiltered 构造。← py:24-30 __init__
func NewTagFiltered(fallbackTag *string, fallbackStrategy Strategy) *TagFiltered {
	if fallbackStrategy == nil {
		// ← py:29 fallback_strategy or OrderedFailoverStrategy()
		fallbackStrategy = NewOrderedFailover()
	}
	return &TagFiltered{FallbackTag: fallbackTag, fallbackStrategy: fallbackStrategy}
}

// SelectDeployment 按 tag 过滤后委托选择。← py:35-52 select_deployment
func (s *TagFiltered) SelectDeployment(deployments []*intelliroutercore.Deployment, context *intelliroutercore.RoutingContext) *intelliroutercore.Deployment {
	if len(deployments) == 0 {
		return nil
	}
	fallbackTag := s.requestFallbackTag()
	if fallbackTag != nil {
		var filtered []*intelliroutercore.Deployment
		for _, d := range deployments {
			// ← py:43-45 deployment.fallback_tag == fallback_tag
			if d.FallbackTag != nil && *d.FallbackTag == *fallbackTag {
				filtered = append(filtered, d)
			}
		}
		deployments = filtered
	}
	if len(deployments) == 0 {
		return nil // ← py:47-48
	}
	return s.fallbackStrategy.SelectDeployment(deployments, context)
}

// OnSuccess 委托 fallback 策略。← py:54-56
func (s *TagFiltered) OnSuccess(d *intelliroutercore.Deployment, latency float64, tokens int) {
	s.fallbackStrategy.OnSuccess(d, latency, tokens)
}

// OnFailure 委托 fallback 策略。← py:57-59
func (s *TagFiltered) OnFailure(d *intelliroutercore.Deployment, err error) {
	s.fallbackStrategy.OnFailure(d, err)
}

// StrictFallbackErrors ← py:25 strict_fallback_errors = True
func (s *TagFiltered) StrictFallbackErrors() bool { return true }

// FallbackToFirstOnEmptySelection ← py:26 fallback_to_first_on_empty_selection = False
func (s *TagFiltered) FallbackToFirstOnEmptySelection() bool { return false }

// ──────────────────────────── 非导出函数 ────────────────────────────

// requestFallbackTag 请求级 fallback 标签。← py:32-34 _request_fallback_tag
func (s *TagFiltered) requestFallbackTag() *string {
	if s.FallbackTag != nil && *s.FallbackTag != "" {
		return s.FallbackTag
	}
	return nil
}
