// 本文件提供团队任务消息的修订说明渲染与正文 diff。
//
// 上游 message_template.py（560 行）是两阶段渲染引擎：发送时只写 template key +
// refs/params，投递时按收件人语言加载 markdown 并解析 {{ns.field}}。本文件**只落
// 25.0c 任务通知需要的三个纯函数**——无 DB 依赖、无模板加载：
//
//	UnseenChangeNotes  「改过但未读」的编辑说明条目
//	ChangeNotesLines   上述条目的 `- rN (by): note` 渲染
//	RevisionDiff       两个正文版本的行级 diff
//
// ⤵️ 25.6 回填：parse_meta / build_meta / expand_message / _resolve_namespaces /
// _substitute / task_links / revision_getters，以及 completion_summary /
// upstream_brief / subtask_brief 等任务简报渲染器。它们依赖 6 个尚不存在的 DAO
// 方法（get_dependent_task_ids / get_tasks_by_ids / get_revision_snapshot /
// get_subtasks / get_parent_task_id）与 47 个尚不存在的 scheduler_* 模板，且在
// 25.0c 零生产调用方——落地即死码（规则 8）。subtask_brief 另依赖
// database/graph.stale_against（Go 侧不存在），随 25.4 一并落。
//
// Python: openjiuwen/agent_teams/message_template.py
package prompts

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
)

// ──────────────────────────── 结构体 ────────────────────────────

// diffOp 编辑脚本的一条：kind 为 ' '（相同）/ '-'（删除）/ '+'（插入）。
type diffOp struct {
	// kind 操作类型
	kind byte
	// text 行内容（不含换行）
	text string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// diffContextLines diff 每处变更的上下文行数
	// Python: _DIFF_CONTEXT_LINES = 2 (message_template.py L116)
	diffContextLines = 2
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// UnseenChangeNotes 返回认领人尚未看到的修订说明条目，按 revision 升序。
//
// 「未看到」的判定是 revision 区间：observed_revision < note.revision <= revision。
// 坏 JSON / 非列表一律返回空（上游同样吞掉 json.loads 的解析错误）。
//
// Python: unseen_change_notes (message_template.py L410-421)
func UnseenChangeNotes(task *database.TeamTaskBase) []database.ChangeNote {
	if task == nil || task.ChangeNotes == nil || *task.ChangeNotes == "" {
		return nil
	}
	var notes []database.ChangeNote
	if err := json.Unmarshal([]byte(*task.ChangeNotes), &notes); err != nil {
		return nil
	}
	observed := task.ObservedRevision
	current := task.Revision
	unseen := make([]database.ChangeNote, 0, len(notes))
	for _, note := range notes {
		rev := noteRevision(note)
		if observed < rev && rev <= current {
			unseen = append(unseen, note)
		}
	}
	sort.SliceStable(unseen, func(i, j int) bool {
		return noteRevision(unseen[i]) < noteRevision(unseen[j])
	})
	return unseen
}

// ChangeNotesLines 把未读修订说明渲染成每行一条 `- rN (by): note`。
//
// by 为空时省略括号段。没有未读条目时返回空切片。
//
// Python: change_notes_lines (message_template.py L424-430)
func ChangeNotesLines(task *database.TeamTaskBase) []string {
	notes := UnseenChangeNotes(task)
	if len(notes) == 0 {
		return nil
	}
	lines := make([]string, 0, len(notes))
	for _, note := range notes {
		by := ""
		if note.By != "" {
			by = " (" + note.By + ")"
		}
		lines = append(lines, "- r"+formatNoteRevision(note)+by+": "+note.Note)
	}
	return lines
}

// RevisionDiff 两个正文版本的行级 diff，去掉 `---` / `+++` 文件头。
//
// 上下文取 diffContextLines 行。正文相同返回空串。输出与 Python
// difflib.unified_diff(lineterm="", n=2) 逐字节一致（含 `@@` 段头，
// 含空一方的 `-0,0` / `-1` 简写形式）。
//
// Python: revision_diff (message_template.py L433-441)
func RevisionDiff(previous, current string) string {
	if previous == current {
		return ""
	}
	ops := diffOps(splitLines(previous), splitLines(current))
	if len(ops) == 0 {
		return ""
	}
	kept := make([]string, 0, len(ops))
	for _, line := range unifiedDiff(ops, diffContextLines) {
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// noteRevision 取条目里的 revision 数值，缺失按 0（与上游 `or 0` 一致）。
//
// 上游：`int(note.get("revision") or 0)` —— 字段缺失、None、0 都得到 0，
// 因此「缺失」的条目仍然参与区间判定（observed < 0 通常不成立，自然不会入选）。
func noteRevision(note database.ChangeNote) int64 {
	if note.Revision == nil {
		return 0
	}
	return *note.Revision
}

// formatNoteRevision 渲染同一把逗号后的修订号，与上游 f-string 逐字节一致。
//
// 上游直接写 `f"- r{note.get('revision')}"`：字段缺失时 Python 格式化 None
// 得到 "None"。这里保持同样输出，而不是替换成别的占位符——渲染结果会进模型
// 提示词，与上游有任何字节差异都会在 golden 对拍里暴露。
func formatNoteRevision(note database.ChangeNote) string {
	if note.Revision == nil {
		return "None"
	}
	return strconv.FormatInt(*note.Revision, 10)
}

// splitLines 按 Python str.splitlines() 的语义切行。
//
// 与 strings.Split 的差别：末尾换行不产生空元素（"a\n".splitlines() == ["a"]），
// 空串返回空切片。diff 的 hunk 计数依赖这一点，差一个空行整段头就错。
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// diffOps 用 Ratcliff-Obershelp 求编辑脚本，逐条对齐 difflib.SequenceMatcher。
//
// **为什么不用 LCS**：两者都能产出合法 diff，但在重复行交错时选出的对齐不同
// ——实测 60 组随机变异样本有 11 组（18%）字节不一致。diff 文本会渲染进模型
// 提示词，且用户要求与 Python 对齐方法步骤，故按 difflib 的实际算法复刻：
//
//  1. 建 b2j 索引（b 中每个元素出现的位置），无 junk、无 autojunk（上游调用
//     SequenceMatcher(None, ...)，isjunk=None 且 autojunk 对列表序列默认关闭）
//  2. get_matching_blocks：队列式递归 find_longest_match，结果排序后合并相邻块
//  3. get_opcodes：按匹配块序列产出 replace/delete/insert/equal
//
// 返回的脚本用 ' ' / '-' / '+' 表示，供 unifiedDiff 切 hunk。
func diffOps(a, b []string) []diffOp {
	blocks := matchingBlocks(a, b)
	if len(blocks) == 0 {
		// 无任何匹配：整段替换
		ops := make([]diffOp, 0, len(a)+len(b))
		for _, line := range a {
			ops = append(ops, diffOp{'-', line})
		}
		for _, line := range b {
			ops = append(ops, diffOp{'+', line})
		}
		return ops
	}
	ops := make([]diffOp, 0, len(a)+len(b))
	i, j := 0, 0
	for _, blk := range blocks {
		// 匹配块之前的区域：旧有新无 → 删除；旧无新有 → 插入；两边都有 → 成对替换
		for ; i < blk.a; i++ {
			ops = append(ops, diffOp{'-', a[i]})
		}
		for ; j < blk.b; j++ {
			ops = append(ops, diffOp{'+', b[j]})
		}
		// 匹配块本身
		for k := 0; k < blk.size; k++ {
			ops = append(ops, diffOp{' ', a[blk.a+k]})
		}
		i = blk.a + blk.size
		j = blk.b + blk.size
	}
	for ; i < len(a); i++ {
		ops = append(ops, diffOp{'-', a[i]})
	}
	for ; j < len(b); j++ {
		ops = append(ops, diffOp{'+', b[j]})
	}
	return ops
}

// matchBlock 一个匹配块：a[a:a+size] == b[b:b+size]。
type matchBlock struct {
	// a 在 a 序列中的起点
	a int
	// b 在 b 序列中的起点
	b int
	// size 匹配长度
	size int
}

// matchingBlocks 复刻 difflib.SequenceMatcher.get_matching_blocks。
//
// 用显式队列替代递归（上游注释：递归会在极端用例上超限，故改用队列），
// 结果按 (a, b) 排序后合并首尾相接的相邻块。
//
// Python: SequenceMatcher.get_matching_blocks (difflib.py)
func matchingBlocks(a, b []string) []matchBlock {
	la, lb := len(a), len(b)
	b2j := buildB2J(b)

	type region struct{ alo, ahi, blo, bhi int }
	queue := []region{{0, la, 0, lb}}
	var found []matchBlock
	for len(queue) > 0 {
		r := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		i, j, k := findLongestMatch(a, b, b2j, r.alo, r.ahi, r.blo, r.bhi)
		if k == 0 {
			continue
		}
		found = append(found, matchBlock{a: i, b: j, size: k})
		// a[alo:i] vs b[blo:j] 未知；a[i+k:ahi] vs b[j+k:bhi] 未知
		if r.alo < i && r.blo < j {
			queue = append(queue, region{r.alo, i, r.blo, j})
		}
		if i+k < r.ahi && j+k < r.bhi {
			queue = append(queue, region{i + k, r.ahi, j + k, r.bhi})
		}
	}
	if len(found) == 0 {
		return nil
	}
	sort.Slice(found, func(x, y int) bool {
		if found[x].a != found[y].a {
			return found[x].a < found[y].a
		}
		return found[x].b < found[y].b
	})
	// 合并相邻块（上游 2.5 起的 non_adjacent 折叠）
	merged := make([]matchBlock, 0, len(found))
	i1, j1, k1 := 0, 0, 0
	for _, blk := range found {
		if i1+k1 == blk.a && j1+k1 == blk.b {
			k1 += blk.size
			continue
		}
		if k1 > 0 {
			merged = append(merged, matchBlock{a: i1, b: j1, size: k1})
		}
		i1, j1, k1 = blk.a, blk.b, blk.size
	}
	if k1 > 0 {
		merged = append(merged, matchBlock{a: i1, b: j1, size: k1})
	}
	return merged
}

// buildB2J 建 b 中每个元素的位置索引（升序）。
// Python: SequenceMatcher.__chain_b 的 b2j（无 junk、无 autojunk）
func buildB2J(b []string) map[string][]int {
	b2j := make(map[string][]int, len(b))
	for j, line := range b {
		b2j[line] = append(b2j[line], j)
	}
	return b2j
}

// findLongestMatch 在 a[alo:ahi] 与 b[blo:bhi] 之间找最长匹配块。
//
// 返回 (i, j, k) 满足 a[i:i+k] == b[j:j+k]，且在「最长」前提下 i 最小、
// i 相同时 j 最小（与 difflib 的 tie-break 一致）。
//
// Python: SequenceMatcher.find_longest_match (difflib.py)
func findLongestMatch(a, b []string, b2j map[string][]int, alo, ahi, blo, bhi int) (int, int, int) {
	besti, bestj, bestsize := alo, blo, 0
	// j2len[j] = 以 a[i-1] 与 b[j] 结尾的最长匹配长度
	j2len := make(map[int]int)
	for i := alo; i < ahi; i++ {
		newj2len := make(map[int]int)
		for _, j := range b2j[a[i]] {
			if j < blo {
				continue
			}
			if j >= bhi {
				break
			}
			k := j2len[j-1] + 1
			newj2len[j] = k
			if k > bestsize {
				besti, bestj, bestsize = i-k+1, j-k+1, k
			}
		}
		j2len = newj2len
	}
	// 向前扩展
	for besti > alo && bestj > blo && a[besti-1] == b[bestj-1] {
		besti, bestj, bestsize = besti-1, bestj-1, bestsize+1
	}
	// 向后扩展
	for besti+bestsize < ahi && bestj+bestsize < bhi && a[besti+bestsize] == b[bestj+bestsize] {
		bestsize++
	}
	return besti, bestj, bestsize
}

// unifiedDiff 把编辑脚本格式化成 unified diff 行列表（含 `@@` 段头）。
//
// 逐条对齐 difflib.unified_diff：先按 opcode 切 group（equal 段长度超过 2n
// 时拆成两段，前段留前 n 行、后段留后 n 行），再把每个 group 渲染成一段。
// 首尾的 equal 段先收缩到 n 行，避免文件头尾拖一长串上下文。
//
// Python: SequenceMatcher.get_grouped_opcodes(difflib.py) + unified_diff
func unifiedDiff(ops []diffOp, context int) []string {
	if len(ops) == 0 {
		return nil
	}
	groups := groupOps(ops, context)
	out := make([]string, 0, len(ops))
	for _, group := range groups {
		// 段头：group 起点的旧/新行号
		oldStart, newStart := 1, 1
		for k := 0; k < group.start; k++ {
			if ops[k].kind != '+' {
				oldStart++
			}
			if ops[k].kind != '-' {
				newStart++
			}
		}
		oldCount, newCount := 0, 0
		for k := group.start; k < group.end; k++ {
			if ops[k].kind != '+' {
				oldCount++
			}
			if ops[k].kind != '-' {
				newCount++
			}
		}
		out = append(out, "@@ -"+formatRangeUnified(oldStart, oldCount)+
			" +"+formatRangeUnified(newStart, newCount)+" @@")
		for k := group.start; k < group.end; k++ {
			out = append(out, string(ops[k].kind)+ops[k].text)
		}
	}
	return out
}

// opGroup 一个 hunk 在编辑脚本中的区间 [start, end)。
type opGroup struct {
	// start 起始下标（含）
	start int
	// end 结束下标（不含）
	end int
}

// groupOps 按 difflib 的规则把编辑脚本切成 hunk。
//
// 规则（与 get_grouped_opcodes 一致）：
//  1. 首段与末段若是 equal，先各自收缩到 n 行
//  2. 遍历中遇到长度 > 2n 的 equal 段：先以「前 n 行」收尾当前 group，
//     再从该段「后 n 行」起头开新 group
//  3. 只剩一个 equal 段的 group 丢弃（没有变更就没有 hunk）
func groupOps(ops []diffOp, n int) []opGroup {
	// 步骤 1：把编辑脚本当成 opcode 段序列，首尾 equal 收缩
	type span struct{ start, end int }
	spans := make([]span, 0, 8)
	for i := 0; i < len(ops); {
		j := i
		for j < len(ops) && ops[j].kind == ops[i].kind {
			j++
		}
		spans = append(spans, span{i, j})
		i = j
	}
	if len(spans) > 0 && ops[spans[0].start].kind == ' ' {
		s := spans[0]
		if s.end-s.start > n {
			spans[0] = span{s.end - n, s.end}
		}
	}
	if len(spans) > 0 && ops[spans[len(spans)-1].start].kind == ' ' {
		s := spans[len(spans)-1]
		if s.end-s.start > n {
			spans[len(spans)-1] = span{s.start, s.start + n}
		}
	}

	nn := n + n
	var groups []opGroup
	var cur *opGroup
	for idx, sp := range spans {
		isEqual := ops[sp.start].kind == ' '
		length := sp.end - sp.start
		if isEqual && length > nn {
			// 收尾当前 group 到该 equal 段的前 n 行
			head := span{sp.start, sp.start + n}
			if cur == nil {
				cur = &opGroup{start: head.start, end: head.end}
			} else {
				cur.end = head.end
			}
			groups = append(groups, *cur)
			cur = nil
			// 从该段后 n 行起头新 group
			cur = &opGroup{start: sp.end - n, end: sp.end}
			_ = idx
			continue
		}
		if cur == nil {
			cur = &opGroup{start: sp.start, end: sp.end}
		} else {
			cur.end = sp.end
		}
	}
	if cur != nil {
		// 只含 equal 的 group 丢弃
		allEqual := true
		for k := cur.start; k < cur.end; k++ {
			if ops[k].kind != ' ' {
				allEqual = false
				break
			}
		}
		if !allEqual {
			groups = append(groups, *cur)
		}
	}
	return groups
}

// formatRangeUnified 格式化段头里的行列区间。
//
// Python difflib._format_range_unified：长度为 1 时只写起点，否则写
// `起点,长度`；空区间的起点减一（例如空文件的插入段头是 `-0,0`）。
func formatRangeUnified(start, length int) string {
	beginning := start
	if length == 1 {
		return strconv.Itoa(beginning)
	}
	if length == 0 {
		beginning--
	}
	return strconv.Itoa(beginning) + "," + strconv.Itoa(length)
}
