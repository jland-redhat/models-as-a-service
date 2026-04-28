package tenantreconcile

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type workloadImagePatch struct {
	kind           string
	name           string
	containerName  string
	cmKey          string
	containersPath []string
}

// Patches applied after kustomize so workloads match the live operator-owned maas-parameters CM
// (image cannot use valueFrom on Deployment.spec).
var maasParametersWorkloadImagePatches = []workloadImagePatch{
	{kind: "Deployment", name: MaaSAPIDeploymentName, containerName: "maas-api", cmKey: "maas-api-image",
		containersPath: []string{"spec", "template", "spec", "containers"}},
	{kind: "Deployment", name: "payload-processing", containerName: "payload-processing", cmKey: "payload-processing-image",
		containersPath: []string{"spec", "template", "spec", "containers"}},
	{kind: "CronJob", name: "maas-api-key-cleanup", containerName: "cleanup", cmKey: "maas-api-key-cleanup-image",
		containersPath: []string{"spec", "jobTemplate", "spec", "template", "spec", "containers"}},
}

// StripGeneratedMaaSParametersConfigMap removes the kustomize-generated maas-parameters so Tenant
// reconcile does not SSA the operator-owned ConfigMap.
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

// injectWorkloadImagesFromLiveMaaSParameters reads the cluster maas-parameters ConfigMap and sets
// container images on rendered workloads so they match the operator (RHOAI digest pins, etc.).
func injectWorkloadImagesFromLiveMaaSParameters(ctx context.Context, log logr.Logger, c client.Client, appNamespace string, resources []unstructured.Unstructured) error {
	if c == nil || appNamespace == "" {
		return nil
	}
	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: appNamespace, Name: MaaSParametersConfigMapName}, cm); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get ConfigMap %s/%s: %w", appNamespace, MaaSParametersConfigMapName, err)
	}
	for _, p := range maasParametersWorkloadImagePatches {
		img := cm.Data[p.cmKey]
		if img == "" {
			continue
		}
		for i := range resources {
			if resources[i].GetKind() != p.kind || resources[i].GetName() != p.name {
				continue
			}
			if err := setContainerImageInUnstructured(resources[i].Object, p.containersPath, p.containerName, img); err != nil {
				return fmt.Errorf("%s %s: %w", p.kind, p.name, err)
			}
			log.V(2).Info("set workload image from live maas-parameters", "kind", p.kind, "name", p.name, "key", p.cmKey)
		}
	}
	return nil
}

func setContainerImageInUnstructured(obj map[string]interface{}, containersPath []string, containerName, image string) error {
	containers, found, err := unstructured.NestedSlice(obj, containersPath...)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("containers path %v not found", containersPath)
	}
	for i, c := range containers {
		m, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		name, _, _ := unstructured.NestedString(m, "name")
		if name != containerName {
			continue
		}
		if err := unstructured.SetNestedField(m, image, "image"); err != nil {
			return err
		}
		containers[i] = m
		return unstructured.SetNestedSlice(obj, containers, containersPath...)
	}
	return fmt.Errorf("container %q not found", containerName)
}
