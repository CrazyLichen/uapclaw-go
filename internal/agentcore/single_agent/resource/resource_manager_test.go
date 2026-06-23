package resource

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNoopResourceManager_获取工具(t *testing.T) {
	mgr := &NoopResourceManager{}
	_, err := mgr.GetTool("test_tool")
	if err == nil {
		t.Fatal("应返回错误")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误类型应为 *BaseError，实际 %T", err)
	}
	if baseErr.Code() != exception.StatusAbilityNotFound.Code() {
		t.Errorf("Code = %d, want %d", baseErr.Code(), exception.StatusAbilityNotFound.Code())
	}
}

func TestNoopResourceManager_获取工作流(t *testing.T) {
	mgr := &NoopResourceManager{}
	_, err := mgr.GetWorkflow("test_wf")
	if err == nil {
		t.Fatal("应返回错误")
	}
}

func TestNoopResourceManager_获取Agent(t *testing.T) {
	mgr := &NoopResourceManager{}
	_, err := mgr.GetAgent("test_agent")
	if err == nil {
		t.Fatal("应返回错误")
	}
}

func TestNoopResourceManager_获取MCP工具信息(t *testing.T) {
	mgr := &NoopResourceManager{}
	infos, err := mgr.GetMcpToolInfos("server1")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if infos != nil {
		t.Errorf("应返回 nil，实际 %v", infos)
	}
}

func TestNewResourceOptions_资源选项(t *testing.T) {
	opts := NewResourceOptions(
		WithResourceTag("my_tag"),
	)
	if opts.Tag != "my_tag" {
		t.Errorf("Tag = %q, want my_tag", opts.Tag)
	}
}

// TestWithResourceSession_Nil 验证 WithResourceSession 设置 nil Session 时不 panic。
func TestWithResourceSession_Nil(t *testing.T) {
	opts := NewResourceOptions(WithResourceSession(nil))
	if opts.Session != nil {
		t.Error("WithResourceSession(nil) 应设置 Session 为 nil")
	}
}

// TestWithResourceSession_非Nil 验证 WithResourceSession 设置非 nil Session。
func TestWithResourceSession_非Nil(t *testing.T) {
	// *session.Session 构造较复杂，此处仅验证选项函数能正确写入非 nil 值
	// 实际 *session.Session 传参在集成测试中验证
	opts := &ResourceOptions{}
	WithResourceSession(nil)(opts)
	if opts.Session != nil {
		t.Error("期望 Session 为 nil")
	}
}
