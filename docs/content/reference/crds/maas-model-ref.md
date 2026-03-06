# MaaSModelRef

Represents an AI/ML model endpoint in the MaaS catalog. The MaaS API lists models from MaaSModelRef resources (using `status.endpoint` and `status.phase`).

## MaaSModelRefSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| modelRef | ModelReference | Yes | Reference to the model endpoint |

## ModelReference

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| kind | string | Yes | One of: `LLMInferenceService`, `ExternalModel` (not yet implemented -- setting this kind will result in Phase=Failed with Reason=Unsupported) |
| name | string | Yes | Name of the model resource |
| namespace | string | No | Namespace of the model resource (defaults to same namespace as MaaSModelRef) |

## MaaSModelRefStatus

| Field | Type | Description |
|-------|------|-------------|
| phase | string | One of: `Pending`, `Ready`, `Unhealthy`, `Failed` |
| endpoint | string | Endpoint URL for the model |
| httpRouteName | string | Name of the HTTPRoute associated with this model |
| httpRouteNamespace | string | Namespace of the HTTPRoute |
| httpRouteGatewayName | string | Name of the Gateway that the HTTPRoute references |
| httpRouteGatewayNamespace | string | Namespace of the Gateway that the HTTPRoute references |
| httpRouteHostnames | []string | Hostnames configured on the HTTPRoute |
| conditions | []Condition | Latest observations of the model's state |
