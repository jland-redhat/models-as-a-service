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

## Resource names (important)

Praxis **reuses** the same Kubernetes resource names as llm-d so the shared
reconciler patchers apply with no profile-specific naming code. Tell them apart
with the annotation:

```text
maas.opendatahub.io/ipp-profile: praxis
```

| Kind | Name (both profiles) | How to tell Praxis |
|------|----------------------|--------------------|
| Deployment / Service / EnvoyFilter / SA / NetworkPolicy | `payload-processing` | annotation `ipp-profile=praxis` |
| Pre-auth Deployment / Service | `payload-pre-processing` | same annotation |
| Plugins ConfigMap | `payload-processing-plugins` | same annotation |
| ClusterRole / Binding | `payload-processing-reader` | same annotation |
| Manifest directory | `deployment/base/payload-processing-praxis/` | vs `.../payload-processing/` |

Envoy dedicated cluster names (Praxis EnvoyFilter CLUSTER ADD, MX-free):
`payload-processing-extproc`, `payload-pre-processing-extproc`.

Container / image stub stays `praxis-extproc` (image rename only); K8s object
names stay `payload-processing*`.

---

## What changed in this repo

### New manifests
- `deployment/base/payload-processing-praxis/` — Praxis ExtProc Deployment,
  Service, DestinationRule (SNI), EnvoyFilter (MX-free clusters), plugins
  ConfigMap stubs, NetworkPolicy, RBAC, pre-processing clone. Annotated
  `maas.opendatahub.io/ipp-profile: praxis` via `commonAnnotations`.
- `maas-api/deploy/overlays/odh-praxis/` — same as `overlays/odh`, but points at
  the Praxis base (RHOAI/OCP path)

**Not added:** any `xks` / `xks-praxis` overlays (explicitly out of scope).

### Controller selection (`MAAS_IPP_PROFILE`)
- Env: `MAAS_IPP_PROFILE=llm-d` (legacy) or `praxis`
- Related image: `RELATED_IMAGE_PRAXIS_EXTPROC_IMAGE` ← ConfigMap key
  `praxis-extproc-image`
- Path remapping: stock `.../overlays/odh` → `.../overlays/odh-praxis` when
  profile is praxis
- Resource names are **not** profile-aware; only overlay path + image + Envoy
  cluster style differ
- EnvoyFilter patcher supports 8 auth-anchor HTTP_FILTER patches + Praxis
  dedicated clusters (`service + "-extproc"`)
- Plugins ConfigMap: re-apply when data keys change (llm-d ↔ praxis) even if
  `opendatahub.io/managed=false`

### Build / params / deploy wiring
- Dockerfiles COPY `deployment/base/payload-processing-praxis`
- `praxis-extproc-image` in params.env files
- Legacy `payload-processing` EnvoyFilter expanded for multi-Istio auth anchors
- `scripts/deploy.sh` writes `praxis-extproc-image` into `maas-parameters` and
  skips cert-manager re-apply when already running

### OpenShift SCC fix
- No hardcoded `runAsUser` / `runAsGroup: 65534` on Praxis Deployments

### Images built during the experiment
| Image | Tag / notes |
|-------|-------------|
| `quay.io/maas/praxis-extproc` | `:llmisvc-model-provider-resolver` |
| `quay.io/maas/maas-controller` | `:praxis-dev` |

ExtProc source: [szedan-rh/extproc](https://github.com/szedan-rh/extproc)
with a local rustls CryptoProvider fix.

---

## How selection works (quick)

```text
MAAS_IPP_PROFILE=praxis
  → ManifestPathForPlatform → /maas-api/deploy/overlays/odh-praxis
  → deployment/base/payload-processing-praxis
  → RELATED_IMAGE_PRAXIS_EXTPROC_IMAGE
  → K8s resources named payload-processing* + annotation ipp-profile=praxis
```

---

## Issues faced (and outcomes)

### 1. Kubeconfig `~` not expanded
Use absolute `--kubeconfig=/home/jland/.kube/config.praxis`.

### 2. Full `./scripts/deploy.sh --operator-type rhoai` on this pool
Conflicts with existing `rhoai-catalog-dev` / `rhoai-operator-dev` subscription.
Prefer surgical controller install + DSC enablement.

### 3. cert-manager OperatorGroup conflict
Mitigated by skipping cert-manager apply when pods already run.

### 4. AIGateway reconciler overwrites maas-controller image
Needed `opendatahub.io/managed=false` on the Deployment for custom images.

### 5. OpenShift SCC vs `runAsUser: 65534`
Fixed by dropping hardcoded UIDs.

### 6. rustls CryptoProvider panic
Still required in ExtProc `main` (`rustls` `ring` feature +
`CryptoProvider::install_default`). Rebuilds of
`:llmisvc-model-provider-resolver` without that fix CrashLoop on TLS startup.

### 7. Shared names with llm-d IPP
Kept shared `payload-processing*` names (old approach). Distinguish Praxis with
`maas.opendatahub.io/ipp-profile=praxis` on the manifests.

### 8. Validation: API key mint still fails
Missing `X-MaaS-Username` — Authorino identity headers; not fixed.

### 9. Feature gap
Praxis plugins ConfigMap still missing translation / apikey / ExternalModel
provider resolve. LLMISvc body rewrite is wired:
`model_to_header` (pre) → `llmisvc_model_provider_resolver` (post) on
`X-Gateway-Model-Name`.

---

## Commands cheat sheet

```bash
export KUBECONFIG=/home/jland/.kube/config.praxis

# Shared names — filter by Praxis annotation:
oc get deploy,svc,envoyfilter,cm,sa,networkpolicy -n openshift-ingress \
  -l app.kubernetes.io/name=payload-processing \
  -o custom-columns=KIND:.kind,NAME:.metadata.name,PROFILE:.metadata.annotations.maas\.opendatahub\.io/ipp-profile

oc get deploy payload-processing payload-pre-processing -n openshift-ingress \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.annotations.maas\.opendatahub\.io/ipp-profile}{"\t"}{.spec.template.spec.containers[0].image}{"\n"}{end}'

./scripts/validate-deployment.sh
```
