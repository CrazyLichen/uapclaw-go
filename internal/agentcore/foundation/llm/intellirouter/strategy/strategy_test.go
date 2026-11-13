package strategy

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	intelliroutercore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
)

func dep(t *testing.T, id string, fallbackTag *string) *intelliroutercore.Deployment {
	t.Helper()
	d, err := intelliroutercore.NewDeployment(intelliroutercore.DeploymentParams{
		ModelName: "m", APIKey: "k", APIBase: "u", ID: id, Provider: "openai", FallbackTag: fallbackTag,
	})
	require.NoError(t, err)
	return d
}

// TestOrderedFailover_SelectsFirstAvailable 空表返回 nil；非空取首（py:19-23）。
func TestOrderedFailover_SelectsFirstAvailable(t *testing.T) {
	s := NewOrderedFailover()
	require.Nil(t, s.SelectDeployment(nil, nil)) // 空列表 → None（基类契约 py:27）
	d0 := dep(t, "d0", nil)
	d1 := dep(t, "d1", nil)
	got := s.SelectDeployment([]*intelliroutercore.Deployment{d0, d1}, nil)
	require.Same(t, d0, got)
	// 旗标 ← py:17 strict_fallback_errors = True
	require.True(t, s.StrictFallbackErrors())
	require.True(t, s.FallbackToFirstOnEmptySelection()) // getattr 默认 True
}

// TestSimpleShuffle_权重选择 权重占比决定选择（py:34-59）。
func TestSimpleShuffle_权重选择(t *testing.T) {
	// 固定种子可复现：rng 注入（对齐模块级 random 的可测性等价实现）
	rng := rand.New(rand.NewSource(42))
	s := NewSimpleShuffle(map[string]float64{"d0": 100, "d1": 1}, 1.0, rng)
	d0 := dep(t, "d0", nil)
	d1 := dep(t, "d1", nil)
	got := s.SelectDeployment([]*intelliroutercore.Deployment{d0, d1}, nil)
	require.NotNil(t, got)
	require.Contains(t, []*intelliroutercore.Deployment{d0, d1}, got)

	// total == 0 → random.choice（py:46）
	s2 := NewSimpleShuffle(map[string]float64{"d0": 0, "d1": 0}, 0.0, rng)
	require.NotNil(t, s2.SelectDeployment([]*intelliroutercore.Deployment{d0, d1}, nil))

	// 空表 → nil（py:35-36）
	require.Nil(t, s.SelectDeployment(nil, nil))

	// 旗标：类属性未定义 → getattr 默认 False/True（router:245/242）
	require.False(t, s.StrictFallbackErrors())
	require.True(t, s.FallbackToFirstOnEmptySelection())
}

// TestSimpleShuffle_兜底 累计和兜底取末位（py:54）。
func TestSimpleShuffle_兜底(t *testing.T) {
	// r > 累计和的病态随机值（Float64 恰为 1.0 极端场景）→ 末位兜底
	rng := rand.New(rand.NewSource(7))
	s := NewSimpleShuffle(nil, 1.0, rng)
	d0 := dep(t, "d0", nil)
	require.NotNil(t, s.SelectDeployment([]*intelliroutercore.Deployment{d0}, nil))
}

// TestTagFiltered 请求级 tag 过滤与回退（py:32-52）。
func TestTagFiltered(t *testing.T) {
	tagA := "qwen-plus"
	s := NewTagFiltered(&tagA, nil) // fallback_strategy 缺省 → OrderedFailover（py:29）
	d0 := dep(t, "d0", nil)
	d1 := dep(t, "d1", &tagA)
	// 请求 fallback_tag 匹配 d1 → 选 d1（py:43-45 过滤）
	got := s.SelectDeployment([]*intelliroutercore.Deployment{d0, d1}, nil)
	require.Same(t, d1, got)

	// 无匹配 tag → 过滤后为空 → nil（py:47-48）
	tagB := "other"
	s2 := NewTagFiltered(&tagB, nil)
	require.Nil(t, s2.SelectDeployment([]*intelliroutercore.Deployment{d0, d1}, nil))

	// fallback_tag 未设置（nil）→ 不过滤全量委托（py:39-41 _request_fallback_tag nil）
	s3 := NewTagFiltered(nil, nil)
	got3 := s3.SelectDeployment([]*intelliroutercore.Deployment{d0, d1}, nil)
	require.Same(t, d0, got3)

	// fallback_tag 空串 → 视为未设置（py:33 `if self.fallback_tag`）
	empty := ""
	s4 := NewTagFiltered(&empty, nil)
	require.Same(t, d0, s4.SelectDeployment([]*intelliroutercore.Deployment{d0, d1}, nil))

	// 旗标 ← py:25/26
	require.True(t, s.StrictFallbackErrors())
	require.False(t, s.FallbackToFirstOnEmptySelection())
}

// TestTagFiltered_委托回调 on_success/on_failure 委托 fallback 策略（py:54-59）。
func TestTagFiltered_委托回调(t *testing.T) {
	s := NewTagFiltered(nil, nil)
	d := dep(t, "d0", nil)
	s.OnSuccess(d, 1.0, 10) // 委托 OrderedFailover 的 no-op，不 panic
	s.OnFailure(d, nil)
}
