---
title: Usage metering — customer requirements (working notes)
description: Captured customer requirements for usage capture and optional weights, backing the 3.7 metering plan and control-layer proposal.
---

# Usage metering — customer requirements (working notes)

> [!NOTE]
> Working capture from field / product conversations. Complements [Usage metering (TP) for AI Gateway 3.7](UsageMetering3.7.md) and the [usage capture / weights control-layer proposal](subscription-usage-weights-and-capture.md). Not an implemented feature.

**Parent:** [UsageMetering3.7.md](UsageMetering3.7.md)

**Date:** 2026-10-26 (updated 2026-10-30 — budget-on-CRD withdrawn; pricing → weights)

---

## Usage capture and weights

### What customers need

1. **Per-model, per-billable-unit weighting (optional)** — not a single flat number per model. **Token types** (input, output, cached, reasoning, and similar) and **non-token modes** (image, audio, video, per-request, and similar) can each have different **weights** relative to a **stable unit**. Some deployments may still choose a single total-token weight; that should remain possible, but the product must not assume “one number per model” or “tokens only” is enough. Weights are for observability / optional rate-limit math — **not** invoice price.

2. **Defaults by model / usage type** — models marked OpenAI Chat Completions–compatible (or similar) get a **built-in capture profile**: core `prompt_tokens` / `completion_tokens`, optional cache/reasoning with missing details treated as 0. Explicit capture overrides remain available. Non-chat shapes (embeddings, audio, image, Responses, …) need their own profiles.

3. **Declare which billable units to capture from the response** — providers expose usage in different shapes. Customers map a named unit to a **path** on `usage` (or inherit that map from a profile). Prefer **raw API counts** for showback so reported totals match the provider.

   ```json
   "usage": {
     "prompt_tokens": 2006,
     "completion_tokens": 300,
     "total_tokens": 2306,
     "prompt_tokens_details": {
       "cached_tokens": 1920,
       "cache_creation_tokens": 0,
       "text_tokens": 86,
       "audio_tokens": 0,
       "image_tokens": 0
     },
     "completion_tokens_details": {
       "reasoning_tokens": 0,
       "text_tokens": 300,
       "audio_tokens": 0
     }
   }
   ```

   Example: capture `prompt` from `usage.prompt_tokens` and `cached` from `usage.prompt_tokens_details.cached_tokens` as reported.

4. **Weighted usage defaults to rate weights; expressions only when needed** — if the operator only sets weights, weighted usage is `Σ (count × weight)`. Optional **`weightExpr`** covers overlap / complex cases. Profiles (e.g. `openai-chat`) may ship a **default `weightExpr`** so OpenAI cache-aware weighting stays correct without writing a formula on every model.

5. **Overlapping totals must not double-count** — on OpenAI, `cached_tokens` is a **subset** of `prompt_tokens`. Independent weights on both without a weight rule over-count. Fix that in **weights** (profile default or explicit `weightExpr`), not by rewriting reported token counts.

6. **Defaults on the model, overrides on the subscription**
   - **`MaaSModelRef`** — usage **profile** (built-in), **ConfigMap `profileRef`**, or **inline** capture on the ModelRef; optional default weights.
   - **`MaaSSubscription`** — optionally **override** weights for models on that subscription. Capture stays on the model.

7. **Showback without becoming the billing ledger** — raw counts (+ optional weighted usage) are for visibility in product; **cost, wallets, and cut-off** remain with external metering (Price Tag, Monetize 360, Cost Management / BYO).

### Control-plane split

| Concern | Where | Notes |
| --- | --- | --- |
| Usage shape / capture profile | `MaaSModelRef` | Built-in name, ConfigMap ref, or inline profile object |
| Which billable units exist for this model | `MaaSModelRef` | From profile and/or named units + path |
| Default weight per unit | `MaaSModelRef` | Optional; relative to **stable unit** |
| Effective weight for a given entitlement | `MaaSSubscription` | Optional override of ModelRef defaults |
| Cost / wallet / go/no-go | External meter | Export + push talk-back; **not** a MaaS CRD budget |

### Open (capture / weights)

- Partial vs full override on subscription (merge per unit vs replace entire weight map).
- Path language and behavior when a path is missing.
- `weightExpr` language; when profiles inject `defaultWeightExpr`; warn if overlapping units are weighted with no weight rule.
- Profile catalog, auto-detect vs explicit, and merge with hand-written `billableUnits` / `weightExpr`.
- Whether `total_tokens` alone is a supported “simple mode” alongside typed units.
- Relation to existing `billingRate.perToken`.

---

## Spend control (external — not CRD)

### What customers need

1. **Hard stop when entitlement is exhausted** — deny further admission once the **external** meter says no-go. Not a `budgetPolicy` field on `MaaSSubscription`.
2. **Accept lag** — cost is computed off the hot path; push into the gateway as fast as practical; meter chooses **leeway** (working example: Monetize 360 minimum ~$10 wallet).
3. **Integrations** — first pass: accurate metrics + bind Price Tag and Monetize 360; Cost Management if we get a commitment.

### Out of scope on MaaS CRs

- Wallet / top-up UX as a MaaS product surface.
- Period allotment / drain / rollover on the subscription CR.
- Hierarchical org → team → key pools as first-class MaaS CRs.
- MaaS-owned remaining-balance DB as source of truth.
