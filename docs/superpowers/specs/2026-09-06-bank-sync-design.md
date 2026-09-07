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
  unions isn't guaranteed the way it might be with Plaid. Coverage is
  generally per-institution, but individual product lines at the same
  institution can vary; worth confirming against each of the household's
  specific banks and account types during implementation, not assumed
  from this document.
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
  below — but an update is only accepted if the new URL's reported
  accounts overlap with this connection's existing
  `LinkedAccount.external_account_id`s; one that looks like an entirely
  different set of accounts is rejected rather than silently redirecting
  future syncs to unrelated bank data. If this connection has zero
  `LinkedAccount` rows yet — nothing has ever been linked, so there's no
  existing set to compare against — the overlap check is skipped and the
  update is accepted unconditionally; otherwise a connection that needed
  re-auth before its first successful link could never be updated at all)
- `status` (`ok` | `error` | `reauth_required`; defaults to `ok` on
  creation. A sync attempt that fails with an authentication error sets
  it to `reauth_required`; any other sync failure (e.g. transient
  API/rate-limit error) sets it to `error`. A successful `access_url`
  update (passing the overlap check above) or a successful sync run
  resets it to `ok`. **`error` connections are still attempted on every
  sync run — only `reauth_required` ones are excluded** (see the Sync
  process gate below); otherwise a merely transient failure with a
  perfectly valid credential would permanently strand the connection,
  since nothing would ever attempt a sync that could reset it back to
  `ok`. `reauth_required` connections are excluded because retrying
  without a credential fix is pointless — they resume once `access_url`
  is updated)
- `created_at`
- `last_synced_at` (nullable)
- `deleted_at` (nullable — soft-delete, consistent with the core doc's
  convention: disconnecting stops future syncs but leaves everything
  already imported untouched)

### LinkedAccount
- `id`
- `simplefin_connection_id`
- `external_account_id` (SimpleFin's stable identifier for this bank
  account — stable, and unique, only *within* the connection that
  reported it, not globally: step 2's "no `LinkedAccount` row yet" lookup
  is keyed on `(simplefin_connection_id, external_account_id)` together.
  This is why two different connections linking the same real joint
  account produce two separate `LinkedAccount` rows with two different
  `external_account_id`s in the first place — see the joint-account
  double-import risk below.)
- `external_org_name` / `external_display_name` (bank name and account
  nickname as reported by SimpleFin — shown to help the user pick which
  `Account` this maps to, not used in any logic)
- `account_id` (nullable — **null means this SimpleFin-reported account
  hasn't been linked to one of the household's `Account` rows yet**; it
  sits in the connecting user's pending-link queue. No transactions
  import for an external account until this is set. Must reference an
  `Account` belonging to the connecting user's household — not
  necessarily solely owned by them, since linking a joint account is
  legitimate, but it must be a household account, not an arbitrary one.)
- `linked_at` (nullable, set when `account_id` is set)

Linking an account triggers a backfill sync for its available history.
How far back that history goes is determined by SimpleFin/the
institution, not something this app controls.

`account_id` can be **cleared** (set back to `null`, returning the row to
the pending-link queue) but cannot be changed directly from one non-null
value to another — re-pointing to a different `Account` means unlinking
first, then linking fresh. This avoids silently reattaching a stream of
future transactions to a different `Account` than the one already-imported
history sits under, with no clear boundary between "old account's history"
and "new account's history." Clearing `account_id` doesn't touch
`Transaction` rows already imported under it — they stay attached to
their `Account`, untouched, exactly like transactions on a soft-deleted
`Account` do. In particular, any that were still `pending` stay `pending`
indefinitely, since sync no longer touches this `external_account_id`
once unlinked; they'll only resume updating if the same external account
is linked again later.

Multiple `LinkedAccount` rows **may** reference the same `account_id` —
this isn't prevented. That's a deliberate (if imperfect) choice: it's the
only way for a jointly-held bank account to be linked by more than one
household member's own `SimpleFinConnection`, which is a legitimate case,
not a mistake to guard against. It's also the exact mechanism behind the
"joint-account double-import risk" noted under Open Questions — allowing
it is what creates that risk, and this document doesn't yet resolve the
tension between the two.

### `Transaction` — four new fields (extends the core domain model)
- `imported_id` (nullable) — SimpleFin's transaction ID. Used as the
  upsert key so repeated syncs update existing rows instead of creating
  duplicates. When an existing `imported_id` posts with a `pending`
  transition from `true` to `false`, that's an ordinary field update
  under the same upsert, same as any other reported change.
- `pending` (boolean, system-managed) — reflects SimpleFin's
  pending/posted state. Shown distinctly; can change (amount/date update)
  or the row can be deleted (cascading to its `BudgetAssignment` rows,
  per the core model's existing cascade rule) if SimpleFin later reports
  it differently or drops it — see "Pending transaction reconciliation"
  under Open Questions for how this interacts with the core model's
  existing edit-invariant checks. If a deleted "vanished pending"
  transaction has a `transfer_id`, the deletion also clears (sets to
  `null`) `transfer_id` **and** resets `is_transfer` to `false` on the
  transaction it was paired with — the pairing no longer has a real
  counterpart to point at, and rather than leave a dangling reference or
  guess whether the survivor is still a transfer at all, it drops back to
  needing review, the same as an unpaired transaction would.
- `transfer_id` (nullable, self-referencing `Transaction.id`) — resolves
  the core doc's provisional note. `is_transfer` remains independently
  settable (e.g. a cash withdrawal with no corresponding linked account
  to match against); `transfer_id` is set on *both* sides only once a
  suggested match (see below) is explicitly confirmed.
- `transfer_match_dismissed_at` (nullable, system/user-managed) — set
  when a user explicitly rejects a suggested transfer match involving
  this transaction. Excluded from future transfer-match candidate
  queries, the same as a transaction with a `transfer_id` or
  `BudgetAssignment` already is — without this, a rejected suggestion
  would simply reappear on every subsequent sync, since rejecting it
  doesn't otherwise change any field's value.

## Sync process

Triggered on a schedule (proposing once or twice a day) and on-demand
(user-initiated refresh). For each active (`deleted_at` null,
`status != reauth_required`) `SimpleFinConnection` — i.e. `ok` or `error`,
per that field's reset-path note above:

1. Call SimpleFin's accounts endpoint, get external accounts + their
   transactions since the last sync.
2. For each external account with no `LinkedAccount` row yet: create one
   with `account_id = null` — surfaces in the pending-link queue, no
   transactions imported yet.
3. For each linked external account whose `Account` is **not**
   soft-deleted (`deleted_at` null), upsert its transactions by
   `imported_id`:
   - New `imported_id` → create a `Transaction`.
   - Existing `imported_id` → update `pending`/`amount`/`date` if
     SimpleFin now reports it differently.
   A `LinkedAccount` whose `Account` has been soft-deleted is skipped
   entirely — no new `Transaction` rows are created against it, matching
   the core model's convention that a soft-deleted row stops being a
   target for new rows. Its `SimpleFinConnection` keeps syncing other,
   still-active linked accounts normally.
4. **Vanished pending transactions**: scoped only to transactions on
   `LinkedAccount`s actually processed in step 3 (i.e. **excluding**
   accounts skipped for a soft-deleted `Account`, or `LinkedAccount`s
   that have since been unlinked) — this check is about "SimpleFin
   stopped reporting a transaction we're actively syncing," not "we
   didn't happen to look this run." Within that scope, a transaction
   previously imported as `pending`, whose own `date` is not more than
   14 days in the past as of this sync run (i.e. `date >= now - 14 days`
   — this has no upper bound, so a future-dated or todays-dated pending
   transaction is included, not left in limbo), that's missing from the
   latest sync, is deleted as canceled (also see the `transfer_id`
   cleanup this can trigger, noted on that field above). Transactions
   dated more than 14 days in the past are left alone even if still
   `pending`, to avoid misfiring on something just outside a query's
   date range rather than genuinely canceled.
5. **Transfer-match suggestion**: runs after steps 3 and 4 have fully
   completed for this connection (so any pair whose `transfer_id` was
   just cleared by the amount-change reconciliation below, or by step
   4's vanished-pending cleanup, is already back to looking like an
   ordinary unpaired transaction by the time this step's query runs, and
   is eligible for a fresh match in the same run rather than waiting for
   the next sync). A matching query looks for candidate pairs among
   transactions with no `transfer_id`, no `transfer_match_dismissed_at`,
   and no `BudgetAssignment` yet — on **two different accounts**
   (excluding same-account pairs, e.g. a purchase and its refund on the
   same card, which trivially "share a common owner" with themselves but
   aren't a transfer between accounts at all), same absolute amount,
   opposite sign, dates within ±3 days of each other, and **both accounts
   sharing at least one common owner** (matching the core model's own
   definition of `is_transfer` as money moving "between the user's own
   accounts" — this is deliberately narrower than "both accounts
   belonging to the household," which would also match, say, one member
   e-transferring the other, a real payment between two people, not a
   self-transfer). A transaction with `is_transfer` already manually set
   `true` (e.g. a cash withdrawal, per the core doc's example) is still
   an eligible candidate — pairing it doesn't change what it means, it
   just adds a confirmed link if a real match turns up, and it's still
   only ever committed on explicit confirmation, never silently. Matches
   are surfaced for confirmation, never auto-committed.

   Confirming re-checks both transactions still have no `BudgetAssignment`
   **and no `transfer_id`** at that moment (not just when the suggestion
   was generated) — if either condition no longer holds (assigned to a
   budget, or already claimed by a different confirmed match, which can
   happen when three or more transactions share the same amount within
   the date window), the suggestion is discarded rather than confirmed,
   rather than overwriting an existing pairing or conflicting with the
   core model's `BudgetAssignment`/`is_transfer` mutual-exclusion rule.
   This check-then-set must happen atomically (e.g. a single conditional
   write per transaction, not a separate read followed by a separate
   write) — otherwise two overlapping suggestions confirmed at nearly the
   same moment could both pass the check before either write lands,
   letting one transaction end up claimed by two different pairs. Once
   the check passes, confirming sets `is_transfer = true` and
   `transfer_id` on both sides. Once a transaction has any
   `BudgetAssignment`, it's implicitly "not a transfer" and drops out of
   matching; explicitly rejecting a suggestion sets
   `transfer_match_dismissed_at` to the same effect.

   If a transaction's `amount` changes after its transfer pair was
   confirmed (a `pending` transaction posting with a different amount —
   see `imported_id` above, applied during step 3), the pair no longer
   satisfies the "same absolute amount" condition it was confirmed under.
   The sync process clears `transfer_id` on both sides and resets
   `is_transfer` to `false` on both, the same "drop back to needing
   review" treatment used for a vanished-pending pair, rather than
   leaving a pairing that no longer matches on the books.

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
- **Stale old pending transactions have no cleanup path.** The vanished-
  pending cancellation (see Sync process step 4) only reaps transactions
  dated within the last 14 days; a `pending` transaction older than that,
  if genuinely canceled by the bank, has no mechanism in this document
  to ever detect or clean it up — it stays `pending` indefinitely. Known
  limitation, not resolved here; a longer reconciliation window or a
  periodic full-history re-check are possible future fixes, deliberately
  not designed now.
- **Institution coverage** — see the Provider decision section; not
  verified against this household's actual banks yet.
