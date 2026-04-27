package tenantreconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestMergeParamsForTenantReconcile_tenantOverridesCM(t *testing.T) {
	base := map[string]string{
		"gateway-name":   "from-disk",
		"maas-api-image": "diskimg",
	}
	cm := map[string]string{"maas-api-image": "cmimg", "metadata-cache-ttl": "5m"}
	tenant := map[string]string{"gateway-name": "gw-tenant"}
	mergeParamsForTenantReconcile(base, ImageParamKeys, cm, tenant)
	if got := base["gateway-name"]; got != "gw-tenant" {
		t.Fatalf("gateway-name: want %q got %q", "gw-tenant", got)
	}
	if got := base["maas-api-image"]; got != "cmimg" {
		t.Fatalf("maas-api-image: want CM value %q got %q", "cmimg", got)
	}
	if got := base["metadata-cache-ttl"]; got != "5m" {
		t.Fatalf("metadata-cache-ttl: want %q got %q", "5m", got)
	}
}

func TestMergeParamsForTenantReconcile_relatedImageFallback(t *testing.T) {
	t.Setenv("RELATED_IMAGE_ODH_MAAS_API_IMAGE", "registry.example/maas-api:env")
	base := map[string]string{}
	mergeParamsForTenantReconcile(base, ImageParamKeys, nil, map[string]string{"gateway-name": "gw"})
	if got := base["maas-api-image"]; got != "registry.example/maas-api:env" {
		t.Fatalf("maas-api-image from env: want %q got %q", "registry.example/maas-api:env", got)
	}
}

func TestMergeAndWriteParamsEnv_withConfigMap(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "params.env"), []byte("gateway-name=old\nmaas-api-image=oldimg\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: MaaSParametersConfigMapName, Namespace: "app-ns"},
		Data: map[string]string{
			"maas-api-image": "from-cm",
			"gateway-name":   "should-be-overridden",
		},
	}
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()
	if err := mergeAndWriteParamsEnv(context.Background(), c, dir, "params.env", ImageParamKeys, "app-ns",
		map[string]string{"gateway-name": "tenant-gw"}); err != nil {
		t.Fatal(err)
	}
	out, err := parseParams(filepath.Join(dir, "params.env"))
	if err != nil {
		t.Fatal(err)
	}
	if out["gateway-name"] != "tenant-gw" {
		t.Fatalf("gateway-name %q", out["gateway-name"])
	}
	if out["maas-api-image"] != "from-cm" {
		t.Fatalf("maas-api-image %q", out["maas-api-image"])
	}
}
