# MaaS ODH Overlay

This overlay deploys Models-as-a-Service (MaaS) for OpenDataHub (ODH) with:
- maas-api with TLS enabled
- maas-controller with per-route authentication and rate limits
- Integration with ODH's default Gateway in `openshift-ingress` namespace

## Prerequisites

**CRITICAL:** Before deploying this overlay or enabling modelsAsService in ODH, you **must** create the database Secret.

### 1. PostgreSQL Database

MaaS requires a PostgreSQL database for API key management. Provision one of:
- **AWS RDS for PostgreSQL** (recommended for AWS)
- **Azure Database for PostgreSQL** (recommended for Azure)
- **Crunchy PostgreSQL Operator** (recommended for on-premises)
- **Self-managed PostgreSQL** with backups and HA

### 2. Create Database Secret

Create the `maas-db-config` Secret in the target namespace (typically `opendatahub`):

```bash
kubectl create secret generic maas-db-config \
  -n opendatahub \
  --from-literal=DB_CONNECTION_URL='postgresql://username:password@hostname:5432/maas?sslmode=require'
```

**Connection String Format:**
```
postgresql://USERNAME:PASSWORD@HOSTNAME:PORT/DATABASE?sslmode=require
```

**Example:**
```bash
# AWS RDS
kubectl create secret generic maas-db-config \
  -n opendatahub \
  --from-literal=DB_CONNECTION_URL='postgresql://maasuser:SecurePass123@mydb.abc123.us-east-1.rds.amazonaws.com:5432/maas?sslmode=require'

# Crunchy PostgreSQL Operator
kubectl create secret generic maas-db-config \
  -n opendatahub \
  --from-literal=DB_CONNECTION_URL='postgresql://maasuser:SecurePass123@postgres-primary.opendatahub.svc.cluster.local:5432/maas?sslmode=require'
```

## Deployment via ODH Operator (Recommended)

When MaaS is integrated with ODH, the operator deploys it automatically:

```yaml
apiVersion: datasciencecluster.opendatahub.io/v1
kind: DataScienceCluster
metadata:
  name: default-dsc
spec:
  components:
    kserve:
      managementState: Managed
      modelsAsService:
        managementState: Managed  # Deploys maas-api and maas-controller
```

The operator reads manifests from this overlay and applies them to the `opendatahub` namespace.

## Manual Deployment (Development/Testing)

For development or testing outside of ODH operator:

```bash
# Ensure the database Secret exists first!
kubectl get secret maas-db-config -n opendatahub

# Deploy MaaS
kustomize build deployment/overlays/odh | kubectl apply -f -
```

## Configuration

This overlay is configured via `params.env`:

```env
maas-api-image=quay.io/opendatahub/maas-api:latest
gateway-namespace=openshift-ingress
gateway-name=maas-default-gateway
app-namespace=opendatahub
```

## Verification

After deployment, verify maas-api is running:

```bash
# Check maas-api pod status
kubectl get pods -n opendatahub -l app.kubernetes.io/name=maas-api

# Check logs for successful database connection
kubectl logs -n opendatahub -l app.kubernetes.io/name=maas-api

# Should see:
# "Server starting" address=":8443" secure=true
# NOT: "configuration validation failed: db connection URL is required"
```

## Troubleshooting

### Pod CrashLoopBackOff with "Secret not found"

**Cause:** The `maas-db-config` Secret was not created before deployment.

**Fix:**
```bash
kubectl create secret generic maas-db-config \
  -n opendatahub \
  --from-literal=DB_CONNECTION_URL='postgresql://...'

# Restart the deployment
kubectl rollout restart deployment/maas-api -n opendatahub
```

### Pod fails with "db connection URL is required"

**Cause:** The Secret exists but the `DB_CONNECTION_URL` key is missing or empty.

**Fix:**
```bash
# Check Secret contents
kubectl get secret maas-db-config -n opendatahub -o jsonpath='{.data.DB_CONNECTION_URL}' | base64 -d

# Delete and recreate with correct key
kubectl delete secret maas-db-config -n opendatahub
kubectl create secret generic maas-db-config \
  -n opendatahub \
  --from-literal=DB_CONNECTION_URL='postgresql://...'
```

### Cannot connect to database

**Cause:** Invalid connection string or database unreachable.

**Fix:**
- Verify database hostname is reachable from the cluster
- Check credentials are correct
- Ensure database accepts connections from cluster IP range
- For RDS/Azure, check security groups/firewall rules

## Documentation

- [PostgreSQL Configuration Guide](../../docs/content/configuration-and-management/POSTGRESQL_DEPLOYMENT.md)
- [MaaS Installation Prerequisites](../../docs/content/install/prerequisites.md)
- [MaaS Setup Guide](../../docs/content/install/maas-setup.md)
