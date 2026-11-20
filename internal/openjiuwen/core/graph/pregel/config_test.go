package pregel

import "testing"

// TestCreateInnerConfig_零值补默认上限 零值 RecursionLimit 视同未设置，补为 MaxRecursiveLimit。
func TestCreateInnerConfig_零值补默认上限(t *testing.T) {
	inner := CreateInnerConfig(PregelConfig{})
	if inner.RecursionLimit != MaxRecursiveLimit {
		t.Errorf("RecursionLimit = %d，期望 %d", inner.RecursionLimit, MaxRecursiveLimit)
	}
}

// TestCreateInnerConfig_显式上限保留 显式正数原样保留。
func TestCreateInnerConfig_显式上限保留(t *testing.T) {
	inner := CreateInnerConfig(PregelConfig{RecursionLimit: 500})
	if inner.RecursionLimit != 500 {
		t.Errorf("RecursionLimit = %d，期望 500", inner.RecursionLimit)
	}
}

// TestCreateInnerConfig_负上限保留 负数在 Python 为真值，不补默认值。
func TestCreateInnerConfig_负上限保留(t *testing.T) {
	inner := CreateInnerConfig(PregelConfig{RecursionLimit: -1})
	if inner.RecursionLimit != -1 {
		t.Errorf("RecursionLimit = %d，期望 -1", inner.RecursionLimit)
	}
}

// TestCreateInnerConfig_不修改入参 对齐 Python create_inner_config 的 config.copy()。
func TestCreateInnerConfig_不修改入参(t *testing.T) {
	cfg := PregelConfig{RecursionLimit: 0}
	_ = CreateInnerConfig(cfg)
	if cfg.RecursionLimit != 0 {
		t.Errorf("入参被修改：RecursionLimit = %d，期望 0", cfg.RecursionLimit)
	}
}

// TestCreateInnerConfig_字段透传 SessionID/Ns 原样带出，ParentNs 保持空。
func TestCreateInnerConfig_字段透传(t *testing.T) {
	inner := CreateInnerConfig(PregelConfig{SessionID: "sess-1", Ns: "wf-1", RecursionLimit: 10})
	if inner.SessionID != "sess-1" {
		t.Errorf("SessionID = %q，期望 \"sess-1\"", inner.SessionID)
	}
	if inner.Ns != "wf-1" {
		t.Errorf("Ns = %q，期望 \"wf-1\"", inner.Ns)
	}
	if inner.ParentNs != "" {
		t.Errorf("ParentNs = %q，期望空串", inner.ParentNs)
	}
}

// TestDefaultPregelConfig_字段值 默认配置仅递归上限非零。
func TestDefaultPregelConfig_字段值(t *testing.T) {
	cfg := DefaultPregelConfig()
	if cfg.RecursionLimit != MaxRecursiveLimit {
		t.Errorf("RecursionLimit = %d，期望 %d", cfg.RecursionLimit, MaxRecursiveLimit)
	}
	if cfg.SessionID != "" {
		t.Errorf("SessionID = %q，期望空串", cfg.SessionID)
	}
	if cfg.Ns != "" {
		t.Errorf("Ns = %q，期望空串", cfg.Ns)
	}
}

// TestDefaultPregelConfig_每次返回独立值 修改一次返回值不得影响下一次调用。
func TestDefaultPregelConfig_每次返回独立值(t *testing.T) {
	first := DefaultPregelConfig()
	first.RecursionLimit = 1
	first.SessionID = "mutated"
	second := DefaultPregelConfig()
	if second.RecursionLimit != MaxRecursiveLimit {
		t.Errorf("第二次 RecursionLimit = %d，期望 %d", second.RecursionLimit, MaxRecursiveLimit)
	}
	if second.SessionID != "" {
		t.Errorf("第二次 SessionID = %q，期望空串", second.SessionID)
	}
}

// TestInnerPregelConfig_嵌入字段提升 嵌入字段可直接访问，且可回转为基础配置。
func TestInnerPregelConfig_嵌入字段提升(t *testing.T) {
	inner := InnerPregelConfig{
		PregelConfig: PregelConfig{SessionID: "sess-2", Ns: "wf-2", RecursionLimit: 7},
		ParentNs:     "wf-2:nodeA:0",
	}
	if inner.SessionID != "sess-2" {
		t.Errorf("提升字段 SessionID = %q，期望 \"sess-2\"", inner.SessionID)
	}
	if inner.ParentNs != "wf-2:nodeA:0" {
		t.Errorf("ParentNs = %q，期望 \"wf-2:nodeA:0\"", inner.ParentNs)
	}
	base := inner.PregelConfig
	if base.Ns != "wf-2" {
		t.Errorf("回转后 Ns = %q，期望 \"wf-2\"", base.Ns)
	}
}
