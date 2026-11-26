package flow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/visualization"
)

// TestBranchRouterDrawable_env开启 D-19-1 回填①②③：env 开启 → 构造期创建投影、
// add_branch 填充、GetDrawableBranchTargets 返回（branch_router.py:47-72）
func TestBranchRouterDrawable_env开启(t *testing.T) {
	t.Setenv("WORKFLOW_DRAWABLE", "true")
	r := NewBranchRouter(false)
	require.NoError(t, r.AddBranch("a > 1", []string{"x"}, "b1"))
	require.NoError(t, r.AddBranch("b > 1", []string{"y"}, "b2"))
	targets, datas := r.GetDrawableBranchTargets()
	// branch_router.py:59-64：branch_data=branch_id（str 条件时用条件串）
	assert.Equal(t, []string{"x", "y"}, targets)
	assert.Equal(t, []string{"a > 1", "b > 1"}, datas)
}

// TestBranchRouterDrawable_env开启_无branchID branch_id 空时 branch_data=""（:59）
func TestBranchRouterDrawable_env开启_无branchID(t *testing.T) {
	t.Setenv("WORKFLOW_DRAWABLE", "true")
	r := NewBranchRouter(false)
	require.NoError(t, r.AddBranch("a > 1", []string{"x"}, ""))
	_, datas := r.GetDrawableBranchTargets()
	assert.Equal(t, []string{"a > 1"}, datas) // str 条件覆盖
}

// TestBranchRouterDrawable_env开启_非str条件 非字符串条件时 branch_data=branch_id
func TestBranchRouterDrawable_env开启_非str条件(t *testing.T) {
	t.Setenv("WORKFLOW_DRAWABLE", "true")
	r := NewBranchRouter(false)
	require.NoError(t, r.AddBranch(func() bool { return true }, []string{"x"}, "bid"))
	_, datas := r.GetDrawableBranchTargets()
	assert.Equal(t, []string{"bid"}, datas)
}

// TestBranchRouterDrawable_env关闭 env 关闭 → 投影 nil（Python get_drawable_branch_router None）
func TestBranchRouterDrawable_env关闭(t *testing.T) {
	t.Setenv("WORKFLOW_DRAWABLE", "")
	r := NewBranchRouter(false)
	require.NoError(t, r.AddBranch("a > 1", []string{"x"}, "b1"))
	targets, datas := r.GetDrawableBranchTargets()
	assert.Nil(t, targets)
	assert.Nil(t, datas)
}

// TestBranchRouter编译期断言 D-13-5 接口满足
func TestBranchRouter编译期断言(t *testing.T) {
	var _ visualization.DrawableBranchTargetsSource = NewBranchRouter(false)
}
