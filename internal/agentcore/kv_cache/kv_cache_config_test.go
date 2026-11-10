package kv_cache

import "testing"

// TestKVCacheAffinityConfig_默认关闭 对齐 kv_cache_config.py:31 的 default=False。
func TestKVCacheAffinityConfig_默认关闭(t *testing.T) {
	var c KVCacheAffinityConfig
	if c.EnableKVCacheAffinity {
		t.Fatal("默认必须 false，对齐上游 enable_kv_cache_affinity: Field(default=False)")
	}
}

// TestResolveKVCActionTimeout_四分支 对齐 kv_cache_config.py:16-28。
func TestResolveKVCActionTimeout_四分支(t *testing.T) {
	cases := []struct {
		name, action, target string
		want                 float64
	}{
		{"messages 区间", "evict", "messages", 1.5},
		{"tools 区间", "prefetch", "tools", 1.5},
		{"evict 非区间", "evict", "session", 3.0},
		{"prefetch 非区间", "prefetch", "session", 2.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolveKVCActionTimeout(c.action, c.target, nil); got != c.want {
				t.Errorf("got %v want %v", got, c.want)
			}
		})
	}
	nine := 9.0
	if got := ResolveKVCActionTimeout("evict", "messages", &nine); got != 9.0 {
		t.Errorf("显式 timeout 应优先: got %v", got)
	}
}
