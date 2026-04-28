// Package tenantreconcile mirrors the Open Data Hub operator modelsasservice component pipeline
// (initialize → dependencies → prerequisites → gateway → params → kustomize → post-render → apply → deployment status).
package tenantreconcile

import "k8s.io/apimachinery/pkg/runtime/schema"

const (
	// ComponentName matches the ODH modelsasservice component label key suffix (app.opendatahub.io/<name>).
	ComponentName = "modelsasservice"

	LabelODHAppPrefix    = "app.opendatahub.io"
	LabelK8sPartOf       = "app.kubernetes.io/part-of"
	LabelTenantName      = "maas.opendatahub.io/tenant-name"
	LabelTenantNamespace = "maas.opendatahub.io/tenant-namespace"

	DefaultGatewayNamespace = "openshift-ingress"
	DefaultGatewayName      = "maas-default-gateway"

	GatewayDefaultAuthPolicyName               = "gateway-default-auth"
	GatewayTokenRateLimitDefaultDenyPolicyName = "gateway-default-deny"
	MaaSAPIAuthPolicyName                      = "maas-api-auth-policy"
	GatewayDestinationRuleName                 = "maas-api-backend-tls"
	TelemetryPolicyName                        = "maas-telemetry"
	IstioTelemetryName                         = "latency-per-subscription"
	MaaSParametersConfigMapName                = "maas-parameters"
	MaaSAPIDeploymentName                      = "maas-api"
	MaaSDBSecretName                           = "maas-db-config" //nolint:gosec // secret name reference, not a credential
	MaaSDBSecretKey                            = "DB_CONNECTION_URL"

	MonitoringNamespace         = "openshift-monitoring"
	ClusterMonitoringConfigName = "cluster-monitoring-config"

	// Condition types aligned with ODH internal/controller/status for DSC aggregation parity.
	ConditionDependenciesAvailable      = "DependenciesAvailable"
	ConditionMaaSPrerequisitesAvailable = "MaaSPrerequisitesAvailable"
	ConditionDeploymentsAvailable       = "DeploymentsAvailable"
	ConditionTypeDegraded               = "Degraded"
	ReadyConditionType                  = "Ready"
)

// ImageParamKeys maps params.env keys to RELATED_IMAGE_* env vars on the controller/operator process
// (same as ODH modelsasservice_support.go imagesMap). Tenant reconcile merges live maas-parameters
// ConfigMap data first, then fills any still-empty image keys from these env vars (dev / partial CM):
// RELATED_IMAGE_ODH_MAAS_API_IMAGE, RELATED_IMAGE_ODH_MAAS_CONTROLLER_IMAGE,
// RELATED_IMAGE_ODH_AI_GATEWAY_PAYLOAD_PROCESSING_IMAGE, RELATED_IMAGE_UBI_MINIMAL_IMAGE.
var ImageParamKeys = map[string]string{
	"maas-api-image":             "RELATED_IMAGE_ODH_MAAS_API_IMAGE",
	"maas-controller-image":      "RELATED_IMAGE_ODH_MAAS_CONTROLLER_IMAGE",
	"payload-processing-image":   "RELATED_IMAGE_ODH_AI_GATEWAY_PAYLOAD_PROCESSING_IMAGE",
	"maas-api-key-cleanup-image": "RELATED_IMAGE_UBI_MINIMAL_IMAGE",
}

// OptionalAPIGroups lists API groups whose CRDs may be installed after MaaS; Tenant reconcile watches
// these CRDs and skips apply NoMatch for matching groups (see ApplyRendered and tenant_controller predicates).
var OptionalAPIGroups = map[string]struct{}{
	"perses.dev": {},
}

// GVKs used for post-render and readiness (mirrors opendatahub-operator/pkg/cluster/gvk selections for modelsasservice).
var (
	GVKConfigMap            = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	GVKDeployment           = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	GVKAuthPolicy           = schema.GroupVersionKind{Group: "kuadrant.io", Version: "v1", Kind: "AuthPolicy"}
	GVKTokenRateLimitPolicy = schema.GroupVersionKind{Group: "kuadrant.io", Version: "v1alpha1", Kind: "TokenRateLimitPolicy"}
	GVKDestinationRule      = schema.GroupVersionKind{Group: "networking.istio.io", Version: "v1", Kind: "DestinationRule"}
	GVKTelemetryPolicy      = schema.GroupVersionKind{Group: "extensions.kuadrant.io", Version: "v1alpha1", Kind: "TelemetryPolicy"}
	GVKEnvoyFilter          = schema.GroupVersionKind{Group: "networking.istio.io", Version: "v1alpha3", Kind: "EnvoyFilter"}
	GVKIstioTelemetry       = schema.GroupVersionKind{Group: "telemetry.istio.io", Version: "v1", Kind: "Telemetry"}
	GVKAuthConfig           = schema.GroupVersionKind{Group: "authorino.kuadrant.io", Version: "v1beta3", Kind: "AuthConfig"}
	GVKAuthorino            = schema.GroupVersionKind{Group: "operator.authorino.kuadrant.io", Version: "v1beta1", Kind: "Authorino"}
)
