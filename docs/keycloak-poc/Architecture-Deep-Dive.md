# Keycloak Architecture Deep Dive

Comprehensive documentation of the Keycloak-based authentication and authorization flow for MaaS.

## Overview

The Keycloak PoC replaces Kubernetes Service Account token minting with Keycloak OAuth 2.0 token exchange, enabling external Identity Provider (IDP) integration while maintaining tier-based access control.

## Architecture Components

### 1. Keycloak Server

**Deployment**: `keycloak` namespace
**Purpose**: OAuth 2.0 / OIDC Identity Provider

**Configuration**:
- **Realm**: `maas`
- **Clients**:
  - `maas-api`: Service account client for token exchange
  - `maas-model-access`: Public client (audience for model tokens)
- **Groups**: `tier-free-users`, `tier-premium-users`, `tier-enterprise-users`
- **Users**: Test users assigned to tier groups

### 2. Gateway AuthPolicy

**Target**: `maas-default-gateway` (Gateway)
**Authentication**: Keycloak JWT via OIDC discovery
**Metadata**: Tier lookup via `/v1/tiers/lookup` endpoint
**Response Headers**: `X-MaaS-Username`, `X-MaaS-Group`

### 3. MaaS API AuthPolicy

**Target**: `maas-api-route` (HTTPRoute)
**Authentication**: Keycloak JWT
**Purpose**: Validates Keycloak tokens for token minting requests

### 4. MaaS API Service

**Token Manager**: KeycloakTokenManager
- Accepts user's Keycloak token
- Uses token directly (no exchange needed for PoC)
- Extracts user identity from token claims

## Authentication Flow

```mermaid
sequenceDiagram
    participant User
    participant Keycloak
    participant Gateway
    participant Authorino
    participant MaaSAPI
    participant Model

    Note over User,Keycloak: Step 1: User Authentication
    User->>Keycloak: POST /realms/maas/protocol/openid-connect/token<br/>(username, password, grant_type=password)
    Keycloak-->>User: Access Token (JWT with preferred_username, groups)

    Note over User,MaaSAPI: Step 2: Token Minting
    User->>Gateway: POST /maas-api/v1/tokens<br/>Authorization: Bearer {Keycloak Token}
    Gateway->>Authorino: Enforce MaaS API AuthPolicy
    Authorino->>Keycloak: OIDC Discovery + JWT Validation
    Keycloak-->>Authorino: Token Valid (user identity)
    Authorino->>Gateway: Authenticated (X-MaaS-Username, X-MaaS-Group)
    Gateway->>MaaSAPI: Forward request with headers
    MaaSAPI->>MaaSAPI: Extract user info from headers
    MaaSAPI->>MaaSAPI: Use Keycloak token directly as minted token
    MaaSAPI-->>User: Minted Token (same Keycloak token)

    Note over User,Model: Step 3: Model Access
    User->>Gateway: GET /maas-api/v1/models<br/>Authorization: Bearer {Minted Token}
    Gateway->>Authorino: Enforce Gateway AuthPolicy
    Authorino->>Keycloak: OIDC Discovery + JWT Validation
    Keycloak-->>Authorino: Token Valid
    Authorino->>MaaSAPI: Tier Lookup (POST /v1/tiers/lookup)
    MaaSAPI-->>Authorino: Tier Information
    Authorino->>Gateway: Authenticated + Tier Metadata
    Gateway->>MaaSAPI: Forward request
    MaaSAPI->>MaaSAPI: Authorization Check (HTTP loopback)
    MaaSAPI->>Gateway: GET /llm/{model}/v1/chat/completions<br/>Authorization: Bearer {User Token}
    Gateway->>Authorino: Enforce Gateway AuthPolicy
    Authorino->>Keycloak: Validate Token
    Keycloak-->>Authorino: Valid
    Gateway->>Model: Forward request
    Model-->>Gateway: Response
    Gateway-->>MaaSAPI: Response
    MaaSAPI-->>User: Model List
```

## Token Minting Flow

```mermaid
sequenceDiagram
    participant User
    participant Gateway
    participant MaaSAPI
    participant Keycloak

    User->>Gateway: POST /maas-api/v1/tokens<br/>Authorization: Bearer {Keycloak Token}
    Gateway->>MaaSAPI: Request with X-MaaS-Username, X-MaaS-Group
    
    Note over MaaSAPI: KeycloakTokenManager.GenerateToken()
    MaaSAPI->>MaaSAPI: Extract user info from headers<br/>(username, groups)
    MaaSAPI->>MaaSAPI: Use Keycloak token directly<br/>(no exchange needed)
    MaaSAPI->>MaaSAPI: Extract claims (jti, iat, exp)
    MaaSAPI->>MaaSAPI: Create Token object
    
    MaaSAPI-->>User: {<br/>  "token": "{Keycloak JWT}",<br/>  "expiration": "1h",<br/>  "expiresAt": 1234567890<br/>}
```

## Model Authorization Flow

```mermaid
sequenceDiagram
    participant User
    participant Gateway
    participant Authorino
    participant MaaSAPI
    participant TierLookup
    participant ModelAuth
    participant Model

    User->>Gateway: POST /llm/{model}/v1/chat/completions<br/>Authorization: Bearer {Keycloak Token}
    Gateway->>Authorino: Enforce Gateway AuthPolicy
    
    Note over Authorino: Authentication Phase
    Authorino->>Keycloak: OIDC Discovery<br/>GET /.well-known/openid-configuration
    Keycloak-->>Authorino: OIDC Configuration
    Authorino->>Keycloak: Validate JWT Signature
    Keycloak-->>Authorino: Token Valid<br/>(preferred_username, groups)
    
    Note over Authorino: Metadata Phase - Tier Lookup
    Authorino->>TierLookup: POST /v1/tiers/lookup<br/>{ "groups": ["tier-free-users"] }
    TierLookup->>TierLookup: Match groups to tier
    TierLookup-->>Authorino: { "tier": "free", "displayName": "Free Tier" }
    Note over Authorino: Store in auth.metadata.matchedTier
    
    Note over Authorino: Metadata Phase - Model Authorization
    Authorino->>ModelAuth: POST /v1/models/authorize<br/>{ "path": "/llm/{model}/v1/chat/completions", "tier": "free" }
    ModelAuth->>ModelAuth: Lookup LLMInferenceService by name
    ModelAuth->>ModelAuth: Check tier annotation<br/>(alpha.maas.opendatahub.io/tiers)
    ModelAuth-->>Authorino: { "allowed": true/false, "reason": "..." }
    Note over Authorino: Store in auth.metadata.modelAccess
    
    Note over Authorino: Authorization Phase
    Authorino->>Authorino: OPA Rego Policy:<br/>Check auth.metadata.modelAccess.allowed
    alt Access Allowed
        Authorino->>Gateway: Authorized
        Gateway->>Model: Forward request
        Model-->>Gateway: Response
        Gateway-->>User: Model response
    else Access Denied
        Authorino->>Gateway: 403 Forbidden
        Gateway-->>User: 403 Forbidden
    end
```

## Keycloak Token Structure

```json
{
  "iss": "https://keycloak.example.com/realms/maas",
  "aud": "maas-model-access",
  "sub": "user-uuid",
  "preferred_username": "free-user-1",
  "groups": ["tier-free-users"],
  "exp": 1234567890,
  "iat": 1234564290,
  "jti": "token-id"
}
```

## Tier Mapping

Tiers are determined by user group membership:

| Group | Tier | Level |
|-------|------|-------|
| `tier-free-users` | `free` | 1 |
| `tier-premium-users` | `premium` | 2 |
| `tier-enterprise-users` | `enterprise` | 3 |

The tier lookup endpoint (`/v1/tiers/lookup`) matches user groups against the `tier-to-group-mapping` ConfigMap to determine the user's tier.

## Authorization Check Mechanism

The Gateway AuthPolicy performs model authorization using metadata lookups:

### Tier Lookup Metadata (`matchedTier`)

**Endpoint**: `POST /v1/tiers/lookup`  
**Called by**: Gateway AuthPolicy metadata phase  
**Purpose**: Determine user's subscription tier based on Keycloak group membership

**Request**:
```json
{
  "groups": ["tier-free-users", "tier-premium-users"]
}
```

**Response**:
```json
{
  "tier": "premium",
  "displayName": "Premium Tier"
}
```

**Caching**: 300 seconds per username (`auth.identity.preferred_username`)

**Stored in**: `auth.metadata.matchedTier["tier"]`

### Model Authorization Metadata (`modelAccess`)

**Endpoint**: `POST /v1/models/authorize`  
**Called by**: Gateway AuthPolicy metadata phase (only for `/llm/*` paths)  
**Purpose**: Check if user's tier matches model's tier requirement

**Request**:
```json
{
  "path": "/llm/facebook-opt-125m-simulated/v1/chat/completions",
  "tier": "free"
}
```

**Response**:
```json
{
  "allowed": true,
  "reason": "user tier 'free' matches model's allowed tiers"
}
```

**Authorization Logic**:
1. Extract model name from path (`/llm/{model-name}/...`)
2. Lookup `LLMInferenceService` by name across all namespaces
3. Check `alpha.maas.opendatahub.io/tiers` annotation:
   - Empty annotation or empty array → All tiers allowed
   - Non-empty array → User's tier must be in the list
4. Return `allowed: true/false` with optional reason

**Caching**: Disabled (TTL: 0) for real-time authorization checks

**Stored in**: `auth.metadata.modelAccess.allowed`

### OPA Rego Authorization Policy

The Gateway AuthPolicy uses OPA Rego to evaluate the `modelAccess` metadata:

```rego
package authz

# Allow if this is NOT a model endpoint (maas-api routes, etc.)
allow {
  not startswith(input.request.path, "/llm/")
}

# Allow if this IS a model endpoint AND model access is allowed
allow {
  startswith(input.request.path, "/llm/")
  input.auth.metadata.modelAccess.allowed == true
}
```

**Evaluation**:
- Non-model endpoints (`/maas-api/*`) → Always allowed
- Model endpoints (`/llm/*`) → Allowed only if `modelAccess.allowed == true`
- Returns `403 Forbidden` if authorization fails

## Configuration Files

### Gateway AuthPolicy

**Location**: `deployment/overlays/keycloak/policies/gateway-auth-policy-keycloak.yaml`

**Key Settings**:
- `authentication.keycloak.jwt.issuerUrl`: Keycloak realm URL (auto-substituted)
- `metadata.matchedTier.http.url`: Tier lookup endpoint
- `response.success.headers`: Identity headers injected

### MaaS API AuthPolicy

**Location**: `deployment/overlays/keycloak/policies/maas-api-auth-policy.yaml`

**Key Settings**:
- `authentication.keycloak.jwt.issuerUrl`: Keycloak realm URL
- `response.success.headers`: User identity headers

### MaaS API Deployment

**Location**: `deployment/overlays/keycloak/maas-api-env-patch.yaml`

**Environment Variables**:
- `KEYCLOAK_ENABLED=true`
- `KEYCLOAK_BASE_URL`: Keycloak base URL
- `KEYCLOAK_REALM=maas`
- `KEYCLOAK_CLIENT_ID=maas-api`
- `KEYCLOAK_CLIENT_SECRET=maas-api-secret`
- `KEYCLOAK_AUDIENCE=maas-model-access`

## Security Considerations

### Token Validation

- **Gateway Level**: All tokens validated via OIDC discovery
- **MaaS API Level**: Tokens validated by Gateway AuthPolicy before reaching API
- **Model Level**: Tokens validated again by Gateway AuthPolicy for model endpoints

### Token Exchange

For the PoC, we use the user's Keycloak token directly. In production, consider:
- Token exchange for audience-specific tokens
- Token refresh mechanisms
- Token revocation

### Authorization

Currently, authorization is permissive (all authenticated users can access models). For production:
- Implement OPA Rego policies
- Use Kubernetes SubjectAccessReview
- Integrate Keycloak Authorization Services

## Troubleshooting

### Token Validation Failures

**Symptom**: `401 Unauthorized` when accessing models

**Check**:
```bash
# Verify Keycloak issuer URL
kubectl get authpolicy gateway-auth-policy -n openshift-ingress -o jsonpath='{.spec.rules.authentication.keycloak.jwt.issuerUrl}'

# Check Authorino logs
kubectl logs -n kuadrant-system deployment/authorino | grep -i oidc
```

### Tier Lookup Failures

**Symptom**: Models not appearing in list or tier not determined

**Check**:
```bash
# Verify tier mapping ConfigMap
kubectl get configmap tier-to-group-mapping -n opendatahub -o yaml

# Check tier lookup endpoint
kubectl logs -n opendatahub deployment/maas-api | grep -i tier

# Test tier lookup directly
curl -k -X POST "https://maas.${CLUSTER_DOMAIN}/maas-api/v1/tiers/lookup" \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"groups": ["tier-free-users"]}'
```

### Model Authorization Failures

**Symptom**: `403 Forbidden` when accessing model endpoints

**Check**:
```bash
# Verify LLMInferenceService exists and has tier annotation
kubectl get llminferenceservice -A -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.annotations.alpha\.maas\.opendatahub\.io/tiers}{"\n"}{end}'

# Check model authorization endpoint logs
kubectl logs -n opendatahub deployment/maas-api | grep -i "ModelAuthorize"

# Test model authorization directly
curl -k -X POST "https://maas.${CLUSTER_DOMAIN}/maas-api/v1/models/authorize" \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"path": "/llm/facebook-opt-125m-simulated/v1/chat/completions", "tier": "free"}'
```

### TLS Certificate Issues

**Symptom**: `x509: certificate signed by unknown authority` in Authorino logs when calling maas-api metadata endpoints

**Description**: Authorino may fail to verify TLS certificates when calling internal maas-api endpoints for metadata lookups (`/v1/tiers/lookup` and `/v1/models/authorize`).

**Solution**: Follow the TLS configuration instructions in the repository to configure Authorino with the appropriate CA bundle. See the deployment scripts for automated CA bundle configuration.

**Check**:
```bash
# Check Authorino logs for TLS errors
kubectl logs -n kuadrant-system deployment/authorino | grep -i "certificate\|tls\|x509"

# Verify Authorino has CA bundle mounted
kubectl get authorino authorino -n kuadrant-system -o jsonpath='{.spec.volumes}'
```

## Production Recommendations

1. **Persistent Keycloak Storage**: Use PostgreSQL instead of ephemeral database
2. **High Availability**: Deploy Keycloak with multiple replicas
3. **TLS**: Enable TLS for all Keycloak communications
4. **Token Exchange**: Implement proper token exchange for audience-specific tokens
5. **Authorization**: Add OPA Rego policies or Keycloak Authorization Services
6. **Monitoring**: Add Prometheus metrics for Keycloak operations
7. **Backup**: Implement Keycloak realm backup/restore procedures
