package tenantreconcile

import (
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	maasv1alpha1 "github.com/opendatahub-io/models-as-a-service/maas-controller/api/maas/v1alpha1"
)

// TenantRuntimeParametersMap returns keys stored in maas-tenant-parameters for maas-api runtime env.
func TenantRuntimeParametersMap(tenant *maasv1alpha1.Tenant, appNamespace, clusterAudience string) map[string]string {
	out := map[string]string{
		"gateway-namespace": tenant.Spec.GatewayRef.Namespace,
		"gateway-name":      tenant.Spec.GatewayRef.Name,
		"app-namespace":     appNamespace,
	}
	if clusterAudience != "" {
		out["cluster-audience"] = clusterAudience
	}
	if tenant.Spec.APIKeys != nil && tenant.Spec.APIKeys.MaxExpirationDays != nil {
		out["api-key-max-expiration-days"] = strconv.FormatInt(int64(*tenant.Spec.APIKeys.MaxExpirationDays), 10)
	}
	return out
}

// StripGeneratedMaaSParametersConfigMap removes the kustomize-generated maas-parameters ConfigMap so
// Tenant reconcile does not SSA the operator-owned cluster ConfigMap of the same name.
func StripGeneratedMaaSParametersConfigMap(objs []unstructured.Unstructured) []unstructured.Unstructured {
	out := make([]unstructured.Unstructured, 0, len(objs))
	for i := range objs {
		if objs[i].GetAPIVersion() == "v1" && objs[i].GetKind() == "ConfigMap" && objs[i].GetName() == MaaSParametersConfigMapName {
			continue
		}
		out = append(out, objs[i])
	}
	return out
}

// EnsureTenantRuntimeConfigMap merges Tenant runtime keys into the rendered maas-tenant-parameters
// ConfigMap, or appends one if the overlay did not ship it.
func EnsureTenantRuntimeConfigMap(resources []unstructured.Unstructured, tenant *maasv1alpha1.Tenant, appNamespace, clusterAudience string) ([]unstructured.Unstructured, error) {
	data := TenantRuntimeParametersMap(tenant, appNamespace, clusterAudience)
	for i := range resources {
		if resources[i].GetAPIVersion() != "v1" || resources[i].GetKind() != "ConfigMap" {
			continue
		}
		if resources[i].GetName() != MaaSTenantRuntimeParametersConfigMapName {
			continue
		}
		merged := map[string]string{}
		existing, _, err := unstructured.NestedStringMap(resources[i].Object, "data")
		if err != nil {
			return nil, fmt.Errorf("configmap %q data: %w", MaaSTenantRuntimeParametersConfigMapName, err)
		}
		for k, v := range existing {
			merged[k] = v
		}
		for k, v := range data {
			merged[k] = v
		}
		if err := unstructured.SetNestedStringMap(resources[i].Object, merged, "data"); err != nil {
			return nil, err
		}
		return resources, nil
	}

	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      MaaSTenantRuntimeParametersConfigMapName,
			Namespace: appNamespace,
			Labels: map[string]string{
				LabelK8sPartOf: "models-as-a-service",
			},
		},
		Data: data,
	}
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(cm)
	if err != nil {
		return nil, err
	}
	return append(resources, unstructured.Unstructured{Object: u}), nil
}
