package tenantreconcile

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	maasv1alpha1 "github.com/opendatahub-io/models-as-a-service/maas-controller/api/maas/v1alpha1"
)

const ssaFieldOwner = "maas-controller"

func parseParams(fileName string) (map[string]string, error) {
	paramsEnv, err := os.Open(fileName)
	if err != nil {
		return nil, err
	}
	defer paramsEnv.Close()

	paramsEnvMap := make(map[string]string)
	scanner := bufio.NewScanner(paramsEnv)
	for scanner.Scan() {
		line := scanner.Text()
		key, value, found := strings.Cut(line, "=")
		if found {
			paramsEnvMap[key] = value
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return paramsEnvMap, nil
}

func writeParamsToTmp(params map[string]string, tmpDir string) (string, error) {
	tmp, err := os.CreateTemp(tmpDir, "params.env-")
	if err != nil {
		return "", err
	}
	defer tmp.Close()

	writer := bufio.NewWriter(tmp)
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, err := fmt.Fprintf(writer, "%s=%s\n", key, params[key]); err != nil {
			return "", err
		}
	}
	if err := writer.Flush(); err != nil {
		return "", fmt.Errorf("failed to write to file: %w", err)
	}

	return tmp.Name(), nil
}

// loadMaaSParametersData returns a copy of ConfigMap data for maas-parameters in the application
// namespace. If the ConfigMap does not exist, it returns nil, nil (Tenant reconcile falls back to
// disk params.env plus RELATED_IMAGE_* on the controller process).
func loadMaaSParametersData(ctx context.Context, c client.Client, namespace string) (map[string]string, error) {
	if c == nil || namespace == "" {
		return nil, nil
	}
	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: MaaSParametersConfigMapName}, cm); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get ConfigMap %s/%s: %w", namespace, MaaSParametersConfigMapName, err)
	}
	out := make(map[string]string, len(cm.Data))
	for k, v := range cm.Data {
		out[k] = v
	}
	return out, nil
}

// relatedImageFill sets params keys from RELATED_IMAGE_* env vars when the current value is empty.
func relatedImageFill(params map[string]string, imageParamsMap map[string]string) {
	for paramKey, envName := range imageParamsMap {
		if params[paramKey] != "" {
			continue
		}
		if v := os.Getenv(envName); v != "" {
			params[paramKey] = v
		}
	}
}

// mergeParamsForTenantReconcile merges layers for kustomize params.env:
//  1. base file on disk (overlay template)
//  2. RELATED_IMAGE_* for any image key still empty (dev / partial CM)
//  3. live maas-parameters ConfigMap in the app namespace (ODH operator / DSC truth)
//  4. tenant-specific keys (gateway, app-namespace, cluster-audience, API key TTL, …)
//  5. RELATED_IMAGE_* again for keys still empty after CM (partial CM)
func mergeParamsForTenantReconcile(base map[string]string, imageParamsMap map[string]string, cmData map[string]string, tenantParams map[string]string) {
	relatedImageFill(base, imageParamsMap)
	if cmData != nil {
		for k, v := range cmData {
			base[k] = v
		}
	}
	for k, v := range tenantParams {
		base[k] = v
	}
	relatedImageFill(base, imageParamsMap)
}

// mergeAndWriteParamsEnv writes the merged params map to componentPath/file. When client and
// cmNamespace are set, the live maas-parameters ConfigMap is merged in (see mergeParamsForTenantReconcile).
func mergeAndWriteParamsEnv(ctx context.Context, c client.Client, componentPath, file string, imageParamsMap map[string]string, cmNamespace string, tenantParams map[string]string) error {
	paramsFile := filepath.Join(componentPath, file)

	base, err := parseParams(paramsFile)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		base = make(map[string]string)
	}

	cmData, err := loadMaaSParametersData(ctx, c, cmNamespace)
	if err != nil {
		return err
	}

	mergeParamsForTenantReconcile(base, imageParamsMap, cmData, tenantParams)

	tmp, err := writeParamsToTmp(base, componentPath)
	if err != nil {
		return err
	}
	if err = os.Rename(tmp, paramsFile); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("failed rename %s to %s: %w", tmp, paramsFile, err)
	}
	return nil
}

// ApplyParams mirrors opendatahub-operator/pkg/deploy.ApplyParams for params.env substitution.
// It does not read the live maas-parameters ConfigMap; Tenant reconcile uses mergeAndWriteParamsEnv instead.
func ApplyParams(componentPath, file string, imageParamsMap map[string]string, extraParamsMaps ...map[string]string) error {
	tenant := make(map[string]string)
	for _, extraParamsMap := range extraParamsMaps {
		for k, v := range extraParamsMap {
			tenant[k] = v
		}
	}
	return mergeAndWriteParamsEnv(context.Background(), nil, componentPath, file, imageParamsMap, "", tenant)
}

// ApplyRendered server-side-applies rendered objects with Tenant as controller owner (ODH deploy parity).
// Same-namespace children get a standard ownerReference; cluster-scoped and cross-namespace children
// get tracking labels instead (Kubernetes forbids cross-namespace and namespaced-to-cluster ownerReferences).
func ApplyRendered(ctx context.Context, c client.Client, scheme *runtime.Scheme, tenant *maasv1alpha1.Tenant, objs []unstructured.Unstructured) error {
	for i := range objs {
		u := objs[i].DeepCopy()

		childNs := u.GetNamespace()
		if childNs != "" && childNs == tenant.Namespace {
			if err := controllerutil.SetControllerReference(tenant, u, scheme); err != nil {
				return fmt.Errorf("set controller reference on %s %s/%s: %w", u.GetKind(), u.GetNamespace(), u.GetName(), err)
			}
		} else {
			setTenantTrackingLabels(u, tenant)
		}
		unstructured.RemoveNestedField(u.Object, "metadata", "managedFields")
		unstructured.RemoveNestedField(u.Object, "metadata", "resourceVersion")
		unstructured.RemoveNestedField(u.Object, "status")
		// ForceOwnership is intentional: maas-controller is the sole manager for
		// Tenant platform resources. During migration from the ODH modelsasservice
		// pipeline, force ensures a clean field-manager handoff without conflicts.
		if err := c.Patch(ctx, u, client.Apply, client.FieldOwner(ssaFieldOwner), client.ForceOwnership); err != nil {
			if apimeta.IsNoMatchError(err) && isOptionalAPIGroup(u.GroupVersionKind().Group) {
				// CRD not yet registered for a known optional dependency (e.g. Perses CRDs
				// installed by COO which may not be present yet). Skip so the rest of the
				// platform manifests are applied and Tenant reconcile does not fail.
				// The CRD watch will re-trigger reconcile once the CRDs appear.
				log.FromContext(ctx).Info("skipping resource: optional CRD not yet registered, will apply once installed",
					"group", u.GroupVersionKind().Group, "kind", u.GetKind(),
					"name", u.GetName(), "namespace", u.GetNamespace())
				continue
			}
			return fmt.Errorf("apply %s %s/%s: %w", u.GetKind(), u.GetNamespace(), u.GetName(), err)
		}
	}
	return nil
}

func setTenantTrackingLabels(obj *unstructured.Unstructured, tenant *maasv1alpha1.Tenant) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[LabelTenantName] = tenant.Name
	labels[LabelTenantNamespace] = tenant.Namespace
	obj.SetLabels(labels)
}

func isOptionalAPIGroup(group string) bool {
	_, ok := OptionalAPIGroups[group]
	return ok
}
