// Package strategy 路由策略。← intelli_router/strategy/ 包
package strategy

import (
	intelliroutercore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Strategy 路由策略接口。← strategy/base_strategy.py:15 RoutingStrategy(ABC)
//
// 契约（py:21-27 原文）：SelectDeployment 收到的 deployments 列表已经过
// 可用性过滤（router 依据 state.deployment_status / cooldown 判断；state 是
// 运行期状态的唯一事实源）。策略不得依赖 Deployment 对象自身的
// status/is_available——那只反映构造时的静态状态，看不到运行期冷却。
// 空列表表示无可用部署，返回 nil 即可。
type Strategy interface {
	// SelectDeployment 选择一个部署；返回 nil 表示无可用。← py:32 select_deployment
	SelectDeployment(deployments []*intelliroutercore.Deployment, context *intelliroutercore.RoutingContext) *intelliroutercore.Deployment
	// OnSuccess 成功回调 - 策略可更新内部状态。← py:51 on_success
	OnSuccess(deployment *intelliroutercore.Deployment, latency float64, tokens int)
	// OnFailure 失败回调 - 策略可更新内部状态。← py:68 on_failure
	OnFailure(deployment *intelliroutercore.Deployment, err error)
	// StrictFallbackErrors 对齐 py 类属性 strict_fallback_errors：
	// reliable_router._uses_strict_fallback_errors（:245）经 getattr 读取，
	// 未定义时默认 False（base_strategy 未定义）。
	StrictFallbackErrors() bool
	// FallbackToFirstOnEmptySelection 对齐 py 类属性
	// fallback_to_first_on_empty_selection：reliable_router
	// ._fallback_to_first_on_empty_selection（:242）经 getattr 读取，
	// 未定义时默认 True。
	FallbackToFirstOnEmptySelection() bool
}
