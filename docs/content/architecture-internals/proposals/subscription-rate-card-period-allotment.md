---
title: Subscription rate card and period allotment (control layer)
description: Proposed MaaS control layer for per-token-type pricing (ModelRef defaults, subscription overrides) and period budgets for metering.
---

# Subscription rate card and period allotment (control layer)

> [!NOTE]
> **Proposed design**
>
> This document describes a planned control-layer contract. It is not documentation of an implemented feature.
> Field names in the YAML sketches are illustrative and subject to API review.
>
> The design is **optimistic**: it describes the product shape we want for operators. Parts of it — especially period forms (calendar month / timezone / start day), reservation estimate rules, remaining-budget API freshness, and profile/`costExpr` evaluation — **may need to be narrowed or staged** based on what the gateway / metering backend can deliver in a given release. Prefer adapting the control-layer mapping to backend capability over inventing a parallel ledger in MaaS.

**Author:** James Land

**Date:** 2026-10-22 (pricing revision 2026-10-26)

**Scope:** Models-as-a-Service control plane (`MaaSModelRef`, `MaaSSubscription`); gateway metering / enforcement backend (implementation TBD)

## What

This document defines the **control layer** customers use to declare metering policies — what a call is worth for showback / cost attribution, and how much an identity may spend in a period — so those policies can be reconciled into the gateway metering / enforcement backend.

**Working assumptions**

- **Pricing capture and defaults** live on **`MaaSModelRef`** (which token types to read from the inference `usage` object, and optional default rates).
- **Reservation estimate defaults** also live on **`MaaSModelRef`** (`max_tokens` vs static hold) because they follow the model’s API shape; **`MaaSSubscription` may override** per entitlement.
- **Budget policy** (and optional **price overrides**) live by **extending `MaaSSubscription`**, rather than introducing a separate first-class budget CR as the centralized driver for metering.
- Enforcement, counters, and remaining-balance storage stay with the pluggable backend — MaaS does not become the durable budget database in v1.

## Why

Operators need a clear split between:

- **How much does a call cost?** (rate card / showback — per model and per billable unit)
- **How much may this identity spend in a window?** (budget / FinOps cap)

Without that split, teams conflate prepaid wallets, calendar quotas, and per-model prices. A flat “one price per model” is not enough when input, output, cached, and reasoning tokens (or image / audio / other units) are billed differently. Capture paths also differ by provider, so the model object must say what to meter and where it appears on the response.

That policy must live in the MaaS control layer so it can be propagated to the right place at reconcile time — capture paths, default rates, subscription overrides, and (later) budget — rather than being re-entered or looked up ad hoc in a separate system at request time.

Early design conversations also point to doing cost and budget calculations in the gateway when possible, using counters captured in the gateway proxy, so admission and settle stay on the hot path with the strongest performance. Putting rates and capture definitions in MaaS is what lets the product push a complete, consistent policy into that in-proxy path.


## Goals

- Declarative pricing and budget policy in MaaS (`MaaSModelRef` + `MaaSSubscription`).
- Explicit pricing across models and billable units — **token types** (input, output, cached, reasoning, and similar) and **non-token modes** (image, audio, video, per-request, and similar) — with capture from response `usage`, default cost as rate weights (`Σ count × rate`), and optional `costExpr` (or profile default) for overlap / complex cases.
- Budget on `MaaSSubscription`: spend **limit**, **period** (including calendar month / timezone when the backend supports it), **hard** stop at the limit, and **alert thresholds** as spend approaches that limit.
- **Reserve-then-settle** admission: hold estimated cost against the budget **before** the upstream response (when true token usage is unknown), then settle to actual cost afterward. Estimate mode defaults on the ModelRef (`max_tokens` or static); subscriptions may override.
- A **real-time remaining-budget API** (not only observability / Loki) so portal and clients can read spend left for the current window.

## Non-Goals (for now)

- Prepaid **wallet** product UX (admin top-up, auto-refill, payment-processor-style balance).
- MaaS-owned **remaining balance** DB as source of truth (remaining is read from the enforcement backend via API).
- A separate first-class budget CR as the metering driver (subscription extension is the working assumption).
- Hierarchical org → team → key pools as first-class CRs (BYO / later).
- Soft-only budgets that never hard-stop (v1 pairs a hard limit with alerts, matching common peer behavior).

## Proposed Design

### Pricing (current focus)

Pricing and capture are split across two objects:

| Concern | Object | Role |
| --- | --- | --- |
| Usage shape / capture profile (+ optional overrides) | `MaaSModelRef` | Built-in defaults by model type, with path overrides when needed |
| Default price per billable unit | `MaaSModelRef` | Catalog / platform default rate card for that model |
| Default reservation estimate | `MaaSModelRef` | How to bound the pre-response hold for this model (`max_tokens` or static) |
| Effective price for this entitlement | `MaaSSubscription` | Optional override of ModelRef defaults for models on the subscription |
| Effective reservation estimate | `MaaSSubscription` | Optional override of ModelRef estimate mode for that model on the entitlement |
| Budget amount + period + alerts | `MaaSSubscription` | See [Budget](#budget) |

`MaaSModelRef` declares **what** to meter, **default rates**, and **default reservation estimate**. `MaaSSubscription` may **override rates** and **estimate mode** for an entitlement. Subscriptions do not redefine capture.

#### Usage capture profiles

`MaaSModelRef` gets its capture definition from one of three places. The expanded form is always the same shape: `billableUnits` plus optional `defaultCostExpr` / `costExpr`. A new CRD is intentionally **not** proposed — reuse built-ins, a ConfigMap, or define the profile object **inline on the ModelRef**.

| Source | How `MaaSModelRef` selects it | Role |
| --- | --- | --- |
| **Built-in** | `usageCapture.profile: openai-chat` | Product-owned defaults for common API shapes; versioned with MaaS / gateway |
| **ConfigMap** | `usageCapture.profileRef` → ConfigMap name/key | Shared site-local or vendor-specific shapes without a CRD |
| **Inline on ModelRef** | `usageCapture.billableUnits` (+ optional `costExpr`) | Per-model definition of the same profile object — no shared ConfigMap required |

**Built-in profiles (illustrative v1 set)**

| Profile | Applies to | Built-in behavior |
| --- | --- | --- |
| `openai-chat` | OpenAI Chat Completions–compatible backends | Raw `prompt` / `cached` / `output` / `reasoning` paths; profile **default `costExpr`** when cache-aware rates are used (prompt ⊇ cached) |
| `openai-chat-basic` | Same chat `usage` shape without cache-aware pricing | `prompt` ← `prompt_tokens`, `output` ← `completion_tokens`; default cost is Σ count × rate |
| `anthropic-messages` | Anthropic Messages API (used by MaaS / gateway internals for Messages-shaped traffic) | Raw `input` / `cache_read` / `cache_creation` / `output` paths; default cost is Σ count × rate (fields are already disjoint — see below) |
| `anthropic-messages-basic` | Same Messages `usage` without cache-aware pricing | `input` ← `input_tokens`, `output` ← `output_tokens` |

**ConfigMap-backed profiles** use the same schema as inline / built-in expansion under a documented key. The controller resolves `profile` or `profileRef` at reconcile time and publishes the expanded capture into the metering path. Prefer ConfigMap when several models share one custom shape; prefer inline when only one model needs a one-off.

Day-1 UX remains a built-in profile name (`openai-chat` or `anthropic-messages`) with no extra objects. ConfigMap covers shared customs. Inline covers per-model override without a CRD.

**`openai-chat` family.** Assumes a Chat Completions–compatible flat `usage` object with at least `prompt_tokens` and `completion_tokens` (and typically `total_tokens`). Optional detail fields such as `prompt_tokens_details.cached_tokens` and `completion_tokens_details.reasoning_tokens` are read when present and treated as **0** when absent. OpenAI-style `cached_tokens` is a **subset** of `prompt_tokens`, so cache-aware pricing uses the profile `defaultCostExpr` (not independent Σ on both).

**`anthropic-messages` family.** Assumes Anthropic Messages `usage` with at least `input_tokens` and `output_tokens`. Optional `cache_read_input_tokens` and `cache_creation_input_tokens` are read when present and treated as **0** when absent. Unlike OpenAI, Anthropic’s `input_tokens` is already the **non-cached** slice (tokens after the last cache breakpoint); cache read/write counts are **additive**, not nested inside `input_tokens`. Total input ≈ `input_tokens + cache_read_input_tokens + cache_creation_input_tokens`. Independent rate weights (`Σ count × rate`) are therefore correct for cache-aware Anthropic pricing without a subtractive `costExpr`.

Missing top-level `usage` when metering is enabled is treated as an incomplete settle, not a successful zero-cost request. Embeddings, audio, image, and other API shapes get their own built-in or ConfigMap profiles rather than reusing these chat/Messages profiles.

#### Cost model

By default, cost is independent rate weights over captured counts:

`cost = Σ (unit_count × unit_rate)`

Most deployments only set `pricing.perUnit`. Reported counts remain whatever capture read from `usage`. Rates and budget limits are dimensionless weights in the same numeric space — the control layer does not name or convert currencies.

When cost is not a simple product sum — for example OpenAI-style overlap where `cached_tokens` is a subset of `prompt_tokens` — the control layer uses an optional **`costExpr`** over unit names and rates. Profiles may ship a **`defaultCostExpr`** so operators still only configure rates for known shapes. An explicit `costExpr` on the ModelRef (or, rarely, the subscription) overrides the profile default.

Capture keeps **raw** API counts so showback matches the wire format. Overlap is handled in pricing, not by inventing a reduced “input” counter. Example OpenAI-shaped default:

`(prompt - cached) * rates.input + cached * rates.cached + output * rates.output`

(with missing `cached` treated as 0).

#### Reservation estimate (ModelRef default, subscription override)

Before upstream responds, the gateway must **hold** an estimated cost against the subscription budget. How that estimate is built is model-shaped by default and entitlement-tunable:

| Mode | Meaning | Typical use |
| --- | --- | --- |
| `max_tokens` | Bound the hold from a request field (e.g. `max_tokens` / Messages equivalent) | Chat / Messages models where clients can declare max output |
| `static` | Hold a fixed amount (tokens or cost — schema TBD) | Embeddings / APIs without `max_tokens`, or entitlements that prefer a fixed floor |

**Defaults on `MaaSModelRef`** (alongside capture and pricing). **Optional override on `MaaSSubscription` `modelRefs[]`** for that entitlement — same pattern as price overrides. When `mode: max_tokens`, operators may **require** the path so missing `max_tokens` rejects the request instead of falling back.

#### Sketch — rates only

```yaml
# Sketch — names subject to API review
apiVersion: maas.opendatahub.io/v1alpha1
kind: MaaSModelRef
metadata:
  name: llama-3
  namespace: llm
spec:
  usageCapture:
    profile: openai-chat-basic          # built-in name
  pricing:
    perUnit:
      prompt: "1"
      output: "20"
  reservationEstimate:                 # default for this model
    mode: max_tokens
    require: true                       # reject if max_tokens (or configured path) absent
```

#### Sketch — OpenAI chat with cache-aware rates

```yaml
apiVersion: maas.opendatahub.io/v1alpha1
kind: MaaSModelRef
metadata:
  name: llama-3
  namespace: llm
spec:
  usageCapture:
    profile: openai-chat                # built-in name
  pricing:
    perUnit:
      input: "0.0001"
      cached: "0.0001"
      output: "0.002"
      reasoning: "0.003"
```

#### Sketch — ConfigMap-backed custom profile (shared)

```yaml
# Platform / operator managed — not a CRD
apiVersion: v1
kind: ConfigMap
metadata:
  name: maas-usage-capture-profiles
  namespace: models-as-a-service          # illustrative
data:
  my-vendor-chat.yaml: |
    billableUnits:
      - name: prompt
        path: usage.prompt_tokens
      - name: output
        path: usage.completion_tokens
    # defaultCostExpr: ...
---
apiVersion: maas.opendatahub.io/v1alpha1
kind: MaaSModelRef
metadata:
  name: vendor-model
  namespace: llm
spec:
  usageCapture:
    profileRef:
      name: maas-usage-capture-profiles
      key: my-vendor-chat.yaml           # illustrative; schema TBD
  pricing:
    perUnit:
      prompt: "1"
      output: "20"
```

#### Sketch — inline profile on `MaaSModelRef` (per-model)

Same object shape as the ConfigMap entry, defined only on this model:

```yaml
apiVersion: maas.opendatahub.io/v1alpha1
kind: MaaSModelRef
metadata:
  name: weird-vendor-model
  namespace: llm
spec:
  usageCapture:
    # no profile / profileRef — full capture defined here
    billableUnits:
      - name: prompt
        path: usage.input_tokens
      - name: output
        path: usage.output_tokens
    costExpr: >
      prompt * rates.prompt + output * rates.output
  pricing:
    perUnit:
      prompt: "1"
      output: "20"
```

Built-in expansion for `profile: openai-chat` (illustrative):

```yaml
billableUnits:
  - name: prompt
    path: usage.prompt_tokens
  - name: cached
    path: usage.prompt_tokens_details.cached_tokens
  - name: output
    path: usage.completion_tokens
  - name: reasoning
    path: usage.completion_tokens_details.reasoning_tokens
defaultCostExpr: >
  (prompt - cached) * rates.input
  + cached * rates.cached
  + output * rates.output
  + reasoning * rates.reasoning
```

Built-in expansion for `profile: anthropic-messages` (illustrative):

```yaml
billableUnits:
  - name: input
    path: usage.input_tokens                         # already non-cached
  - name: cache_read
    path: usage.cache_read_input_tokens              # missing ⇒ 0
  - name: cache_creation
    path: usage.cache_creation_input_tokens          # missing ⇒ 0
  - name: output
    path: usage.output_tokens
# No defaultCostExpr required — fields are disjoint; cost = Σ count × rate
```

#### Sketch — Anthropic Messages with cache-aware rates

```yaml
apiVersion: maas.opendatahub.io/v1alpha1
kind: MaaSModelRef
metadata:
  name: claude-sonnet
  namespace: llm
spec:
  usageCapture:
    profile: anthropic-messages         # built-in; used for Messages-shaped internals
  pricing:
    perUnit:
      input: "1"
      cache_read: "0.1"
      cache_creation: "1.25"
      output: "5"
```

#### Sketch — explicit `costExpr`

```yaml
pricing:
  perUnit:
    prompt: "0.0001"
    cached: "0.00005"
    output: "0.002"
  costExpr: >
    (prompt - cached) * rates.prompt
    + cached * rates.cached
    + output * rates.output
```

#### Sketch — ModelRef static estimate (e.g. embeddings)

```yaml
apiVersion: maas.opendatahub.io/v1alpha1
kind: MaaSModelRef
metadata:
  name: text-embed
  namespace: llm
spec:
  usageCapture:
    profile: openai-embeddings        # illustrative built-in / ConfigMap
  pricing:
    perUnit:
      total: "1"
  reservationEstimate:
    mode: static
    staticAmount: "50"                # hold size; semantics TBD (tokens vs cost)
```

#### Sketch — subscription rate override + budget (+ estimate override)

Budgets **reserve** estimated cost at request time using the **effective** reservation estimate (ModelRef default, optionally overridden on the subscription modelRef), then **settle** to actual usage on the response. Missing required estimate inputs can **reject** the request. Calendar periods may set `startDay` (default **1**). Backend support for calendar/TZ is assumed desirable but not guaranteed in every release.

```yaml
# Sketch — names subject to API review
apiVersion: maas.opendatahub.io/v1alpha1
kind: MaaSSubscription
metadata:
  name: premium-team
spec:
  modelRefs:
    - name: llama-3
      namespace: llm
      pricing:
        perUnit:
          output: "0.0015"
      # optional — omit to keep ModelRef reservationEstimate
      reservationEstimate:
        mode: static
        staticAmount: "25"            # entitlement prefers a fixed hold over max_tokens
    - name: text-embed
      namespace: llm
      # inherits ModelRef reservationEstimate (static)
  budgetPolicy:
    limit: "200"                    # same numeric space as pricing rates
    period:
      # duration: 30d               # or calendar month (backend-dependent):
      calendarMonth: true
      timezone: America/New_York
      startDay: 1                   # default 1 when omitted
    alerts:
      thresholds: [80]              # warn approaching limit; hard stop still at limit
```

#### Working rules (pricing)

- Default cost is `Σ (unit_count × unit_rate)` unless a profile `defaultCostExpr` or explicit `costExpr` applies.
- Subscription price overrides refer only to billable units defined by the model’s capture (profile and/or explicit units).
- Prefer built-in `usageCapture.profile` for common shapes (`openai-chat`, `anthropic-messages`, and `*-basic` variants); `profileRef` (ConfigMap) when several models share a custom shape; inline `billableUnits` / `costExpr` on the ModelRef for per-model override.
- When inline capture is present alongside `profile` / `profileRef`, inline **replaces** the referenced expansion for that model (v1).
- No `UsageCaptureProfile` CRD — built-ins + ConfigMap + inline ModelRef are enough.
- `reservationEstimate` defaults on the ModelRef; subscription `modelRefs[].reservationEstimate` overrides for that entitlement when set.
- Prices without a budget enable observe-only showback; budget remains optional and independent.

### Budget

Budgets live on **`MaaSSubscription`** and consume **calculated cost** from the effective rate card (not raw token totals alone). Reservation estimate **defaults** on the ModelRef and may be **overridden** on the subscription modelRef. Enforcement and counters run in the gateway / metering backend; MaaS declares policy and reconciles it. See the sketches above for `reservationEstimate` and `budgetPolicy` shapes.

#### Must-have (v1)

| Capability | Control-layer shape (illustrative) | Notes |
| --- | --- | --- |
| Spend **limit** | `budgetPolicy.limit` | Same numeric space as pricing rates (dimensionless weights) |
| **Period** | `budgetPolicy.period` | Fixed duration and/or calendar month + timezone + `startDay` (default **1**); calendar/TZ **backend-dependent** |
| **Hard stop** at limit | Implied when `budgetPolicy` is set | Deny when limit would be exceeded (including failed reservation) |
| **Alert thresholds** | `budgetPolicy.alerts.thresholds` | Percent of limit (e.g. 80); warn while approaching the hard stop |
| **Reserve / settle** | ModelRef / subscription `reservationEstimate` + gateway | Hold estimate before upstream (`mode: max_tokens` or `static`); settle/release on response; optional **require** when mode needs a request field |
| Enforcement **key** | Principal + subscription | `userId` + `subscriptionId` (service accounts treated as human-equivalent) |
| **Remaining budget API** | Product / backend HTTP API | Real-time remaining/spend for the current window for portal and clients — not Loki-only |

#### Remaining usage / budget API

Showback in Loki (or similar) is not enough. Product needs an endpoint that returns limit, spend-to-date, **remaining**, period bounds (or reset-at), and alert state for an authenticated principal + subscription, from a path consistent with gateway counters. Exact route/auth are API review; the store stays with the metering backend.

#### Working rules (budget)

- Budget optional; when set, needs a resolvable rate card for models that consume the allotment (completeness rules — open).
- `limit` is in the same numeric space as `pricing.perUnit` rates.
- Reserve at request, settle on response using the effective `reservationEstimate` (ModelRef default, subscription override when present).
- Reject when `mode: max_tokens` with `require: true` (or equivalent) and the bound field is missing.
- Remaining-budget reads are a product requirement on the enforcement backend.

### Backend dependency (direction only)

| Layer | Owns |
| --- | --- |
| **MaaS control layer (this proposal)** | Declarative capture + pricing + default `reservationEstimate` on `MaaSModelRef`; price / estimate overrides + `budgetPolicy` on `MaaSSubscription`; reconcile into the metering / enforcement backend; portal consumption of remaining API |
| **Enforcement / metering backend** | Admit/reserve/settle against priced usage, durable counters, period reset, alert hooks, **remaining-budget read API** |

Prefer adapting reconcile mapping to whatever backend ships over inventing a second ledger in MaaS.

## Consequences

**Positive**

- Common case is rates-only: `cost = Σ count × rate` with no expression to maintain.
- Reported token counts can match the API; overlap lives in optional / profile `costExpr`.
- Profiles keep OpenAI-with-cache correct without forcing every user to write a formula.
- Capture paths stay with the model; subscription adjusts commercial rates and may override reservation estimate.
- Budget consumes the same effective cost; reserve/settle matches common gateway spend-control practice (`max_tokens` or static, model default + entitlement override).
- Remaining-budget API keeps portal aligned with gateway enforcement.

**Negative / risks**

- Operators must maintain capture paths when providers change `usage` shapes.
- Pricing both overlapping units with **only** independent rates (no profile `defaultCostExpr` / `costExpr`) silently over-bills — needs docs + validation.
- Calendar month / TZ periods may lag if the backend only supports fixed duration initially.
- Remaining API consistency vs hot-path counters needs an explicit SLA (how stale is acceptable).
- Existing `billingRate.perToken` on subscription modelRefs overlaps this surface — reconcile or deprecate in API review.

## Open Questions

### Pricing

| # | Topic | Notes |
| --- | --- | --- |
| P1 | **Path language** | Dotted path vs JSONPath; arrays / streaming usage |
| P2 | **Missing path on response** | Zero vs incomplete settle vs reject |
| P3 | **Override merge** | Partial per-unit override (sketch) vs replace entire card |
| P4 | **Simple total-token mode** | First-class alongside typed rates, or just one capture entry named `total` |
| P5 | **Rate scale** | Per-token vs per-1K rates; how portals display cost without a named currency field |
| P6 | **Relation to `billingRate.perToken`** | Reuse, replace, or deprecate the existing subscription field |
| P7 | **Completeness before observe cost / budget** | Must every model on the subscription be priced before budget is valid? |
| P8 | **When to require / warn on `costExpr`** | If `prompt` and `cached` both have rates and no profile default / `costExpr`, warn or reject? |
| P9 | **Profile catalog** | ConfigMap namespace / RBAC / schema validation? Infer `openai-chat` vs `anthropic-messages` from model/provider type vs require explicit? |
| P9b | **Inline vs profile** | v1 = inline replaces referenced profile; later partial overlay? |
| P10 | **Strict vs lenient missing details** | Missing `usage` ⇒ incomplete; missing `cached_tokens` ⇒ 0? |
| P11 | **`costExpr` language** | Minimal ops for v1; who may set it (ModelRef only vs subscription override)? |

### Budget

| # | Topic | Notes |
| --- | --- | --- |
| B1 | **Period forms** | Exact CRD for `duration` vs `calendarMonth` + `timezone` + `startDay` (default 1); what to reject when backend lacks calendar support |
| B2 | **Alert delivery** | Header-only vs webhook/email vs both; who owns notification sinks |
| B3 | **Remaining API shape** | Path(s), auth, dimensions (principal, subscription, model?), freshness SLA vs admit counters |
| B4 | **Rollover / drain** | Needed in v1 or fixed reset-to-zero only? |
| B5 | **Reservation estimate** | How `max_tokens` / `staticAmount` map into rate-card hold (tokens vs cost); prompt-side estimate without usage; TTL on abandoned holds; default `mode` per built-in profile |
| B6 | **Required request fields** | Path for `mode: max_tokens` (OpenAI vs Anthropic); default `require: true` for chat profiles?; interaction with streaming |
| B7 | **Estimate override merge** | Subscription `reservationEstimate` always replaces ModelRef (v1 sketch) vs partial field merge |
