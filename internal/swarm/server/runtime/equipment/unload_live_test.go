package equipment

import "testing"

// fakeUnloadAdapter 注入缝测试桩。
type fakeUnloadAdapter struct {
	calls    []string
	failKind string
}

func (f *fakeUnloadAdapter) UnloadEquipmentIfLoaded(kind, packageID string) error {
	call := kind + "/" + packageID
	f.calls = append(f.calls, call)
	if kind == f.failKind {
		return &PackageConflictError{Message: "unload boom"}
	}
	return nil
}

func TestUnloadLive_注入缝(t *testing.T) {
	fake := &fakeUnloadAdapter{}
	SetUnloadLiveAdapter(fake)
	t.Cleanup(func() { SetUnloadLiveAdapter(nil) })
	fn := NewUnloadLiveFunc()
	if err := fn("agent_templates", "pkg_x"); err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 1 || fake.calls[0] != "agent_templates/pkg_x" {
		t.Fatalf("调用未达: %v", fake.calls)
	}
	// 失败上抛（uninstall 中止语义）
	fake.failKind = "agent_templates"
	if err := fn("agent_templates", "boom"); err == nil {
		t.Fatal("卸载失败应上抛")
	}
}

func TestUnloadLive_未注入NoOp(t *testing.T) {
	SetUnloadLiveAdapter(nil)
	fn := NewUnloadLiveFunc()
	if err := fn("agent_templates", "whatever"); err != nil {
		t.Fatalf("未注入应 no-op: %v", err)
	}
}
