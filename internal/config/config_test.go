package config

import "testing"

func TestLoadUsesRailwayPortWhenListenAddrUnset(t *testing.T) {
	t.Setenv("MATRIX_API_LISTEN", "")
	t.Setenv("PORT", "8080")
	t.Setenv("GOMUKS_ROOT", "")
	t.Setenv("MATRIX_STATE_DIR", "")
	t.Setenv("RAILWAY_VOLUME_MOUNT_PATH", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got, want := cfg.ListenAddr, "0.0.0.0:8080"; got != want {
		t.Fatalf("ListenAddr = %q, want %q", got, want)
	}
}

func TestLoadUsesRailwayVolumeMountWhenStateDirUnset(t *testing.T) {
	t.Setenv("MATRIX_API_LISTEN", "")
	t.Setenv("PORT", "")
	t.Setenv("GOMUKS_ROOT", "")
	t.Setenv("MATRIX_STATE_DIR", "")
	t.Setenv("RAILWAY_VOLUME_MOUNT_PATH", "/data")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got, want := cfg.StateDir, "/data/gomuks"; got != want {
		t.Fatalf("StateDir = %q, want %q", got, want)
	}
}

func TestLoadUsesManageSecret(t *testing.T) {
	t.Setenv("MATRIX_API_LISTEN", "")
	t.Setenv("PORT", "")
	t.Setenv("GOMUKS_ROOT", "")
	t.Setenv("MATRIX_STATE_DIR", "")
	t.Setenv("RAILWAY_VOLUME_MOUNT_PATH", "")
	t.Setenv("EASYMATRIX_MANAGE_SECRET", "super-secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got, want := cfg.ManageSecret, "super-secret"; got != want {
		t.Fatalf("ManageSecret = %q, want %q", got, want)
	}
}
