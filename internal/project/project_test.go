package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNew_DefaultsToDot(t *testing.T) {
	p := New("")
	if p.Root == "" {
		t.Error("Root should not be empty")
	}
}

func TestNew_AbsolutePath(t *testing.T) {
	p := New(".")
	if !filepath.IsAbs(p.Root) {
		t.Errorf("Root should be absolute, got %s", p.Root)
	}
}

func TestPaths_AllStandard(t *testing.T) {
	root := "/tmp/novel2all-test"
	p := &ProjectStructure{Root: root}
	tests := map[string]string{
		"tracking": p.TrackingStateFile(),
		"setup":    p.SetupMD(),
		"style":    p.StyleMD(),
		"world":    p.WorldviewDir(),
		"char":     p.CharactersDir(),
		"outline":  p.OutlineDir(),
		"prose":    p.ProseDir(),
		"ref":      p.ReferenceLibDir(),
		"meta":     p.MetadataDir(),
		"chroma":   p.ChromaDir(),
	}
	for name, pth := range tests {
		if filepath.Base(pth) == "" {
			t.Errorf("%s path is empty", name)
		}
	}
}

func TestChapterOutline_Padded(t *testing.T) {
	p := &ProjectStructure{Root: "/tmp/x"}
	got := p.ChapterOutline(5)
	want := filepath.Join("/tmp/x", "大纲", "细纲_第005章.md")
	if got != want {
		t.Errorf("ChapterOutline(5) = %q, want %q", got, want)
	}
}

func TestChapterProse_Padded(t *testing.T) {
	p := &ProjectStructure{Root: "/tmp/x"}
	got := p.ChapterProse(123)
	want := filepath.Join("/tmp/x", "正文", "第123章.md")
	if got != want {
		t.Errorf("ChapterProse(123) = %q, want %q", got, want)
	}
}

func TestInit_CreatesAllDirs(t *testing.T) {
	dir := t.TempDir()
	p := New(dir)
	if err := p.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	for _, sub := range p.Subdirs() {
		if _, err := os.Stat(sub); err != nil {
			t.Errorf("subdir %s not created: %v", sub, err)
		}
	}
}

func TestInit_Idempotent(t *testing.T) {
	dir := t.TempDir()
	p := New(dir)
	for i := 0; i < 3; i++ {
		if err := p.Init(); err != nil {
			t.Fatalf("Init iteration %d: %v", i, err)
		}
	}
}

func TestExists_FalseWhenEmpty(t *testing.T) {
	p := New(t.TempDir())
	if p.Exists() {
		t.Error("Exists should be false for non-existent project")
	}
}

func TestExists_TrueAfterInit(t *testing.T) {
	dir := t.TempDir()
	p := New(dir)
	if err := p.Init(); err != nil {
		t.Fatal(err)
	}
	if !p.Exists() {
		t.Error("Exists should be true after Init")
	}
}

func TestSubdirs_Returns8(t *testing.T) {
	p := &ProjectStructure{Root: "/tmp"}
	subs := p.Subdirs()
	if len(subs) != 8 {
		t.Errorf("Subdirs count = %d, want 8", len(subs))
	}
}

func TestInit_CreatesEmptyStateFile(t *testing.T) {
	dir := t.TempDir()
	p := New(dir)
	if err := p.Init(); err != nil {
		t.Fatal(err)
	}
	if !p.Exists() {
		t.Error("Exists should be true after Init (V0.27.3 auto-creates state file)")
	}
	data, err := os.ReadFile(p.TrackingStateFile())
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Error("state file should not be empty")
	}
}
