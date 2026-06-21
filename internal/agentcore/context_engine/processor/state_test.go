package processor

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSaveState_默认返回空map 验证默认 SaveState
func TestSaveState_默认返回空map(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	state := p.SaveState()
	if state == nil {
		t.Error("SaveState() 不应返回 nil")
	}
	if len(state) != 0 {
		t.Errorf("默认 SaveState 应返回空 map，实际 %d 项", len(state))
	}
}

// TestLoadState_默认空操作 验证默认 LoadState 不会 panic
func TestLoadState_默认空操作(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	// 不应 panic
	p.LoadState(map[string]any{"key": "value"})
}

// TestSaveState_LoadState_往返 验证默认实现的保存/恢复往返
func TestSaveState_LoadState_往返(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	state := p.SaveState()
	p2 := NewBaseProcessor(c)
	p2.LoadState(state)
	state2 := p2.SaveState()
	if len(state2) != 0 {
		t.Errorf("往返后 SaveState 应仍为空 map，实际 %d 项", len(state2))
	}
}
