package flow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph"
)

// TestBranchComponent_AddBranch_三段校验 对齐 branch_comp.py:22-33 校验链
func TestBranchComponent_AddBranch_三段校验(t *testing.T) {
	c := NewBranchComponent()
	require.Error(t, c.AddBranch(nil, []string{"x"}, ""))
	assert.Contains(t, c.AddBranch(nil, []string{"x"}, "").Error(), "condition is None or empty")
	require.Error(t, c.AddBranch("a>1", []string{}, ""))
	assert.Contains(t, c.AddBranch("a>1", []string{}, "").Error(), "target is None or empty")
	require.Error(t, c.AddBranch("a>1", []string{"x", ""}, ""))
	assert.Contains(t, c.AddBranch("a>1", []string{"x", ""}, "").Error(), "empty item at index 1")
	require.NoError(t, c.AddBranch("${a} > 1", "x", "b1"))
	assert.Equal(t, map[string]struct{}{"x": {}}, c.Router().AllTargets())
}

// TestBranchComponent_AddComponent_CNF登记守卫 对齐 branch_comp.py:45-49（len>1 才登记）
func TestBranchComponent_AddComponent_CNF登记守卫(t *testing.T) {
	g := graph.NewPregelGraph()
	c := NewBranchComponent()
	require.NoError(t, c.AddBranch("true", "e1", "b1")) // 单目标：不登记
	c.AddComponent(g, "br", false)
	assert.Empty(t, g.BranchTargets("br"))

	g2 := graph.NewPregelGraph()
	c2 := NewBranchComponent()
	require.NoError(t, c2.AddBranch("true", "e1", "b1"))
	require.NoError(t, c2.AddBranch("false", "e2", "b2"))
	c2.AddComponent(g2, "br", false) // 双目标：登记
	assert.Len(t, g2.BranchTargets("br"), 2)
}

// TestBranchComponent_SkipTrace_恒真 对齐 skip_trace → True
func TestBranchComponent_SkipTrace_恒真(t *testing.T) {
	assert.True(t, NewBranchComponent().SkipTrace())
}

// TestBranchComponent_默认组合面对齐 对齐 WorkflowComponent 嵌入面（ToExecutable 可用）
func TestBranchComponent_默认组合面对齐(t *testing.T) {
	c := NewBranchComponent()
	assert.NotNil(t, c.ToExecutable())
}
