# Praxis IPP Port (RHOAI experiment notes)

Working notes for porting **Praxis ExtProc** as a selectable alternative to the
legacy llm-d / ai-gateway-payload-processing IPP stack, targeting **RHOAI only**
(no xKS overlays).

Upstream reference:
[aslakknutsen/models-as-a-service `praxis` branch](https://github.com/aslakknutsen/models-as-a-service/tree/praxis)

Cluster used for the install attempt:
`https://api.jland-praxis-pool-97nvt.aws.rh-ods.com:6443`
(kubeconfig: `/home/jland/.kube/config.praxis`)

---

## What changed in this repo

### New manifests
- `deployment/base/payload-processing-praxis/` — Praxis ExtProc Deployment,
  Service, DestinationRule (SNI), EnvoyFilter (MX-free clusters), plugins
  ConfigMap stubs, NetworkPolicy, RBAC, pre-processing clone
- `maas-api/deploy/overlays/odh-praxis/` — same as `overlays/odh`, but points at
  the Praxis IPP base (RHOAI/OCP path)

**Not added:** any `xks` / `xks-praxis` overlays (explicitly out of scope).

### Controller selection (`MAAS_IPP_PROFILE`)
- Env: `MAAS_IPP_PROFILE=llm-d` (legacy) or `praxis`
- Related image: `RELATED_IMAGE_PRAXIS_EXTPROC_IMAGE` ← ConfigMap key
  `praxis-extproc-image`
- Path remapping: stock `.../overlays/odh` → `.../overlays/odh-praxis` when
  profile is praxis (also remaps stock `MAAS_PLATFORM_MANIFESTS` values)
- Go updates under `maas-controller/pkg/platform/tenantreconcile/` and
  `maas-controller/cmd/manager/main.go`
- EnvoyFilter patcher supports 8 auth-anchor HTTP_FILTER patches + Praxis
  dedicated clusters (`payload-*-extproc`) instead of Istio `outbound|*`
- Plugins ConfigMap: re-apply when data keys change (llm-d ↔ praxis) even if
  `opendatahub.io/managed=false`

### Build / params / deploy wiring
- Dockerfiles COPY `payload-processing-praxis`
- `praxis-extproc-image` in `deployment/overlays/odh/params.env` and
  maas-controller `params.env` files
- Legacy `payload-processing` EnvoyFilter expanded for multi-Istio auth anchors
  (needed so `llm-d` still works with `filterPatchCount=8`)
- `scripts/deploy.sh`:
  - writes `praxis-extproc-image` into `maas-parameters`
  - skips cert-manager subscription re-apply when cert-manager pods already run
    (avoids TooManyOperatorGroups)

### OpenShift SCC fix
- Removed hardcoded `runAsUser` / `runAsGroup: 65534` from the Praxis Deployment
  (restricted-v2 rejects fixed UIDs outside the namespace range)

### Images built and pushed during the experiment
| Image | Tag / notes |
|-------|-------------|
| `quay.io/maas/praxis-extproc` | `:dev-crypto-fix` (usable); `:dev` had a rustls crash |
| `quay.io/maas/maas-controller` | `:praxis-dev` (embeds Praxis manifests) |

ExtProc source used for the build: [szedan-rh/extproc](https://github.com/szedan-rh/extproc)
(fork of praxis-proxy/extproc). Local fix: pin rustls `ring` CryptoProvider and
call `install_default()` at process start.

---

## How selection works (quick)

```text
MAAS_IPP_PROFILE=praxis
  → ManifestPathForPlatform → /maas-api/deploy/overlays/odh-praxis
  → payload-processing-praxis base
  → RELATED_IMAGE_PRAXIS_EXTPROC_IMAGE for Deployment image
```

Manager Deployment currently defaults `MAAS_IPP_PROFILE` to `praxis` in this
branch for the RHOAI experiment (upstream praxis branch defaults to `llm-d`).

---

## Issues faced (and outcomes)

### 1. Kubeconfig `~` not expanded
`oc login --kubeconfig=~/.kube/config.praxis` wrote a **literal**
`maas-billing/~/.kube/config.praxis` under the repo. Always use an absolute path:
`--kubeconfig=/home/jland/.kube/config.praxis`.

### 2. Full `./scripts/deploy.sh --operator-type rhoai` on this pool
This cluster already runs RHOAI from catalog `rhoai-catalog-dev` /
subscription `rhoai-operator-dev`. Deploy tried to add a second
`rhods-operator` subscription from `redhat-operators` → OLM constraint
failures. The conflicting sub was deleted; controller was installed via the
direct/FORCE path instead.

### 3. cert-manager OperatorGroup conflict
Re-applying the cert-manager subscription created a second OperatorGroup
(`TooManyOperatorGroups`). Mitigated by skipping cert-manager apply when pods
are already Running.

### 4. AIGateway reconciler overwrites maas-controller image
OwnerRef is `AIGateway/default-aigateway`. Custom
`quay.io/maas/maas-controller:praxis-dev` was reverted to the product
`registry.redhat.io/rhoai/odh-maas-controller-rhel9@sha256:...` until
`opendatahub.io/managed=false` was set on the Deployment.

### 5. OpenShift SCC vs `runAsUser: 65534`
Praxis pods failed to create (`restricted-v2` UID range). Fixed by dropping
hardcoded UIDs from the Praxis Deployment YAML.

### 6. rustls CryptoProvider panic in ExtProc
First image crashed on TLS self-signed cert generation. Fixed in the ExtProc
build (ring provider) and pushed as a **new tag** (`dev-crypto-fix`).

### 7. ImagePullPolicy + tag reuse
`:dev` + `IfNotPresent` left nodes on the broken digest after the crypto fix.
Use unique tags when iterating.

### 8. Validation: API key mint still fails
`./scripts/validate-deployment.sh` → **9 pass / 1 fail / 2 warnings**.

Fail: create API key → missing `X-MaaS-Username` (Authorino identity headers
not reaching maas-api). `GET /maas-api/v1/models` returned 200 with an empty
list. Likely AuthPolicy / wasm filter-chain interaction with the new
multi-anchor EnvoyFilter — **not fixed**.

### 9. Feature gap (expected)
Praxis plugins ConfigMap is stub-only (`request_id`, `model_to_header`). No
parity with llm-d translation / apikey inject / provider resolve yet.

### 10. No models on the cluster
Inference paths were not validated (no LLM namespace / models).

---

## Cluster end state (at time of write-up)

| Component | State |
|-----------|--------|
| DSC `aigateway` + `modelsAsAService` | Managed |
| `maas-controller` | Custom image `praxis-dev`, `MAAS_IPP_PROFILE=praxis`, often `opendatahub.io/managed=false` |
| `payload-processing` / `payload-pre-processing` | Running Praxis ExtProc (`dev-crypto-fix`) |
| EnvoyFilter | MX-free clusters `payload-*-extproc` |
| Validation | Gateway/policies OK; API key mint failing |

---

## Suggested next steps

1. Fix Authorino → `X-MaaS-Username` / `X-MaaS-Group` injection with the Praxis EF chain.
2. Decide default profile (`llm-d` vs `praxis`) before merging; keep both selectable.
3. Wire parent-operator / CSV `RELATED_IMAGE_*` (and `praxis-extproc-image`) so AIGateway does not fight local images.
4. Upstream the rustls CryptoProvider fix into the ExtProc repo; publish a stable Quay tag.
5. Replace stub Praxis filters with real IPP/BBR behavior before claiming feature parity.
6. Re-test with a real model; avoid full `deploy.sh` operator install on pools that already use a custom RHOAI catalog—prefer surgical controller + DSC enablement.

---

## Commands cheat sheet

```bash
export KUBECONFIG=/home/jland/.kube/config.praxis

# Build ExtProc (from cloned szedan-rh/extproc)
podman build --platform=linux/amd64 -t quay.io/maas/praxis-extproc:dev-crypto-fix -f Containerfile .
podman push quay.io/maas/praxis-extproc:dev-crypto-fix

# Build controller (repo root)
podman build --platform=linux/amd64 -t quay.io/maas/maas-controller:praxis-dev \
  -f maas-controller/Dockerfile .
podman push quay.io/maas/maas-controller:praxis-dev

# Validate
./scripts/validate-deployment.sh
```
