package tenantreconcile

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKuadrantAuthFilterAnchors(t *testing.T) {
	got := kuadrantAuthFilterAnchors("istio-system", "maas-default-gateway")
	assert.Equal(t, []string{
		"extenstions.istio.io/wasmplugin/istio-system.kuadrant-maas-default-gateway",
		"extensions.istio.io/wasmplugin/istio-system.kuadrant-maas-default-gateway",
		"extensions.istio.io/trafficextension/istio-system.kuadrant-maas-default-gateway~istio-translated-wasmplugin",
		"envoy.filters.http.wasm",
	}, got)
}

func TestManifestPathForPlatform(t *testing.T) {
	t.Run("returns OCP overlay when isOCP is true", func(t *testing.T) {
		t.Setenv("MAAS_PLATFORM_MANIFESTS", "")
		t.Setenv("MAAS_IPP_PROFILE", "")
		path := ManifestPathForPlatform(true)
		assert.Equal(t, "/maas-api/deploy/overlays/odh", path)
	})

	t.Run("returns xKS overlay when isOCP is false", func(t *testing.T) {
		t.Setenv("MAAS_PLATFORM_MANIFESTS", "")
		t.Setenv("MAAS_IPP_PROFILE", "")
		path := ManifestPathForPlatform(false)
		assert.Equal(t, "/maas-api/deploy/overlays/xks", path)
	})

	t.Run("returns OCP praxis overlay when MAAS_IPP_PROFILE=praxis", func(t *testing.T) {
		t.Setenv("MAAS_PLATFORM_MANIFESTS", "")
		t.Setenv("MAAS_IPP_PROFILE", "praxis")
		path := ManifestPathForPlatform(true)
		assert.Equal(t, "/maas-api/deploy/overlays/odh-praxis", path)
	})

	t.Run("returns xKS praxis overlay when MAAS_IPP_PROFILE=praxis", func(t *testing.T) {
		t.Setenv("MAAS_PLATFORM_MANIFESTS", "")
		t.Setenv("MAAS_IPP_PROFILE", "praxis")
		path := ManifestPathForPlatform(false)
		assert.Equal(t, "/maas-api/deploy/overlays/xks-praxis", path)
	})

	t.Run("treats unknown profile as llm-d", func(t *testing.T) {
		t.Setenv("MAAS_PLATFORM_MANIFESTS", "")
		t.Setenv("MAAS_IPP_PROFILE", "unknown")
		path := ManifestPathForPlatform(true)
		assert.Equal(t, "/maas-api/deploy/overlays/odh", path)
	})

	t.Run("respects custom MAAS_PLATFORM_MANIFESTS over profile", func(t *testing.T) {
		t.Setenv("MAAS_PLATFORM_MANIFESTS", "/custom/path")
		t.Setenv("MAAS_IPP_PROFILE", "praxis")
		path := ManifestPathForPlatform(true)
		assert.Equal(t, "/custom/path", path)
	})

	t.Run("remaps stock xks env to xks-praxis when profile is praxis", func(t *testing.T) {
		t.Setenv("MAAS_PLATFORM_MANIFESTS", "/maas-api/deploy/overlays/xks")
		t.Setenv("MAAS_IPP_PROFILE", "praxis")
		path := ManifestPathForPlatform(false)
		assert.Equal(t, "/maas-api/deploy/overlays/xks-praxis", path)
	})

	t.Run("remaps stock odh env to odh-praxis when profile is praxis", func(t *testing.T) {
		t.Setenv("MAAS_PLATFORM_MANIFESTS", "/maas-api/deploy/overlays/odh")
		t.Setenv("MAAS_IPP_PROFILE", "praxis")
		path := ManifestPathForPlatform(true)
		assert.Equal(t, "/maas-api/deploy/overlays/odh-praxis", path)
	})

	t.Run("remaps stock xks-praxis env back to xks when profile is llm-d", func(t *testing.T) {
		t.Setenv("MAAS_PLATFORM_MANIFESTS", "/maas-api/deploy/overlays/xks-praxis")
		t.Setenv("MAAS_IPP_PROFILE", "llm-d")
		path := ManifestPathForPlatform(false)
		assert.Equal(t, "/maas-api/deploy/overlays/xks", path)
	})
}

func TestIPPProfile(t *testing.T) {
	t.Run("defaults to llm-d", func(t *testing.T) {
		t.Setenv("MAAS_IPP_PROFILE", "")
		assert.Equal(t, IPPProfileLLMD, IPPProfile())
	})

	t.Run("accepts praxis case-insensitively", func(t *testing.T) {
		t.Setenv("MAAS_IPP_PROFILE", "Praxis")
		assert.Equal(t, IPPProfilePraxis, IPPProfile())
	})
}

func TestPayloadProcessingImageForProfile(t *testing.T) {
	t.Run("llm-d uses RELATED_IMAGE_ODH payload image", func(t *testing.T) {
		t.Setenv("MAAS_IPP_PROFILE", "llm-d")
		t.Setenv("RELATED_IMAGE_ODH_AI_GATEWAY_PAYLOAD_PROCESSING_IMAGE", "quay.io/example/payload:test")
		t.Setenv("RELATED_IMAGE_PRAXIS_EXTPROC_IMAGE", "praxis:ignored")
		assert.Equal(t, "quay.io/example/payload:test", payloadProcessingImageForProfile())
	})

	t.Run("praxis uses RELATED_IMAGE_PRAXIS_EXTPROC_IMAGE", func(t *testing.T) {
		t.Setenv("MAAS_IPP_PROFILE", "praxis")
		t.Setenv("RELATED_IMAGE_ODH_AI_GATEWAY_PAYLOAD_PROCESSING_IMAGE", "quay.io/example/payload:test")
		t.Setenv("RELATED_IMAGE_PRAXIS_EXTPROC_IMAGE", "praxis-extproc:custom")
		assert.Equal(t, "praxis-extproc:custom", payloadProcessingImageForProfile())
	})

	t.Run("praxis falls back to default image", func(t *testing.T) {
		t.Setenv("MAAS_IPP_PROFILE", "praxis")
		t.Setenv("RELATED_IMAGE_PRAXIS_EXTPROC_IMAGE", "")
		t.Setenv("RELATED_IMAGE_ODH_AI_GATEWAY_PAYLOAD_PROCESSING_IMAGE", "quay.io/example/payload:test")
		assert.Equal(t, DefaultPraxisPayloadProcessingImage, payloadProcessingImageForProfile())
	})
}
