//go:build test

package local

import (
	"context"
	"strings"
	"testing"
	"time"
)

// ──────────────────────────── 测试 ────────────────────────────

// TestInvoke_大输出不丢字节 子进程快速退出且输出超过管道缓冲时，Invoke 必须读满 EOF。
// 回归：曾用 StdoutPipe + 立即并发 cmd.Wait()，Wait 关闭管道导致尾部/全部输出丢失。
func TestInvoke_大输出不丢字节(t *testing.T) {
	want := helperLargeLines * helperLineWidth
	for i := 0; i < 10; i++ {
		handler := NewAsyncProcessHandler(helperCommand(helperModeLarge), defaultChunkSize, "", 30)
		data, err := handler.Invoke(t.Context())
		if err != nil {
			t.Fatalf("第 %d 次 Invoke 失败: %v", i, err)
		}
		if data.Exception != nil {
			t.Fatalf("第 %d 次执行异常: %v（stderr=%q）", i, data.Exception, data.Stderr)
		}
		if len(data.Stdout) != want {
			t.Fatalf("第 %d 次输出字节数 = %d，want %d（尾部丢失）", i, len(data.Stdout), want)
		}
	}
}

// TestInvoke_超长单行不丢字节 单行 200KB 输出不应触发扫描上限、不应超时挂起。
func TestInvoke_超长单行不丢字节(t *testing.T) {
	handler := NewAsyncProcessHandler(helperCommand(helperModeLongLine), defaultChunkSize, "", 10)

	start := time.Now()
	data, err := handler.Invoke(t.Context())
	if err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}
	if data.Exception != nil {
		t.Fatalf("超长单行不应超时/异常: %v", data.Exception)
	}
	if len(data.Stdout) != helperLongLineBytes {
		t.Fatalf("输出字节数 = %d，want %d", len(data.Stdout), helperLongLineBytes)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("超长单行耗时 %s，疑似读取阻塞", elapsed)
	}
}

// TestStream_超长单行可读完 流式读取按块而非按行，单行超长不得截断或报错。
func TestStream_超长单行可读完(t *testing.T) {
	handler := NewAsyncProcessHandler(helperCommand(helperModeLongLine), 4096, "", 10)

	ch, err := handler.Stream(t.Context())
	if err != nil {
		t.Fatalf("Stream 失败: %v", err)
	}
	var collected strings.Builder
	exitCode := -1
	for event := range ch {
		switch event.Type {
		case StreamEventTypeStdout:
			collected.WriteString(event.Data)
		case StreamEventTypeError:
			t.Fatalf("流式读取报错: %s", event.Data)
		case StreamEventTypeExit:
			exitCode = event.ExitCode
		}
	}
	if exitCode != 0 {
		t.Fatalf("exit_code = %d，want 0", exitCode)
	}
	if collected.Len() != helperLongLineBytes {
		t.Fatalf("流式输出字节数 = %d，want %d", collected.Len(), helperLongLineBytes)
	}
}

// TestStream_GBK跨块多字节 半个多字节字符被切到下一块时不能丢字。
func TestStream_GBK跨块多字节(t *testing.T) {
	// chunkSize=3：4 字节的「你好」被切成 3 + 1，第二个字跨块
	handler := NewAsyncProcessHandler(helperCommand("gbk"), 3, "gbk", 10)

	ch, err := handler.Stream(t.Context())
	if err != nil {
		t.Fatalf("Stream 失败: %v", err)
	}
	var collected strings.Builder
	for event := range ch {
		if event.Type == StreamEventTypeStdout {
			collected.WriteString(event.Data)
		}
	}
	if !strings.Contains(collected.String(), "你好") {
		t.Fatalf("跨块 GBK 输出应解码为「你好」，实际: %q", collected.String())
	}
}

// TestChunkDecoder_分块解码 表驱动验证跨块残留与收尾冲出。
func TestChunkDecoder_分块解码(t *testing.T) {
	cases := []struct {
		name     string
		encoding string
		chunks   [][]byte
		want     string
	}{
		{"gbk跨块", "gbk", [][]byte{{0xC4, 0xE3, 0xBA}, {0xC3}}, "你好"},
		{"gbk整块", "gbk", [][]byte{{0xC4, 0xE3}, {0xBA, 0xC3}}, "你好"},
		{"utf8跨块", "utf-8", [][]byte{[]byte("你")[:2], []byte("你")[2:]}, "你"},
		{"ascii直通", "", [][]byte{[]byte("abc"), []byte("def")}, "abcdef"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec := &chunkDecoder{encoding: tc.encoding}
			var got strings.Builder
			for _, chunk := range tc.chunks {
				got.WriteString(dec.decode(chunk))
			}
			got.WriteString(dec.flush())
			if got.String() != tc.want {
				t.Fatalf("解码结果 = %q，want %q", got.String(), tc.want)
			}
		})
	}
}

// TestChunkDecoder_残留字节收尾冲出 流结束时残留的半个字符必须吐出（不静默丢弃）。
//
// Python 的增量解码器 final=True 会把不完整序列解码为替换字符，Go 侧同样交给
// 解码器处理，因此这里只要求「非空」，不锁定具体替换结果。
func TestChunkDecoder_残留字节收尾冲出(t *testing.T) {
	dec := &chunkDecoder{encoding: "gbk"}
	if got := dec.decode([]byte{0xC4}); got != "" {
		t.Fatalf("半个 GBK 字符不应立即输出，实际 %q", got)
	}
	if got := dec.flush(); got == "" {
		t.Fatal("收尾必须吐出残留字节（不得静默丢弃）")
	}
	if got := dec.flush(); got != "" {
		t.Fatalf("收尾后不应再有残留，实际 %q", got)
	}
}

// TestInvoke_超时kill 无输出子进程超时后被终止，返回超时异常。
func TestInvoke_超时kill(t *testing.T) {
	handler := NewAsyncProcessHandler(helperCommand(helperModeHang), defaultChunkSize, "", 1)

	start := time.Now()
	data, err := handler.Invoke(t.Context())
	if err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}
	if data.Exception == nil || !strings.Contains(data.Exception.Error(), "执行超时") {
		t.Fatalf("应返回执行超时异常，实际: %v", data.Exception)
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("超时链路耗时 %s，kill 未生效", elapsed)
	}
}

// TestStream_消费者放弃后收尾 无人消费 + ctx 取消时读者/收尾协程都必须退出。
func TestStream_消费者放弃后收尾(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	// chunkSize 64 → 3200 个事件，足以填满 queue(128)+ch(64)
	handler := NewAsyncProcessHandler(helperCommand(helperModeLarge), 64, "", 10)

	ch, err := handler.Stream(ctx)
	if err != nil {
		t.Fatalf("Stream 失败: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range ch {
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("取消后流未关闭（读者或收尾协程泄漏）")
	}
}

// TestStream_超时且无人消费 缓冲满时超时错误事件走 default 分支，不阻塞收尾。
func TestStream_超时且无人消费(t *testing.T) {
	handler := NewAsyncProcessHandler(helperCommand(helperModeLarge), 256, "", 1)

	ch, err := handler.Stream(t.Context())
	if err != nil {
		t.Fatalf("Stream 失败: %v", err)
	}
	time.Sleep(1500 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range ch {
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("超时后流未关闭")
	}
}

// TestChunkDecoder_边界 空块、未知编码、半个 UTF-8 字符、纯续字节。
func TestChunkDecoder_边界(t *testing.T) {
	empty := &chunkDecoder{encoding: "gbk"}
	if got := empty.decode(nil); got != "" {
		t.Fatalf("空块应返回空串，实际 %q", got)
	}

	unknown := &chunkDecoder{encoding: "shift_jis"}
	if got := unknown.decode([]byte("abc")); got != "abc" {
		t.Fatalf("未知编码直通，实际 %q", got)
	}

	cases := []struct {
		name   string
		chunks [][]byte
		want   string
	}{
		{"4字节半个", [][]byte{{0xF0, 0x9F}, {0x98, 0x80}}, "😀"},
		{"3字节半个", [][]byte{{0xE4, 0xB8}, {0xAD}}, "中"},
		{"2字节半个", [][]byte{{0xC3}, {0xA9}}, "é"},
		{"纯续字节", [][]byte{{0x80}}, "\x80"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec := &chunkDecoder{encoding: "utf-8"}
			var got strings.Builder
			for _, c := range tc.chunks {
				got.WriteString(dec.decode(c))
			}
			got.WriteString(dec.flush())
			if got.String() != tc.want {
				t.Fatalf("解码结果 = %q，want %q", got.String(), tc.want)
			}
		})
	}
}

// TestDecodeStrict_可解析编码 非 GBK 系编码经 ianaindex 解码。
func TestDecodeStrict_可解析编码(t *testing.T) {
	// shift_jis 的 "あ" = 0x82 0xA0
	got, err := decodeStrict([]byte{0x82, 0xA0}, "shift_jis")
	if err != nil {
		t.Fatalf("shift_jis 解码失败: %v", err)
	}
	if got != "あ" {
		t.Fatalf("解码结果 = %q，want あ", got)
	}
}

// TestStream_超时无输出 无输出子进程超时后下发超时错误事件并关闭流。
func TestStream_超时无输出(t *testing.T) {
	handler := NewAsyncProcessHandler(helperCommand(helperModeHang), defaultChunkSize, "", 1)

	ch, err := handler.Stream(t.Context())
	if err != nil {
		t.Fatalf("Stream 失败: %v", err)
	}

	var timeoutEvent bool
	deadline := time.After(10 * time.Second)
	for {
		select {
		case event, ok := <-ch:
			if !ok {
				if !timeoutEvent {
					t.Fatal("应收到超时错误事件")
				}
				return
			}
			if event.Type == StreamEventTypeError && strings.Contains(event.Data, "执行超时") {
				timeoutEvent = true
			}
		case <-deadline:
			t.Fatal("等待超时事件超时")
		}
	}
}
