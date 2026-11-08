package prompts

import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
)

// TestUnseenChangeNotes_区间过滤 测试只返回 observed < revision <= current 的条目
//
// Python: unseen_change_notes (message_template.py L410-421)
func TestUnseenChangeNotes_区间过滤(t *testing.T) {
	raw := `[{"revision":1,"note":"a","by":"x"},{"revision":2,"note":"b","by":"y"},{"revision":3,"note":"c","by":"z"}]`
	task := &database.TeamTaskBase{ChangeNotes: &raw, Revision: 3, ObservedRevision: 1}
	got := UnseenChangeNotes(task)
	if len(got) != 2 {
		t.Fatalf("UnseenChangeNotes 返回 %d 条, want 2: %v", len(got), got)
	}
	if got[0].Note != "b" || got[1].Note != "c" {
		t.Fatalf("顺序/内容错误: %v", got)
	}
}

// TestUnseenChangeNotes_坏JSON与nil返回空 测试非法 JSON 与 nil task 不 panic
func TestUnseenChangeNotes_坏JSON与nil返回空(t *testing.T) {
	bad := `not-json`
	if got := UnseenChangeNotes(&database.TeamTaskBase{ChangeNotes: &bad, Revision: 1}); len(got) != 0 {
		t.Fatalf("坏 JSON: want empty, got %v", got)
	}
	if got := UnseenChangeNotes(nil); len(got) != 0 {
		t.Fatalf("nil task: want empty, got %v", got)
	}
	empty := ""
	if got := UnseenChangeNotes(&database.TeamTaskBase{ChangeNotes: &empty}); len(got) != 0 {
		t.Fatalf("空串: want empty, got %v", got)
	}
}

// TestChangeNotesLines_格式 测试 `- rN (by): note` 与无 by 时省略括号
//
// Python: change_notes_lines (message_template.py L424-430)
func TestChangeNotesLines_格式(t *testing.T) {
	raw := `[{"revision":2,"note":"改了要求","by":"leader"},{"revision":3,"note":"补充边界"}]`
	task := &database.TeamTaskBase{ChangeNotes: &raw, Revision: 3, ObservedRevision: 1}
	lines := ChangeNotesLines(task)
	if len(lines) != 2 {
		t.Fatalf("返回 %d 行, want 2: %v", len(lines), lines)
	}
	if lines[0] != "- r2 (leader): 改了要求" {
		t.Fatalf("lines[0] = %q", lines[0])
	}
	if lines[1] != "- r3: 补充边界" {
		t.Fatalf("lines[1] = %q", lines[1])
	}
}

// TestChangeNotesLines_无未读返回空 测试全部已读时返回空
func TestChangeNotesLines_无未读返回空(t *testing.T) {
	raw := `[{"revision":1,"note":"a"}]`
	task := &database.TeamTaskBase{ChangeNotes: &raw, Revision: 1, ObservedRevision: 1}
	if lines := ChangeNotesLines(task); len(lines) != 0 {
		t.Fatalf("want empty, got %v", lines)
	}
}

// TestRevisionDiff_对齐PythonGolden 逐字节对拍 Python difflib.unified_diff
//
// 样本由上游 message_template.revision_diff 实际运行生成（n=2、lineterm=""、
// 去掉 ---/+++ 文件头），覆盖单 hunk / 多 hunk / 空输入 / 纯插入 / 纯删除。
//
// Python: revision_diff (message_template.py L433-441)
func TestRevisionDiff_对齐PythonGolden(t *testing.T) {
	cases := []struct {
		name string
		prev string
		cur  string
		want string
	}{
		{
			name: "单行替换",
			prev: "a\nb\nc",
			cur:  "a\nB\nc",
			want: "@@ -1,3 +1,3 @@\n a\n-b\n+B\n c",
		},
		{
			name: "完全相同",
			prev: "same",
			cur:  "same",
			want: "",
		},
		{
			name: "空到非空",
			prev: "",
			cur:  "x",
			want: "@@ -0,0 +1 @@\n+x",
		},
		{
			name: "非空到空",
			prev: "x",
			cur:  "",
			want: "@@ -1 +0,0 @@\n-x",
		},
		{
			name: "尾部追加",
			prev: "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10",
			cur:  "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\nl11",
			want: "@@ -9,2 +9,3 @@\n l9\n l10\n+l11",
		},
		{
			name: "中间替换含全上下文",
			prev: "head\nmid1\nmid2\nmid3\ntail",
			cur:  "head\nmid1\nCHANGED\nmid3\ntail",
			want: "@@ -1,5 +1,5 @@\n head\n mid1\n-mid2\n+CHANGED\n mid3\n tail",
		},
		{
			name: "两处相距较远拆两个 hunk",
			prev: "a\nb\nc\nd\ne\nf\ng\nh\ni\nj",
			cur:  "a\nX\nc\nd\ne\nf\ng\nh\ni\nY",
			want: "@@ -1,4 +1,4 @@\n a\n-b\n+X\n c\n d\n@@ -8,3 +8,3 @@\n h\n i\n-j\n+Y",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := RevisionDiff(c.prev, c.cur)
			if got != c.want {
				t.Fatalf("RevisionDiff 不一致\n got = %q\nwant = %q", got, c.want)
			}
			if strings.Contains(got, "---") || strings.Contains(got, "+++") {
				t.Fatalf("diff 仍含文件头: %q", got)
			}
		})
	}
}

// TestUnseenChangeNotes_缺revision按0处理 测试字段缺失 / null / 0 三者等价
//
// Python: `int(note.get("revision") or 0)` —— 缺失、None、0 都得到 0。
func TestUnseenChangeNotes_缺revision按0处理(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"缺字段", `[{"note":"a"}]`},
		{"null", `[{"revision":null,"note":"a"}]`},
		{"零值", `[{"revision":0,"note":"a"}]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw := c.raw
			// observed=0, revision=3：0 不满足 observed < rev，故不入选
			task := &database.TeamTaskBase{ChangeNotes: &raw, Revision: 3, ObservedRevision: 0}
			if got := UnseenChangeNotes(task); len(got) != 0 {
				t.Fatalf("rev=0 不应入选, got %v", got)
			}
			// observed=-1 时 0 满足 -1 < 0 <= 3，应入选（证明取值确实是 0）
			task.ObservedRevision = -1
			got := UnseenChangeNotes(task)
			if len(got) != 1 {
				t.Fatalf("observed=-1 时 rev=0 应入选, got %v", got)
			}
			if noteRevision(got[0]) != 0 {
				t.Fatalf("noteRevision = %d, want 0", noteRevision(got[0]))
			}
		})
	}
}

// TestFormatNoteRevision_缺字段渲染None 测试缺失 revision 渲染成 None（对齐上游 f-string）
//
// 上游 `f"- r{note.get('revision')}"` 在字段缺失时输出 "None"。
// 该分支在当前区间判定下不可达（rev=0 时 observed<0 不成立），但保留是为了
// 字节一致性——若将来区间判定的下界放宽，渲染结果仍与上游相同。
func TestFormatNoteRevision_缺字段渲染None(t *testing.T) {
	if got := formatNoteRevision(database.ChangeNote{}); got != "None" {
		t.Fatalf("缺 revision 渲染 = %q, want None", got)
	}
	zero := int64(0)
	if got := formatNoteRevision(database.ChangeNote{Revision: &zero}); got != "0" {
		t.Fatalf("零 revision 渲染 = %q, want 0", got)
	}
	seven := int64(7)
	if got := formatNoteRevision(database.ChangeNote{Revision: &seven}); got != "7" {
		t.Fatalf("revision=7 渲染 = %q, want 7", got)
	}
}

// TestChangeNotesLines_字段缺失时的输出 测试 note/by 缺失时不 panic 且格式正确
func TestChangeNotesLines_字段缺失时的输出(t *testing.T) {
	raw := `[{"revision":1}]`
	task := &database.TeamTaskBase{ChangeNotes: &raw, Revision: 1, ObservedRevision: 0}
	lines := ChangeNotesLines(task)
	if len(lines) != 1 {
		t.Fatalf("返回 %d 行, want 1", len(lines))
	}
	if lines[0] != "- r1: " {
		t.Fatalf("lines[0] = %q, want %q", lines[0], "- r1: ")
	}
}
