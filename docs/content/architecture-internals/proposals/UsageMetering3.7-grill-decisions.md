# Usage metering 3.7 — grill decisions log

> **Not for Google Docs paste.** Working log from the 2026-10-28 grill + 2026-10-30 posture shift. Clean TP sheet: [UsageMetering3.7.md](UsageMetering3.7.md).

**Date:** 2026-10-28 (addendum 2026-10-30)  
**Participants:** Jamie (answers) + Cursor grill-me / follow-on stakeholder sync

---

## How to use

- Copy/paste **only** [UsageMetering3.7.md](UsageMetering3.7.md) into Docs.
- Use this file when you need the Q→A trail or to reopen a lock.

---

## 2026-10-30 posture shift (supersedes several 10-28 locks)

| Topic | New lock | Notes |
| ---- | ---- | ---- |
| Budget / allotment on MaaS CRD | **Out** | Do not put wallets / `budgetPolicy` / period allotment on MaaS CRs |
| Cost of record | **External meter** | Price Tag, Monetize 360, Cost Management (if committed), BYO |
| Go / no-go | **Push from meter** | Fastest practical push; lag accepted; meter owns leeway (~$10 Monetize 360 example) |
| Pricing language | **→ weights** | Optional; relative to **stable unit** (docs term, not CRD field) |
| 3.7 priority | **Accurate emit** | Integrations second; local reserve-then-settle CRD budget withdrawn |
| Control-layer doc | Renamed | [subscription-usage-weights-and-capture.md](subscription-usage-weights-and-capture.md); old rate-card filename is a pointer |

**Reopened / superseded from 10-28:** local `budgetPolicy`, HTTP 402 from CRD allotment, reserve/settle against CRD budget, allotment unit / period carry / remaining-budget API as MaaS product Must, sidecar fail-closed tied to local budget.

---

## Locked decisions (2026-10-28 grill order — historical)

| # | Topic | Choice | Notes |
| --: | ---- | ---- | ---- |
| 1 | External meter on critical path (3.7) | **A export-only** | Design with **B** in mind (later dual-mode) — **still valid**; talk-back now first-class direction |
| 2 | “Talk back” shape for B seam | **Open; lean C** | External **pushes** go/no-go — **elevated** by 10-30 |
| 3 | Named external systems (3.7) | **C → refreshed** | Price Tag + **Monetize 360** first-pass; Cost Management if commitment; publish CE |
| 4 | Export contract | **C** | **CloudEvents = external envelope**; OTel/Loki = in-cluster visibility |
| 5 | CE lifecycle events | **B** | Was reserve+settle for local budget — **revisit** once push contract lands (emit usage remains Must) |
| 6 | CE vs admit coupling | **Alex sidecar** | Proxy hands off immediately; sidecar batches/ships/retry/backpressure |
| 7 | Sidecar owner | **Praxis** | Sidecar emits OTel/OTLP |
| 7b | Append-only / aggregates owner | **Gateway** (lean) | SoR semantics; Obs may own impl — **open** |
| 8 | Where CE attaches | **B** | Gateway-owned **projector** from SoR; sidecar not dual-ship CE |
| 9 | Sidecar buffer full / OTLP outage | **Revisit** | Was tied to local budget fail-closed; metering-only drop + gap still lean |
| 10–36 | Local budget / allotment locks | **Superseded** | See 10-30 table; do not paste 10-28 budget locks into Docs without the shift |

Full 10-28 Q→A table retained in git history for this file if needed; do not treat budget rows as current product direction.

---

## Explicit non-locks / reopen

- Push talk-back payload, correlation IDs, freshness SLA, fail-open vs fail-closed.
- How fast meter → gateway push can be for Price Tag / Monetize 360.
- Weight schema vs `billingRate.perToken`.
- Weighted rate limiting in 3.7 vs emit-only weights.
- Deny code when go/no-go is deny (prior lean 402).
- Obs vs Gateway who *runs* the append-only log.
- Cost Management commitment.

---

## Suggested paste checklist (one-pager only)

When updating Google Docs from [UsageMetering3.7.md](UsageMetering3.7.md), skip this file entirely.
