package discovery

import "testing"

func TestDbgCursor(t *testing.T) {
	tk, fs := makeNestedToolkit(t)
	view := fs.Directory()
	t.Logf("Entries direct=%d", len(view.Entries([]string{"/"}, -1, false, false, false)))
	t.Logf("children=%d", func() int { c, _ := view.Children("/"); return len(c) }())
	res, err := tk.SkillIndex(map[string]any{"operation": "list", "limit": 1})
	t.Logf("err=%v out=%q", err, res.ModelContent)
	t.Logf("layout=%s state=%s items=%d", fs.Artifact().Layout, fs.Artifact().IndexState, len(fs.Artifact().Items))
}
