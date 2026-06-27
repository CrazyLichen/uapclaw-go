package resources_manager

import (
	"sort"
	"strings"
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewTagMgr_初始化 测试 NewTagMgr 初始化
func TestNewTagMgr_初始化(t *testing.T) {
	mgr := NewTagMgr()
	if mgr == nil {
		t.Fatal("NewTagMgr 返回 nil")
	}
	// TagGlobal 应存在但为空集合
	if !mgr.HasTag(TagGlobal) {
		t.Error("TagGlobal 标签应存在")
	}
	resources := mgr.GetTagResources(TagGlobal)
	if len(resources) != 0 {
		t.Errorf("TagGlobal 资源集合应为空，实际 %d", len(resources))
	}
}

// TestTagResource_正常添加 测试 TagResource 正常添加标签
func TestTagResource_正常添加(t *testing.T) {
	mgr := NewTagMgr()
	tags := mgr.TagResource("res1", []Tag{"tag1", "tag2"})

	if len(tags) != 2 {
		t.Fatalf("期望 2 个标签，实际 %d", len(tags))
	}
	sort.Strings(tags)
	if tags[0] != "tag1" || tags[1] != "tag2" {
		t.Errorf("标签不匹配: %v", tags)
	}

	if !mgr.HasResource("res1") {
		t.Error("资源 res1 应存在")
	}
	if !mgr.HasResourceTag("res1", "tag1") {
		t.Error("资源 res1 应有 tag1")
	}
	if !mgr.HasResourceTag("res1", "tag2") {
		t.Error("资源 res1 应有 tag2")
	}
}

// TestTagResource_GLOBAL特殊逻辑 测试 GLOBAL 资源不能有其他标签
func TestTagResource_GLOBAL特殊逻辑(t *testing.T) {
	mgr := NewTagMgr()

	// 先添加普通标签
	mgr.TagResource("res1", []Tag{"tag1", "tag2"})

	// 再添加 GLOBAL 标签，应覆盖为只有 GLOBAL
	tags := mgr.TagResource("res1", []Tag{TagGlobal})
	if len(tags) != 1 || tags[0] != TagGlobal {
		t.Errorf("期望只有 GLOBAL 标签，实际: %v", tags)
	}

	// 确认不再有其他标签
	if mgr.HasResourceTag("res1", "tag1") {
		t.Error("GLOBAL 资源不应有 tag1")
	}
	if mgr.HasResourceTag("res1", "tag2") {
		t.Error("GLOBAL 资源不应有 tag2")
	}

	// GLOBAL 资源不能再添加其他标签
	tags2 := mgr.TagResource("res1", []Tag{"tag3"})
	if len(tags2) != 1 || tags2[0] != TagGlobal {
		t.Errorf("GLOBAL 资源添加其他标签应返回 [GLOBAL]，实际: %v", tags2)
	}
}

// TestTagResource_GLOBAL和其他标签同时添加 测试同时添加 GLOBAL 和其他标签
func TestTagResource_GLOBAL和其他标签同时添加(t *testing.T) {
	mgr := NewTagMgr()
	tags := mgr.TagResource("res1", []Tag{TagGlobal, "tag1"})
	// GLOBAL 优先，结果只有 GLOBAL
	if len(tags) != 1 || tags[0] != TagGlobal {
		t.Errorf("期望只有 GLOBAL 标签，实际: %v", tags)
	}
}

// TestRemoveResource 测试 RemoveResource
func TestRemoveResource(t *testing.T) {
	mgr := NewTagMgr()
	mgr.TagResource("res1", []Tag{"tag1", "tag2"})

	removed := mgr.RemoveResource("res1")
	if len(removed) != 2 {
		t.Fatalf("期望移除 2 个标签，实际 %d", len(removed))
	}

	if mgr.HasResource("res1") {
		t.Error("资源 res1 应已移除")
	}
	if mgr.HasTag("tag1") {
		t.Error("标签 tag1 应已移除（无其他资源引用）")
	}

	// 移除不存在的资源返回空
	removed2 := mgr.RemoveResource("nonexistent")
	if len(removed2) != 0 {
		t.Errorf("移除不存在的资源应返回空，实际: %v", removed2)
	}
}

// TestRemoveResourceTags_正常移除 测试 RemoveResourceTags 正常移除
func TestRemoveResourceTags_正常移除(t *testing.T) {
	mgr := NewTagMgr()
	mgr.TagResource("res1", []Tag{"tag1", "tag2", "tag3"})

	remaining, err := mgr.RemoveResourceTags("res1", []Tag{"tag1", "tag3"}, false)
	if err != nil {
		t.Fatalf("移除标签失败: %v", err)
	}
	if len(remaining) != 1 || remaining[0] != "tag2" {
		t.Errorf("期望剩余 [tag2]，实际: %v", remaining)
	}
}

// TestRemoveResourceTags_skipIfNotExists 测试 RemoveResourceTags 的 skipIfNotExists 参数
func TestRemoveResourceTags_skipIfNotExists(t *testing.T) {
	mgr := NewTagMgr()
	mgr.TagResource("res1", []Tag{"tag1"})

	// skipIfNotExists=false，移除不存在的标签应报错
	_, err := mgr.RemoveResourceTags("res1", []Tag{"tag_nonexistent"}, false)
	if err == nil {
		t.Error("skipIfNotExists=false 时移除不存在的标签应报错")
	}

	// skipIfNotExists=true，移除不存在的标签不报错
	remaining, err := mgr.RemoveResourceTags("res1", []Tag{"tag_nonexistent"}, true)
	if err != nil {
		t.Fatalf("skipIfNotExists=true 时不应报错: %v", err)
	}
	if len(remaining) != 1 || remaining[0] != "tag1" {
		t.Errorf("期望剩余 [tag1]，实际: %v", remaining)
	}
}

// TestRemoveResourceTags_资源不存在 测试 RemoveResourceTags 资源不存在时报错
func TestRemoveResourceTags_资源不存在(t *testing.T) {
	mgr := NewTagMgr()
	_, err := mgr.RemoveResourceTags("nonexistent", []Tag{"tag1"}, false)
	if err == nil {
		t.Error("资源不存在时应报错")
	}
}

// TestRemoveResourceTags_移除全部标签后资源被删除 测试移除全部标签后资源自动删除
func TestRemoveResourceTags_移除全部标签后资源被删除(t *testing.T) {
	mgr := NewTagMgr()
	mgr.TagResource("res1", []Tag{"tag1"})

	remaining, err := mgr.RemoveResourceTags("res1", []Tag{"tag1"}, false)
	if err != nil {
		t.Fatalf("移除标签失败: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("期望无剩余标签，实际: %v", remaining)
	}
	if mgr.HasResource("res1") {
		t.Error("资源标签全部移除后，资源应被自动删除")
	}
}

// TestUpdateResourceTags_MERGE 测试 UpdateResourceTags MERGE 策略
func TestUpdateResourceTags_MERGE(t *testing.T) {
	mgr := NewTagMgr()
	mgr.TagResource("res1", []Tag{"tag1"})

	tags, err := mgr.UpdateResourceTags("res1", []Tag{"tag2"}, TagUpdateMerge)
	if err != nil {
		t.Fatalf("MERGE 更新失败: %v", err)
	}
	sort.Strings(tags)
	if len(tags) != 2 || tags[0] != "tag1" || tags[1] != "tag2" {
		t.Errorf("期望 [tag1, tag2]，实际: %v", tags)
	}
}

// TestUpdateResourceTags_REPLACE 测试 UpdateResourceTags REPLACE 策略
func TestUpdateResourceTags_REPLACE(t *testing.T) {
	mgr := NewTagMgr()
	mgr.TagResource("res1", []Tag{"tag1", "tag2"})

	tags, err := mgr.UpdateResourceTags("res1", []Tag{"tag3"}, TagUpdateReplace)
	if err != nil {
		t.Fatalf("REPLACE 更新失败: %v", err)
	}
	if len(tags) != 1 || tags[0] != "tag3" {
		t.Errorf("期望 [tag3]，实际: %v", tags)
	}
	if mgr.HasResourceTag("res1", "tag1") {
		t.Error("REPLACE 后不应有 tag1")
	}
}

// TestUpdateResourceTags_资源不存在 测试 UpdateResourceTags 资源不存在时报错
func TestUpdateResourceTags_资源不存在(t *testing.T) {
	mgr := NewTagMgr()
	_, err := mgr.UpdateResourceTags("nonexistent", []Tag{"tag1"}, TagUpdateMerge)
	if err == nil {
		t.Error("资源不存在时应报错")
	}
}

// TestUpdateResourceTags_GLOBAL 测试 UpdateResourceTags GLOBAL 特殊逻辑
func TestUpdateResourceTags_GLOBAL(t *testing.T) {
	mgr := NewTagMgr()
	mgr.TagResource("res1", []Tag{"tag1", "tag2"})

	tags, err := mgr.UpdateResourceTags("res1", []Tag{TagGlobal}, TagUpdateMerge)
	if err != nil {
		t.Fatalf("GLOBAL 更新失败: %v", err)
	}
	if len(tags) != 1 || tags[0] != TagGlobal {
		t.Errorf("期望 [GLOBAL]，实际: %v", tags)
	}
}

// TestRemoveTag_正常移除 测试 RemoveTag 正常移除
func TestRemoveTag_正常移除(t *testing.T) {
	mgr := NewTagMgr()
	mgr.TagResource("res1", []Tag{"tag1"})
	mgr.TagResource("res2", []Tag{"tag1"})

	affected, err := mgr.RemoveTag("tag1", false)
	if err != nil {
		t.Fatalf("移除标签失败: %v", err)
	}
	sort.Strings(affected)
	if len(affected) != 2 {
		t.Errorf("期望 2 个受影响资源，实际: %v", affected)
	}

	if mgr.HasTag("tag1") {
		t.Error("标签 tag1 应已移除")
	}
}

// TestRemoveTag_skipIfNotExists 测试 RemoveTag 的 skipIfNotExists 参数
func TestRemoveTag_skipIfNotExists(t *testing.T) {
	mgr := NewTagMgr()

	// skipIfNotExists=false，移除不存在的标签应报错
	_, err := mgr.RemoveTag("nonexistent", false)
	if err == nil {
		t.Error("skipIfNotExists=false 时移除不存在的标签应报错")
	}

	// skipIfNotExists=true，移除不存在的标签不报错
	affected, err := mgr.RemoveTag("nonexistent", true)
	if err != nil {
		t.Fatalf("skipIfNotExists=true 时不应报错: %v", err)
	}
	if len(affected) != 0 {
		t.Errorf("期望无受影响资源，实际: %v", affected)
	}
}

// TestRemoveTag_移除后资源无标签被删除 测试移除标签后资源无标签时自动删除
func TestRemoveTag_移除后资源无标签被删除(t *testing.T) {
	mgr := NewTagMgr()
	mgr.TagResource("res1", []Tag{"tag1"})

	affected, err := mgr.RemoveTag("tag1", false)
	if err != nil {
		t.Fatalf("移除标签失败: %v", err)
	}
	if len(affected) != 1 || affected[0] != "res1" {
		t.Errorf("期望受影响资源 [res1]，实际: %v", affected)
	}
	// 资源的唯一标签被移除，资源应被删除
	if mgr.HasResource("res1") {
		t.Error("资源无标签后应被自动删除")
	}
}

// TestFindResourcesByTags_ALL 测试 FindResourcesByTags ALL 策略
func TestFindResourcesByTags_ALL(t *testing.T) {
	mgr := NewTagMgr()
	mgr.TagResource("res1", []Tag{"tag1", "tag2"})
	mgr.TagResource("res2", []Tag{"tag1"})
	mgr.TagResource("res3", []Tag{"tag1", "tag2"})

	found, err := mgr.FindResourcesByTags([]Tag{"tag1", "tag2"}, TagMatchAll, true)
	if err != nil {
		t.Fatalf("查找失败: %v", err)
	}
	sort.Strings(found)
	if len(found) != 2 {
		t.Errorf("期望 2 个资源 (res1, res3)，实际: %v", found)
	}
}

// TestFindResourcesByTags_ANY 测试 FindResourcesByTags ANY 策略
func TestFindResourcesByTags_ANY(t *testing.T) {
	mgr := NewTagMgr()
	mgr.TagResource("res1", []Tag{"tag1"})
	mgr.TagResource("res2", []Tag{"tag2"})
	mgr.TagResource("res3", []Tag{"tag3"})

	found, err := mgr.FindResourcesByTags([]Tag{"tag1", "tag2"}, TagMatchAny, true)
	if err != nil {
		t.Fatalf("查找失败: %v", err)
	}
	sort.Strings(found)
	if len(found) != 2 {
		t.Errorf("期望 2 个资源 (res1, res2)，实际: %v", found)
	}
}

// TestFindResourcesByTags_标签不存在ALL 测试 ALL 策略标签不存在时的行为
func TestFindResourcesByTags_标签不存在ALL(t *testing.T) {
	mgr := NewTagMgr()
	mgr.TagResource("res1", []Tag{"tag1"})

	// skipIfNotExists=true，不报错，返回空
	found, err := mgr.FindResourcesByTags([]Tag{"tag1", "nonexistent"}, TagMatchAll, true)
	if err != nil {
		t.Fatalf("skipIfNotExists=true 时不应报错: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("不存在的标签 ALL 策略应返回空，实际: %v", found)
	}

	// skipIfNotExists=false，非内置标签不存在时报错
	_, err = mgr.FindResourcesByTags([]Tag{"tag1", "nonexistent"}, TagMatchAll, false)
	if err == nil {
		t.Error("skipIfNotExists=false 且非内置标签不存在时应报错")
	}
}

// TestFindResourcesByTags_标签不存在ANY 测试 ANY 策略标签不存在时的行为
func TestFindResourcesByTags_标签不存在ANY(t *testing.T) {
	mgr := NewTagMgr()
	mgr.TagResource("res1", []Tag{"tag1"})

	// skipIfNotExists=false，非内置标签不存在时报错
	_, err := mgr.FindResourcesByTags([]Tag{"tag1", "nonexistent"}, TagMatchAny, false)
	if err == nil {
		t.Error("skipIfNotExists=false 且非内置标签不存在时应报错")
	}

	// skipIfNotExists=true，不报错
	found, err := mgr.FindResourcesByTags([]Tag{"tag1", "nonexistent"}, TagMatchAny, true)
	if err != nil {
		t.Fatalf("skipIfNotExists=true 时不应报错: %v", err)
	}
	if len(found) != 1 {
		t.Errorf("期望找到 1 个资源，实际: %v", found)
	}
}

// TestFindResourcesByTags_GLOBAL内置标签不报错 测试 GLOBAL 内置标签不存在时不报错
func TestFindResourcesByTags_GLOBAL内置标签不报错(t *testing.T) {
	mgr := NewTagMgr()
	// 不添加任何资源，TagGlobal 已存在但为空

	// ALL 策略 + GLOBAL（内置标签，skipIfNotExists=false 也不报错）
	found, err := mgr.FindResourcesByTags([]Tag{TagGlobal}, TagMatchAll, false)
	if err != nil {
		t.Fatalf("GLOBAL 是内置标签，不应报错: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("GLOBAL 无资源时应返回空，实际: %v", found)
	}
}

// TestHasResourceTag 测试 HasResourceTag
func TestHasResourceTag(t *testing.T) {
	mgr := NewTagMgr()
	mgr.TagResource("res1", []Tag{"tag1"})

	if !mgr.HasResourceTag("res1", "tag1") {
		t.Error("res1 应有 tag1")
	}
	if mgr.HasResourceTag("res1", "tag2") {
		t.Error("res1 不应有 tag2")
	}
	if mgr.HasResourceTag("nonexistent", "tag1") {
		t.Error("不存在的资源不应有标签")
	}
}

// TestGetResourcesTags 测试 GetResourcesTags
func TestGetResourcesTags(t *testing.T) {
	mgr := NewTagMgr()
	mgr.TagResource("res1", []Tag{"tag1", "tag2"})

	tags := mgr.GetResourcesTags("res1")
	if len(tags) != 2 {
		t.Fatalf("期望 2 个标签，实际 %d", len(tags))
	}
	sort.Strings(tags)
	if tags[0] != "tag1" || tags[1] != "tag2" {
		t.Errorf("标签不匹配: %v", tags)
	}

	// 不存在的资源返回空
	tags2 := mgr.GetResourcesTags("nonexistent")
	if len(tags2) != 0 {
		t.Errorf("不存在的资源应返回空标签，实际: %v", tags2)
	}
}

// TestGetTagResources 测试 GetTagResources
func TestGetTagResources(t *testing.T) {
	mgr := NewTagMgr()
	mgr.TagResource("res1", []Tag{"tag1"})
	mgr.TagResource("res2", []Tag{"tag1"})

	resources := mgr.GetTagResources("tag1")
	if len(resources) != 2 {
		t.Fatalf("期望 2 个资源，实际 %d", len(resources))
	}
	sort.Strings(resources)
	if resources[0] != "res1" || resources[1] != "res2" {
		t.Errorf("资源不匹配: %v", resources)
	}

	// 不存在的标签返回空
	resources2 := mgr.GetTagResources("nonexistent")
	if len(resources2) != 0 {
		t.Errorf("不存在的标签应返回空，实际: %v", resources2)
	}
}

// TestDisplay_不panic 测试 Display 不 panic
func TestDisplay_不panic(t *testing.T) {
	mgr := NewTagMgr()
	mgr.TagResource("res1", []Tag{"tag1", "tag2"})
	mgr.TagResource("res2", []Tag{TagGlobal})

	// 不 panic 即通过
	msg := mgr.Display(false)
	if msg == "" {
		t.Error("Display 应返回非空字符串")
	}
	if !strings.Contains(msg, "Statistics") {
		t.Error("Display 输出应包含 Statistics")
	}
	if !strings.Contains(msg, "tag1") {
		t.Error("Display 输出应包含 tag1")
	}
}

// TestListTags 测试 ListTags
func TestListTags(t *testing.T) {
	mgr := NewTagMgr()
	// 初始状态，TagGlobal 存在但为空集合，ListTags 应排除
	tags := mgr.ListTags()
	if len(tags) != 0 {
		t.Errorf("初始状态 ListTags 应为空（空标签被排除），实际: %v", tags)
	}

	mgr.TagResource("res1", []Tag{"tag1", "tag2"})
	tags = mgr.ListTags()
	// tag1, tag2 有资源，TagGlobal 为空
	sort.Strings(tags)
	if len(tags) != 2 {
		t.Errorf("期望 [tag1, tag2]，实际: %v", tags)
	}

	// 添加 GLOBAL 资源后，TagGlobal 也应出现在 ListTags
	mgr.TagResource("res2", []Tag{TagGlobal})
	tags = mgr.ListTags()
	sort.Strings(tags)
	if len(tags) != 3 {
		t.Errorf("期望 3 个标签 (tag1, tag2, GLOBAL)，实际: %v", tags)
	}
}

// TestHasTag 测试 HasTag
func TestHasTag(t *testing.T) {
	mgr := NewTagMgr()
	if !mgr.HasTag(TagGlobal) {
		t.Error("TagGlobal 应始终存在")
	}
	if mgr.HasTag("custom") {
		t.Error("未添加的标签不应存在")
	}

	mgr.TagResource("res1", []Tag{"custom"})
	if !mgr.HasTag("custom") {
		t.Error("添加后的标签应存在")
	}
}

// TestHasResource 测试 HasResource
func TestHasResource(t *testing.T) {
	mgr := NewTagMgr()
	if mgr.HasResource("res1") {
		t.Error("未添加的资源不应存在")
	}

	mgr.TagResource("res1", []Tag{"tag1"})
	if !mgr.HasResource("res1") {
		t.Error("添加后的资源应存在")
	}
}

// TestReplaceResourceTags_旧标签被清理 测试 REPLACE 策略下旧标签在 tagToResource 中被正确清理
func TestReplaceResourceTags_旧标签被清理(t *testing.T) {
	mgr := NewTagMgr()
	mgr.TagResource("res1", []Tag{"tag1"})

	// tag1 只有 res1 引用，REPLACE 为 tag2 后 tag1 应被移除
	_, err := mgr.UpdateResourceTags("res1", []Tag{"tag2"}, TagUpdateReplace)
	if err != nil {
		t.Fatalf("REPLACE 更新失败: %v", err)
	}
	if mgr.HasTag("tag1") {
		t.Error("tag1 无其他资源引用时应被移除")
	}
	if !mgr.HasTag("tag2") {
		t.Error("tag2 应存在")
	}
}

// TestTagMgr_多资源共享标签 测试多资源共享同一标签的移除逻辑
func TestTagMgr_多资源共享标签(t *testing.T) {
	mgr := NewTagMgr()
	mgr.TagResource("res1", []Tag{"shared"})
	mgr.TagResource("res2", []Tag{"shared"})

	// 移除 res1，shared 标签仍被 res2 引用
	mgr.RemoveResource("res1")
	if !mgr.HasTag("shared") {
		t.Error("shared 仍被 res2 引用，不应被移除")
	}
	if mgr.HasResourceTag("res1", "shared") {
		t.Error("res1 应已移除")
	}

	// 移除 res2，shared 标签应被移除
	mgr.RemoveResource("res2")
	if mgr.HasTag("shared") {
		t.Error("shared 无资源引用，应被移除")
	}
}

// TestUpdateResourceTags_不支持的策略 测试不支持的更新策略
func TestUpdateResourceTags_不支持的策略(t *testing.T) {
	mgr := NewTagMgr()
	mgr.TagResource("res1", []Tag{"tag1"})

	_, err := mgr.UpdateResourceTags("res1", []Tag{"tag2"}, TagUpdateStrategy(99))
	if err == nil {
		t.Error("不支持的策略应报错")
	}
}

// TestFindResourcesByTags_不支持的策略 测试不支持的匹配策略
func TestFindResourcesByTags_不支持的策略(t *testing.T) {
	mgr := NewTagMgr()
	mgr.TagResource("res1", []Tag{"tag1"})

	_, err := mgr.FindResourcesByTags([]Tag{"tag1"}, TagMatchStrategy(99), true)
	if err == nil {
		t.Error("不支持的策略应报错")
	}
}

// TestRemoveResourceTags_GLOBAL标签移除 测试移除 GLOBAL 标签
func TestRemoveResourceTags_GLOBAL标签移除(t *testing.T) {
	mgr := NewTagMgr()
	mgr.TagResource("res1", []Tag{TagGlobal})

	// 移除 GLOBAL 标签后资源无标签，应被删除
	remaining, err := mgr.RemoveResourceTags("res1", []Tag{TagGlobal}, false)
	if err != nil {
		t.Fatalf("移除 GLOBAL 标签失败: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("期望无剩余标签，实际: %v", remaining)
	}
	// GLOBAL 标签本身不应被删除（是内置标签）
	if !mgr.HasTag(TagGlobal) {
		t.Error("GLOBAL 标签不应被删除")
	}
}

// TestNormalizeTags 测试 normalizeTags 归一化
func TestNormalizeTags(t *testing.T) {
	result := normalizeTags([]Tag{"a", "b", "a"})
	if len(result) != 2 {
		t.Errorf("期望 2 个标签（去重），实际 %d", len(result))
	}
	if _, ok := result["a"]; !ok {
		t.Error("应包含 a")
	}
	if _, ok := result["b"]; !ok {
		t.Error("应包含 b")
	}
}

// TestIsBuiltinTag 测试 isBuiltinTag
func TestIsBuiltinTag(t *testing.T) {
	if !isBuiltinTag(TagGlobal) {
		t.Error("TagGlobal 应为内置标签")
	}
	if isBuiltinTag("custom") {
		t.Error("custom 不应为内置标签")
	}
	if isBuiltinTag(TagAll) {
		t.Error("TagAll 不应为内置标签")
	}
}

// TestTagUpdateStrategy_String 测试 TagUpdateStrategy.String()
func TestTagUpdateStrategy_String(t *testing.T) {
	if TagUpdateMerge.String() != "MERGE" {
		t.Errorf("TagUpdateMerge.String() = %s, 期望 MERGE", TagUpdateMerge.String())
	}
	if TagUpdateReplace.String() != "REPLACE" {
		t.Errorf("TagUpdateReplace.String() = %s, 期望 REPLACE", TagUpdateReplace.String())
	}
}

// TestTagMatchStrategy_String 测试 TagMatchStrategy.String()
func TestTagMatchStrategy_String(t *testing.T) {
	if TagMatchAll.String() != "ALL" {
		t.Errorf("TagMatchAll.String() = %s, 期望 ALL", TagMatchAll.String())
	}
	if TagMatchAny.String() != "ANY" {
		t.Errorf("TagMatchAny.String() = %s, 期望 ANY", TagMatchAny.String())
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
