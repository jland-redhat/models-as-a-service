package tenantreconcile

import (
	"context"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSyncMaaSParametersConfigMap_NotFound(t *testing.T) {
	c := fake.NewClientBuilder().Build()
	params := PlatformParams{APIKeyMaxExpirationDays: "365"}

	err := syncMaaSParametersConfigMap(context.Background(), c, "test-ns", params, logr.Discard())

	assert.NoError(t, err)
}

func TestSyncMaaSParametersConfigMap_AlreadyCorrect(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      maasParametersConfigMapName,
			Namespace: "test-ns",
		},
		Data: map[string]string{
			"api-key-max-expiration-days": "365",
		},
	}
	c := fake.NewClientBuilder().WithObjects(cm).Build()
	params := PlatformParams{APIKeyMaxExpirationDays: "365"}

	err := syncMaaSParametersConfigMap(context.Background(), c, "test-ns", params, logr.Discard())

	assert.NoError(t, err)

	var updated corev1.ConfigMap
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: maasParametersConfigMapName, Namespace: "test-ns"}, &updated))
	assert.Equal(t, "365", updated.Data["api-key-max-expiration-days"])
}

func TestSyncMaaSParametersConfigMap_UpdatesValue(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      maasParametersConfigMapName,
			Namespace: "test-ns",
		},
		Data: map[string]string{
			"api-key-max-expiration-days": "90",
		},
	}
	c := fake.NewClientBuilder().WithObjects(cm).Build()
	params := PlatformParams{APIKeyMaxExpirationDays: "365"}

	err := syncMaaSParametersConfigMap(context.Background(), c, "test-ns", params, logr.Discard())

	assert.NoError(t, err)

	var updated corev1.ConfigMap
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: maasParametersConfigMapName, Namespace: "test-ns"}, &updated))
	assert.Equal(t, "365", updated.Data["api-key-max-expiration-days"])
}

func TestPayloadProcessingEnvoyFilterName(t *testing.T) {
	assert.Equal(t, "payload-processing-maas-default-gateway", PayloadProcessingEnvoyFilterName("maas-default-gateway"))
	assert.Equal(t, "payload-processing-team-a-gw", PayloadProcessingEnvoyFilterName("team-a-gw"))

	longGW := strings.Repeat("a", 50)
	got := PayloadProcessingEnvoyFilterName(longGW)
	assert.LessOrEqual(t, len(got), payloadProcessingEnvoyFilterNameMaxLen)
	assert.True(t, strings.HasPrefix(got, PayloadProcessingName+"-"))
	assert.NotEqual(t, PayloadProcessingEnvoyFilterName(longGW+"x"), got, "different gateways must not collide after truncate")
}

func TestDeleteLegacySingletonPayloadEnvoyFilter(t *testing.T) {
	const gwNS = "openshift-ingress"

	t.Run("noop when missing", func(t *testing.T) {
		c := fake.NewClientBuilder().Build()
		err := deleteLegacySingletonPayloadEnvoyFilter(context.Background(), c, logr.Discard(), gwNS)
		assert.NoError(t, err)
	})

	t.Run("deletes singleton", func(t *testing.T) {
		ef := &unstructured.Unstructured{}
		ef.SetGroupVersionKind(GVKEnvoyFilter)
		ef.SetNamespace(gwNS)
		ef.SetName(PayloadProcessingName)
		c := fake.NewClientBuilder().WithObjects(ef).Build()

		err := deleteLegacySingletonPayloadEnvoyFilter(context.Background(), c, logr.Discard(), gwNS)
		require.NoError(t, err)

		got := &unstructured.Unstructured{}
		got.SetGroupVersionKind(GVKEnvoyFilter)
		err = c.Get(context.Background(), types.NamespacedName{Namespace: gwNS, Name: PayloadProcessingName}, got)
		assert.True(t, apierrors.IsNotFound(err), "legacy singleton should be deleted")
	})

	t.Run("does not delete per-gateway copy", func(t *testing.T) {
		ef := &unstructured.Unstructured{}
		ef.SetGroupVersionKind(GVKEnvoyFilter)
		ef.SetNamespace(gwNS)
		ef.SetName(PayloadProcessingEnvoyFilterName("maas-default-gateway"))
		c := fake.NewClientBuilder().WithObjects(ef).Build()

		err := deleteLegacySingletonPayloadEnvoyFilter(context.Background(), c, logr.Discard(), gwNS)
		require.NoError(t, err)

		got := &unstructured.Unstructured{}
		got.SetGroupVersionKind(GVKEnvoyFilter)
		err = c.Get(context.Background(), types.NamespacedName{
			Namespace: gwNS,
			Name:      PayloadProcessingEnvoyFilterName("maas-default-gateway"),
		}, got)
		assert.NoError(t, err, "per-gateway EnvoyFilter must remain")
	})
}
