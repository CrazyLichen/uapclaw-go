package equipment

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestProbeFileRead(t *testing.T) {
	root := t.TempDir()
	os.Setenv("UAPCLAW_DATA_DIR", root)
	resetPathCacheForCards()
	t.Cleanup(resetPathCacheForCards)
	pkgDir := filepath.Join(equipmentLocalRoot(agentTemplateKind, ""), "probe_tpl")
	_ = os.MkdirAll(filepath.Join(pkgDir, "persona"), 0o755)
	_ = os.WriteFile(filepath.Join(pkgDir, "manifest.json"),
		[]byte(`{"package_type":"agent_template","name":"probe_tpl","description":"d","persona":{"dir":"./persona"},"skills":[]}`), 0o644)
	_ = os.WriteFile(filepath.Join(pkgDir, "persona", "p.md"), []byte("## p"), 0o644)
	got, err := ReadPackageFile("agent_template", "probe_tpl", "persona/p.md")
	fmt.Printf("PROBE err=%v got=%+v\n", err, got)
	if got == nil {
		t.Skip("probe")
	}
}
