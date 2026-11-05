package tool

import "testing"

// TestToolCard_EffectiveExposure 验证曝光策略的零值与显式值。
func TestToolCard_EffectiveExposure(t *testing.T) {
	card := &ToolCard{}
	if got := card.EffectiveExposure(); got != ToolExposureDirect {
		t.Fatalf("零值应回落 direct，got %s", got)
	}
	card.Exposure = ToolExposureDeferred
	if got := card.EffectiveExposure(); got != ToolExposureDeferred {
		t.Fatalf("应返回 deferred，got %s", got)
	}
}

// TestToolCard_ExposureDeclared 验证三态声明标记。
func TestToolCard_ExposureDeclared(t *testing.T) {
	card := &ToolCard{}
	if _, ok := card.GetExposureDeclared(); ok {
		t.Fatal("初始应为未声明")
	}
	card.SetExposureDeclared(false)
	if value, ok := card.GetExposureDeclared(); !ok || value {
		t.Fatalf("显式声明 false 应可读回，got (%v,%v)", value, ok)
	}
	card.SetExposureDeclared(true)
	if value, ok := card.GetExposureDeclared(); !ok || !value {
		t.Fatalf("显式声明 true 应可读回，got (%v,%v)", value, ok)
	}
}

// TestToolExposure_常量值 验证枚举取值与上游一致。
func TestToolExposure_常量值(t *testing.T) {
	if ToolExposureDirect != "direct" || ToolExposureDeferred != "deferred" {
		t.Fatal("常量取值须与上游一致")
	}
}
