package adapter

import (
	"sync"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
)

// TestDeepAdapter_StateConcurrentPublishAndRead 并发发布模型/配置与读取，
// 验证不会出现 concurrent map iteration and map write（配 -race 运行）。
func TestDeepAdapter_StateConcurrentPublishAndRead(t *testing.T) {
	d := NewDeepAdapter()
	d.defaultModelConfigReader = nil // 关掉兜底读盘，聚焦内存状态

	makeCache := func(tag string) (map[string]*llm.Model, map[string][]string) {
		cache := map[string]*llm.Model{
			"m":   nil,
			tag:   nil,
			"m#0": nil,
		}
		return cache, map[string][]string{"m": {"m#0"}}
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// 写侧：模拟 reload 反复整体替换
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			cache, nameToKeys := makeCache("m")
			d.publishModelState(nil, cache, nameToKeys, "m")
			d.setConfigSnapshots(
				map[string]any{"iteration": i},
				map[string]any{"models": map[string]any{"default": map[string]any{"model_client_config": map[string]any{"api_key": "k", "api_base": "https://api.example.com"}}}},
			)
			i++
		}
	}()

	// 读侧：请求路径的三类读取
	for n := 0; n < 4; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = d.hasValidModelConfig("m")
				_, _, _ = d.modelsSnapshot()
				_ = d.configCacheSnapshot()
				_ = d.configBaseSnapshot()
				_ = d.modelSnapshot()
				_ = d.defaultModelNameSnapshot()
			}
		}()
	}

	// 跑一小段时间后停止
	for i := 0; i < 2000; i++ {
		_ = d.hasValidModelConfig("m")
	}
	close(stop)
	wg.Wait()
}

// TestDeepAdapter_ActiveSessionsConcurrent 并发 mark/unmark 与遍历，
// 验证计数表不再出现并发 map 读写。
func TestDeepAdapter_ActiveSessionsConcurrent(t *testing.T) {
	d := NewDeepAdapter()

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for n := 0; n < 4; n++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sid := string(rune('a' + id))
			i := 0
			for {
				select {
				case <-stop:
					return
				default:
				}
				d.markSessionActive(sid)
				d.unmarkSessionActive(sid)
				i++
			}
		}(n)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = d.otherActiveSessions("a")
			_ = d.isSessionActive("a")
			_ = d.activeSessionIDSnapshot()
		}
	}()

	for i := 0; i < 5000; i++ {
		_ = d.otherActiveSessions("b")
	}
	close(stop)
	wg.Wait()

	if got := d.otherActiveSessions(""); got != 0 {
		t.Errorf("全部 unmark 后活跃数应为 0，实际 %d", got)
	}
}
