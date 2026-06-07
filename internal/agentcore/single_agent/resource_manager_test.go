package single_agent

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNoopResourceManager_GetTool(t *testing.T) {
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

func TestNoopResourceManager_GetWorkflow(t *testing.T) {
	mgr := &NoopResourceManager{}
	_, err := mgr.GetWorkflow("test_wf")
	if err == nil {
		t.Fatal("应返回错误")
	}
}

func TestNoopResourceManager_GetAgent(t *testing.T) {
	mgr := &NoopResourceManager{}
	_, err := mgr.GetAgent("test_agent")
	if err == nil {
		t.Fatal("应返回错误")
	}
}

func TestNoopResourceManager_GetMcpToolInfos(t *testing.T) {
	mgr := &NoopResourceManager{}
	infos, err := mgr.GetMcpToolInfos("server1")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if infos != nil {
		t.Errorf("应返回 nil，实际 %v", infos)
	}
}

func TestNewResourceOptions(t *testing.T) {
	opts := NewResourceOptions(
		WithResourceTag("my_tag"),
	)
	if opts.Tag != "my_tag" {
		t.Errorf("Tag = %q, want my_tag", opts.Tag)
	}
}
