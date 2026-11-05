package main

import (
	"fmt"
	"sort"
	"strings"
)

const (
	// statusOK 三方一致（上游有、本仓有）。
	statusOK = "✅ 一致"
	// statusDrift 本仓有但上游未注册（自造或上游走 HTTP 映射）。
	statusDrift = "⚠️ 漂移"
	// statusMissing 上游有、本仓无。
	statusMissing = "❌ 缺"
)

// matrixRow 矩阵一行。
type matrixRow struct {
	// Method RPC 方法名
	Method string
	// Frontend 前端引用位置
	Frontend []string
	// Local 本仓注册位置
	Local []string
	// Upstream 上游注册位置
	Upstream []string
	// Status 覆盖状态
	Status string
	// Down 下游状态
	Down downstream
}

// buildMatrix 合并三方集合，按方法名聚合后逐行判定覆盖状态。
func buildMatrix(frontend, local, upstream map[string][]string) []matrixRow {
	methods := map[string]bool{}
	for m := range frontend {
		methods[m] = true
	}
	for m := range local {
		methods[m] = true
	}
	for m := range upstream {
		methods[m] = true
	}

	rows := make([]matrixRow, 0, len(methods))
	for m := range methods {
		row := matrixRow{Method: m, Frontend: frontend[m], Local: local[m], Upstream: upstream[m]}
		switch {
		case len(upstream[m]) > 0 && len(local[m]) > 0:
			row.Status = statusOK
		case len(upstream[m]) > 0:
			row.Status = statusMissing
		default:
			row.Status = statusDrift
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Method < rows[j].Method })
	return rows
}

// attachDownstream 把下游状态挂到对应行上。
func attachDownstream(rows []matrixRow, downs map[string]downstream) {
	for i := range rows {
		if d, ok := downs[rows[i].Method]; ok {
			rows[i].Down = d
			continue
		}
		rows[i].Down = downstream{Status: downLocal, Note: "非上游方法（本仓自造/漂移）"}
	}
}

// missing 返回状态为「缺」的行。
func missing(rows []matrixRow) []matrixRow {
	var out []matrixRow
	for _, r := range rows {
		if r.Status == statusMissing {
			out = append(out, r)
		}
	}
	return out
}

// countByDownstream 统计缺口里各下游状态的条数。
func countByDownstream(rows []matrixRow) map[string]int {
	out := map[string]int{}
	for _, r := range rows {
		if r.Status != statusMissing {
			continue
		}
		out[r.Down.Status]++
	}
	return out
}

// render 生成 Markdown 矩阵文档。
func render(rows []matrixRow) string {
	var ok, drift int
	for _, r := range rows {
		switch r.Status {
		case statusOK:
			ok++
		case statusDrift:
			drift++
		}
	}
	miss := len(rows) - ok - drift
	counts := countByDownstream(rows)

	var b strings.Builder
	b.WriteString("# Web WS RPC 契约矩阵\n\n")
	b.WriteString("> 由 `go run ./cmd/rpc-matrix` 生成，勿手工编辑。\n\n")
	fmt.Fprintf(&b, "- 方法总数：%d（✅ 一致 %d / ⚠️ 漂移 %d / ❌ 缺 %d）\n", len(rows), ok, drift, miss)
	fmt.Fprintf(&b, "- 缺口下游分布：local %d / agent:ok %d / agent:const-only %d / agent:missing %d / dep %d\n\n",
		counts[downLocal], counts[downAgentOK], counts[downAgentConst], counts[downAgentMissing], counts[downDep])
	b.WriteString("下游状态含义：`local` 网关本地可实现；`agent:ok` 代理且本仓已有处理器；")
	b.WriteString("`agent:const-only` 有常量无处理器；`agent:missing` 常量与处理器皆无；`dep` 依赖未迁移子系统。\n\n")
	b.WriteString("| 方法 | 覆盖 | 下游 | 下游说明 | 前端引用 | 本仓注册 | 上游注册 |\n")
	b.WriteString("|---|---|---|---|---|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s | %s | %s |\n",
			r.Method, r.Status, r.Down.Status, r.Down.Note,
			joinSites(r.Frontend), joinSites(r.Local), joinSites(r.Upstream))
	}
	return b.String()
}

// renderGaps 生成下游缺口清单（只含 ❌ 且下游非 local 的方法）。
func renderGaps(rows []matrixRow) string {
	var b strings.Builder
	b.WriteString("# Web WS RPC 下游缺口清单\n\n")
	b.WriteString("> 由 `go run ./cmd/rpc-matrix` 生成，勿手工编辑。\n")
	b.WriteString("> 本清单只列**网关层补齐也不能真正工作**的方法：它们在上游是薄代理，")
	b.WriteString("但本仓缺 req method 常量、AgentServer 处理器或底层子系统。\n\n")

	groups := map[string][]matrixRow{}
	for _, r := range rows {
		if r.Status != statusMissing || r.Down.Status == downLocal {
			continue
		}
		groups[r.Down.Status] = append(groups[r.Down.Status], r)
	}
	order := []string{downAgentConst, downAgentMissing, downDep}
	titles := map[string]string{
		downAgentConst:   "一、有 req method 常量、AgentServer 无处理器",
		downAgentMissing: "二、既无常量也无处理器",
		downDep:          "三、依赖未迁移子系统",
	}
	for _, key := range order {
		list := groups[key]
		if len(list) == 0 {
			continue
		}
		fmt.Fprintf(&b, "## %s（%d 个）\n\n", titles[key], len(list))
		b.WriteString("| 方法 | 下游说明 | 上游注册 |\n|---|---|---|\n")
		for _, r := range list {
			fmt.Fprintf(&b, "| `%s` | %s | %s |\n", r.Method, r.Down.Note, joinSites(r.Upstream))
		}
		b.WriteString("\n")
	}

	total := 0
	for _, list := range groups {
		total += len(list)
	}
	if total == 0 {
		b.WriteString("当前没有下游缺口。\n")
	}
	return b.String()
}

// joinSites 拼接登记位置；为空时输出 —。
func joinSites(sites []string) string {
	if len(sites) == 0 {
		return "—"
	}
	if len(sites) > 3 {
		sites = sites[:3]
	}
	return strings.Join(sites, "<br>")
}
