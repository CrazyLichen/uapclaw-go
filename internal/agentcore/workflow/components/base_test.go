package components

import "testing"

// TestComponentAbility_NameDesc 四成员的 Name/Desc 与 Python 元组全量对齐
// Python: components/base.py INVOKE=("invoke","batch in, batch out") 等
func TestComponentAbility_NameDesc(t *testing.T) {
	cases := []struct {
		ability ComponentAbility
		name    string
		desc    string
	}{
		{ComponentAbilityInvoke, "invoke", "batch in, batch out"},
		{ComponentAbilityStream, "stream", "batch in, stream out"},
		{ComponentAbilityCollect, "collect", "stream in, batch out"},
		{ComponentAbilityTransform, "transform", "stream in, stream out"},
	}
	for _, c := range cases {
		if got := c.ability.Name(); got != c.name {
			t.Errorf("Name() = %q, want %q", got, c.name)
		}
		if got := c.ability.Desc(); got != c.desc {
			t.Errorf("Desc() = %q, want %q", got, c.desc)
		}
	}
}

// TestComponentAbility_未知值 未知成员返回空串（Go 无 Enum ValueError 对应物，空串承载）
func TestComponentAbility_未知值(t *testing.T) {
	var unknown ComponentAbility = 999
	if got := unknown.Name(); got != "" {
		t.Errorf("未知值 Name() = %q, want 空", got)
	}
	if got := unknown.Desc(); got != "" {
		t.Errorf("未知值 Desc() = %q, want 空", got)
	}
}
