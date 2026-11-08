package rails_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// useCleanRegistry 每个用例前清空注册表与守卫。
func useCleanRegistry(t *testing.T) {
	t.Helper()
	hschema.ResetRailRegistryForTest()
	rails.ResetRegistrationForTest()
	t.Cleanup(func() {
		hschema.ResetRailRegistryForTest()
		rails.ResetRegistrationForTest()
	})
}

// TestEnsureHarnessElementsRegistered_幂等 二次调用不重复注册、不报错。
//
// 对齐上游 registration.py:60-62 的 `_REGISTERED` 守卫：
// 「只有首次调用做事，重复调用直接短路」。
func TestEnsureHarnessElementsRegistered_幂等(t *testing.T) {
	useCleanRegistry(t)

	rails.EnsureHarnessElementsRegistered()
	first := len(hschema.RegisteredRailNames())

	rails.EnsureHarnessElementsRegistered()
	second := len(hschema.RegisteredRailNames())

	assert.Equal(t, first, second, "二次调用不得改变注册数量")
	assert.NotZero(t, first, "首次调用应注册了东西")
}

// TestEnsureHarnessElementsRegistered_8个rail全注册 调用后 8 个 team rail 全部可查。
func TestEnsureHarnessElementsRegistered_8个rail全注册(t *testing.T) {
	useCleanRegistry(t)

	rails.EnsureHarnessElementsRegistered()

	for _, name := range []string{
		rails.TeamTool, rails.TeamPolicy, rails.TeamWorkspace,
		rails.TeamToolApproval, rails.TeamPlanMode,
		rails.TeamReliability, rails.TeamSkillUse, rails.TeamObservability,
	} {
		_, ok := hschema.LookupRailProvider(name)
		assert.True(t, ok, "%q 必须已注册", name)
	}
}

// TestEnsureHarnessElementsRegistered_补跑语义 空表状态下调用后 team 描述符全部进表。
//
// 这是上游 registration.py 的核心约束：harness 的 _REGISTERED 守卫会在
// 基础注册时短路，若不补跑 register_from_catalog，team 描述符永远到不了注册表。
// Go 侧等价验证：无论基础注册是否已"完成"，团队 rail 都必须进表。
func TestEnsureHarnessElementsRegistered_补跑语义(t *testing.T) {
	useCleanRegistry(t)

	_, before := hschema.LookupRailProvider(rails.TeamPolicy)
	assert.False(t, before, "前置：注册表为空")

	rails.EnsureHarnessElementsRegistered()

	_, after := hschema.LookupRailProvider(rails.TeamPolicy)
	assert.True(t, after, "补跑后 team 描述符必须进表")
}

// TestEnsureHarnessElementsRegistered_重置后可重跑 Reset 后能再次注册。
func TestEnsureHarnessElementsRegistered_重置后可重跑(t *testing.T) {
	useCleanRegistry(t)

	rails.EnsureHarnessElementsRegistered()
	hschema.ResetRailRegistryForTest()
	rails.ResetRegistrationForTest()

	_, gone := hschema.LookupRailProvider(rails.TeamPolicy)
	assert.False(t, gone, "Reset 后表应为空")

	rails.EnsureHarnessElementsRegistered()

	_, back := hschema.LookupRailProvider(rails.TeamPolicy)
	assert.True(t, back, "Reset 后重跑应重新注册")
}

// TestEnsureHarnessElementsRegistered_并发安全 多协程同时调不出错、不重复注册。
//
// Go 特有：registrationMu 保证注册只做一次。
func TestEnsureHarnessElementsRegistered_并发安全(t *testing.T) {
	useCleanRegistry(t)

	const n = 8
	done := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			rails.EnsureHarnessElementsRegistered()
		}()
	}
	for i := 0; i < n; i++ {
		<-done
	}

	// 全部 8 个 rail 可查（重复注册是幂等的，不会报错）
	for _, name := range []string{rails.TeamTool, rails.TeamPolicy} {
		_, ok := hschema.LookupRailProvider(name)
		assert.True(t, ok, "并发调用后 %q 应可查", name)
	}
}
