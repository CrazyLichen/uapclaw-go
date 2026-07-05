package interfaces

import "testing"

// TestDeepAgentInterface_CompileCheck 确保 DeepAgentInterface 接口定义可编译
func TestDeepAgentInterface_CompileCheck(t *testing.T) {
	// 此测试仅验证接口定义编译通过
	// 编译时接口检查在实现侧（task_loop、harness）执行
	var _ DeepAgentInterface = (DeepAgentInterface)(nil)
}

// TestLoopCoordinatorInterface_CompileCheck 确保 LoopCoordinatorInterface 接口定义可编译
func TestLoopCoordinatorInterface_CompileCheck(t *testing.T) {
	var _ LoopCoordinatorInterface = (LoopCoordinatorInterface)(nil)
}
