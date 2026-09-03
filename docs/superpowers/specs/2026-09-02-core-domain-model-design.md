# so-budget: Core Domain Model & Access Control

Date: 2026-09-02
Status: Draft, approved by user pending final review

## Purpose

so-budget is a household budgeting app for a household of 2+ people who each
bring their own personal accounts/credit cards plus one or more joint
accounts. Unlike most budgeting apps, it does **not** show every transaction
to every household member. A transaction is only visible to someone who owns
the account it came from, or to whom it has been explicitly shared by
assigning it (in whole or part) to a budget they belong to.

This document defines the core data model and the access-control rules that
govern it: who can own what, who can see what, how transactions get split
across budgets, and how shared-budget settlement (who owes whom) is
calculated. It intentionally excludes tech stack, UI, and bank-sync/import —
those are separate design efforts.

## Scope

In scope:
- Users, households, accounts, and ownership
- Transactions and their visibility rules
- Budgets, budget membership, and assigning transactions (or portions of
  them) to budgets
- Per-budget monthly commitments, actual-spend tracking, and settlement
  (who owes whom, and how imbalances get resolved)
- Envelopes (YNAB-style category budgeting) nested within a budget, with
  rollover balances and versioned allocation history

Out of scope (future design docs):
- Technology/framework/storage choices
- UI/UX
- Bank/card sync or manual transaction import
- Notifications, multi-currency, multi-household membership for a user

## Core entities

### User
- `id`
- `name`
- `email`

### Household
- `id`
- `name`

### HouseholdMembership
- `household_id`
- `user_id`
- `joined_at`

A household supports any number of members (starting usage will typically be
2, but the model does not assume exactly 2).

### Account
- `id`
- `household_id`
- `name`
- `type` (checking, savings, credit card, ...)
- `currency`

### AccountOwnership
- `account_id`
- `user_id`

Many-to-many: an account with exactly one owner is a personal account; an
account with two or more owners is a joint account. There is no structural
difference between the two beyond the number of owners.

### Transaction
- `id`
- `account_id`
- `date`
- `amount` (signed; negative for refunds/credits, by convention)
- `description` / `merchant`

A transaction always belongs to exactly one account and inherits that
account's currency.

### Budget
- `id`
- `household_id`
- `name`
- `created_by` (user_id)

Budgets are arbitrary and user-defined (e.g. "Household", "Vacation",
"Kids") — there is no fixed "personal" vs. "shared" type. A budget with one
member behaves like a personal budget; a budget with two or more members
behaves like a shared one. Settlement math (see below) only applies to
budgets with 2+ members with commitments defined.

### BudgetMembership
- `budget_id`
- `user_id`

### Commitment
- `id`
- `budget_id`
- `user_id`
- `amount`
- `effective_from` (month)
- `created_at`

Represents how much a member has committed to contribute to a budget per
month. **Immutable and versioned**: changing a commitment inserts a new row
rather than editing an existing one. The commitment that applies to a given
month is the row with the latest `effective_from <= that month`; if two rows
tie on `effective_from`, the one with the latest `created_at` wins. If no
row's `effective_from` is `<= that month` (nothing has been committed yet as
of that month), `committed(user)` is `0`.

### Envelope
- `id`
- `budget_id`
- `name`
- `created_at`

A named spending category nested within a budget (e.g. the "Household"
budget contains envelopes "Groceries", "Insurance", "Internet & Mobile").
Envelopes do not exist independently of a budget.

### EnvelopeAllocation
- `id`
- `envelope_id`
- `amount`
- `effective_from` (month)
- `created_at`
- `created_by`

The monthly amount allocated to an envelope — one shared amount for the
whole budget, not per-member. **Immutable and versioned**, exactly like
`Commitment`: changing the allocation inserts a new row rather than editing
one, so the full history of changes is preserved. Same lookup and tie-break
rule as `Commitment`: the applicable row for month `m` is the latest
`effective_from <= m`, ties broken by `created_at`; if none exists yet,
`allocation(m)` is `0`.

### BudgetAssignment
- `id`
- `transaction_id`
- `budget_id`
- `envelope_id` (nullable; if set, must belong to the same `budget_id`)
- `amount`

Links a transaction (or a portion of it) to a budget, optionally tagging
that portion with one envelope within the budget. A single transaction can
have multiple `BudgetAssignment` rows across different budgets (e.g. a
grocery run split between "Household" and a personal "Treats" budget), and
each row is independently taggable with at most one envelope. Finer
splitting across envelopes reuses this same mechanism — e.g. a $30 Sobeys
transaction becomes a $20 row (Household / Groceries) and a $10 row
(Household / Household Supplies) rather than requiring a separate
envelope-splitting feature. Assignment amounts must share the transaction's
sign, and the sum of their absolute values must not exceed the absolute
value of the transaction's amount — e.g. a −$50 refund can have assignments
summing to anywhere from $0 to −$50, but never past −$50.

If a `Transaction` is deleted, its `BudgetAssignment` rows are deleted with
it (cascade). Editing a transaction's `amount` to a value smaller in
magnitude than the current sum of its assignments is rejected — the
assignments must be reduced first, to preserve the invariant above.

### Settlement
- `id`
- `budget_id`
- `month`
- `type` (`accept` | `transfer`)
- `from_user_id`
- `to_user_id` (for `transfer`; null for `accept`)
- `amount`
- `created_at`
- `created_by`

Records how an outstanding balance on a budget was resolved for a given
month: `transfer` records money actually moving from `from_user_id` (payer)
to `to_user_id` (payee); `accept` records `from_user_id` (the member who was
owed reimbursement, i.e. the one with the positive balance) forgiving it —
no money moves, and `to_user_id` is left null.

## Visibility rules

1. A user can see a `Transaction` if, and only if:
   - they own the transaction's `Account` (via `AccountOwnership`), **or**
   - the transaction has at least one `BudgetAssignment` to a `Budget` the
     user belongs to (via `BudgetMembership`).
2. Visibility is all-or-nothing at the transaction level: once visible, the
   full transaction (merchant, full amount, date, account) is shown — not
   just the assigned slice. (Default behavior; a per-assignment "share only
   the amount" option is a possible future refinement, not in this design.)
3. A user may create a `BudgetAssignment` only for transactions on accounts
   they own, and only targeting budgets they are themselves a member of.
   This prevents a user from granting visibility into someone else's
   transaction, or assigning into a budget they don't belong to.
4. **Assumption:** visibility is not retroactively revoked. If a user is
   later removed from a budget, transactions already assigned/visible to
   them remain visible; only future transactions are affected.

## Settlement & balance calculation

Balances are computed on read (queried/aggregated), not precomputed or
event-sourced — household transaction volume is small enough that this is
never a performance concern, and it keeps the model simple to reason about
and audit.

For a given `Budget` and `month`:

- `committed(user)` = that user's applicable `Commitment.amount` for the
  month (see versioning rule above).
- `actual(user)` = sum of `BudgetAssignment.amount` for that budget/month,
  restricted to transactions whose account is **solely owned** by that user.
  Joint-account transactions assigned to the budget are excluded from this
  per-user total — that money is already shared/pooled, so it reduces the
  household's picture of total spend but isn't attributed to either
  individual for settlement purposes.
- `balance(user)` = `actual(user) − committed(user)`. Positive means the
  user paid more than they committed (they're owed reimbursement);
  negative means they paid less than committed (they owe).
- `total_spend(month)` = sum of **all** `BudgetAssignment.amount` for the
  budget/month, regardless of which account (personal or joint) the
  transaction came from. Unlike `actual(user)`, this is not restricted to
  solely-owned accounts — it's the household's full picture of what was
  spent against the budget.
- `total_variance(month)` = `total_spend(month) − sum(committed)` across
  the budget's members — the household's over/underspend for that month.
  (This is deliberately not the same as `sum(actual(user))`, which excludes
  joint-account spend; `total_variance` is the only figure that includes
  it.)
- Outstanding `Settlement` records for the budget/month reduce the balance:
  `accept` zeroes it out (forgiven, no money moves); `transfer` records an
  actual payment between members. The data model does not constrain
  `Settlement.amount` to match the computed `balance(user)` — the app layer
  is expected to suggest the computed figure, but recording a different
  amount is structurally legal and simply changes the running balance
  accordingly.
- **Unsettled balances carry forward** month over month as a running total
  per user, until settled via `accept` or `transfer`.

### Example (from the design conversation)

Household budget, commitments: you $1000/month, partner $1200/month
(total committed $2200). Actual spend this month: you $1450 (personal-account
purchases assigned to Household), partner $1000 — no joint-account spend was
assigned to the budget this month, so `total_spend` equals the sum of
personal spend here; if it had, `total_variance` would include it while
`balance(you)`/`balance(partner)` still would not.

- `balance(you)` = 1450 − 1000 = **+450**
- `balance(partner)` = 1000 − 1200 = **−200**
- `total_spend` = 1450 + 1000 = **2450**
- `total_variance` = 2450 − 2200 = **+250** (household overspent by $250
  this month)

The app surfaces both: partner owes you a net amount reflecting the +450/−200
split, and the household overspent by $250 relative to its combined
commitment. Settling can be a `transfer` (partner pays you) and/or an
`accept` (you agree to absorb some or all of the $250 overspend), recorded
per your actual conversation about it — the model doesn't prescribe how the
$250 gets divided, only that a `Settlement` row records what you decided.

## Envelope tracking (rollover budgeting)

Envelopes track spend against a category allocation the way YNAB does:
unspent money rolls forward as available balance, and overspending leaves
the envelope negative until future allocations (or a manual fix) cover it.
This is independent of, and not reconciled against, the budget's own
per-member `Commitment` totals — an envelope's allocations are a separate
set of numbers the budget members choose to set (see open questions).

For a given `Envelope` as of month `M`:

- `allocation(m)` = the envelope's applicable `EnvelopeAllocation.amount` for
  month `m` (latest row with `effective_from <= m`, ties broken by
  `created_at`, same rule as `Commitment`); `0` if no row applies yet.
- `spend(m)` = sum of `BudgetAssignment.amount` tagged with this envelope,
  for transactions dated in month `m`; `0` if none.
- `running_balance(M)` = sum, over every month `m` from the envelope's
  `created_at` month through `M`, of `allocation(m) − spend(m)`. Because
  both terms default to `0` when absent, this is well-defined even for
  months before the envelope's first `EnvelopeAllocation`, or if spend is
  ever tagged to an envelope that has no allocation at all.

A positive running balance is money still available in the envelope; a
negative one is overspending being carried forward.

### Example

"Groceries" envelope, allocated $500/month starting Jan 2027. January spend
is $420 (balance: +$80, carried forward). February allocation stays $500;
February spend is $560 → `allocation(Feb) − spend(Feb)` = −$60, so the
running balance going into March is $80 − $60 = **+$20**.

## Open questions / explicit assumptions

- Single currency per household is assumed for v1. This is a convention,
  not an enforced constraint: `currency` lives on `Account`, nothing
  requires all of a household's accounts to share one, and every aggregate
  in this doc (`actual`, `total_spend`, `total_variance`, envelope
  `spend(m)`) sums raw amounts with no currency check. Mixing currencies
  within a household would silently produce meaningless totals rather than
  an error. Acceptable for v1; real multi-currency support is out of scope.
- Who may edit/delete a `BudgetAssignment` after creation (only its creator,
  or any budget member?) is not yet decided — flagged for the implementation
  plan.
- Whether a `Budget` can be deleted/archived, and what happens to its
  historical `Commitment`/`Settlement` records when it is, is not yet
  decided.
- Settlement UX (how a $250 combined-overspend gets divided into individual
  `accept`/`transfer` actions) is a product/UI decision, not a data-model
  one; this doc only defines the record types involved.
- There is no enforced relationship between a budget's total `Commitment`
  amounts and the sum of its envelope allocations (true YNAB "give every
  dollar a job" reconciliation is not required for v1). This may be worth
  revisiting once envelopes are actually used day to day.
- Whether an `Envelope` can be deleted/archived, and what happens to its
  historical `EnvelopeAllocation` rows and running balance when it is, is
  not yet decided.
- What happens when a user is removed from a `Household`
  (`HouseholdMembership` deleted) — their solely-owned accounts, budget
  memberships, and commitments would become orphaned — is explicitly
  deferred. Not addressed in v1; worth its own design pass if/when the app
  needs to support it.
