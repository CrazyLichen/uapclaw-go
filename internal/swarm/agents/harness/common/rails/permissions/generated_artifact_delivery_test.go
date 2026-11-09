package permissions

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// ──────────────────────────── NormalizeSendFileTargetChannels ────────────────────────────

func TestNormalizeSendFileTargetChannels_JSON数组(t *testing.T) {
	got := NormalizeSendFileTargetChannels(`["web","cli"]`)
	want := []string{"cli", "web"} // Python sorted(set(...))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNormalizeSendFileTargetChannels_单字符串(t *testing.T) {
	got := NormalizeSendFileTargetChannels("web")
	if len(got) != 1 || got[0] != "web" {
		t.Errorf("got %v", got)
	}
}

func TestNormalizeSendFileTargetChannels_去重排序(t *testing.T) {
	got := NormalizeSendFileTargetChannels([]any{"web", "cli", "web"})
	want := []string{"cli", "web"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNormalizeSendFileTargetChannels_空与nil(t *testing.T) {
	if got := NormalizeSendFileTargetChannels(nil); len(got) != 0 {
		t.Errorf("nil -> %v", got)
	}
	if got := NormalizeSendFileTargetChannels("   "); len(got) != 0 {
		t.Errorf("blank -> %v", got)
	}
	if got := NormalizeSendFileTargetChannels(""); len(got) != 0 {
		t.Errorf("empty -> %v", got)
	}
}

func TestNormalizeSendFileTargetChannels_非字符串元素(t *testing.T) {
	got := NormalizeSendFileTargetChannels([]any{1, 2})
	want := []string{"1", "2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNormalizeSendFileTargetChannels_非法JSON当单值(t *testing.T) {
	got := NormalizeSendFileTargetChannels("not-json{")
	if len(got) != 1 || got[0] != "not-json{" {
		t.Errorf("got %v", got)
	}
}

func TestNormalizeSendFileTargetChannels_空白元素被过滤(t *testing.T) {
	got := NormalizeSendFileTargetChannels([]any{"web", "  ", ""})
	if len(got) != 1 || got[0] != "web" {
		t.Errorf("got %v", got)
	}
}

// ──────────────────────────── NormalizeSendFilePaths ────────────────────────────

func TestNormalizeSendFilePaths_字符串JSON(t *testing.T) {
	got := NormalizeSendFilePaths(`["/a/b.txt","/c/d.txt"]`)
	want := []string{"/a/b.txt", "/c/d.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNormalizeSendFilePaths_单字符串(t *testing.T) {
	got := NormalizeSendFilePaths("/a/b.txt")
	if len(got) != 1 || got[0] != "/a/b.txt" {
		t.Errorf("got %v", got)
	}
}

func TestNormalizeSendFilePaths_有序保序不去重(t *testing.T) {
	got := NormalizeSendFilePaths([]any{"/z.txt", "/a.txt", "/z.txt"})
	want := []string{"/z.txt", "/a.txt", "/z.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v（不应去重或排序）", got, want)
	}
}

func TestNormalizeSendFilePaths_空与nil(t *testing.T) {
	if got := NormalizeSendFilePaths(nil); len(got) != 0 {
		t.Errorf("nil -> %v", got)
	}
	if got := NormalizeSendFilePaths("  "); len(got) != 0 {
		t.Errorf("blank -> %v", got)
	}
}

func TestNormalizeSendFilePaths_空白元素过滤(t *testing.T) {
	got := NormalizeSendFilePaths([]any{"/a.txt", "", "  ", "/b.txt"})
	want := []string{"/a.txt", "/b.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNormalizeSendFilePaths_非法JSON当单值(t *testing.T) {
	got := NormalizeSendFilePaths("/a/not-json{")
	if len(got) != 1 {
		t.Errorf("got %v", got)
	}
}

// ──────────────────────────── CreateSendFileExecutionGrant ────────────────────────────

func TestCreateSendFileExecutionGrant_路径解析(t *testing.T) {
	dir := t.TempDir()
	got := CreateSendFileExecutionGrant([]string{filepath.Join(dir, "a.txt")}, nil)
	if len(got.Items) != 1 {
		t.Fatalf("items = %v", got.Items)
	}
	if !filepath.IsAbs(got.Items[0].ResolvedPath) {
		t.Errorf("resolved_path should be absolute: %q", got.Items[0].ResolvedPath)
	}
}

func TestCreateSendFileExecutionGrant_空路径报错(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("empty paths should raise send_file_authorization_missing_path")
		}
	}()
	CreateSendFileExecutionGrant(nil, nil)
}

func TestCreateSendFileExecutionGrant_目标渠道(t *testing.T) {
	dir := t.TempDir()
	got := CreateSendFileExecutionGrant([]string{filepath.Join(dir, "a.txt")}, []any{"web", "cli"})
	want := []string{"cli", "web"}
	if !reflect.DeepEqual(got.TargetChannels, want) {
		t.Errorf("got %v, want %v", got.TargetChannels, want)
	}
}

func TestCreateSendFileExecutionGrant_nil目标渠道为空(t *testing.T) {
	dir := t.TempDir()
	got := CreateSendFileExecutionGrant([]string{filepath.Join(dir, "a.txt")}, nil)
	if len(got.TargetChannels) != 0 {
		t.Errorf("got %v, want empty", got.TargetChannels)
	}
}

// ──────────────────────────── Publish / Current / Clear / Consume ────────────────────────────

func TestPublishSendFileExecutionGrant_发布后可读(t *testing.T) {
	dir := t.TempDir()
	grant := CreateSendFileExecutionGrant([]string{filepath.Join(dir, "a.txt")}, nil)
	ctx := PublishSendFileExecutionGrant(context.Background(), grant)

	got := CurrentSendFileExecutionGrant(ctx)
	if got == nil {
		t.Fatal("grant should be readable after publish")
	}
	if len(got.Items) != 1 {
		t.Errorf("items = %v", got.Items)
	}
}

func TestPublishSendFileExecutionGrant_覆盖旧值(t *testing.T) {
	dir := t.TempDir()
	first := CreateSendFileExecutionGrant([]string{filepath.Join(dir, "a.txt")}, nil)
	second := CreateSendFileExecutionGrant([]string{filepath.Join(dir, "b.txt")}, nil)

	ctx := PublishSendFileExecutionGrant(context.Background(), first)
	ctx = PublishSendFileExecutionGrant(ctx, second)

	got := CurrentSendFileExecutionGrant(ctx)
	if got == nil || got.Items[0].ResolvedPath != second.Items[0].ResolvedPath {
		t.Errorf("should publish the second grant, got %+v", got)
	}
}

func TestCurrentSendFileExecutionGrant_未发布为nil(t *testing.T) {
	if got := CurrentSendFileExecutionGrant(context.Background()); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestClearSendFileExecutionGrant_清除后不可读(t *testing.T) {
	dir := t.TempDir()
	grant := CreateSendFileExecutionGrant([]string{filepath.Join(dir, "a.txt")}, nil)
	ctx := PublishSendFileExecutionGrant(context.Background(), grant)
	ctx = ClearSendFileExecutionGrant(ctx)

	if got := CurrentSendFileExecutionGrant(ctx); got != nil {
		t.Errorf("cleared grant should be nil, got %+v", got)
	}
}

func TestClearSendFileExecutionGrant_旧ctx的lease被标记终态(t *testing.T) {
	// Python: clear 会 set(None) 并把旧 lease.terminal = True，
	// 因此持有旧 ctx 的分支也读不到 grant（共享终态）。
	dir := t.TempDir()
	grant := CreateSendFileExecutionGrant([]string{filepath.Join(dir, "a.txt")}, nil)
	oldCtx := PublishSendFileExecutionGrant(context.Background(), grant)
	_ = ClearSendFileExecutionGrant(oldCtx)

	if got := CurrentSendFileExecutionGrant(oldCtx); got != nil {
		t.Errorf("old ctx lease should be terminal, got %+v", got)
	}
}

func TestConsumeSendFileExecutionGrant_正常消费(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	grant := CreateSendFileExecutionGrant([]string{p}, nil)
	ctx := PublishSendFileExecutionGrant(context.Background(), grant)

	items, err := ConsumeSendFileExecutionGrant(ctx, []string{p}, nil)
	if err != nil {
		t.Fatalf("consume failed: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("items = %v", items)
	}
}

func TestConsumeSendFileExecutionGrant_二次消费失败(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	grant := CreateSendFileExecutionGrant([]string{p}, nil)
	ctx := PublishSendFileExecutionGrant(context.Background(), grant)

	if _, err := ConsumeSendFileExecutionGrant(ctx, []string{p}, nil); err != nil {
		t.Fatalf("first consume should succeed: %v", err)
	}
	_, err := ConsumeSendFileExecutionGrant(ctx, []string{p}, nil)
	if err == nil {
		t.Fatal("second consume should fail")
	}
	if err.Error() != "send_file_execution_grant_missing" {
		t.Errorf("err = %v", err)
	}
}

func TestConsumeSendFileExecutionGrant_未发布失败(t *testing.T) {
	_, err := ConsumeSendFileExecutionGrant(context.Background(), []string{"/a.txt"}, nil)
	if err == nil || err.Error() != "send_file_execution_grant_missing" {
		t.Errorf("err = %v", err)
	}
}

func TestConsumeSendFileExecutionGrant_路径不匹配失败(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	grant := CreateSendFileExecutionGrant([]string{a}, nil)
	ctx := PublishSendFileExecutionGrant(context.Background(), grant)

	_, err := ConsumeSendFileExecutionGrant(ctx, []string{b}, nil)
	if err == nil || err.Error() != "send_file_execution_grant_mismatch" {
		t.Errorf("err = %v", err)
	}
}

func TestConsumeSendFileExecutionGrant_渠道不匹配失败(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	grant := CreateSendFileExecutionGrant([]string{p}, []any{"web"})
	ctx := PublishSendFileExecutionGrant(context.Background(), grant)

	_, err := ConsumeSendFileExecutionGrant(ctx, []string{p}, []any{"cli"})
	if err == nil || err.Error() != "send_file_execution_grant_mismatch" {
		t.Errorf("err = %v", err)
	}
}

func TestConsumeSendFileExecutionGrant_消费后不可再读(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	grant := CreateSendFileExecutionGrant([]string{p}, nil)
	ctx := PublishSendFileExecutionGrant(context.Background(), grant)
	_, _ = ConsumeSendFileExecutionGrant(ctx, []string{p}, nil)

	if got := CurrentSendFileExecutionGrant(ctx); got != nil {
		t.Errorf("after consume should be nil, got %+v", got)
	}
}

func TestPublishSendFileExecutionGrant_nil不panic(t *testing.T) {
	// Python 会 TypeError；Go 侧登记为不校验（值类型无 nil 概念）
	ctx := PublishSendFileExecutionGrant(context.Background(), SendFileExecutionGrant{})
	if got := CurrentSendFileExecutionGrant(ctx); got == nil {
		t.Error("empty grant should still be readable")
	}
}

// ──────────────────────────── 补充：私有分支 ────────────────────────────

func TestNormalizeSendFileCandidates_各类型(t *testing.T) {
	// []string 直接返回
	if got := normalizeSendFileCandidates([]string{"a", "b"}); len(got) != 2 {
		t.Errorf("[]string -> %v", got)
	}
	// 非字符串非数组 → 单值
	if got := normalizeSendFileCandidates(42); len(got) != 1 || got[0] != "42" {
		t.Errorf("int -> %v", got)
	}
	// JSON 解析成对象 → 单值 str(dict)
	if got := normalizeSendFileCandidates(`{"k":"v"}`); len(got) != 1 {
		t.Errorf("object -> %v", got)
	}
}

func TestAnyToStringValue_nil与数字(t *testing.T) {
	if got := anyToStringValue(nil); got != "" {
		t.Errorf("nil -> %q", got)
	}
	if got := anyToStringValue(7); got != "7" {
		t.Errorf("int -> %q", got)
	}
	if got := anyToStringValue("s"); got != "s" {
		t.Errorf("string -> %q", got)
	}
}

func TestResolveSendFilePath_波浪线(t *testing.T) {
	got := resolveSendFilePath("~/a.txt")
	if strings.HasPrefix(got, "~") {
		t.Errorf("~/ should expand, got %q", got)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("should be absolute: %q", got)
	}
}

func TestSendFileGrantEqual_不等分支(t *testing.T) {
	base := SendFileExecutionGrant{
		Items:          []SendFileAuthorizationItem{{ResolvedPath: "/a"}},
		TargetChannels: []string{"web"},
	}
	if !sendFileGrantEqual(base, base) {
		t.Error("identical grants should be equal")
	}
	// items 长度不同
	if sendFileGrantEqual(base, SendFileExecutionGrant{TargetChannels: []string{"web"}}) {
		t.Error("different item count should not be equal")
	}
	// items 内容不同
	other := SendFileExecutionGrant{
		Items:          []SendFileAuthorizationItem{{ResolvedPath: "/b"}},
		TargetChannels: []string{"web"},
	}
	if sendFileGrantEqual(base, other) {
		t.Error("different path should not be equal")
	}
	// channels 内容不同
	otherChan := SendFileExecutionGrant{
		Items:          []SendFileAuthorizationItem{{ResolvedPath: "/a"}},
		TargetChannels: []string{"cli"},
	}
	if sendFileGrantEqual(base, otherChan) {
		t.Error("different channel should not be equal")
	}
}

func TestLeaseFromCtx_显式nil(t *testing.T) {
	dir := t.TempDir()
	grant := CreateSendFileExecutionGrant([]string{filepath.Join(dir, "a.txt")}, nil)
	ctx := PublishSendFileExecutionGrant(context.Background(), grant)
	ctx = ClearSendFileExecutionGrant(ctx)
	if leaseFromCtx(ctx) != nil {
		t.Error("explicit nil lease should read back as nil")
	}
}
