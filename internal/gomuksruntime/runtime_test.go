package gomuksruntime

import (
	"path/filepath"
	"testing"

	"github.com/batuhan/easymatrix/internal/config"
)

func TestNewUsesGomuksDataHomeWhenStateDirUnset(t *testing.T) {
	t.Setenv("GOMUKS_ROOT", "")
	t.Setenv("GOMUKS_CONFIG_HOME", t.TempDir())
	t.Setenv("GOMUKS_CACHE_HOME", t.TempDir())
	t.Setenv("GOMUKS_LOGS_HOME", t.TempDir())

	dataHome := t.TempDir()
	t.Setenv("GOMUKS_DATA_HOME", dataHome)

	rt, err := New(config.Config{})
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}

	if got, want := rt.StateDir(), filepath.Clean(dataHome); got != want {
		t.Fatalf("unexpected state dir: got %q want %q", got, want)
	}
}

func TestNewUsesExplicitStateDirAsGomuksRoot(t *testing.T) {
	root := t.TempDir()

	rt, err := New(config.Config{StateDir: root})
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}

	want := filepath.Join(root, "data")
	if got := rt.StateDir(); got != want {
		t.Fatalf("unexpected state dir: got %q want %q", got, want)
	}
}
