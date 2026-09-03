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

Out of scope (future design docs):
- Technology/framework/storage choices
- UI/UX
- Bank/card sync or manual transaction import
- Categories/tags for personal reporting (separate from budgets)
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
month is the row with the latest `effective_from <= that month`. This
preserves full history (e.g. "$1000/month from Feb 2027, $1200/month from
Jan 2028") without needing full event sourcing.

### BudgetAssignment
- `id`
- `transaction_id`
- `budget_id`
- `amount`

Links a transaction (or a portion of it) to a budget. A single transaction
can have multiple `BudgetAssignment` rows across different budgets (e.g. a
grocery run split between "Household" and a personal "Treats" budget). The
sum of a transaction's assignment amounts must not exceed the transaction's
total amount (same sign).

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
month: `accept` zeroes out (forgives) the imbalance with no money changing
hands; `transfer` records that money actually moved between members.

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
- `total_variance(month)` = `sum(actual)` − `sum(committed)` across all
  budget members — the household's over/underspend for that month.
- Outstanding `Settlement` records for the budget/month reduce the balance:
  `accept` zeroes it out (forgiven, no money moves); `transfer` records an
  actual payment between members.
- **Unsettled balances carry forward** month over month as a running total
  per user, until settled via `accept` or `transfer`.

### Example (from the design conversation)

Household budget, commitments: you $1000/month, partner $1200/month
(total committed $2200). Actual spend this month: you $1450 (personal-account
purchases assigned to Household), partner $1000. Joint-account spend on the
budget, if any, doesn't affect these per-user numbers.

- `balance(you)` = 1450 − 1000 = **+450**
- `balance(partner)` = 1000 − 1200 = **−200**
- `total_variance` = (1450+1000) − (1000+1200) = **+250** (household
  overspent by $250 this month)

The app surfaces both: partner owes you a net amount reflecting the +450/−200
split, and the household overspent by $250 relative to its combined
commitment. Settling can be a `transfer` (partner pays you) and/or an
`accept` (you agree to absorb some or all of the $250 overspend), recorded
per your actual conversation about it — the model doesn't prescribe how the
$250 gets divided, only that a `Settlement` row records what you decided.

## Open questions / explicit assumptions

- Single currency per household is assumed for v1; multi-currency handling
  (conversion, mismatched account currencies) is out of scope.
- Who may edit/delete a `BudgetAssignment` after creation (only its creator,
  or any budget member?) is not yet decided — flagged for the implementation
  plan.
- Whether a `Budget` can be deleted/archived, and what happens to its
  historical `Commitment`/`Settlement` records when it is, is not yet
  decided.
- Settlement UX (how a $250 combined-overspend gets divided into individual
  `accept`/`transfer` actions) is a product/UI decision, not a data-model
  one; this doc only defines the record types involved.
