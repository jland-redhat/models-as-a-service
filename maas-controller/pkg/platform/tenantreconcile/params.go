package tenantreconcile

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	maasv1alpha1 "github.com/opendatahub-io/models-as-a-service/maas-controller/api/maas/v1alpha1"
)

// PlatformParams holds resolved runtime values for PostRender patching.
type PlatformParams struct {
	AppNamespace          string
	ControllerNamespace   string
	GatewayNamespace      string
	GatewayName           string
	ClusterAudience       string
	SubscriptionNamespace string
	ExternalOIDC          *maasv1alpha1.TenantExternalOIDCConfig

	// TenantIdentifier is the tenant name used for per-tenant resource naming.
	// Empty string ("") for default/legacy tenant, non-empty (e.g., "redteam") for AITenant-managed tenants.
	TenantIdentifier string

	MaaSAPIImage           string
	PayloadProcessingImage string
	MaaSAPIKeyCleanupImage string

	APIKeyMaxExpirationDays string

	// MaaSAPIReplicas overrides the maas-api Deployment replica count when non-nil.
	MaaSAPIReplicas *int32
	// PayloadProcessingReplicas overrides the payload-processing Deployment replica count when non-nil.
	PayloadProcessingReplicas *int32

	// Warnings collects non-fatal issues found during param resolution (e.g. invalid annotations).
	Warnings []string
}

// BuildPlatformParams resolves all runtime parameters from the tenant config object,
// platform context, cluster state, and RELATED_IMAGE_* env vars. No disk I/O.
func BuildPlatformParams(tenant client.Object, platformContext PlatformContext, appNamespace, controllerNamespace, clusterAudience string, log logr.Logger) (PlatformParams, error) {
	tenantID, err := TenantIdentifierFor(tenant)
	if err != nil {
		return PlatformParams{}, fmt.Errorf("resolve tenant identifier: %w", err)
	}

	params := PlatformParams{
		AppNamespace:            appNamespace,
		ControllerNamespace:     controllerNamespace,
		GatewayNamespace:        platformContext.GatewayRef.Namespace,
		GatewayName:             platformContext.GatewayRef.Name,
		ClusterAudience:         clusterAudience,
		SubscriptionNamespace:   tenant.GetNamespace(),
		ExternalOIDC:            platformContext.ExternalOIDC.DeepCopy(),
		TenantIdentifier:        tenantID,
		MaaSAPIImage:            firstNonEmpty(os.Getenv("RELATED_IMAGE_ODH_MAAS_API_IMAGE"), DefaultMaaSAPIImage),
		PayloadProcessingImage:  payloadProcessingImageForProfile(),
		MaaSAPIKeyCleanupImage:  firstNonEmpty(os.Getenv("RELATED_IMAGE_UBI_MINIMAL_IMAGE"), DefaultMaaSAPIKeyCleanupImage),
		APIKeyMaxExpirationDays: resolveAPIKeyMaxExpirationDays(tenant),
	}

	params.MaaSAPIReplicas, params.PayloadProcessingReplicas, params.Warnings = resolveReplicaAnnotations(tenant, log)

	log.Info("Built platform params",
		"tenant", tenant.GetNamespace()+"/"+tenant.GetName(),
		"tenantID", tenantID,
		"ippProfile", IPPProfile(),
		"subscriptionNamespace", params.SubscriptionNamespace,
		"gatewayName", params.GatewayName)

	return params, nil
}

// payloadProcessingImageForProfile resolves the IPP container image for the
// active MAAS_IPP_PROFILE. Praxis uses RELATED_IMAGE_PRAXIS_EXTPROC_IMAGE so the
// llm-d ConfigMap image is not applied to Praxis pods.
func payloadProcessingImageForProfile() string {
	if IPPProfile() == IPPProfilePraxis {
		return firstNonEmpty(os.Getenv("RELATED_IMAGE_PRAXIS_EXTPROC_IMAGE"), DefaultPraxisPayloadProcessingImage)
	}
	return firstNonEmpty(os.Getenv("RELATED_IMAGE_ODH_AI_GATEWAY_PAYLOAD_PROCESSING_IMAGE"), DefaultPayloadProcessingImage)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// resolveReplicaAnnotations reads replica-count annotations from the tenant object
// and returns parsed values (nil if not set) plus any validation warnings.
func resolveReplicaAnnotations(tenant client.Object, log logr.Logger) (maasAPIReplicas, payloadProcessingReplicas *int32, warnings []string) {
	annotations := tenant.GetAnnotations()
	if annotations == nil {
		return nil, nil, nil
	}

	var w []string
	if v, ok := annotations[AnnotationMaaSAPIReplicas]; ok {
		r, warn := parseReplicaAnnotation(AnnotationMaaSAPIReplicas, v)
		if warn != "" {
			w = append(w, warn)
			log.Info("Invalid replica annotation", "annotation", AnnotationMaaSAPIReplicas, "value", v, "warning", warn)
		} else {
			maasAPIReplicas = r
			log.Info("Resolved maas-api replicas from annotation", "replicas", *r)
		}
	}
	if v, ok := annotations[AnnotationPayloadProcessingReplicas]; ok {
		r, warn := parseReplicaAnnotation(AnnotationPayloadProcessingReplicas, v)
		if warn != "" {
			w = append(w, warn)
			log.Info("Invalid replica annotation", "annotation", AnnotationPayloadProcessingReplicas, "value", v, "warning", warn)
		} else {
			payloadProcessingReplicas = r
			log.Info("Resolved payload-processing replicas from annotation", "replicas", *r)
		}
	}
	return maasAPIReplicas, payloadProcessingReplicas, w
}

const maxReplicaCount = 100

func parseReplicaAnnotation(annotationKey, value string) (*int32, string) {
	n, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return nil, fmt.Sprintf("annotation %s has invalid value %q: must be a positive integer; remove the annotation to use the default replica count", annotationKey, value)
	}
	if n < 1 {
		return nil, fmt.Sprintf("annotation %s has invalid value %q: must be >= 1; remove the annotation to use the default replica count", annotationKey, value)
	}
	if n > maxReplicaCount {
		return nil, fmt.Sprintf("annotation %s has invalid value %q: must be <= %d; remove the annotation to use the default replica count", annotationKey, value, maxReplicaCount)
	}
	r := int32(n)
	return &r, ""
}

func resolveAPIKeyMaxExpirationDays(tenant client.Object) string {
	cfg := apiKeysConfigFor(tenant)
	if cfg != nil && cfg.MaxExpirationDays != nil {
		return strconv.FormatInt(int64(*cfg.MaxExpirationDays), 10)
	}
	return DefaultAPIKeyMaxExpirationDays
}

func apiKeysConfigFor(tenant client.Object) *maasv1alpha1.TenantAPIKeysConfig {
	switch t := tenant.(type) {
	case *maasv1alpha1.MaasTenantConfig:
		return t.Spec.APIKeys
	case *maasv1alpha1.Tenant:
		return t.Spec.APIKeys
	default:
		return nil
	}
}

func telemetryConfigFor(tenant client.Object) *maasv1alpha1.TenantTelemetryConfig {
	switch t := tenant.(type) {
	case *maasv1alpha1.MaasTenantConfig:
		return t.Spec.Telemetry
	case *maasv1alpha1.Tenant:
		return t.Spec.Telemetry
	default:
		return nil
	}
}

// applyPlatformParams patches all dynamic values into rendered resources.
func applyPlatformParams(log logr.Logger, resources []unstructured.Unstructured, params PlatformParams) error {
	for i := range resources {
		if err := patchResource(log, &resources[i], params); err != nil {
			return err
		}
	}
	return nil
}

// patchResource applies tenant-specific patches to a single resource.
func patchResource(log logr.Logger, r *unstructured.Unstructured, params PlatformParams) error {
	gvk := r.GroupVersionKind()
	name := r.GetName()
	tenantID := params.TenantIdentifier

	switch {
	case gvk == GVKDeployment && name == baseMaaSAPIDeploymentName:
		// Rename and patch maas-api Deployment for this tenant
		r.SetName(MaaSAPIDeploymentName(tenantID))
		return patchMaaSAPIDeployment(log, r, params)
	case gvk == GVKDeployment && name == PayloadProcessingName:
		r.SetName(PayloadProcessingDeploymentName(tenantID))
		return patchPayloadProcessingDeployment(log, r, params)
	case gvk == GVKCronJob && name == baseMaaSAPIKeyCleanupCronJobName:
		// Rename and patch cleanup CronJob for this tenant
		r.SetName(MaaSAPIKeyCleanupCronJobName(tenantID))
		return patchCleanupCronJobImage(log, r, params)
	case gvk == GVKHTTPRoute && name == baseMaaSAPIRouteName:
		// Rename and patch HTTPRoute for this tenant
		r.SetName(MaaSAPIRouteName(tenantID))
		return patchHTTPRoute(log, r, params)
	case gvk == GVKDestinationRule && name == baseGatewayDestinationRuleName:
		// Rename and patch DestinationRule for this tenant
		r.SetName(GatewayDestinationRuleName(tenantID))
		return patchMaaSAPIDestinationRule(log, r, params)
	case gvk == GVKDestinationRule && (name == PayloadProcessingName || name == PayloadPreProcessingName):
		if name == PayloadPreProcessingName {
			r.SetName(PayloadPreProcessingDeploymentName(tenantID))
		} else {
			r.SetName(PayloadProcessingDeploymentName(tenantID))
		}
		return patchPayloadDestinationRule(log, r, params)
	case gvk == GVKEnvoyFilter && name == PayloadProcessingName:
		r.SetName(PayloadProcessingEnvoyFilterName(tenantID))
		return patchPayloadProcessingEnvoyFilter(log, r, params)
	case gvk == GVKDeployment && name == PayloadPreProcessingName:
		r.SetName(PayloadPreProcessingDeploymentName(tenantID))
		return patchPreProcessingDeployment(log, r, params)
	case gvk == GVKService && name == baseMaaSAPIServiceName:
		// Rename and patch maas-api Service for this tenant
		r.SetName(MaaSAPIServiceName(tenantID))
		return patchMaaSAPIService(log, r, params)
	case gvk == GVKService && (name == PayloadProcessingName || name == PayloadPreProcessingName):
		if name == PayloadPreProcessingName {
			r.SetName(PayloadPreProcessingServiceName(tenantID))
			return patchPayloadPreProcessingService(log, r, params)
		}
		r.SetName(PayloadProcessingServiceName(tenantID))
		return patchPayloadProcessingService(log, r, params)
	case gvk == GVKServiceAccount && name == PayloadProcessingName:
		r.SetName(PayloadProcessingServiceAccountName(tenantID))
		r.SetNamespace(params.GatewayNamespace)
	case gvk == GVKConfigMap && name == PayloadProcessingPluginsConfigMapName:
		r.SetName(PayloadProcessingPluginsConfigMapForTenant(tenantID))
		r.SetNamespace(params.GatewayNamespace)
	case gvk == GVKNetworkPolicy && name == baseMaaSAPIDeploymentNSNetworkPolicyName:
		return patchDeploymentNSNetworkPolicy(r, params.ControllerNamespace)
	case gvk == GVKNetworkPolicy && name == PayloadProcessingName:
		r.SetName(PayloadProcessingNetworkPolicyName(tenantID))
		return patchPayloadProcessingNetworkPolicy(log, r, params)
	case gvk == GVKClusterRoleBinding && name == PayloadProcessingReaderClusterRoleBindingName:
		r.SetName(PayloadProcessingReaderClusterRoleBindingNameForTenant(tenantID))
		return patchPayloadProcessingClusterRoleBinding(r, params)
	case gvk == GVKCertificate && name == baseMaaSAPIServingCertName:
		return patchMaaSAPIServingCert(log, r, params)
	}
	return nil
}

// patchDeploymentNSNetworkPolicy rewrites the namespaceSelector in the
// deployment-ns NetworkPolicy to use the actual controller namespace.
func patchDeploymentNSNetworkPolicy(r *unstructured.Unstructured, controllerNamespace string) error {
	if controllerNamespace == "" {
		return nil
	}
	return patchNetworkPolicyIngressNamespace(r, 0, controllerNamespace)
}

// patchMaaSAPIServingCert remaps the Certificate's secretName and dnsNames to use
// the actual infra namespace (replacing the kustomize overlay's hardcoded "opendatahub"
// placeholder). This makes the controller self-sufficient for TLS — no dependency on
// the Helm chart hook to create the cert in the correct namespace.
func patchMaaSAPIServingCert(log logr.Logger, r *unstructured.Unstructured, params PlatformParams) error {
	tenantID := params.TenantIdentifier
	certName := MaaSAPIServingCertName(tenantID)
	r.SetName(certName)

	secretName := certName
	if err := unstructured.SetNestedField(r.Object, secretName, "spec", "secretName"); err != nil {
		return fmt.Errorf("patch Certificate secretName: %w", err)
	}

	serviceName := MaaSAPIServiceName(tenantID)
	newDNSNames := []any{
		fmt.Sprintf("%s.%s.svc", serviceName, params.AppNamespace),
		fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, params.AppNamespace),
	}
	if err := unstructured.SetNestedSlice(r.Object, newDNSNames, "spec", "dnsNames"); err != nil {
		return fmt.Errorf("patch Certificate dnsNames: %w", err)
	}

	log.V(4).Info("Patched maas-api serving Certificate",
		"name", certName, "secretName", secretName,
		"dnsNames", newDNSNames, "namespace", params.AppNamespace)
	return nil
}

func patchMaaSAPIDeployment(log logr.Logger, r *unstructured.Unstructured, params PlatformParams) error {
	if params.MaaSAPIReplicas != nil {
		if err := unstructured.SetNestedField(r.Object, int64(*params.MaaSAPIReplicas), "spec", "replicas"); err != nil {
			return fmt.Errorf("patch maas-api replicas: %w", err)
		}
		log.V(4).Info("Patching maas-api replicas", "replicas", *params.MaaSAPIReplicas)
	}

	log.V(4).Info("Patching maas-api image", "image", params.MaaSAPIImage)
	if err := setContainerImage(r, "maas-api", params.MaaSAPIImage); err != nil {
		return fmt.Errorf("patch maas-api image: %w", err)
	}
	if err := setOrAddEnvVar(r, "maas-api", "GATEWAY_NAMESPACE", params.GatewayNamespace); err != nil {
		return fmt.Errorf("patch GATEWAY_NAMESPACE: %w", err)
	}
	if err := setOrAddEnvVar(r, "maas-api", "GATEWAY_NAME", params.GatewayName); err != nil {
		return fmt.Errorf("patch GATEWAY_NAME: %w", err)
	}
	if err := setOrAddEnvVar(r, "maas-api", "MAAS_SUBSCRIPTION_NAMESPACE", params.SubscriptionNamespace); err != nil {
		return fmt.Errorf("patch MAAS_SUBSCRIPTION_NAMESPACE: %w", err)
	}
	if err := setOrAddEnvVar(r, "maas-api", "API_KEY_MAX_EXPIRATION_DAYS", params.APIKeyMaxExpirationDays); err != nil {
		return fmt.Errorf("patch API_KEY_MAX_EXPIRATION_DAYS: %w", err)
	}

	// Set TENANT_NAME environment variable for per-tenant maas-api instances.
	// This value is used by maas-api for database queries (WHERE tenant = $TENANT_NAME)
	// and for validating the X-MaaS-Tenant header from Authorino.
	// Value: "models-as-a-service" for default tenant, tenant name (e.g., "redteam") for AITenant-managed tenants.
	// Note: TenantIdentifier is "" for default tenant (used for resource naming),
	// but TENANT_NAME must be "models-as-a-service" for DB consistency.
	tenantName := params.TenantIdentifier
	if tenantName == "" {
		// Default tenant: resource names use empty string (e.g., "maas-api"),
		// but TENANT_NAME must match DB default and AuthPolicy header value
		tenantName = "models-as-a-service"
	}
	if err := setOrAddEnvVar(r, "maas-api", "TENANT_NAME", tenantName); err != nil {
		return fmt.Errorf("patch TENANT_NAME: %w", err)
	}

	// Add tenant-instance label to pod template for unique Service selector matching.
	// This ensures each tenant's Service only routes to its own pods.
	// Use deployment name as the label value since it's already unique per tenant.
	deploymentName := MaaSAPIDeploymentName(params.TenantIdentifier)
	if err := addPodTemplateLabel(r, "maas.opendatahub.io/tenant-instance", deploymentName); err != nil {
		return fmt.Errorf("patch tenant-instance label: %w", err)
	}

	return nil
}

func patchMaaSAPIService(log logr.Logger, r *unstructured.Unstructured, params PlatformParams) error {
	// Add tenant-instance label to Service selector to ensure it only routes to its own pods.
	// This matches the label we added to the Deployment's pod template.
	deploymentName := MaaSAPIDeploymentName(params.TenantIdentifier)
	if err := addServiceSelectorLabel(r, "maas.opendatahub.io/tenant-instance", deploymentName); err != nil {
		return fmt.Errorf("patch tenant-instance selector: %w", err)
	}
	return nil
}

func patchPayloadProcessingDeployment(log logr.Logger, r *unstructured.Unstructured, params PlatformParams) error {
	r.SetNamespace(params.GatewayNamespace)
	deploymentName := PayloadProcessingDeploymentName(params.TenantIdentifier)

	if params.PayloadProcessingReplicas != nil {
		if err := unstructured.SetNestedField(r.Object, int64(*params.PayloadProcessingReplicas), "spec", "replicas"); err != nil {
			return fmt.Errorf("patch payload-processing replicas: %w", err)
		}
		log.V(4).Info("Patching payload-processing replicas", "deployment", deploymentName, "replicas", *params.PayloadProcessingReplicas)
	}

	log.V(4).Info("Patching payload-processing image", "deployment", deploymentName, "image", params.PayloadProcessingImage)
	if err := setContainerImage(r, "payload-processing", params.PayloadProcessingImage); err != nil {
		return fmt.Errorf("patch payload-processing image: %w", err)
	}
	if err := setOrAddEnvVar(r, "payload-processing", "GATEWAY_NAMESPACE", params.GatewayNamespace); err != nil {
		return fmt.Errorf("patch GATEWAY_NAMESPACE: %w", err)
	}
	if err := setOrAddEnvVar(r, "payload-processing", "GATEWAY_NAME", params.GatewayName); err != nil {
		return fmt.Errorf("patch GATEWAY_NAME: %w", err)
	}
	if err := setOrAddEnvVar(r, "payload-processing", "TENANT_NAMESPACE", params.SubscriptionNamespace); err != nil {
		return fmt.Errorf("patch TENANT_NAMESPACE: %w", err)
	}
	if params.TenantIdentifier != "" {
		if err := setOrAddEnvVar(r, "payload-processing", "DISABLE_EXTERNAL_MODEL_CONTROLLER", "true"); err != nil {
			return fmt.Errorf("patch DISABLE_EXTERNAL_MODEL_CONTROLLER: %w", err)
		}
	}
	if err := addPodTemplateLabel(r, LabelTenantInstance, deploymentName); err != nil {
		return fmt.Errorf("patch tenant-instance label: %w", err)
	}
	if err := patchIPPDeploymentSelector(r, params.TenantIdentifier, PayloadProcessingName, deploymentName); err != nil {
		return fmt.Errorf("patch deployment selector: %w", err)
	}
	if err := patchDeploymentServiceAccountName(r, PayloadProcessingServiceAccountName(params.TenantIdentifier)); err != nil {
		return fmt.Errorf("patch serviceAccountName: %w", err)
	}
	if err := patchConfigMapVolumeRef(r, "plugins-config-volume", PayloadProcessingPluginsConfigMapForTenant(params.TenantIdentifier)); err != nil {
		return fmt.Errorf("patch plugins ConfigMap volume: %w", err)
	}
	return nil
}

func patchPreProcessingDeployment(log logr.Logger, r *unstructured.Unstructured, params PlatformParams) error {
	r.SetNamespace(params.GatewayNamespace)
	deploymentName := PayloadPreProcessingDeploymentName(params.TenantIdentifier)
	if params.PayloadProcessingImage != "" {
		if err := setContainerImage(r, PayloadPreProcessingName, params.PayloadProcessingImage); err != nil {
			return fmt.Errorf("patch payload-pre-processing image: %w", err)
		}
	}
	// Pre-processing is data-plane only; it must not run ExternalModel reconcile.
	if err := setOrAddEnvVar(r, PayloadPreProcessingName, "DISABLE_EXTERNAL_MODEL_CONTROLLER", "true"); err != nil {
		return fmt.Errorf("patch DISABLE_EXTERNAL_MODEL_CONTROLLER: %w", err)
	}
	if err := addPodTemplateLabel(r, LabelTenantInstance, deploymentName); err != nil {
		return fmt.Errorf("patch tenant-instance label: %w", err)
	}
	if err := patchIPPDeploymentSelector(r, params.TenantIdentifier, PayloadPreProcessingName, deploymentName); err != nil {
		return fmt.Errorf("patch deployment selector: %w", err)
	}
	if err := patchDeploymentServiceAccountName(r, PayloadProcessingServiceAccountName(params.TenantIdentifier)); err != nil {
		return fmt.Errorf("patch serviceAccountName: %w", err)
	}
	if err := patchConfigMapVolumeRef(r, "plugins-config-volume", PayloadProcessingPluginsConfigMapForTenant(params.TenantIdentifier)); err != nil {
		return fmt.Errorf("patch plugins ConfigMap volume: %w", err)
	}
	return nil
}

// patchIPPDeploymentSelector sets the Deployment pod selector for per-tenant IPP stacks.
// Default-tenant Deployments are left unchanged because spec.selector is immutable on
// upgrade; pod-template and Service selectors carry tenant-instance instead (same
// pattern as per-tenant maas-api). Suffixed tenant Deployments are created fresh with
// both labels in the selector.
func patchIPPDeploymentSelector(r *unstructured.Unstructured, tenantID, appLabel, deploymentName string) error {
	if tenantID == "" {
		// Never mutate default Deployment spec.selector: it is immutable and may
		// already be {app} or {app, tenant-instance} depending on release history.
		return nil
	}
	return setDeploymentSelectorMatchLabels(r, map[string]string{
		"app":               appLabel,
		LabelTenantInstance: deploymentName,
	})
}

func patchPayloadProcessingService(log logr.Logger, r *unstructured.Unstructured, params PlatformParams) error {
	r.SetNamespace(params.GatewayNamespace)
	deploymentName := PayloadProcessingDeploymentName(params.TenantIdentifier)
	if err := setServiceSelectorMatchLabels(r, map[string]string{
		"app":               PayloadProcessingName,
		LabelTenantInstance: deploymentName,
	}); err != nil {
		return fmt.Errorf("patch payload-processing service selector: %w", err)
	}
	log.V(4).Info("Configured payload-processing Service selector", "deployment", deploymentName)
	return nil
}

func patchPayloadPreProcessingService(log logr.Logger, r *unstructured.Unstructured, params PlatformParams) error {
	r.SetNamespace(params.GatewayNamespace)
	deploymentName := PayloadPreProcessingDeploymentName(params.TenantIdentifier)
	if err := setServiceSelectorMatchLabels(r, map[string]string{
		"app":               PayloadPreProcessingName,
		LabelTenantInstance: deploymentName,
	}); err != nil {
		return fmt.Errorf("patch payload-pre-processing service selector: %w", err)
	}
	log.V(4).Info("Configured payload-pre-processing Service selector", "deployment", deploymentName)
	return nil
}

func patchCleanupCronJobImage(log logr.Logger, r *unstructured.Unstructured, params PlatformParams) error {
	log.V(4).Info("Patching cleanup CronJob image", "image", params.MaaSAPIKeyCleanupImage)
	if err := setCronJobContainerImage(r, "cleanup", params.MaaSAPIKeyCleanupImage); err != nil {
		return fmt.Errorf("patch cleanup CronJob image: %w", err)
	}

	// Patch the cleanup command to use tenant-specific service name
	containers, found, err := unstructured.NestedSlice(r.Object,
		"spec", "jobTemplate", "spec", "template", "spec", "containers")
	if err != nil {
		return fmt.Errorf("read cleanup CronJob containers: %w", err)
	}
	if found && len(containers) > 0 {
		container, ok := containers[0].(map[string]any)
		if !ok {
			return errors.New("cleanup CronJob container is not a map")
		}
		command, ok := container["command"].([]any)
		if ok && len(command) > 0 {
			tenantServiceName := MaaSAPIServiceName(params.TenantIdentifier)
			// Look for the curl command with maas-api:8443
			modified := false
			for i, cmdInterface := range command {
				if cmd, ok := cmdInterface.(string); ok && strings.Contains(cmd, "maas-api:8443") {
					// Replace maas-api with tenant-specific service name
					newCmd := strings.ReplaceAll(cmd, "maas-api:8443", tenantServiceName+":8443")
					command[i] = newCmd
					modified = true
					log.V(4).Info("Patching cleanup CronJob command URL", "old", "maas-api:8443", "new", tenantServiceName+":8443")
				}
			}
			if modified {
				container["command"] = command
				containers[0] = container
				if err := unstructured.SetNestedSlice(r.Object, containers,
					"spec", "jobTemplate", "spec", "template", "spec", "containers"); err != nil {
					return fmt.Errorf("write cleanup CronJob containers: %w", err)
				}
			}
		}
	}

	return nil
}

func patchHTTPRoute(log logr.Logger, r *unstructured.Unstructured, params PlatformParams) error {
	log.V(4).Info("Patching HTTPRoute parentRefs", "namespace", params.GatewayNamespace, "name", params.GatewayName)
	parentRefs, found, err := unstructured.NestedSlice(r.Object, "spec", "parentRefs")
	if err != nil {
		return fmt.Errorf("read HTTPRoute parentRefs: %w", err)
	}
	if !found || len(parentRefs) == 0 {
		return errors.New("HTTPRoute parentRefs not found")
	}
	ref, ok := parentRefs[0].(map[string]any)
	if !ok {
		return errors.New("HTTPRoute parentRefs[0] is not an object")
	}
	ref["namespace"] = params.GatewayNamespace
	ref["name"] = params.GatewayName
	parentRefs[0] = ref
	if err := unstructured.SetNestedSlice(r.Object, parentRefs, "spec", "parentRefs"); err != nil {
		return fmt.Errorf("write HTTPRoute parentRefs: %w", err)
	}

	// Patch backendRefs to point to the per-tenant maas-api Service.
	// The HTTPRoute has multiple rules (for /v1/models and /maas-api paths),
	// and each rule has backendRefs that need to be updated.
	tenantServiceName := MaaSAPIServiceName(params.TenantIdentifier)
	rules, found, err := unstructured.NestedSlice(r.Object, "spec", "rules")
	if err != nil {
		return fmt.Errorf("read HTTPRoute rules: %w", err)
	}
	if !found {
		return errors.New("HTTPRoute rules not found")
	}

	for i, ruleRaw := range rules {
		rule, ok := ruleRaw.(map[string]any)
		if !ok {
			return fmt.Errorf("HTTPRoute rule[%d] is not an object", i)
		}
		backendRefs, found, err := unstructured.NestedSlice(rule, "backendRefs")
		if err != nil {
			return fmt.Errorf("read HTTPRoute rule[%d] backendRefs: %w", i, err)
		}
		if !found {
			return fmt.Errorf("HTTPRoute rule[%d] has no backendRefs", i)
		}
		rewritten := false
		for j, backendRefRaw := range backendRefs {
			backendRef, ok := backendRefRaw.(map[string]any)
			if !ok {
				return fmt.Errorf("HTTPRoute rule[%d] backendRef[%d] is not an object", i, j)
			}
			// Update the Service name to the per-tenant Service
			if name, exists := backendRef["name"]; exists && name == "maas-api" {
				backendRef["name"] = tenantServiceName
				backendRefs[j] = backendRef
				rewritten = true
			}
		}
		if !rewritten {
			return fmt.Errorf("HTTPRoute rule[%d] has no \"maas-api\" backendRef to rewrite", i)
		}
		if err := unstructured.SetNestedSlice(rule, backendRefs, "backendRefs"); err != nil {
			return fmt.Errorf("write HTTPRoute rule[%d] backendRefs: %w", i, err)
		}
		rules[i] = rule
	}

	if err := unstructured.SetNestedSlice(r.Object, rules, "spec", "rules"); err != nil {
		return fmt.Errorf("write HTTPRoute rules: %w", err)
	}

	log.V(4).Info("Patched HTTPRoute backendRefs", "service", tenantServiceName)
	return nil
}

func patchMaaSAPIDestinationRule(log logr.Logger, r *unstructured.Unstructured, params PlatformParams) error {
	r.SetNamespace(params.GatewayNamespace)
	host, found, err := unstructured.NestedString(r.Object, "spec", "host")
	if err != nil {
		return fmt.Errorf("read maas-api DestinationRule host: %w", err)
	}
	if !found {
		return errors.New("maas-api DestinationRule host not found")
	}
	if host != "" {
		newHost := fmt.Sprintf("%s.%s.svc.cluster.local", MaaSAPIServiceName(params.TenantIdentifier), params.AppNamespace)
		log.V(4).Info("Patching maas-api DestinationRule host", "old", host, "new", newHost)
		if err := unstructured.SetNestedField(r.Object, newHost, "spec", "host"); err != nil {
			return fmt.Errorf("write maas-api DestinationRule host: %w", err)
		}
	}
	return nil
}

func patchPayloadDestinationRule(log logr.Logger, r *unstructured.Unstructured, params PlatformParams) error {
	name := r.GetName()
	r.SetNamespace(params.GatewayNamespace)
	newHost := fmt.Sprintf("%s.%s.svc.cluster.local", name, params.GatewayNamespace)
	host, found, err := unstructured.NestedString(r.Object, "spec", "host")
	if err != nil {
		return fmt.Errorf("read %s DestinationRule host: %w", name, err)
	}
	if found && host != newHost {
		log.V(4).Info("Patching payload DestinationRule host", "name", name, "old", host, "new", newHost)
		if err := unstructured.SetNestedField(r.Object, newHost, "spec", "host"); err != nil {
			return fmt.Errorf("write %s DestinationRule host: %w", name, err)
		}
	}
	// Keep tls.sni aligned with the service hostname. Without an explicit SNI,
	// Envoy falls back to the Istio cluster name (outbound|port||host), which
	// rustls rejects (illegal SNI) when the ExtProc upstream uses TLS.
	if err := unstructured.SetNestedField(r.Object, newHost, "spec", "trafficPolicy", "tls", "sni"); err != nil {
		return fmt.Errorf("write %s DestinationRule tls.sni: %w", name, err)
	}
	return nil
}

const rhclWasmFilterName = "envoy.filters.http.wasm"

// Kuadrant auth filter names in the gateway HTTP chain vary by Istio release.
// Emit INSERT_BEFORE/AFTER for each known name; istiod applies only the pair
// whose subFilter exists (others are no-ops). Avoids probing istiod version.
//
//	≤1.25:  extenstions.istio.io/wasmplugin/...  (Istio typo)
//	1.26–1.29: extensions.istio.io/wasmplugin/...
//	≥1.30:  extensions.istio.io/trafficextension/...~istio-translated-wasmplugin
//	RHCL 1.4: envoy.filters.http.wasm
func wasmpluginAnchorNameLegacyTypo(gatewayNamespace, gatewayName string) string {
	return fmt.Sprintf("extenstions.istio.io/wasmplugin/%s.kuadrant-%s", gatewayNamespace, gatewayName)
}

func wasmpluginAnchorName(gatewayNamespace, gatewayName string) string {
	return fmt.Sprintf("extensions.istio.io/wasmplugin/%s.kuadrant-%s", gatewayNamespace, gatewayName)
}

func trafficExtensionAnchorName(gatewayNamespace, gatewayName string) string {
	return fmt.Sprintf("extensions.istio.io/trafficextension/%s.kuadrant-%s~istio-translated-wasmplugin", gatewayNamespace, gatewayName)
}

// kuadrantAuthFilterAnchors returns auth-filter names used as EnvoyFilter
// INSERT anchors, one entry per supported mesh variant (order matches YAML).
func kuadrantAuthFilterAnchors(gatewayNamespace, gatewayName string) []string {
	return []string{
		wasmpluginAnchorNameLegacyTypo(gatewayNamespace, gatewayName),
		wasmpluginAnchorName(gatewayNamespace, gatewayName),
		trafficExtensionAnchorName(gatewayNamespace, gatewayName),
		rhclWasmFilterName,
	}
}

// grpcClusterName is the Istio CDS name for a Service (used by the llm-d IPP
// EnvoyFilter). Prefer extProcClusterName + CLUSTER ADD for Praxis: outbound|*
// clusters inject istio.metadata_exchange and break non-mesh gRPC.
func grpcClusterName(service, namespace string, port int) string {
	return fmt.Sprintf("outbound|%d||%s.%s.svc.cluster.local", port, service, namespace)
}

// extProcClusterName returns the dedicated Envoy cluster name for a Praxis
// ExtProc Service. These clusters are ADD'd by the payload-processing-praxis
// EnvoyFilter without istio.metadata_exchange.
func extProcClusterName(service string) string {
	return service + "-extproc"
}

func patchPayloadProcessingEnvoyFilter(log logr.Logger, r *unstructured.Unstructured, params PlatformParams) error {
	r.SetNamespace(params.GatewayNamespace)

	// Ensure we patch after Kuadrant's wasm INSERT (priority 0). Without this,
	// RHCL subFilter matches on envoy.filters.http.wasm never fire — especially
	// on secondary tenant gateways whose payload-processing EF is often created
	// before Kuadrant's per-gateway EF (same priority 0 → creationTimestamp order).
	if err := unstructured.SetNestedField(r.Object, PayloadProcessingEnvoyFilterPriority, "spec", "priority"); err != nil {
		return fmt.Errorf("write EnvoyFilter priority: %w", err)
	}

	if err := unstructured.SetNestedStringMap(r.Object,
		map[string]string{"gateway.networking.k8s.io/gateway-name": params.GatewayName},
		"spec", "workloadSelector", "labels"); err != nil {
		return fmt.Errorf("write EnvoyFilter workloadSelector: %w", err)
	}
	// targetRefs and workloadSelector are mutually exclusive (Istio 1.26+). Drop any
	// leftover targetRefs from older manifests so SSA/admission never sees both.
	unstructured.RemoveNestedField(r.Object, "spec", "targetRefs")
	unstructured.RemoveNestedField(r.Object, "spec", "targetRef")

	anchors := kuadrantAuthFilterAnchors(params.GatewayNamespace, params.GatewayName)
	beforeService := PayloadPreProcessingDeploymentName(params.TenantIdentifier)
	afterService := PayloadProcessingDeploymentName(params.TenantIdentifier)
	beforeHost := fmt.Sprintf("%s.%s.svc.cluster.local", beforeService, params.GatewayNamespace)
	afterHost := fmt.Sprintf("%s.%s.svc.cluster.local", afterService, params.GatewayNamespace)

	configPatches, found, err := unstructured.NestedSlice(r.Object, "spec", "configPatches")
	if err != nil {
		return fmt.Errorf("read EnvoyFilter configPatches: %w", err)
	}
	const (
		// One INSERT_BEFORE + INSERT_AFTER pair per kuadrantAuthFilterAnchors entry.
		filterPatchCount       = 8
		routeDisablePatchBase  = filterPatchCount
		routeDisablePatchCount = 4
		clusterPatchBase       = routeDisablePatchBase + routeDisablePatchCount
		clusterPatchCount      = 2
		legacyConfigPatches    = clusterPatchBase // llm-d: 8 filter + 4 route
		praxisConfigPatches    = clusterPatchBase + clusterPatchCount
	)
	if !found || len(configPatches) < legacyConfigPatches {
		return fmt.Errorf("EnvoyFilter configPatches: expected at least %d entries, got %d", legacyConfigPatches, len(configPatches))
	}
	if len(anchors)*2 != filterPatchCount {
		return fmt.Errorf("internal: filterPatchCount %d != 2*%d auth anchors", filterPatchCount, len(anchors))
	}

	useDedicatedClusters := len(configPatches) >= praxisConfigPatches
	beforeCluster := grpcClusterName(beforeService, params.GatewayNamespace, 9004)
	afterCluster := grpcClusterName(afterService, params.GatewayNamespace, 9004)
	if useDedicatedClusters {
		beforeCluster = extProcClusterName(beforeService)
		afterCluster = extProcClusterName(afterService)
	}

	subFilterByIndex := make([]string, 0, filterPatchCount)
	clusterByIndex := make([]string, 0, filterPatchCount)
	for _, anchor := range anchors {
		subFilterByIndex = append(subFilterByIndex, anchor, anchor)
		clusterByIndex = append(clusterByIndex, beforeCluster, afterCluster)
	}

	for i := 0; i < filterPatchCount; i++ {
		patch, ok := configPatches[i].(map[string]any)
		if !ok {
			return fmt.Errorf("EnvoyFilter configPatches[%d] is not an object", i)
		}

		subFilterPath := []string{"match", "listener", "filterChain", "filter", "subFilter", "name"}
		if err := unstructured.SetNestedField(patch, subFilterByIndex[i], subFilterPath...); err != nil {
			return fmt.Errorf("write configPatches[%d] subFilter.name: %w", i, err)
		}

		clusterPath := []string{"patch", "value", "typed_config", "grpc_service", "envoy_grpc", "cluster_name"}
		if err := unstructured.SetNestedField(patch, clusterByIndex[i], clusterPath...); err != nil {
			return fmt.Errorf("write configPatches[%d] grpc cluster_name: %w", i, err)
		}

		configPatches[i] = patch
	}

	// Patches after filter inserts disable ext_proc on non-inference maas-api routes.
	// Route name uses Istio's Gateway API convention: <namespace>.<httproute-name>.<rule-index>.
	// Rule indices: 0=/v1/models, 1=/v1/subscriptions, 2=/v1/api-keys, 3=/maas-api/*
	for i := routeDisablePatchBase; i < clusterPatchBase; i++ {
		patch, ok := configPatches[i].(map[string]any)
		if !ok {
			return fmt.Errorf("EnvoyFilter configPatches[%d] is not an object", i)
		}
		if err := unstructured.SetNestedField(patch,
			fmt.Sprintf("%s.%s.%d", params.AppNamespace, MaaSAPIRouteName(params.TenantIdentifier), i-routeDisablePatchBase),
			"match", "routeConfiguration", "vhost", "route", "name"); err != nil {
			return fmt.Errorf("write configPatches[%d] route name: %w", i, err)
		}
		configPatches[i] = patch
	}

	// Praxis: CLUSTER ADD patches (pre then post). DestinationRule does not apply
	// to these custom cluster names — keep TLS SNI + STRICT_DNS host in sync here.
	if useDedicatedClusters {
		clusterDefs := []struct {
			name string
			host string
		}{
			{beforeCluster, beforeHost},
			{afterCluster, afterHost},
		}
		for i, def := range clusterDefs {
			idx := clusterPatchBase + i
			patch, ok := configPatches[idx].(map[string]any)
			if !ok {
				return fmt.Errorf("EnvoyFilter configPatches[%d] is not an object", idx)
			}
			if err := patchExtProcClusterAdd(patch, def.name, def.host); err != nil {
				return fmt.Errorf("write configPatches[%d] CLUSTER ADD: %w", idx, err)
			}
			configPatches[idx] = patch
		}
	}

	if err := unstructured.SetNestedSlice(r.Object, configPatches, "spec", "configPatches"); err != nil {
		return fmt.Errorf("write EnvoyFilter configPatches: %w", err)
	}
	return nil
}

// patchExtProcClusterAdd rewrites a CLUSTER ADD patch's cluster name, SNI, and
// STRICT_DNS upstream address for the current gateway namespace / tenant.
func patchExtProcClusterAdd(patch map[string]any, clusterName, host string) error {
	if err := unstructured.SetNestedField(patch, clusterName, "patch", "value", "name"); err != nil {
		return fmt.Errorf("cluster name: %w", err)
	}
	if err := unstructured.SetNestedField(patch, clusterName, "patch", "value", "load_assignment", "cluster_name"); err != nil {
		return fmt.Errorf("load_assignment.cluster_name: %w", err)
	}
	if err := unstructured.SetNestedField(patch, host, "patch", "value", "transport_socket", "typed_config", "sni"); err != nil {
		return fmt.Errorf("tls sni: %w", err)
	}
	endpoints, found, err := unstructured.NestedSlice(patch, "patch", "value", "load_assignment", "endpoints")
	if err != nil {
		return fmt.Errorf("read endpoints: %w", err)
	}
	if !found || len(endpoints) == 0 {
		return errors.New("load_assignment.endpoints missing")
	}
	ep0, ok := endpoints[0].(map[string]any)
	if !ok {
		return errors.New("endpoints[0] is not an object")
	}
	lbEndpoints, found, err := unstructured.NestedSlice(ep0, "lb_endpoints")
	if err != nil {
		return fmt.Errorf("read lb_endpoints: %w", err)
	}
	if !found || len(lbEndpoints) == 0 {
		return errors.New("lb_endpoints missing")
	}
	lb0, ok := lbEndpoints[0].(map[string]any)
	if !ok {
		return errors.New("lb_endpoints[0] is not an object")
	}
	if err := unstructured.SetNestedField(lb0, host, "endpoint", "address", "socket_address", "address"); err != nil {
		return fmt.Errorf("socket_address.address: %w", err)
	}
	lbEndpoints[0] = lb0
	if err := unstructured.SetNestedSlice(ep0, lbEndpoints, "lb_endpoints"); err != nil {
		return fmt.Errorf("write lb_endpoints: %w", err)
	}
	endpoints[0] = ep0
	if err := unstructured.SetNestedSlice(patch, endpoints, "patch", "value", "load_assignment", "endpoints"); err != nil {
		return fmt.Errorf("write endpoints: %w", err)
	}
	return nil
}

func patchPayloadProcessingClusterRoleBinding(r *unstructured.Unstructured, params PlatformParams) error {
	subjects, found, err := unstructured.NestedSlice(r.Object, "subjects")
	if err != nil {
		return fmt.Errorf("read ClusterRoleBinding subjects: %w", err)
	}
	if !found || len(subjects) == 0 {
		return errors.New("ClusterRoleBinding subjects not found")
	}
	subj, ok := subjects[0].(map[string]any)
	if !ok {
		return errors.New("ClusterRoleBinding subjects[0] is not an object")
	}
	subj["namespace"] = params.GatewayNamespace
	subj["name"] = PayloadProcessingServiceAccountName(params.TenantIdentifier)
	subjects[0] = subj
	if err := unstructured.SetNestedSlice(r.Object, subjects, "subjects"); err != nil {
		return fmt.Errorf("write ClusterRoleBinding subjects: %w", err)
	}
	return nil
}

func patchPayloadProcessingNetworkPolicy(log logr.Logger, r *unstructured.Unstructured, params PlatformParams) error {
	r.SetNamespace(params.GatewayNamespace)

	tenantInstances := []any{
		PayloadProcessingDeploymentName(params.TenantIdentifier),
		PayloadPreProcessingDeploymentName(params.TenantIdentifier),
	}
	if err := unstructured.SetNestedField(r.Object, map[string]any{
		"matchExpressions": []any{
			map[string]any{
				"key":      LabelTenantInstance,
				"operator": "In",
				"values":   tenantInstances,
			},
		},
	}, "spec", "podSelector"); err != nil {
		return fmt.Errorf("write NetworkPolicy podSelector: %w", err)
	}

	// Rewrite the ext_proc ingress peer to match Istio-managed gateway pods in
	// GatewayNamespace. The base manifest hardcodes openshift-ingress, and
	// kustomize includeSelectors can pollute from[].podSelector with
	// payload-processing labels (gateway pods do not have those).
	// Keep only gateway.istio.io/managed — OpenShift managed ingress rejects
	// NetworkPolicies in openshift-ingress that match on gateway-name.
	if params.GatewayNamespace != "" {
		if err := patchNetworkPolicyExtProcPeer(r, params.GatewayNamespace); err != nil {
			return fmt.Errorf("write NetworkPolicy ext_proc ingress peer: %w", err)
		}
	}

	log.V(4).Info("Configured payload-processing NetworkPolicy",
		"tenantInstances", tenantInstances,
		"gatewayNamespace", params.GatewayNamespace)
	return nil
}

// patchNetworkPolicyExtProcPeer sets ingress[0].from[0] to allow gateway pods
// in the given namespace. Leaves monitoring rules untouched.
func patchNetworkPolicyExtProcPeer(r *unstructured.Unstructured, gatewayNamespace string) error {
	ingress, found, err := unstructured.NestedSlice(r.Object, "spec", "ingress")
	if err != nil {
		return err
	}
	if !found || len(ingress) == 0 {
		return nil
	}
	rule, ok := ingress[0].(map[string]any)
	if !ok {
		return nil
	}
	rule["from"] = []any{
		map[string]any{
			"namespaceSelector": map[string]any{
				"matchLabels": map[string]any{
					"kubernetes.io/metadata.name": gatewayNamespace,
				},
			},
			"podSelector": map[string]any{
				"matchLabels": map[string]any{
					"gateway.istio.io/managed": "istio.io-gateway-controller",
				},
			},
		},
	}
	return unstructured.SetNestedSlice(r.Object, ingress, "spec", "ingress")
}

// patchNetworkPolicyIngressNamespace sets kubernetes.io/metadata.name on the
// namespaceSelector of ingress[ruleIndex].from[0]. Leaves podSelector and other
// rules (e.g. monitoring) untouched.
func patchNetworkPolicyIngressNamespace(r *unstructured.Unstructured, ruleIndex int, namespace string) error {
	ingress, found, err := unstructured.NestedSlice(r.Object, "spec", "ingress")
	if err != nil {
		return err
	}
	if !found || ruleIndex < 0 || ruleIndex >= len(ingress) {
		return nil
	}
	rule, ok := ingress[ruleIndex].(map[string]any)
	if !ok {
		return nil
	}
	from, ok := rule["from"].([]any)
	if !ok || len(from) == 0 {
		return nil
	}
	peer, ok := from[0].(map[string]any)
	if !ok {
		return nil
	}
	nsSelector, ok := peer["namespaceSelector"].(map[string]any)
	if !ok {
		return nil
	}
	nsSelector["matchLabels"] = map[string]any{
		"kubernetes.io/metadata.name": namespace,
	}
	return unstructured.SetNestedSlice(r.Object, ingress, "spec", "ingress")
}

// replaceHostNamespace replaces the second segment of a dot-separated FQDN.
// e.g. "maas-api.maas-api.svc.cluster.local" → "maas-api.opendatahub.svc.cluster.local"
func replaceHostNamespace(host, ns string) string {
	parts := strings.SplitN(host, ".", 3)
	if len(parts) >= 2 {
		parts[1] = ns
		return strings.Join(parts, ".")
	}
	return host
}

func setContainerImage(r *unstructured.Unstructured, containerName, image string) error {
	containers, found, err := unstructured.NestedSlice(r.Object, "spec", "template", "spec", "containers")
	if err != nil || !found {
		return errors.New("containers not found")
	}
	for i, c := range containers {
		if cm, ok := c.(map[string]any); ok && cm["name"] == containerName {
			cm["image"] = image
			containers[i] = cm
			return unstructured.SetNestedSlice(r.Object, containers, "spec", "template", "spec", "containers")
		}
	}
	return fmt.Errorf("container %q not found", containerName)
}

func setOrAddEnvVar(r *unstructured.Unstructured, containerName, envName, envValue string) error {
	containers, found, err := unstructured.NestedSlice(r.Object, "spec", "template", "spec", "containers")
	if err != nil || !found {
		return errors.New("containers not found")
	}
	for i, c := range containers {
		cm, ok := c.(map[string]any)
		if !ok || cm["name"] != containerName {
			continue
		}
		envSlice, _ := cm["env"].([]any)
		for j, e := range envSlice {
			if em, ok := e.(map[string]any); ok && em["name"] == envName {
				em["value"] = envValue
				delete(em, "valueFrom")
				envSlice[j] = em
				cm["env"] = envSlice
				containers[i] = cm
				return unstructured.SetNestedSlice(r.Object, containers, "spec", "template", "spec", "containers")
			}
		}
		envSlice = append(envSlice, map[string]any{"name": envName, "value": envValue})
		cm["env"] = envSlice
		containers[i] = cm
		return unstructured.SetNestedSlice(r.Object, containers, "spec", "template", "spec", "containers")
	}
	return fmt.Errorf("container %q not found", containerName)
}

func setCronJobContainerImage(r *unstructured.Unstructured, containerName, image string) error {
	containers, found, err := unstructured.NestedSlice(r.Object, "spec", "jobTemplate", "spec", "template", "spec", "containers")
	if err != nil || !found {
		return errors.New("containers not found")
	}
	for i, c := range containers {
		if cm, ok := c.(map[string]any); ok && cm["name"] == containerName {
			cm["image"] = image
			containers[i] = cm
			return unstructured.SetNestedSlice(r.Object, containers, "spec", "jobTemplate", "spec", "template", "spec", "containers")
		}
	}
	return fmt.Errorf("container %q not found", containerName)
}

// addPodTemplateLabel adds a label to the Deployment's pod template spec.
// This label will be set on all pods created by the Deployment.
func addPodTemplateLabel(r *unstructured.Unstructured, key, value string) error {
	labels, found, err := unstructured.NestedStringMap(r.Object, "spec", "template", "metadata", "labels")
	if err != nil {
		return fmt.Errorf("read pod template labels: %w", err)
	}
	if !found || labels == nil {
		labels = make(map[string]string)
	}
	labels[key] = value
	return unstructured.SetNestedStringMap(r.Object, labels, "spec", "template", "metadata", "labels")
}

func setDeploymentSelectorMatchLabels(r *unstructured.Unstructured, labels map[string]string) error {
	return unstructured.SetNestedStringMap(r.Object, labels, "spec", "selector", "matchLabels")
}

func setServiceSelectorMatchLabels(r *unstructured.Unstructured, labels map[string]string) error {
	return unstructured.SetNestedStringMap(r.Object, labels, "spec", "selector")
}

func patchDeploymentServiceAccountName(r *unstructured.Unstructured, serviceAccountName string) error {
	return unstructured.SetNestedField(r.Object, serviceAccountName, "spec", "template", "spec", "serviceAccountName")
}

// addServiceSelectorLabel adds a label to the Service selector.
// This ensures the Service only routes to pods with matching labels.
func addServiceSelectorLabel(r *unstructured.Unstructured, key, value string) error {
	selector, found, err := unstructured.NestedStringMap(r.Object, "spec", "selector")
	if err != nil {
		return fmt.Errorf("read service selector: %w", err)
	}
	if !found || selector == nil {
		selector = make(map[string]string)
	}
	selector[key] = value
	return unstructured.SetNestedStringMap(r.Object, selector, "spec", "selector")
}

func patchConfigMapVolumeRef(r *unstructured.Unstructured, volumeName, configMapName string) error {
	volumes, found, err := unstructured.NestedSlice(r.Object, "spec", "template", "spec", "volumes")
	if err != nil {
		return fmt.Errorf("read pod volumes: %w", err)
	}
	if !found {
		return nil
	}
	for i, vol := range volumes {
		volMap, ok := vol.(map[string]any)
		if !ok {
			continue
		}
		name, _, _ := unstructured.NestedString(volMap, "name")
		if name != volumeName {
			continue
		}
		if err := unstructured.SetNestedField(volMap, configMapName, "configMap", "name"); err != nil {
			return fmt.Errorf("write volume %q configMap name: %w", volumeName, err)
		}
		volumes[i] = volMap
		return unstructured.SetNestedSlice(r.Object, volumes, "spec", "template", "spec", "volumes")
	}
	return nil
}
