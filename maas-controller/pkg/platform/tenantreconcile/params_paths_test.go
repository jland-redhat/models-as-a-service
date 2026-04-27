package tenantreconcile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParamsEnvDirForManifest_deploymentOverlay(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "deployment", "base", "maas-controller", "default")
	overlay := filepath.Join(root, "deployment", "overlays", "odh")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(overlay, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "params.env"), []byte("k=v\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "kustomization.yaml"), []byte("resources: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ParamsEnvDirForManifest(overlay)
	if err != nil {
		t.Fatal(err)
	}
	if got != base {
		t.Fatalf("ParamsEnvDirForManifest: want %q got %q", base, got)
	}
}

func TestParamsEnvDirForManifest_maasApiOverlay(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "deployment", "base", "maas-controller", "default")
	overlay := filepath.Join(root, "maas-api", "deploy", "overlays", "odh")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(overlay, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "params.env"), []byte("k=v\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "kustomization.yaml"), []byte("resources: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ParamsEnvDirForManifest(overlay)
	if err != nil {
		t.Fatal(err)
	}
	if got != base {
		t.Fatalf("ParamsEnvDirForManifest: want %q got %q", base, got)
	}
}
