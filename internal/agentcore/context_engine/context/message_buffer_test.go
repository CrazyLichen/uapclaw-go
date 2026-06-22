package context

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// ──────────────────────────── ContextMessageBuffer 测试 ────────────────────────────

// TestNewContextMessageBuffer 测试创建消息缓冲区
func TestNewContextMessageBuffer(t *testing.T) {
	t.Run("有缓冲区限制", func(t *testing.T) {
		history := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("h1"),
			llm_schema.NewUserMessage("h2"),
			llm_schema.NewUserMessage("h3"),
		}
		buf := NewContextMessageBuffer(history, 2)
		// maxBufferSize=2，history 有 3 条，应截取尾部 2 条
		if buf.Size() != 2 {
			t.Errorf("期望 Size()=2, 实际=%d", buf.Size())
		}
	})

	t.Run("无缓冲区限制", func(t *testing.T) {
		history := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("h1"),
			llm_schema.NewUserMessage("h2"),
		}
		buf := NewContextMessageBuffer(history, 0)
		if buf.Size() != 2 {
			t.Errorf("期望 Size()=2, 实际=%d", buf.Size())
		}
	})

	t.Run("空历史消息", func(t *testing.T) {
		buf := NewContextMessageBuffer(nil, 5)
		if buf.Size() != 0 {
			t.Errorf("期望 Size()=0, 实际=%d", buf.Size())
		}
	})
}

// TestContextMessageBuffer_Size 测试 Size 方法
func TestContextMessageBuffer_Size(t *testing.T) {
	t.Run("maxBufferSize大于实际长度", func(t *testing.T) {
		history := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("a"),
			llm_schema.NewUserMessage("b"),
		}
		buf := NewContextMessageBuffer(history, 10)
		if buf.Size() != 2 {
			t.Errorf("期望 Size()=2, 实际=%d", buf.Size())
		}
	})

	t.Run("maxBufferSize小于实际长度", func(t *testing.T) {
		history := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("a"),
			llm_schema.NewUserMessage("b"),
			llm_schema.NewUserMessage("c"),
		}
		buf := NewContextMessageBuffer(history, 2)
		if buf.Size() != 2 {
			t.Errorf("期望 Size()=2, 实际=%d", buf.Size())
		}
	})

	t.Run("maxBufferSize为零", func(t *testing.T) {
		history := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("a"),
			llm_schema.NewUserMessage("b"),
			llm_schema.NewUserMessage("c"),
		}
		buf := NewContextMessageBuffer(history, 0)
		if buf.Size() != 3 {
			t.Errorf("期望 Size()=3, 实际=%d", buf.Size())
		}
	})
}

// TestContextMessageBuffer_AddBack 测试追加消息
func TestContextMessageBuffer_AddBack(t *testing.T) {
	history := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("h1"),
	}
	buf := NewContextMessageBuffer(history, 0)

	buf.AddBack([]llm_schema.BaseMessage{
		llm_schema.NewUserMessage("c1"),
		llm_schema.NewUserMessage("c2"),
	})
	if buf.Size() != 3 {
		t.Errorf("追加后期望 Size()=3, 实际=%d", buf.Size())
	}
}

// TestContextMessageBuffer_GetBack 测试获取尾部消息
func TestContextMessageBuffer_GetBack(t *testing.T) {
	// 构造缓冲区：2条历史 + 3条上下文，maxBufferSize=0
	history := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("h1"),
		llm_schema.NewUserMessage("h2"),
	}
	buf := NewContextMessageBuffer(history, 0)
	buf.AddBack([]llm_schema.BaseMessage{
		llm_schema.NewUserMessage("c1"),
		llm_schema.NewUserMessage("c2"),
		llm_schema.NewUserMessage("c3"),
	})

	t.Run("size为nil且withHistory为true_返回全部消息", func(t *testing.T) {
		result := buf.GetBack(0, true)
		if len(result) != 5 {
			t.Errorf("期望 5 条消息, 实际=%d", len(result))
		}
	})

	t.Run("size为nil且withHistory为false_仅返回上下文消息", func(t *testing.T) {
		result := buf.GetBack(0, false)
		if len(result) != 3 {
			t.Errorf("期望 3 条上下文消息, 实际=%d", len(result))
		}
		// 验证内容为上下文部分
		if result[0].GetContent().Text() != "c1" {
			t.Errorf("期望第一条内容为 c1, 实际=%s", result[0].GetContent().Text())
		}
	})

	t.Run("size非nil且withHistory为true_返回尾部N条", func(t *testing.T) {
		size := 2
		result := buf.GetBack(size, true)
		if len(result) != 2 {
			t.Errorf("期望 2 条消息, 实际=%d", len(result))
		}
		if result[0].GetContent().Text() != "c2" {
			t.Errorf("期望第一条内容为 c2, 实际=%s", result[0].GetContent().Text())
		}
		if result[1].GetContent().Text() != "c3" {
			t.Errorf("期望第二条内容为 c3, 实际=%s", result[1].GetContent().Text())
		}
	})

	t.Run("size非nil且withHistory为false_返回尾部N条上下文消息", func(t *testing.T) {
		size := 2
		result := buf.GetBack(size, false)
		if len(result) != 2 {
			t.Errorf("期望 2 条上下文消息, 实际=%d", len(result))
		}
		if result[0].GetContent().Text() != "c2" {
			t.Errorf("期望第一条内容为 c2, 实际=%s", result[0].GetContent().Text())
		}
	})

	t.Run("size超出上下文长度且withHistory为false", func(t *testing.T) {
		size := 10
		result := buf.GetBack(size, false)
		if len(result) != 3 {
			t.Errorf("期望 3 条上下文消息, 实际=%d", len(result))
		}
	})

	t.Run("size超出总长度且withHistory为true", func(t *testing.T) {
		size := 100
		result := buf.GetBack(size, true)
		if len(result) != 5 {
			t.Errorf("期望 5 条消息, 实际=%d", len(result))
		}
	})

	t.Run("maxBufferSize限制下的GetBack", func(t *testing.T) {
		// maxBufferSize=3，历史2条+上下文3条=5条，窗口截取后3条
		h := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("h1"),
			llm_schema.NewUserMessage("h2"),
		}
		b := NewContextMessageBuffer(h, 3)
		b.AddBack([]llm_schema.BaseMessage{
			llm_schema.NewUserMessage("c1"),
			llm_schema.NewUserMessage("c2"),
			llm_schema.NewUserMessage("c3"),
		})
		// GetBack(nil, true) 应返回窗口内 3 条（c1, c2, c3）
		result := b.GetBack(0, true)
		if len(result) != 3 {
			t.Errorf("期望 3 条, 实际=%d", len(result))
		}
	})
}

// TestContextMessageBuffer_PopBack 测试弹出尾部消息
func TestContextMessageBuffer_PopBack(t *testing.T) {
	t.Run("从上下文部分弹出", func(t *testing.T) {
		history := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("h1"),
			llm_schema.NewUserMessage("h2"),
		}
		buf := NewContextMessageBuffer(history, 0)
		buf.AddBack([]llm_schema.BaseMessage{
			llm_schema.NewUserMessage("c1"),
			llm_schema.NewUserMessage("c2"),
		})

		popped := buf.PopBack(1, true)
		if len(popped) != 1 {
			t.Fatalf("期望弹出 1 条, 实际=%d", len(popped))
		}
		if popped[0].GetContent().Text() != "c2" {
			t.Errorf("期望弹出内容为 c2, 实际=%s", popped[0].GetContent().Text())
		}
		if buf.Size() != 3 {
			t.Errorf("弹出后期望 Size()=3, 实际=%d", buf.Size())
		}
	})

	t.Run("弹出超过上下文部分_从历史中减少", func(t *testing.T) {
		history := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("h1"),
			llm_schema.NewUserMessage("h2"),
		}
		buf := NewContextMessageBuffer(history, 0)
		buf.AddBack([]llm_schema.BaseMessage{
			llm_schema.NewUserMessage("c1"),
		})
		// 总共 3 条，弹出 2 条（超过上下文 1 条）
		popped := buf.PopBack(2, true)
		if len(popped) != 2 {
			t.Fatalf("期望弹出 2 条, 实际=%d", len(popped))
		}
		if buf.Size() != 1 {
			t.Errorf("弹出后期望 Size()=1, 实际=%d", buf.Size())
		}
	})

	t.Run("withHistory为false仅弹出上下文部分", func(t *testing.T) {
		history := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("h1"),
		}
		buf := NewContextMessageBuffer(history, 0)
		buf.AddBack([]llm_schema.BaseMessage{
			llm_schema.NewUserMessage("c1"),
			llm_schema.NewUserMessage("c2"),
		})

		popped := buf.PopBack(1, false)
		if len(popped) != 1 {
			t.Fatalf("期望弹出 1 条, 实际=%d", len(popped))
		}
		if popped[0].GetContent().Text() != "c2" {
			t.Errorf("期望弹出内容为 c2, 实际=%s", popped[0].GetContent().Text())
		}
		// 历史消息不变
		allMsgs := buf.GetBack(0, true)
		if len(allMsgs) != 2 {
			t.Errorf("期望剩余 2 条, 实际=%d", len(allMsgs))
		}
	})

	t.Run("弹出数量为零", func(t *testing.T) {
		history := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("h1"),
		}
		buf := NewContextMessageBuffer(history, 0)
		popped := buf.PopBack(0, true)
		if popped != nil {
			t.Errorf("期望弹出 nil, 实际=%v", popped)
		}
	})
}

// TestContextMessageBuffer_SetMessages 测试替换消息
func TestContextMessageBuffer_SetMessages(t *testing.T) {
	t.Run("withHistory为true_完全替换", func(t *testing.T) {
		history := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("h1"),
			llm_schema.NewUserMessage("h2"),
		}
		buf := NewContextMessageBuffer(history, 0)

		newMsgs := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("n1"),
			llm_schema.NewUserMessage("n2"),
		}
		buf.SetMessages(newMsgs, true)
		result := buf.GetBack(0, true)
		if len(result) != 2 {
			t.Fatalf("期望 2 条消息, 实际=%d", len(result))
		}
		if result[0].GetContent().Text() != "n1" {
			t.Errorf("期望第一条内容为 n1, 实际=%s", result[0].GetContent().Text())
		}
		// withHistory=true 时 historyMessagesSize 置零
		ctxOnly := buf.GetBack(0, false)
		if len(ctxOnly) != 2 {
			t.Errorf("期望上下文消息 2 条（historyMessagesSize=0）, 实际=%d", len(ctxOnly))
		}
	})

	t.Run("withHistory为false_保留历史前缀", func(t *testing.T) {
		history := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("h1"),
			llm_schema.NewUserMessage("h2"),
		}
		buf := NewContextMessageBuffer(history, 0)

		newCtxMsgs := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("ctx1"),
		}
		buf.SetMessages(newCtxMsgs, false)

		allMsgs := buf.GetBack(0, true)
		if len(allMsgs) != 3 {
			t.Fatalf("期望 3 条消息（2历史+1上下文）, 实际=%d", len(allMsgs))
		}
		if allMsgs[0].GetContent().Text() != "h1" {
			t.Errorf("期望第一条为 h1, 实际=%s", allMsgs[0].GetContent().Text())
		}
		if allMsgs[2].GetContent().Text() != "ctx1" {
			t.Errorf("期望第三条为 ctx1, 实际=%s", allMsgs[2].GetContent().Text())
		}

		// 上下文部分只有新设置的 1 条
		ctxOnly := buf.GetBack(0, false)
		if len(ctxOnly) != 1 {
			t.Errorf("期望上下文消息 1 条, 实际=%d", len(ctxOnly))
		}
	})
}

// TestContextMessageBuffer_Rebuild 测试重建缓冲区
func TestContextMessageBuffer_Rebuild(t *testing.T) {
	t.Run("无maxBufferSize时保留全部", func(t *testing.T) {
		buf := &ContextMessageBuffer{maxBufferSize: 0}
		history := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("a"),
			llm_schema.NewUserMessage("b"),
			llm_schema.NewUserMessage("c"),
		}
		buf.Rebuild(history)
		if buf.Size() != 3 {
			t.Errorf("期望 Size()=3, 实际=%d", buf.Size())
		}
	})

	t.Run("有maxBufferSize时截取尾部", func(t *testing.T) {
		buf := &ContextMessageBuffer{maxBufferSize: 2}
		history := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("a"),
			llm_schema.NewUserMessage("b"),
			llm_schema.NewUserMessage("c"),
		}
		buf.Rebuild(history)
		result := buf.GetBack(0, true)
		if len(result) != 2 {
			t.Fatalf("期望 2 条消息, 实际=%d", len(result))
		}
		// 应截取尾部 2 条：b, c
		if result[0].GetContent().Text() != "b" {
			t.Errorf("期望第一条为 b, 实际=%s", result[0].GetContent().Text())
		}
		if result[1].GetContent().Text() != "c" {
			t.Errorf("期望第二条为 c, 实际=%s", result[1].GetContent().Text())
		}
	})

	t.Run("historyMessagesSize应等于截取后的长度", func(t *testing.T) {
		buf := &ContextMessageBuffer{maxBufferSize: 2}
		history := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("a"),
			llm_schema.NewUserMessage("b"),
			llm_schema.NewUserMessage("c"),
		}
		buf.Rebuild(history)
		// 全部是历史消息，GetBack(nil, false) 应返回空
		ctxOnly := buf.GetBack(0, false)
		if len(ctxOnly) != 0 {
			t.Errorf("期望上下文为空（全部是历史）, 实际=%d", len(ctxOnly))
		}
	})
}

// TestContextMessageBuffer_IfNeedResize 测试自动裁剪
func TestContextMessageBuffer_IfNeedResize(t *testing.T) {
	t.Run("超过2倍maxBufferSize时自动裁剪", func(t *testing.T) {
		history := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("h1"),
			llm_schema.NewUserMessage("h2"),
		}
		// maxBufferSize=2，需要超过 4 条才触发裁剪
		buf := NewContextMessageBuffer(history, 2)

		// 追加 3 条上下文，总共 5 条 > 2*2=4，触发裁剪
		buf.AddBack([]llm_schema.BaseMessage{
			llm_schema.NewUserMessage("c1"),
			llm_schema.NewUserMessage("c2"),
			llm_schema.NewUserMessage("c3"),
		})

		// 裁剪后应丢弃前 maxBufferSize=2 条，剩余 3 条
		if len(buf.contextMessages) != 3 {
			t.Errorf("期望裁剪后剩余 3 条, 实际=%d", len(buf.contextMessages))
		}
		// 裁剪后第一条应为 c1
		if buf.contextMessages[0].GetContent().Text() != "c1" {
			t.Errorf("期望裁剪后第一条为 c1, 实际=%s", buf.contextMessages[0].GetContent().Text())
		}
	})

	t.Run("未超过2倍时不裁剪", func(t *testing.T) {
		history := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("h1"),
			llm_schema.NewUserMessage("h2"),
		}
		buf := NewContextMessageBuffer(history, 10)

		buf.AddBack([]llm_schema.BaseMessage{
			llm_schema.NewUserMessage("c1"),
		})
		// 总共 3 条 < 2*10=20，不裁剪
		if len(buf.contextMessages) != 3 {
			t.Errorf("期望不裁剪，剩余 3 条, 实际=%d", len(buf.contextMessages))
		}
	})

	t.Run("historyMessagesSize调整不小于零", func(t *testing.T) {
		// maxBufferSize=1，history 1 条
		history := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("h1"),
		}
		buf := NewContextMessageBuffer(history, 1)
		// 追加 2 条，总共 3 条 > 2*1=2，触发裁剪
		buf.AddBack([]llm_schema.BaseMessage{
			llm_schema.NewUserMessage("c1"),
			llm_schema.NewUserMessage("c2"),
		})
		// 裁剪丢前 1 条（h1），historyMessagesSize 从 1 减 1 = 0
		if buf.historyMessagesSize != 0 {
			t.Errorf("期望 historyMessagesSize=0, 实际=%d", buf.historyMessagesSize)
		}
	})
}

// ──────────────────────────── OffloadMessageBuffer 测试 ────────────────────────────

// TestNewOffloadMessageBuffer 测试创建卸载消息缓冲区
func TestNewOffloadMessageBuffer(t *testing.T) {
	t.Run("无初始消息", func(t *testing.T) {
		buf := NewOffloadMessageBuffer(nil)
		if buf.GetAll() == nil {
			t.Error("期望初始化为空 map, 不为 nil")
		}
		if len(buf.GetAll()) != 0 {
			t.Errorf("期望空 map, 实际=%d", len(buf.GetAll()))
		}
	})

	t.Run("有初始消息", func(t *testing.T) {
		initMsgs := map[string][]llm_schema.BaseMessage{
			"handle1": {
				llm_schema.NewUserMessage("msg1"),
			},
		}
		buf := NewOffloadMessageBuffer(initMsgs)
		if len(buf.GetAll()) != 1 {
			t.Errorf("期望 1 个 handle, 实际=%d", len(buf.GetAll()))
		}
	})
}

// TestOffloadMessageBuffer_SetSysOperation 测试设置系统操作接口
func TestOffloadMessageBuffer_SetSysOperation(t *testing.T) {
	buf := NewOffloadMessageBuffer(nil)
	op := "test_op"
	buf.SetSysOperation(op)
	if buf.sysOperation != op {
		t.Errorf("期望 sysOperation=%v, 实际=%v", op, buf.sysOperation)
	}
}

// TestOffloadMessageBuffer_SetWorkspaceInfo 测试设置工作空间信息
func TestOffloadMessageBuffer_SetWorkspaceInfo(t *testing.T) {
	buf := NewOffloadMessageBuffer(nil)
	buf.SetWorkspaceInfo("/tmp/workspace", "session123")
	if buf.workspaceDir != "/tmp/workspace" {
		t.Errorf("期望 workspaceDir=/tmp/workspace, 实际=%s", buf.workspaceDir)
	}
	if buf.sessionID != "session123" {
		t.Errorf("期望 sessionID=session123, 实际=%s", buf.sessionID)
	}
}

// TestOffloadMessageBuffer_Offload 测试卸载消息
func TestOffloadMessageBuffer_Offload(t *testing.T) {
	buf := NewOffloadMessageBuffer(nil)

	msgs := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("offload1"),
		llm_schema.NewUserMessage("offload2"),
	}
	buf.Offload("handle1", "in_memory", msgs)

	all := buf.GetAll()
	if len(all) != 1 {
		t.Fatalf("期望 1 个 handle, 实际=%d", len(all))
	}
	if len(all["handle1"]) != 2 {
		t.Errorf("期望 2 条消息, 实际=%d", len(all["handle1"]))
	}
}

// TestOffloadMessageBuffer_Reload 测试重新加载消息
func TestOffloadMessageBuffer_Reload(t *testing.T) {
	t.Run("从内存重新加载", func(t *testing.T) {
		buf := NewOffloadMessageBuffer(nil)
		msgs := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("r1"),
			llm_schema.NewUserMessage("r2"),
		}
		buf.Offload("handle1", "in_memory", msgs)

		loaded := buf.Reload("handle1", "in_memory")
		if len(loaded) != 2 {
			t.Fatalf("期望 2 条消息, 实际=%d", len(loaded))
		}
		if loaded[0].GetContent().Text() != "r1" {
			t.Errorf("期望第一条内容为 r1, 实际=%s", loaded[0].GetContent().Text())
		}
	})

	t.Run("不存在的handle返回nil", func(t *testing.T) {
		buf := NewOffloadMessageBuffer(nil)
		loaded := buf.Reload("nonexistent", "in_memory")
		if loaded != nil {
			t.Errorf("期望 nil, 实际=%v", loaded)
		}
	})

	t.Run("从文件系统重新加载", func(t *testing.T) {
		tmpDir := t.TempDir()
		sessionDir := filepath.Join(tmpDir, "session1")
		if err := os.MkdirAll(sessionDir, 0o755); err != nil {
			t.Fatalf("创建目录失败: %v", err)
		}

		// 构造 JSON 文件：[]BaseMessage 的序列化格式
		msg := llm_schema.NewUserMessage("test")
		msgJSON, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("序列化消息失败: %v", err)
		}
		// 包装为数组
		arrayJSON, err := json.Marshal([]json.RawMessage{msgJSON})
		if err != nil {
			t.Fatalf("序列化消息数组失败: %v", err)
		}

		filePath := filepath.Join(sessionDir, "myhandle.json")
		if err := os.WriteFile(filePath, arrayJSON, 0o644); err != nil {
			t.Fatalf("写入文件失败: %v", err)
		}

		buf := NewOffloadMessageBuffer(nil)
		buf.SetWorkspaceInfo(tmpDir, "session1")

		loaded := buf.Reload("myhandle", "filesystem")
		if len(loaded) != 1 {
			t.Fatalf("期望 1 条消息, 实际=%d", len(loaded))
		}
		if loaded[0].GetContent().Text() != "test" {
			t.Errorf("期望内容为 test, 实际=%s", loaded[0].GetContent().Text())
		}
	})
}

// TestOffloadMessageBuffer_Clear 测试清除卸载消息
func TestOffloadMessageBuffer_Clear(t *testing.T) {
	buf := NewOffloadMessageBuffer(nil)
	msgs := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("m1"),
	}
	buf.Offload("handle1", "in_memory", msgs)
	buf.Offload("handle2", "in_memory", msgs)

	buf.Clear("handle1", "in_memory")
	all := buf.GetAll()
	if _, exists := all["handle1"]; exists {
		t.Error("期望 handle1 已被清除")
	}
	if _, exists := all["handle2"]; !exists {
		t.Error("期望 handle2 仍然存在")
	}
}

// TestOffloadMessageBuffer_GetAll 测试获取全部卸载消息
func TestOffloadMessageBuffer_GetAll(t *testing.T) {
	buf := NewOffloadMessageBuffer(nil)
	buf.Offload("h1", "in_memory", []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("a"),
	})
	buf.Offload("h2", "in_memory", []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("b"),
		llm_schema.NewUserMessage("c"),
	})

	all := buf.GetAll()
	if len(all) != 2 {
		t.Errorf("期望 2 个 handle, 实际=%d", len(all))
	}
	if len(all["h1"]) != 1 {
		t.Errorf("期望 h1 有 1 条消息, 实际=%d", len(all["h1"]))
	}
	if len(all["h2"]) != 2 {
		t.Errorf("期望 h2 有 2 条消息, 实际=%d", len(all["h2"]))
	}
}

// ──────────────────────────── filesystemReloadPaths 测试 ────────────────────────────

// TestFilesystemReloadPaths_无workspaceDir 测试无工作空间目录时返回 handle 本身
func TestFilesystemReloadPaths_无workspaceDir(t *testing.T) {
	buf := NewOffloadMessageBuffer(nil)
	paths := buf.filesystemReloadPaths("myhandle")
	if len(paths) != 1 || paths[0] != "myhandle" {
		t.Errorf("无 workspaceDir 时应返回 [myhandle]，实际 %v", paths)
	}
}

// TestFilesystemReloadPaths_有workspaceDir 测试有工作空间目录时构建路径
func TestFilesystemReloadPaths_有workspaceDir(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "sess1")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}

	// 创建匹配的文件
	filePath := filepath.Join(sessionDir, "myhandle.json")
	if err := os.WriteFile(filePath, []byte("[]"), 0644); err != nil {
		t.Fatalf("创建文件失败: %v", err)
	}
	// 创建额外匹配文件
	extraPath := filepath.Join(sessionDir, "myhandle_extra.json")
	if err := os.WriteFile(extraPath, []byte("[]"), 0644); err != nil {
		t.Fatalf("创建额外文件失败: %v", err)
	}

	buf := NewOffloadMessageBuffer(nil)
	buf.SetWorkspaceInfo(tmpDir, "sess1")
	paths := buf.filesystemReloadPaths("myhandle")

	if len(paths) < 2 {
		t.Errorf("期望至少 2 个路径，实际 %d", len(paths))
	}
	// 精确路径应排在最前
	expectedPath := filepath.Join(sessionDir, "myhandle.json")
	if paths[0] != expectedPath {
		t.Errorf("精确路径应排在最前，期望 %q，实际 %q", expectedPath, paths[0])
	}
}

// ──────────────────────────── reloadFromFilesystem 边界测试 ────────────────────────────

// TestReloadFromFilesystem_文件不存在 测试文件不存在时返回 nil
func TestReloadFromFilesystem_文件不存在(t *testing.T) {
	tmpDir := t.TempDir()
	buf := NewOffloadMessageBuffer(nil)
	buf.SetWorkspaceInfo(tmpDir, "nonexistent-session")
	result := buf.Reload("nonexistent-handle", "filesystem")
	if result != nil {
		t.Errorf("文件不存在应返回 nil，实际 %v", result)
	}
}

// TestReloadFromFilesystem_有效JSON文件 测试读取有效 JSON 文件
func TestReloadFromFilesystem_有效JSON文件(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "sess1")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}

	// 构造有效的消息 JSON
	msg := llm_schema.NewUserMessage("test message")
	msgJSON, _ := json.Marshal(msg)
	arrayJSON, _ := json.Marshal([]json.RawMessage{msgJSON})

	filePath := filepath.Join(sessionDir, "myhandle.json")
	if err := os.WriteFile(filePath, arrayJSON, 0644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}

	buf := NewOffloadMessageBuffer(nil)
	buf.SetWorkspaceInfo(tmpDir, "sess1")
	result := buf.Reload("myhandle", "filesystem")
	if len(result) != 1 {
		t.Fatalf("期望 1 条消息，实际 %d", len(result))
	}
	if result[0].GetContent().Text() != "test message" {
		t.Errorf("期望内容 'test message'，实际 '%s'", result[0].GetContent().Text())
	}
}

// ──────────────────────────── ContextMessageBuffer 边界测试 ────────────────────────────

// TestContextMessageBuffer_GetBack_contextStart超出长度 测试 historyMessagesSize 超出消息长度
func TestContextMessageBuffer_GetBack_contextStart超出长度(t *testing.T) {
	buf := &ContextMessageBuffer{
		maxBufferSize:       0,
		contextMessages:     []llm_schema.BaseMessage{llm_schema.NewUserMessage("a")},
		historyMessagesSize: 5, // 超出实际长度
	}
	result := buf.GetBack(0, false)
	if result != nil {
		t.Errorf("contextStart 超出长度时应返回 nil，实际 %v", result)
	}
}

// TestContextMessageBuffer_GetBack_size为负数 测试 size ≤ 0 表示不限制，返回全部消息
func TestContextMessageBuffer_GetBack_size为负数(t *testing.T) {
	buf := NewContextMessageBuffer([]llm_schema.BaseMessage{llm_schema.NewUserMessage("a")}, 0)
	result := buf.GetBack(-1, true)
	if len(result) != 1 {
		t.Errorf("size ≤ 0 时应返回全部消息，实际长度 %d", len(result))
	}
}

// TestContextMessageBuffer_GetBack_withHistoryFalse_size超出 测试 withHistory=false 且 size 超出上下文
func TestContextMessageBuffer_GetBack_withHistoryFalse_size超出(t *testing.T) {
	buf := NewContextMessageBuffer([]llm_schema.BaseMessage{llm_schema.NewUserMessage("h1")}, 0)
	buf.AddBack([]llm_schema.BaseMessage{llm_schema.NewUserMessage("c1")})
	size := 100
	result := buf.GetBack(size, false)
	if len(result) != 1 {
		t.Errorf("期望 1 条上下文消息，实际 %d", len(result))
	}
}

// TestContextMessageBuffer_SetMessages_withHistoryFalse历史截断 测试 withHistory=false 时历史前缀正确截断
func TestContextMessageBuffer_SetMessages_withHistoryFalse历史截断(t *testing.T) {
	buf := &ContextMessageBuffer{
		maxBufferSize:       0,
		contextMessages:     []llm_schema.BaseMessage{llm_schema.NewUserMessage("a")},
		historyMessagesSize: 3, // 超出实际长度，应截取全部
	}
	newMsgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("b")}
	buf.SetMessages(newMsgs, false)
	// 历史部分应截取 min(len, historyMessagesSize)
	allMsgs := buf.GetBack(0, true)
	if len(allMsgs) != 2 { // 1 (history) + 1 (new)
		t.Errorf("期望 2 条消息，实际 %d", len(allMsgs))
	}
}
