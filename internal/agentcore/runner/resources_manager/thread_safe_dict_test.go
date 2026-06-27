package resources_manager

import (
	"sync"
	"testing"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestThreadSafeDict_基本操作 测试 Set/Get/Delete
func TestThreadSafeDict_基本操作(t *testing.T) {
	d := NewThreadSafeDict[string, int]()
	d.Set("a", 1)
	d.Set("b", 2)

	if v := d.Get("a"); v != 1 {
		t.Errorf("Get(a) = %d, want 1", v)
	}
	if v := d.Get("b"); v != 2 {
		t.Errorf("Get(b) = %d, want 2", v)
	}

	d.Delete("a")
	if d.Contains("a") {
		t.Error("Delete(a) 后 Contains(a) 不应为 true")
	}
}

// TestThreadSafeDict_不存在键 测试 Get 返回零值
func TestThreadSafeDict_不存在键(t *testing.T) {
	d := NewThreadSafeDict[string, int]()
	if v := d.Get("notexist"); v != 0 {
		t.Errorf("Get(notexist) = %d, want 0", v)
	}
}

// TestThreadSafeDict_GetOrSet 测试 GetOrSet
func TestThreadSafeDict_GetOrSet(t *testing.T) {
	d := NewThreadSafeDict[string, int]()
	d.Set("a", 10)

	// 存在时返回已有值
	if v := d.GetOrSet("a", 99); v != 10 {
		t.Errorf("GetOrSet(a, 99) = %d, want 10", v)
	}
	// 不存在时设置默认值
	if v := d.GetOrSet("b", 20); v != 20 {
		t.Errorf("GetOrSet(b, 20) = %d, want 20", v)
	}
	if v := d.Get("b"); v != 20 {
		t.Errorf("Get(b) = %d, want 20", v)
	}
}

// TestThreadSafeDict_GetOrCreate 测试 GetOrCreate
func TestThreadSafeDict_GetOrCreate(t *testing.T) {
	d := NewThreadSafeDict[string, int]()
	d.Set("a", 10)

	// 存在时返回已有值
	if v := d.GetOrCreate("a", func() int { return 99 }); v != 10 {
		t.Errorf("GetOrCreate(a) = %d, want 10", v)
	}
	// 不存在时调用 creator
	if v := d.GetOrCreate("b", func() int { return 30 }); v != 30 {
		t.Errorf("GetOrCreate(b) = %d, want 30", v)
	}
}

// TestThreadSafeDict_Pop 测试 Pop
func TestThreadSafeDict_Pop(t *testing.T) {
	d := NewThreadSafeDict[string, int]()
	d.Set("a", 10)

	if v := d.Pop("a"); v != 10 {
		t.Errorf("Pop(a) = %d, want 10", v)
	}
	if d.Contains("a") {
		t.Error("Pop(a) 后 Contains(a) 不应为 true")
	}
	// 不存在时返回零值
	if v := d.Pop("notexist"); v != 0 {
		t.Errorf("Pop(notexist) = %d, want 0", v)
	}
}

// TestThreadSafeDict_SetDefault 测试 SetDefault
func TestThreadSafeDict_SetDefault(t *testing.T) {
	d := NewThreadSafeDict[string, int]()
	d.Set("a", 10)

	if v := d.SetDefault("a", 99); v != 10 {
		t.Errorf("SetDefault(a, 99) = %d, want 10", v)
	}
	if v := d.SetDefault("b", 20); v != 20 {
		t.Errorf("SetDefault(b, 20) = %d, want 20", v)
	}
}

// TestThreadSafeDict_Update 测试 Update
func TestThreadSafeDict_Update(t *testing.T) {
	d := NewThreadSafeDict[string, int]()
	d.Set("a", 1)
	d.Update(map[string]int{"b": 2, "c": 3})

	if v := d.Get("a"); v != 1 {
		t.Errorf("Get(a) = %d, want 1", v)
	}
	if v := d.Get("b"); v != 2 {
		t.Errorf("Get(b) = %d, want 2", v)
	}
	if v := d.Get("c"); v != 3 {
		t.Errorf("Get(c) = %d, want 3", v)
	}
}

// TestThreadSafeDict_Clear 测试 Clear
func TestThreadSafeDict_Clear(t *testing.T) {
	d := NewThreadSafeDict[string, int]()
	d.Set("a", 1)
	d.Set("b", 2)
	d.Clear()

	if d.Len() != 0 {
		t.Errorf("Len() = %d, want 0", d.Len())
	}
}

// TestThreadSafeDict_KeysValuesItems 测试 Keys/Values/Items
func TestThreadSafeDict_KeysValuesItems(t *testing.T) {
	d := NewThreadSafeDict[string, int]()
	d.Set("a", 1)
	d.Set("b", 2)

	if len(d.Keys()) != 2 {
		t.Errorf("len(Keys()) = %d, want 2", len(d.Keys()))
	}
	if len(d.Values()) != 2 {
		t.Errorf("len(Values()) = %d, want 2", len(d.Values()))
	}
	if len(d.Items()) != 2 {
		t.Errorf("len(Items()) = %d, want 2", len(d.Items()))
	}
}

// TestThreadSafeDict_LenContains 测试 Len 和 Contains
func TestThreadSafeDict_LenContains(t *testing.T) {
	d := NewThreadSafeDict[string, int]()
	if d.Len() != 0 {
		t.Errorf("Len() = %d, want 0", d.Len())
	}
	if d.Contains("a") {
		t.Error("Contains(a) 不应为 true")
	}

	d.Set("a", 1)
	if d.Len() != 1 {
		t.Errorf("Len() = %d, want 1", d.Len())
	}
	if !d.Contains("a") {
		t.Error("Contains(a) 应为 true")
	}
}

// TestThreadSafeDict_并发安全 测试多 goroutine 并发读写
func TestThreadSafeDict_并发安全(t *testing.T) {
	d := NewThreadSafeDict[int, int]()
	var wg sync.WaitGroup
	const n = 100

	// 并发写
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			d.Set(idx, idx*10)
		}(i)
	}
	wg.Wait()

	// 验证所有写入
	if d.Len() != n {
		t.Errorf("Len() = %d, want %d", d.Len(), n)
	}

	// 并发读
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if v := d.Get(idx); v != idx*10 {
				t.Errorf("Get(%d) = %d, want %d", idx, v, idx*10)
			}
		}(i)
	}
	wg.Wait()
}

// TestNewThreadSafeDictWithInitial 测试带初始数据创建
func TestNewThreadSafeDictWithInitial(t *testing.T) {
	d := NewThreadSafeDictWithInitial(map[string]int{"a": 1, "b": 2})
	if d.Len() != 2 {
		t.Errorf("Len() = %d, want 2", d.Len())
	}
	if v := d.Get("a"); v != 1 {
		t.Errorf("Get(a) = %d, want 1", v)
	}
}
