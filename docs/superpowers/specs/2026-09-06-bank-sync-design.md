# so-budget: Bank Sync & Transaction Import

Date: 2026-09-06
Status: Draft, pending user review

## Purpose

Confirmed transactions won't be entered manually — the app needs a live
connection to each household member's bank(s) to pull transactions in
automatically. This document defines how a bank connection is set up, how
discovered bank accounts get linked to the `Account` entities from the
core domain model, and how transactions are synced in — including
deduplication, pending-vs-posted handling, and transfer detection.

This document depends on and extends
[`2026-09-02-core-domain-model-design.md`](2026-09-02-core-domain-model-design.md):
it assumes `User`, `Household`, `Account`, `AccountOwnership`, and
`Transaction` as already defined there, and resolves that document's
"provisional" note on `Transaction.is_transfer`.

## Provider decision

**SimpleFin** (via SimpleFin Bridge), not Plaid or another enterprise
aggregator. Reasoning:

- Plaid and similar providers (Flinks, MX) are built for companies
  building fintech products — even minimal use requires a developer/
  business relationship with the provider. SimpleFin is built for
  exactly this use case: individual users subscribe directly to SimpleFin
  Bridge for their own access token (a small flat monthly fee paid by the
  user, not the app), so the app itself never has a business
  relationship with a data aggregator at all.
- Integration is much simpler: a single polling REST endpoint
  (`GET /accounts`, optionally scoped by date), versus Plaid's client-side
  "Link" widget and webhook infrastructure.
- SimpleFin has real Canadian bank coverage (this household's region),
  though — being a smaller aggregator — coverage of small/local credit
  unions isn't guaranteed the way it might be with Plaid; worth
  confirming against the household's specific institutions during
  implementation.
- Trade-off accepted: no real-time push updates. SimpleFin is poll-only,
  so freshness depends on how often the app syncs (see below).

## Scope

In scope:
- Connecting a household member's SimpleFin Bridge account
- Linking SimpleFin-discovered bank accounts to `Account` rows
- Syncing transactions: creation, dedup, pending/posted updates
- Suggesting (not auto-committing) transfer matches between linked
  accounts, resolving `Transaction.is_transfer`/`transfer_id`

Out of scope:
- SimpleFin Bridge account setup itself (happens on SimpleFin's own site)
- UI/UX for any of the above
- The actual encryption implementation for stored credentials (this doc
  states the requirement, not the mechanism)
- Manual file import (CSV/OFX) — not needed given live sync is the sole
  import method (per product decision)

## New entities

### SimpleFinConnection
- `id`
- `user_id` (immutable — the connecting user; owns and is the only one
  who can see this connection or its credential, per the core model's
  ownership-based visibility principle — no new rule needed, it falls
  directly out of existing principles)
- `access_url` (the bearer-credential URL SimpleFin issues — **must be
  encrypted at rest**; this is a stronger security requirement than
  anything in the core domain model, since it's a live credential to
  real bank data, not just app data. Mutable — see "Re-authentication"
  below)
- `status` (`ok` | `error` | `reauth_required`)
- `created_at`
- `last_synced_at` (nullable)
- `deleted_at` (nullable — soft-delete, consistent with the core doc's
  convention: disconnecting stops future syncs but leaves everything
  already imported untouched)

### LinkedAccount
- `id`
- `simplefin_connection_id`
- `external_account_id` (SimpleFin's stable identifier for this bank
  account)
- `external_org_name` / `external_display_name` (bank name and account
  nickname as reported by SimpleFin — shown to help the user pick which
  `Account` this maps to, not used in any logic)
- `account_id` (nullable — **null means this SimpleFin-reported account
  hasn't been linked to one of the household's `Account` rows yet**; it
  sits in the connecting user's pending-link queue. No transactions
  import for an external account until this is set.)
- `linked_at` (nullable, set when `account_id` is set)

Linking an account triggers a backfill sync for its available history.
How far back that history goes is determined by SimpleFin/the
institution, not something this app controls.

### `Transaction` — three new fields (extends the core domain model)
- `imported_id` (nullable) — SimpleFin's transaction ID. Used as the
  upsert key so repeated syncs update existing rows instead of creating
  duplicates.
- `pending` (boolean, system-managed) — reflects SimpleFin's
  pending/posted state. Shown distinctly; can change (amount/date update)
  or the row can be deleted (cascading to its `BudgetAssignment` rows,
  per the core model's existing cascade rule) if SimpleFin later reports
  it differently or drops it — see "Pending transaction reconciliation"
  under Open Questions for how this interacts with the core model's
  existing edit-invariant checks.
- `transfer_id` (nullable, self-referencing `Transaction.id`) — resolves
  the core doc's provisional note. `is_transfer` remains independently
  settable (e.g. a cash withdrawal with no corresponding linked account
  to match against); `transfer_id` is set on *both* sides only once a
  suggested match (see below) is explicitly confirmed.

## Sync process

Triggered on a schedule (proposing once or twice a day) and on-demand
(user-initiated refresh). For each active (`deleted_at` null,
`status = ok`) `SimpleFinConnection`:

1. Call SimpleFin's accounts endpoint, get external accounts + their
   transactions since the last sync.
2. For each external account with no `LinkedAccount` row yet: create one
   with `account_id = null` — surfaces in the pending-link queue, no
   transactions imported yet.
3. For each linked external account, upsert its transactions by
   `imported_id`:
   - New `imported_id` → create a `Transaction`.
   - Existing `imported_id` → update `pending`/`amount`/`date` if
     SimpleFin now reports it differently.
4. **Vanished pending transactions**: a transaction previously imported
   as `pending` that's missing from the latest sync, *within a 14-day
   rolling window*, is deleted as canceled. Older pending transactions are
   left alone even if stale, to avoid misfiring on something just outside
   a query's date range rather than genuinely canceled.
5. **Transfer-match suggestion**: after import, a matching query looks
   for candidate pairs among transactions with no `transfer_id` and no
   `BudgetAssignment` yet — same absolute amount, opposite sign, both
   accounts belonging to the household, dates within ±3 days of each
   other. Matches are surfaced for confirmation, never
   auto-committed. Confirming sets `is_transfer = true` and `transfer_id`
   on both sides. Once a transaction has any `BudgetAssignment`, it's
   implicitly "not a transfer" and drops out of matching — no separate
   dismissal-tracking field needed.

## Open questions / explicit assumptions

- **Pending transaction reconciliation vs. the core model's edit
  invariants.** If a `pending` transaction already has `BudgetAssignment`
  rows (assigned while still pending) and SimpleFin later reports a
  different amount on posting, the core domain model's existing rule
  ("editing a transaction's amount below its current assignment sum is
  rejected") would block a sync-driven update. Recommended default,
  not yet confirmed: sync-driven updates should be allowed to override
  that check (since it reflects ground truth from the bank, unlike a
  user edit), leaving the assignment sum inconsistent with the new
  amount and requiring the user's attention (e.g. surfaced similarly to
  the "To Review" pattern) rather than auto-adjusting assignment amounts
  on the user's behalf. This needs an explicit decision before
  implementation.
- **Re-authentication.** When `status = reauth_required`, updating
  `access_url` in place (not creating a new `SimpleFinConnection` row)
  preserves all existing `LinkedAccount` mappings and transaction
  history, since those are keyed by `external_account_id`, not by
  connection identity. The UX for detecting/prompting this is out of
  scope (UI).
- **Joint-account double-import risk.** If, once this household has a
  joint account, *both* members set up their own `SimpleFinConnection`
  and both happen to have online access to the same joint account, each
  connection would likely report it under its own distinct
  `external_account_id` (SimpleFin has no concept of shared accounts).
  If both get linked to the same `Account`, and SimpleFin doesn't
  guarantee identical `imported_id` values for the same real transaction
  across two different bridge sessions, this could produce **duplicate
  Transaction rows** for the same real-world transaction. Not resolved
  here — flagged because it's a real, concrete risk this household will
  eventually hit given they've said joint accounts are a "near future"
  possibility, not a hypothetical.
- **Sync frequency specifics** (exact schedule interval, backoff on
  errors, rate-limit handling) are implementation details deferred to
  the implementation plan, not a data-modeling concern.
- **Institution coverage** — whether SimpleFin actually supports this
  household's specific banks — needs to be verified against SimpleFin's
  current coverage during implementation, not assumed from this document.
