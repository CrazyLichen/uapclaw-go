package ability

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// newExposureCard 构造带名称的测试卡片。
func newExposureCard(name string) *tool.ToolCard {
	card := &tool.ToolCard{}
	card.Name = name
	return card
}

// TestApplyToolExposurePolicy_四分支 验证注册期曝光策略的四个分支。
func TestApplyToolExposurePolicy_四分支(t *testing.T) {
	am := NewAbilityManager(nil)

	// ① 未开启渐进 → 全 direct
	card := newExposureCard("x")
	if got := am.applyToolExposurePolicy(card); got != tool.ToolExposureDirect {
		t.Fatalf("关闭渐进应 direct，got %s", got)
	}

	// ② 开启渐进 → 非名单 deferred
	am.SetToolExposurePolicy(true, []string{"read_file"})
	card = newExposureCard("xlsx_read")
	if got := am.applyToolExposurePolicy(card); got != tool.ToolExposureDeferred {
		t.Fatalf("开启渐进且不在名单应 deferred，got %s", got)
	}
	if value, ok := card.GetExposureDeclared(); !ok || !value {
		t.Fatal("解析后应锁定声明")
	}

	// ③ 名单内 → direct
	card = newExposureCard("read_file")
	if got := am.applyToolExposurePolicy(card); got != tool.ToolExposureDirect {
		t.Fatalf("名单内应 direct，got %s", got)
	}

	// ④ 已声明卡片不被改写
	declared := newExposureCard("y")
	declared.Exposure = tool.ToolExposureDirect
	declared.SetExposureDeclared(true)
	if got := am.applyToolExposurePolicy(declared); got != tool.ToolExposureDirect {
		t.Fatalf("已声明应保持 direct，got %s", got)
	}
}

// TestAbilityManager_RegistryRevision 验证注册版本推进规则。
func TestAbilityManager_RegistryRevision(t *testing.T) {
	am := NewAbilityManager(nil)
	base := am.RegistryRevision()

	am.Add(newExposureCard("a"))
	if am.RegistryRevision() != base+1 {
		t.Fatalf("新增工具应 +1，got %d", am.RegistryRevision())
	}

	// 上游既有行为：移除普通工具不涨版本
	revisionAfterAdd := am.RegistryRevision()
	am.Remove("a")
	if am.RegistryRevision() != revisionAfterAdd {
		t.Fatalf("移除普通工具不应涨版本，got %d", am.RegistryRevision())
	}
}

// TestSetToolExposurePolicy_nil名单不覆盖 验证 nil 名单保持原值。
func TestSetToolExposurePolicy_nil名单不覆盖(t *testing.T) {
	am := NewAbilityManager(nil)
	if _, ok := am.directToolNames["tool_search"]; !ok {
		t.Fatal("默认名单应含 tool_search")
	}
	am.SetToolExposurePolicy(true, nil)
	if _, ok := am.directToolNames["tool_search"]; !ok {
		t.Fatal("nil 名单不应覆盖默认值")
	}
}
