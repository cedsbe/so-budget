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
- Per-budget monthly commitments, actual-spend tracking, and the running
  balance/settlement records used to track who owes whom (the policy for
  *how* a given imbalance should be divided is a UX/product decision, out
  of scope — see Open Questions)
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
- `left_at` (nullable)

A household supports any number of members (starting usage will typically be
2, but the model does not assume exactly 2). This entity is given the same
`left_at` shape as `BudgetMembership`, for structural consistency, but what
removal actually *does* to the user's accounts/budgets/commitments is still
an open question (see below) — this only ensures the schema doesn't
foreclose answering it later.

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
difference between the two beyond the number of owners. Unlike
`BudgetMembership` (below), this is **not** versioned — it reflects only
current ownership. Ownership changes (e.g. converting a personal account to
joint) are expected to be rare/administrative, and visibility (see
Visibility rule 1) follows current ownership: it is not treated as a
consent-driven sharing action the way budget membership is, so it is not
covered by the "not retroactively revoked" principle. **Invariant:** an
`Account` must always retain at least one owner; removing its last
remaining `AccountOwnership` row is disallowed (ownership must be
transferred to someone else first, or the account archived — archival is
undecided, see Open Questions).

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
behaves like a shared one. Settlement math (see below) is only meaningful
for budgets with 2+ members — it's computed per-member using
`committed(user)`, which is `0` for any member who has never set a
`Commitment`. That's an ordinary case, not an error: a member with no
commitment simply has `balance(user) = actual(user)`, so their balance
tracks their spend one-for-one until they set a commitment. The app is
expected to prompt members to set one before relying on settlement figures,
but the data model doesn't require it.

### BudgetMembership
- `budget_id`
- `user_id`
- `joined_at`
- `left_at` (nullable)

Unlike `AccountOwnership`, this **is** versioned: leaving a budget sets
`left_at` rather than deleting the row, so past membership windows stay
queryable. This is what makes Visibility rule 4 well-defined (see below):
visibility granted by a `BudgetAssignment` depends on membership *at the
time the assignment was created*, not on live membership. A user may have
more than one `BudgetMembership` row for the same budget over time (leave,
then rejoin later): each row is its own non-overlapping `[joined_at,
left_at)` window, and "was a member at time T" means T falls inside any one
of them — there's no assumption of a single lifetime window per
`(budget_id, user_id)` pair.

### Commitment
- `id`
- `budget_id`
- `user_id`
- `amount`
- `effective_from` (month)
- `created_at`
- `created_by`

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
- `envelope_id` (nullable; if set, must belong to the same `budget_id` —
  enforced structurally via a composite reference to `(Envelope.id,
  Envelope.budget_id)` rather than by prose alone)
- `amount`
- `created_at` (used by Visibility rule 1 to determine budget membership
  as of the moment this assignment was created)

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
summing to anywhere from $0 to −$50, but never past −$50. All month-scoped
aggregates in this document (`actual(user)`, `total_spend(month)`, and
envelope `spend(m)`) key off the assignment's `Transaction.date`, not
`BudgetAssignment.created_at`.

If a `Transaction` is deleted, its `BudgetAssignment` rows are deleted with
it (cascade). Editing a transaction in a way that would violate the
invariant above for any of its existing assignments — shrinking `amount`
below the current assignment sum, or flipping its sign while assignments
exist — is rejected; the assignments must be adjusted first.

### Settlement
- `id`
- `budget_id`
- `month`
- `type` (`accept` | `transfer`)
- `from_user_id` (the debtor — the member whose negative `balance` this row
  resolves)
- `to_user_id` (the creditor — the member whose positive `balance` this row
  resolves)
- `amount`
- `created_at`
- `created_by`

Records how an outstanding balance between exactly two members of a budget
was resolved for a given month, moving both `balance(from_user_id)` and
`balance(to_user_id)` toward `0` by `amount`. Both `from_user_id` and
`to_user_id` are always set, for both types — the only difference is
whether money actually moved: `transfer` means `from_user_id` paid
`to_user_id` that amount; `accept` means `to_user_id` (the creditor) agreed
to forgive it, with no money moving. For a budget with more than two
members, fully resolving a month's imbalances may require multiple
`Settlement` rows, each still tying exactly one debtor to one creditor.

## Visibility rules

1. A user can see a `Transaction` if, and only if:
   - they currently own the transaction's `Account` (via `AccountOwnership`
     — this check uses live ownership, since it is unversioned), **or**
   - the transaction has at least one `BudgetAssignment` to a `Budget` of
     which the user was a member **at the time that `BudgetAssignment` was
     created** — i.e. some `BudgetMembership` row for that user/budget has
     `joined_at <= assignment.created_at` and (`left_at` is null or
     `assignment.created_at < left_at`).
2. Visibility is all-or-nothing at the transaction level: once visible, the
   full transaction (merchant, full amount, date, account) is shown — not
   just the assigned slice. (Default behavior; a per-assignment "share only
   the amount" option is a possible future refinement, not in this design.)
3. A user may create a `BudgetAssignment` only for transactions on accounts
   they own, and only targeting budgets they are themselves a member of.
   This prevents a user from granting visibility into someone else's
   transaction, or assigning into a budget they don't belong to.
4. **Assumption:** budget-membership-driven visibility is not retroactively
   revoked — rule 1's second clause checks membership as of the
   assignment's creation time, not live membership, so a member who later
   leaves a budget (`left_at` set) keeps seeing transactions already shared
   with them; only new assignments are affected. This principle is
   specific to `BudgetMembership`; it does not extend to `AccountOwnership`
   (see that entity's note above).

## Settlement & balance calculation

Balances are computed on read (queried/aggregated), not precomputed or
event-sourced — household transaction volume is small enough that this is
never a performance concern, and it keeps the model simple to reason about
and audit.

For a given `Budget`, single-month figures:

- `committed(user, m)` = that user's applicable `Commitment.amount` for
  month `m` (see versioning rule above).
- `actual(user, m)` = sum of `BudgetAssignment.amount` for that budget in
  month `m`, restricted to transactions whose account is **solely owned**
  by that user. Joint-account transactions assigned to the budget are
  excluded from this per-user total — that money is already shared/pooled,
  so it isn't attributed to either individual for settlement purposes.
- `monthly_delta(user, m)` = `actual(user, m) − committed(user, m)`.
  Positive means the user paid more that month than they'd committed to;
  negative means they paid less.
- `total_spend(m)` = sum of **all** `BudgetAssignment.amount` for the
  budget in month `m`, regardless of which account (personal or joint) the
  transaction came from — unlike `actual(user, m)`, not restricted to
  solely-owned accounts.
- `total_variance(m)` = `total_spend(m) − sum(committed(user, m))`, summed
  over every user who has ever had a `BudgetMembership` row for this budget
  (current or past — `committed` defaults to `0` for anyone without an
  applicable `Commitment`, so including former members is always safe, and
  this stays symmetric with `total_spend`, which likewise counts all
  assignments regardless of current membership). This is deliberately not
  the same as `sum(monthly_delta(user, m))`, which excludes joint-account
  spend; `total_variance` is the only figure that includes it.

The figure that actually answers "who owes whom right now" is cumulative,
**not** the single-month `monthly_delta` — it carries forward until
settled, the same way `Envelope.running_balance` does:

- `balance(user, M)` = `sum over every month m <= M of monthly_delta(user, m)`,
  **plus** `amount` for every `Settlement` (this budget, `month <= M`)
  where `user = from_user_id`, **minus** `amount` for every `Settlement`
  where `user = to_user_id`. (A settlement's own `month` marks which
  month's running balance it's intended to true up, but its effect is
  permanent from that point forward — it's netted into every later `M`
  too, not just that one month.)
- `Settlement.amount` isn't constrained by the data model to match the
  computed `balance` at recording time — the app layer is expected to
  suggest the computed figure, but recording a different amount is
  structurally legal and simply changes the running balance accordingly.
- **Important consequence:** a `transfer` or `accept` always moves
  `balance(from_user_id, M)` up by `amount` and `balance(to_user_id, M)`
  down by the same `amount` — it redistributes between the two named
  members but never changes their *sum*. Summed across a budget's members,
  `balance` cannot be brought to zero by settlement alone if cumulative
  `total_variance` is nonzero: settling only moves the household's
  overspend/underspend around between members, it doesn't erase it. That
  can only shrink over time by future months running the other way, or by
  raising commitments — the data model doesn't otherwise resolve it.

### Example (from the design conversation)

Household budget, commitments: you $1000/month, partner $1200/month
(total committed $2200). This is the first month this budget has any
history, so the cumulative and single-month figures coincide. Actual spend
this month: you $1450 (personal-account purchases assigned to Household),
partner $1000 — no joint-account spend was assigned to the budget this
month, so `total_spend` equals the sum of personal spend here; if it had,
`total_variance` would include it while `monthly_delta` still would not.

- `monthly_delta(you)` = 1450 − 1000 = **+450**
- `monthly_delta(partner)` = 1000 − 1200 = **−200**
- `total_spend` = 1450 + 1000 = **2450**
- `total_variance` = 2450 − 2200 = **+250** (household overspent by $250
  this month)
- With no prior history, `balance(you) = +450` and `balance(partner) = −200`
  going into settlement.

Say you record a `transfer` (`from_user_id`: partner, `to_user_id`: you,
`amount`: $200 — partner pays you). That brings `balance(partner)` to
`−200 + 200 = 0`, and `balance(you)` to `450 − 200 = 250` — **not** zero:
partner's shortfall against *their own* commitment is now fully covered,
but your $250 remaining balance is exactly the household's `total_variance`
— spend that exceeded the *combined* commitment, which a two-party transfer
structurally cannot erase (see "Important consequence" above). With
partner's balance already at `0`, there's no debtor left to record an
`accept` against, either — `Settlement` always requires a genuine
debtor/creditor pair. In practice your $250 simply carries forward as your
`balance` until a future month's underspend offsets it, or you two
renegotiate commitments upward; the data model doesn't provide a way for
one member to unilaterally write off their own residual balance.

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
- `running_balance(M)` = sum, over every month `m <= M`, of
  `allocation(m) − spend(m)`. Both terms are `0` for any month before the
  envelope has an `EnvelopeAllocation` or tagged `BudgetAssignment`, so in
  practice the sum only has as many nonzero terms as the envelope has
  actually been used for — no separate "start month" needs to be tracked.

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
  or any budget member?), and who may create a `Commitment` or
  `EnvelopeAllocation` (any budget member, or is it restricted?), are not
  yet decided — flagged for the implementation plan.
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
  (`HouseholdMembership.left_at` set) — their solely-owned accounts, budget
  memberships, and commitments would become orphaned — is explicitly
  deferred. Not addressed in v1; worth its own design pass if/when the app
  needs to support it.
