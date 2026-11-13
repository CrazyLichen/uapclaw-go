package strategy

import (
	"math/rand"

	intelliroutercore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SimpleShuffle 简单随机策略 - 按权重随机选择。
// ← strategy/simple_shuffle.py:15 SimpleShuffleStrategy
//
// 传入的 deployments 已由 router 按可用性过滤（state 唯一事实源）。
// 算法: 1. 按权重构概率分布；2. 随机选择。（py:20-24）
type SimpleShuffle struct {
	// Weights 部署 ID → 权重 ← py:28 self.weights
	Weights map[string]float64
	// DefaultWeight 未登记部署的默认权重 ← py:29
	DefaultWeight float64
	// rng 随机源（构造注入，测试可复现；Python 模块级 random）
	rng *rand.Rand
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSimpleShuffle 构造。← py:26-30 __init__
func NewSimpleShuffle(weights map[string]float64, defaultWeight float64, rng *rand.Rand) *SimpleShuffle {
	if weights == nil {
		// ← py:28 self.weights = weights or {}
		weights = map[string]float64{}
	}
	if defaultWeight == 0 {
		// ← py:29 default_weight: float = 1.0
		defaultWeight = 1.0
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(rand.Int63()))
	}
	return &SimpleShuffle{Weights: weights, DefaultWeight: defaultWeight, rng: rng}
}

// SelectDeployment 按权重随机选择。← py:34-59 select_deployment
func (s *SimpleShuffle) SelectDeployment(deployments []*intelliroutercore.Deployment, _ *intelliroutercore.RoutingContext) *intelliroutercore.Deployment {
	if len(deployments) == 0 {
		return nil
	}
	// 获取权重 ← py:38-40
	weights := make([]float64, len(deployments))
	for i, d := range deployments {
		if w, ok := s.Weights[d.ID]; ok {
			weights[i] = w
		} else {
			weights[i] = s.DefaultWeight
		}
	}
	// 归一化 ← py:42-45
	total := 0.0
	for _, w := range weights {
		total += w
	}
	if total == 0 {
		// ← py:46 random.choice(deployments)
		return deployments[s.rng.Intn(len(deployments))]
	}
	// 随机选择 ← py:47-53
	r := s.rng.Float64()
	cumsum := 0.0
	for i, w := range weights {
		cumsum += w / total
		if r <= cumsum {
			return deployments[i]
		}
	}
	return deployments[len(deployments)-1] // ← py:54 兜底
}

// OnSuccess 失败/成功回调 - 随机策略无状态更新。← py:57-64
func (s *SimpleShuffle) OnSuccess(_ *intelliroutercore.Deployment, _ float64, _ int) {}

// OnFailure 随机策略无状态更新。← py:61-64
func (s *SimpleShuffle) OnFailure(_ *intelliroutercore.Deployment, _ error) {}

// StrictFallbackErrors 类属性未定义 → getattr 默认 False（router:246）。
func (s *SimpleShuffle) StrictFallbackErrors() bool { return false }

// FallbackToFirstOnEmptySelection 类属性未定义 → getattr 默认 True（router:243）。
func (s *SimpleShuffle) FallbackToFirstOnEmptySelection() bool { return true }
