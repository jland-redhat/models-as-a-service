# PostgreSQL Deployment for MaaS API Key Management

## Overview

PostgreSQL is **required** for the MaaS API Key Management system. It provides persistent storage for:

- API key metadata (name, creation time, expiration)
- Hashed API keys (BCrypt hashes for validation)
- Usage tracking (last_used_at timestamps)
- Revocation status

Without PostgreSQL, the maas-api will fail to start with error:
```
configuration validation failed: db connection URL is required
```

---

## Table of Contents

1. [Production ODH/RHOAI Prerequisite](#production-odhrhoai-prerequisite)
2. [Quick Start (Development)](#quick-start-development)
3. [Deployment Methods](#deployment-methods)
4. [Configuration](#configuration)
5. [Database Schema](#database-schema)
6. [Connection Details](#connection-details)
7. [Production Considerations](#production-considerations)
8. [Troubleshooting](#troubleshooting)

---

## Production ODH/RHOAI Prerequisite

**IMPORTANT:** For production ODH or RHOAI deployments, you **must** create the database Secret **before** enabling modelsAsService in your DataScienceCluster.

### Step 1: Provision PostgreSQL Database

Choose one of the following production-grade PostgreSQL solutions:

- **AWS RDS for PostgreSQL** (recommended for AWS)
- **Azure Database for PostgreSQL** (recommended for Azure)
- **Crunchy PostgreSQL Operator** (recommended for on-premises OpenShift)
- **Self-managed PostgreSQL cluster** with backups and high availability

### Step 2: Create Database Secret

Create the `maas-db-config` Secret in your ODH/RHOAI namespace (typically `opendatahub`):

```bash
kubectl create secret generic maas-db-config \
  -n opendatahub \
  --from-literal=DB_CONNECTION_URL='postgresql://username:password@hostname:5432/maas?sslmode=require'
```

**Connection String Format:**
```
postgresql://USERNAME:PASSWORD@HOSTNAME:PORT/DATABASE?sslmode=require
```

**Example for AWS RDS:**
```bash
kubectl create secret generic maas-db-config \
  -n opendatahub \
  --from-literal=DB_CONNECTION_URL='postgresql://maasuser:mypassword@mydb.abc123.us-east-1.rds.amazonaws.com:5432/maas?sslmode=require'
```

### Step 3: Enable modelsAsService

After creating the Secret, enable MaaS in your DataScienceCluster:

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
        managementState: Managed  # Enable after creating maas-db-config Secret
```

!!! warning "Deployment Order"
    The maas-api Deployment will fail if the Secret does not exist. The Pod status will show:
    ```
    Error: secret "maas-db-config" not found
    ```

---

## Quick Start (Development)

### Option 1: Automatic Deployment with deploy.sh (Development Only)

PostgreSQL is deployed **automatically** when using `scripts/deploy.sh` for development:

```bash
# Deploy MaaS with PostgreSQL (included by default)
./scripts/deploy.sh

# PostgreSQL is deployed to the target namespace (opendatahub or maas-system)
```

### Option 2: Manual Deployment

PostgreSQL is deployed automatically as a prerequisite. For manual deployment outside the standard flow:

```bash
# Use the deploy.sh script (PostgreSQL is deployed automatically)
./scripts/deploy.sh

# Or deploy PostgreSQL manually using kubectl
# (See scripts/deploy.sh deploy_postgresql() function for manifest)

### Option 3: E2E Tests

PostgreSQL is deployed automatically by `test/e2e/bootstrap.sh`:

```bash
cd test/e2e
./bootstrap.sh

# PostgreSQL is deployed before tests run
# E2E tests will fail if PostgreSQL is not available
```

---

## Deployment Methods

### Method 1: Integrated with deploy.sh (Production)

**When**: Deploying the full MaaS stack

**Script**: `scripts/deploy.sh`

**What it does**:
1. Installs policy engine (Kuadrant/RHCL)
2. **Deploys PostgreSQL** (new step)
3. Deploys maas-api (connects to PostgreSQL automatically)
4. Deploys maas-controller

**Configuration**:
- Namespace: Same as MaaS deployment (opendatahub or maas-system)
- Credentials: Default (maas/maaspassword)
- Database: maas
- Service: postgres:5432
- Secret: maas-db-config (contains DB_CONNECTION_URL)

**Example**:
```bash
# Deploy to RHOAI with PostgreSQL
./scripts/deploy.sh --operator-type rhoai

# Deploy to ODH with PostgreSQL
./scripts/deploy.sh --operator-type odh
```

---

### Method 2: Standalone Deployment (Development/Testing)

**When**: Deploying only PostgreSQL for local dev or debugging

**How**: Use inline `deploy_postgresql()` function from `scripts/deploy.sh`

**What it does**:
1. Checks if PostgreSQL exists (skips if found)
2. Creates postgres deployment with ephemeral storage (emptyDir)
3. Creates postgres service (ClusterIP)
4. Creates secrets:
   - `postgres-creds` (POSTGRES_USER, POSTGRES_PASSWORD, POSTGRES_DB)
   - `maas-db-config` (DB_CONNECTION_URL for maas-api)

**Configuration Options**:
```bash
# Environment Variables (used by deploy.sh)
POSTGRES_USER=maas              # PostgreSQL username (default: maas)
POSTGRES_PASSWORD=mypassword    # PostgreSQL password (default: maaspassword)
POSTGRES_DB=maas                # Database name (default: maas)
```

**Examples**:
```bash
# Deploy via deploy.sh (PostgreSQL deployed automatically)
POSTGRES_PASSWORD=secure123 ./scripts/deploy.sh

# Or manually using kubectl (copy manifest from deploy.sh)
kubectl apply -n opendatahub -f - <<EOF
# See scripts/deploy.sh deploy_postgresql() function for manifest
EOF
```

---

### Method 3: E2E Test Bootstrap (Testing)

**When**: Running E2E tests

**Script**: `test/e2e/bootstrap.sh`

**What it does**:
1. Deploys PostgreSQL to MAAS_NS (default: opendatahub)
2. Skips if PostgreSQL already exists
3. Continues even if deployment fails (with warning)
4. Sets up test environment

**Configuration**:
```bash
# Environment Variables
MAAS_NS=opendatahub    # Namespace for MaaS API (default: opendatahub)
```

**Example**:
```bash
cd test/e2e

# Bootstrap will deploy PostgreSQL automatically
./bootstrap.sh

# Run smoke tests (require PostgreSQL)
./smoke.sh

# Run API key tests (require PostgreSQL)
./run_api_key_tests.sh
```

---

## Configuration

### Default Configuration

The POC PostgreSQL deployment uses these defaults:

| Setting | Value | Purpose |
|---------|-------|---------|
| **User** | `maas` | PostgreSQL superuser |
| **Password** | `<randomly generated>` | 32-character random password (or use POSTGRES_PASSWORD env var) |
| **Database** | `maas` | Database name |
| **Service** | `postgres:5432` | Kubernetes service endpoint |
| **Image** | `registry.redhat.io/rhel9/postgresql-15:latest` | Red Hat PostgreSQL 15 |
| **Storage** | `emptyDir` | Ephemeral (⚠️ data lost on pod restart) |
| **Resources** | 256Mi-512Mi RAM, 100m-500m CPU | Resource limits |

### Secrets Created

#### 1. postgres-creds
Contains PostgreSQL credentials for the database pod:
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: postgres-creds
stringData:
  POSTGRES_USER: maas
  POSTGRES_PASSWORD: maaspassword
  POSTGRES_DB: maas
```

#### 2. maas-db-config
Contains connection URL for maas-api:
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: maas-db-config
stringData:
  DB_CONNECTION_URL: postgresql://maas:<RANDOM_PASSWORD>@postgres:5432/maas?sslmode=disable
```

**Retrieving the Password**:
```bash
# Get PostgreSQL password from secret
kubectl get secret postgres-creds -n opendatahub \
  -o jsonpath='{.data.POSTGRES_PASSWORD}' | base64 -d

# Get full connection URL
kubectl get secret maas-db-config -n opendatahub \
  -o jsonpath='{.data.DB_CONNECTION_URL}' | base64 -d
```

### MaaS API Integration

The maas-api deployment automatically picks up the connection URL via kustomize overlay:

**File**: `deployment/base/maas-api/overlays/with-postgres/kustomization.yaml`

```yaml
patches:
  - patch: |-
      apiVersion: apps/v1
      kind: Deployment
      metadata:
        name: maas-api
      spec:
        template:
          spec:
            containers:
            - name: maas-api
              env:
              - name: DB_CONNECTION_URL
                valueFrom:
                  secretKeyRef:
                    name: maas-db-config
                    key: DB_CONNECTION_URL
```

This overlay is included in:
- `deployment/base/maas-api/overlays/tls` (TLS backend)
- Any deployment that needs PostgreSQL support

---

## Database Schema

### Schema Migrations

Schema migrations are applied **automatically** when maas-api starts:

1. **Migration Tool**: golang-migrate
2. **Migration Location**: `maas-api/scripts/migrations/*.sql`
3. **Execution**: On maas-api startup (before accepting requests)
4. **Version Tracking**: `schema_migrations` table

**Migration Files**:
```
maas-api/scripts/migrations/
├── 000001_create_api_keys_table.up.sql
├── 000001_create_api_keys_table.down.sql
├── 000002_add_groups_to_api_keys.up.sql
└── 000002_add_groups_to_api_keys.down.sql
```

### Current Schema (Latest Migration)

**Table**: `api_keys`

```sql
CREATE TABLE api_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id TEXT NOT NULL,
    key_hash TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMP,
    revoked_at TIMESTAMP,
    last_used_at TIMESTAMP,
    groups TEXT[]
);

CREATE INDEX idx_api_keys_user_id ON api_keys(user_id);
CREATE INDEX idx_api_keys_key_hash ON api_keys(key_hash);
CREATE INDEX idx_api_keys_revoked_at ON api_keys(revoked_at) WHERE revoked_at IS NULL;
```

**Table**: `schema_migrations`
```sql
CREATE TABLE schema_migrations (
    version bigint NOT NULL PRIMARY KEY,
    dirty boolean NOT NULL
);
```

### Schema Evolution

To add new migrations:

1. Create migration files:
   ```bash
   cd maas-api/scripts/migrations
   # Create new migration (e.g., add column)
   cat > 000003_add_api_key_scopes.up.sql <<EOF
   ALTER TABLE api_keys ADD COLUMN scopes TEXT[];
   EOF

   cat > 000003_add_api_key_scopes.down.sql <<EOF
   ALTER TABLE api_keys DROP COLUMN scopes;
   EOF
   ```

2. Restart maas-api (migrations run automatically)

3. Verify migration:
   ```bash
   kubectl exec -it -n opendatahub deploy/postgres -- psql -U maas -d maas -c "SELECT * FROM schema_migrations;"
   ```

---

## Connection Details

### Connecting from maas-api Pod

**Automatic** - Uses secret `maas-db-config`:
```yaml
env:
- name: DB_CONNECTION_URL
  valueFrom:
    secretKeyRef:
      name: maas-db-config
      key: DB_CONNECTION_URL
```

### Connecting from Local Development

**Get PostgreSQL password**:
```bash
POSTGRES_PASSWORD=$(kubectl get secret postgres-creds -n opendatahub \
  -o jsonpath='{.data.POSTGRES_PASSWORD}' | base64 -d)
```

**Port-forward PostgreSQL**:
```bash
kubectl port-forward -n opendatahub svc/postgres 5432:5432
```

**Connect with psql**:
```bash
export DB_CONNECTION_URL="postgresql://maas:${POSTGRES_PASSWORD}@localhost:5432/maas?sslmode=disable"
psql $DB_CONNECTION_URL

# Or get the full connection URL from secret
export DB_CONNECTION_URL=$(kubectl get secret maas-db-config -n opendatahub \
  -o jsonpath='{.data.DB_CONNECTION_URL}' | base64 -d | sed 's/@postgres:/@localhost:/')
psql $DB_CONNECTION_URL
```

**Connect with pgAdmin/DBeaver**:
- Host: localhost
- Port: 5432
- User: maas
- Password: (get from `kubectl get secret postgres-creds -n opendatahub -o jsonpath='{.data.POSTGRES_PASSWORD}' | base64 -d`)
- Database: maas

### Testing Connection

```bash
# Get connection URL from secret
DB_URL=$(kubectl get secret maas-db-config -n opendatahub \
  -o jsonpath='{.data.DB_CONNECTION_URL}' | base64 -d)

# Test connection from within cluster
kubectl run -it --rm --restart=Never pg-test \
  --image=registry.redhat.io/rhel9/postgresql-15:latest \
  --namespace=opendatahub \
  -- psql "$DB_URL" -c "SELECT 1;"

# Expected output: 1 row with value 1
```

---

## Production Considerations

### ⚠️ Current POC Limitations

The current PostgreSQL deployment is **NOT production-ready**:

1. **Ephemeral Storage** (emptyDir)
   - Data is lost when pod restarts
   - No backup/restore capability
   - Not suitable for production workloads

2. **Limited Security Features**
   - Random password (better than fixed default)
   - No secret rotation
   - No encryption at rest

3. **Single Replica**
   - No high availability
   - Downtime during pod restart
   - No read replicas

4. **No SSL/TLS**
   - `sslmode=disable` in connection string
   - Traffic not encrypted in transit

5. **No Resource Isolation**
   - Runs in same namespace as maas-api
   - No dedicated node/resources

### Production Recommendations

For production deployments, use one of these options:

#### Option 1: External Managed Database (Recommended)

**AWS RDS for PostgreSQL**:
```bash
# Create RDS instance
aws rds create-db-instance \
  --db-instance-identifier maas-postgres \
  --db-instance-class db.t3.medium \
  --engine postgres \
  --engine-version 15.4 \
  --master-username maas \
  --master-user-password <SECURE_PASSWORD> \
  --allocated-storage 20 \
  --vpc-security-group-ids <VPC_SG> \
  --db-subnet-group-name <SUBNET_GROUP>

# Update secret with RDS endpoint
kubectl create secret generic maas-db-config -n opendatahub \
  --from-literal=DB_CONNECTION_URL="postgresql://maas:<PASSWORD>@<RDS_ENDPOINT>:5432/maas?sslmode=require"
```

**Benefits**:
- ✅ Managed backups (automated, point-in-time recovery)
- ✅ High availability (Multi-AZ)
- ✅ Automatic failover
- ✅ SSL/TLS encryption
- ✅ Monitoring and alerting
- ✅ Scaling (read replicas, storage auto-scaling)

#### Option 2: Crunchy PostgreSQL Operator (OpenShift)

**Install Operator**:
```bash
# Install via OperatorHub
oc apply -f - <<EOF
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: crunchy-postgres-operator
  namespace: openshift-operators
spec:
  channel: v5
  name: crunchy-postgres-operator
  source: certified-operators
  sourceNamespace: openshift-marketplace
EOF
```

**Create PostgreSQL Cluster**:
```yaml
apiVersion: postgres-operator.crunchydata.com/v1beta1
kind: PostgresCluster
metadata:
  name: maas-postgres
  namespace: opendatahub
spec:
  postgresVersion: 15
  instances:
    - name: instance1
      replicas: 2
      dataVolumeClaimSpec:
        accessModes:
          - ReadWriteOnce
        resources:
          requests:
            storage: 10Gi
  backups:
    pgbackrest:
      repos:
        - name: repo1
          volume:
            volumeClaimSpec:
              accessModes:
                - ReadWriteOnce
              resources:
                requests:
                  storage: 20Gi
```

**Benefits**:
- ✅ OpenShift native
- ✅ High availability (multi-replica)
- ✅ Automated backups (pgBackRest)
- ✅ Persistent storage (PVC)
- ✅ SSL/TLS support
- ✅ Monitoring integration

#### Option 3: Azure Database for PostgreSQL

Similar to AWS RDS, Azure provides managed PostgreSQL with:
- Automated backups
- High availability
- SSL enforcement
- Monitoring

**Update connection string**:
```bash
kubectl create secret generic maas-db-config -n opendatahub \
  --from-literal=DB_CONNECTION_URL="postgresql://maas@<SERVER_NAME>:<PASSWORD>@<SERVER_NAME>.postgres.database.azure.com:5432/maas?sslmode=require"
```

### Security Best Practices

1. **Secret Rotation**
   ```bash
   # Generate new password
   NEW_PASSWORD=$(openssl rand -base64 32 | tr -d '/+=' | cut -c1-32)

   # Update secrets
   kubectl create secret generic postgres-creds -n opendatahub \
     --from-literal=POSTGRES_USER=maas \
     --from-literal=POSTGRES_PASSWORD="${NEW_PASSWORD}" \
     --from-literal=POSTGRES_DB=maas \
     --dry-run=client -o yaml | kubectl apply -f -

   kubectl create secret generic maas-db-config -n opendatahub \
     --from-literal=DB_CONNECTION_URL="postgresql://maas:${NEW_PASSWORD}@postgres:5432/maas?sslmode=disable" \
     --dry-run=client -o yaml | kubectl apply -f -

   # Restart both postgres and maas-api to pick up new secret
   kubectl rollout restart deployment/postgres -n opendatahub
   kubectl rollout restart deployment/maas-api -n opendatahub
   ```

2. **Network Policies**
   ```yaml
   apiVersion: networking.k8s.io/v1
   kind: NetworkPolicy
   metadata:
     name: postgres-ingress
     namespace: opendatahub
   spec:
     podSelector:
       matchLabels:
         app: postgres
     ingress:
     - from:
       - podSelector:
           matchLabels:
             app: maas-api
       ports:
       - protocol: TCP
         port: 5432
   ```

3. **Enable SSL/TLS**
   - Generate server certificates
   - Mount certificates in PostgreSQL pod
   - Update connection string: `sslmode=require`
   - Configure PostgreSQL to require SSL

---

## Troubleshooting

### PostgreSQL Pod Not Starting

**Symptom**:
```bash
kubectl get pods -n opendatahub | grep postgres
postgres-xxxx-xxx   0/1   CrashLoopBackOff
```

**Diagnosis**:
```bash
kubectl logs -n opendatahub deploy/postgres
kubectl describe pod -n opendatahub -l app=postgres
```

**Common Causes**:

1. **Resource Constraints**
   ```bash
   # Check node resources
   kubectl top nodes

   # Increase limits if needed
   kubectl patch deployment postgres -n opendatahub --type='json' \
     -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/resources/limits/memory", "value": "1Gi"}]'
   ```

2. **Image Pull Issues**
   ```bash
   # Check image pull secrets
   kubectl get events -n opendatahub | grep postgres

   # Use public mirror if registry auth fails
   kubectl set image deployment/postgres -n opendatahub \
     postgres=docker.io/postgres:15-alpine
   ```

### MaaS API Cannot Connect to PostgreSQL

**Symptom**:
```bash
kubectl logs -n opendatahub deploy/maas-api
# Error: failed to connect to PostgreSQL: connection refused
```

**Diagnosis**:
```bash
# Check if secret exists
kubectl get secret maas-db-config -n opendatahub

# Check secret contents
kubectl get secret maas-db-config -n opendatahub -o jsonpath='{.data.DB_CONNECTION_URL}' | base64 -d

# Test connection from maas-api pod
kubectl exec -it -n opendatahub deploy/maas-api -- \
  sh -c 'apk add postgresql-client && psql $DB_CONNECTION_URL -c "SELECT 1;"'
```

**Solutions**:

1. **Secret Not Found**
   ```bash
   # Redeploy PostgreSQL
   SKIP_IF_EXISTS=false ./scripts/deploy-postgres.sh opendatahub
   ```

2. **Wrong Namespace**
   ```bash
   # PostgreSQL in different namespace
   # Update connection URL to use FQDN
   kubectl create secret generic maas-db-config -n opendatahub \
     --from-literal=DB_CONNECTION_URL="postgresql://maas:maaspassword@postgres.other-namespace.svc.cluster.local:5432/maas?sslmode=disable"
   ```

3. **Service DNS Not Resolving**
   ```bash
   # Test DNS from maas-api pod
   kubectl exec -it -n opendatahub deploy/maas-api -- nslookup postgres

   # If fails, check CoreDNS
   kubectl get pods -n kube-system -l k8s-app=kube-dns
   ```

### Migration Failures

**Symptom**:
```bash
kubectl logs -n opendatahub deploy/maas-api
# Error: migration failed: duplicate key violates unique constraint
```

**Diagnosis**:
```bash
# Check migration status
kubectl exec -it -n opendatahub deploy/postgres -- \
  psql -U maas -d maas -c "SELECT * FROM schema_migrations;"
```

**Solutions**:

1. **Dirty Migration**
   ```bash
   # Mark migration as clean
   kubectl exec -it -n opendatahub deploy/postgres -- \
     psql -U maas -d maas -c "UPDATE schema_migrations SET dirty=false WHERE version=1;"

   # Restart maas-api to retry
   kubectl rollout restart deployment/maas-api -n opendatahub
   ```

2. **Schema Conflict**
   ```bash
   # Drop and recreate database (⚠️ DELETES ALL DATA)
   kubectl exec -it -n opendatahub deploy/postgres -- \
     psql -U maas -d postgres -c "DROP DATABASE maas; CREATE DATABASE maas;"

   # Restart maas-api to re-run migrations
   kubectl rollout restart deployment/maas-api -n opendatahub
   ```

### Data Loss After Pod Restart

**Symptom**: API keys disappear after PostgreSQL pod restart

**Cause**: Using emptyDir storage (ephemeral)

**Solution**: Migrate to persistent storage

```yaml
# Update deployment to use PVC
apiVersion: apps/v1
kind: Deployment
metadata:
  name: postgres
spec:
  template:
    spec:
      volumes:
      - name: data
        persistentVolumeClaim:
          claimName: postgres-data
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: postgres-data
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
```

---

## Summary

**Key Points**:

1. ✅ **PostgreSQL is required** for API key management
2. ✅ **Automatic deployment** via scripts/deploy.sh
3. ✅ **E2E tests require PostgreSQL** (auto-deployed by bootstrap.sh)
4. ⚠️ **POC deployment is NOT production-ready** (ephemeral storage, default credentials)
5. ✅ **Production options**: AWS RDS, Crunchy Operator, Azure Database
6. ✅ **Schema migrations are automatic** (run on maas-api startup)

**Next Steps**:

1. Deploy PostgreSQL (automatic with scripts/deploy.sh)
2. Verify maas-api connects successfully
3. Run E2E tests to validate API key CRUD operations
4. Plan migration to production-grade PostgreSQL

---

**Document Status**: Production Ready
**Last Updated**: March 1, 2026
**Maintainer**: MaaS Team
