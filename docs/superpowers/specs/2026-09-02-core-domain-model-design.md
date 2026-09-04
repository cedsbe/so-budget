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
calculated. It intentionally excludes tech stack, UI, and the detailed
bank-sync/import design — those are separate design efforts (see the
scope note on bank-sync below — it's confirmed needed for v1, just not
designed in this document).

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
  rollover balances and versioned allocation history, optionally organized
  into one level of envelope groups
- Marking a transaction as a self-transfer, excluded from budget
  assignment to prevent double-counting money moved between own accounts

Deferred (future design docs, not needed for v1):
- Technology/framework/storage choices
- UI/UX
- Notifications, multi-currency, multi-household membership for a user

Confirmed needed for v1, but not designed here — requires its own
dedicated design doc:
- Bank/card sync and transaction import. This directly affects one
  decision left open in this document: whether `Transaction.is_transfer`
  stays an independent per-transaction flag or gets upgraded to a linked
  `transfer_id` pair (see that field's note) — the right answer depends on
  how transactions actually get imported and matched, which isn't decided
  yet. Treat the current `is_transfer` design as provisional pending that
  work, not as a settled decision the way the rest of this document is.

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

The entity supports any number of members, but **for v1 the product
assumption is exactly 2 concurrent members per household** (this app's
first household is one couple). This isn't structurally enforced —
`HouseholdMembership` would technically allow more — it's a stated v1
scope decision, not a modeling limit: the entity is kept general because
generalizing later should mean relaxing an assumption, not restructuring
the schema. One concrete consequence: since a `Budget`'s members are drawn
from its household, no budget can have more than 2 members in v1 either,
which makes the "3+ member budget" contingencies described later in this
document (e.g. multiple `Settlement` rows per month) currently dormant —
built for when it's needed, not because v1 exercises it.

This entity is given the same `left_at` shape as `BudgetMembership`, for
structural consistency, but what removal actually *does* to the user's
accounts/budgets/commitments is still an open question (see below) — this
only ensures the schema doesn't foreclose answering it later.

### Account
- `id`
- `household_id`
- `name`
- `type` (checking, savings, credit card, ...)
- `currency`
- `deleted_at` (nullable — see "Soft-delete" note below)

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
- `is_transfer` (boolean, default `false`, mutable)

A transaction always belongs to exactly one account and inherits that
account's currency.

`is_transfer` marks a transaction as money moving between the user's own
accounts (e.g. a checking withdrawal that pays off a credit card) rather
than real spend — structurally, `BudgetAssignment` creation is disallowed
for a transaction with `is_transfer = true` (see Visibility rule 3a),
preventing self-transfers from being double-counted as household spend
once on the source account and again for the underlying purchases already
recorded on the destination account. This is deliberately a per-transaction
flag with no linkage between the two sides of a transfer (no attempt to
match "this checking withdrawal corresponds to that credit card payment").
**Provisional:** whether transfers need to be automatically paired (e.g. a
linked `transfer_id`, the way Actual Budget does it) instead of relying on
each side being independently marked depends on how bank-sync/import
actually works, which is confirmed needed for v1 but not yet designed —
see the Scope section. Until that design happens, each transaction is just
independently marked, with no cross-account matching. `is_transfer` can be
toggled at any time, except that setting it to `true` is rejected while
the transaction has any `BudgetAssignment` rows — those must be deleted
first, the same edit-rejection pattern used elsewhere in this document
rather than a silent cascade.

### Budget
- `id`
- `household_id`
- `name`
- `created_by` (user_id)
- `deleted_at` (nullable — see "Soft-delete" note below)

Budgets are arbitrary and user-defined (e.g. "Household", "Vacation",
"Kids") — there is no fixed "personal" vs. "shared" type. A budget with one
member behaves like a personal budget; a budget with two or more members
behaves like a shared one. Settlement math (see below) is only meaningful
for budgets with 2+ members — it's computed per-member using
`committed(user, m)`, which is `0` for any member who has never set a
`Commitment` applicable to month `m`. That's an ordinary case, not an
error: a member with no commitment simply has `monthly_delta(user, m) =
actual(user, m)` for every such month, so their running `balance` tracks
their spend one-for-one until they set a commitment. The app is expected
to prompt members to set one before relying on settlement figures, but the
data model doesn't require it.

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
of that month), `committed(user, m)` is `0`.

`amount` is always a fixed dollar figure. Percentage-of-spend commitments
(e.g. "I cover 40% of whatever we spend") were considered during design
and deliberately dropped: unlike a fixed amount, a percentage needs a base
to apply to, and "40% of `total_spend(m)`" would make a member's
commitment depend on the very spend it's meant to be compared against —
changing what `balance` even measures (proportional-share fairness, not
promise-vs-actual). Fixed amounts are sufficient for how this household
plans to use commitments; this may be revisited if that changes.

### EnvelopeGroup
- `id`
- `budget_id` (immutable once set, same reasoning as `Envelope.budget_id`
  below)
- `name`
- `deleted_at` (nullable — see "Soft-delete" note below)

A named organizational grouping of envelopes within a budget (e.g.
"Immediate Obligations" containing the Rent/Insurance/Internet envelopes),
one level deep — groups do not contain other groups. `EnvelopeGroup` plays
no role in any formula in this document (`allocation`, `spend`,
`running_balance` are all defined per-`Envelope`, group membership is
purely organizational); it exists only so a budget with many envelopes
doesn't present as one flat list.

### Envelope
- `id`
- `budget_id` (immutable once set — `BudgetAssignment.envelope_id`'s
  same-budget constraint is enforced via a composite reference to
  `(Envelope.id, Envelope.budget_id)`, which only holds if this can't
  change out from under existing assignments)
- `envelope_group_id` (nullable, mutable; if set, must belong to the same
  `budget_id` — same composite-reference pattern as `envelope_id` on
  `BudgetAssignment`)
- `name`
- `deleted_at` (nullable — see "Soft-delete" note below)

A named spending category nested within a budget (e.g. the "Household"
budget contains envelopes "Groceries", "Insurance", "Internet & Mobile").
Envelopes do not exist independently of a budget. Grouping is optional —
an envelope with no `envelope_group_id` is simply ungrouped (e.g. a
"To Review" triage envelope has no obvious group to sit in). Since
grouping has no effect on any balance calculation, moving an envelope
between groups (or clearing its group) is unrestricted.

### Soft-delete convention (Account, Budget, Envelope, EnvelopeGroup)

These four entities are never hard-deleted — "deleting" one sets
`deleted_at` to the current time rather than removing the row. This
resolves what was previously an open question for each of them in one
consistent stroke: every historical `Commitment`, `EnvelopeAllocation`,
`BudgetAssignment`, and `Settlement` row keeps referencing a real,
unchanged parent row, so nothing about past balances or visibility breaks
when an account/budget/envelope/group is retired. A soft-deleted row
simply stops appearing in active lists (e.g. as a target for new
`BudgetAssignment`/`Commitment`/`EnvelopeAllocation` rows) but remains
valid for anything that already references it. `AccountOwnership`'s
last-owner invariant applies to soft-deleting an `Account` the same way it
applies to removing its last owner. This convention doesn't extend to the
ledger entities themselves (`Commitment`, `EnvelopeAllocation`,
`BudgetAssignment`, `Settlement`) — those follow their own, already-stated
immutability/deletion rules.

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
- `transaction_id` (immutable once set)
- `budget_id` (immutable once set — see below)
- `envelope_id` (nullable, mutable; if set, must belong to the same
  `budget_id` — enforced structurally via a composite reference to
  `(Envelope.id, Envelope.budget_id)` rather than by prose alone)
- `amount` (mutable, subject to the invariant below; must be nonzero)
- `created_at` (immutable once set — used by Visibility rule 1 to
  determine budget membership as of the moment this assignment was
  created; if it could be edited after the fact, that visibility
  determination could be rewritten retroactively, which would undermine
  rule 4)
- `created_by` (immutable once set)

Links a transaction (or a portion of it) to a budget, optionally tagging
that portion with one envelope within the budget. A single transaction can
have multiple `BudgetAssignment` rows across different budgets (e.g. a
grocery run split between "Household" and a personal "Treats" budget), and
each row is independently taggable with at most one envelope. Finer
splitting across envelopes reuses this same mechanism — e.g. a $30 Sobeys
transaction becomes a $20 row (Household / Groceries) and a $10 row
(Household / Household Supplies) rather than requiring a separate
envelope-splitting feature. Assignment amounts must be nonzero and share
the transaction's sign, and the sum of their absolute values must not
exceed the absolute value of the transaction's amount — e.g. a −$50 refund
can have assignments summing to anywhere from $0 (i.e. no assignment rows
at all) to −$50, but never past −$50 — and no single row's `amount` may
itself be `0`. This invariant is checked symmetrically on either edit path:
editing a `Transaction.amount` below the current assignment sum (or
flipping its sign) is rejected, and so is editing a `BudgetAssignment.amount`
(or adding a new one) in a way that would push the sum past the
transaction's own amount, or to exactly `0` — whichever side changes, the
other side's existing values are what's checked against. Requiring a
nonzero amount matters beyond bookkeeping: Visibility rule 1 grants access
based on a `BudgetAssignment` merely *existing*, so a `0`-amount row would
otherwise be a permanent, functionally-empty grant of full transaction
visibility with nothing actually assigned — banning it means "unsharing" a
transaction from a budget always means deleting the row, which is also the
only thing that can end the visibility that row granted (see Visibility
rule 4a below). All month-scoped aggregates in this document
(`actual(user, m)`, `total_spend(m)`, and envelope `spend(m)`) key off the
assignment's `Transaction.date`, not `BudgetAssignment.created_at`.

Moving an assignment to a *different* `budget_id` is not supported as an
in-place edit — `budget_id` is immutable for the same reason `created_at`
is: changing it would retroactively alter who could see the transaction
under Visibility rule 1. To reassign a transaction's portion to a different
budget, delete the row and create a new one (a fresh `BudgetAssignment`
with its own `created_at`, evaluated against membership at that new time).
`envelope_id`, by contrast, is a plain re-taggable field with no visibility
implications — since balances are computed on read from current data
(this doc's chosen approach, see below), correcting a mis-tagged envelope
is expected to retroactively update that envelope's past `spend(m)` and
`running_balance`, the same way fixing a data-entry mistake should.

If a `Transaction` is deleted, its `BudgetAssignment` rows are deleted with
it (cascade).

### Settlement
- `id` (all fields immutable once set — see below)
- `budget_id`
- `month`
- `type` (`accept` | `transfer`)
- `from_user_id` (the debtor — the member whose negative `balance` this row
  resolves; must currently or previously have held `BudgetMembership` for
  `budget_id`, and must differ from `to_user_id` — no self-settlement)
- `to_user_id` (the creditor — the member whose positive `balance` this row
  resolves; same membership constraint as `from_user_id`)
- `amount`
- `created_at`
- `created_by`

Records how an outstanding balance between exactly two members of a budget
was resolved for a given month, moving both `balance(from_user_id, M)` and
`balance(to_user_id, M)` toward `0` by `amount` for every `M >= month`. Both
`from_user_id` and `to_user_id` are always set, for both types — the only
difference is whether money actually moved: `transfer` means `from_user_id`
paid `to_user_id` that amount; `accept` means `to_user_id` (the creditor)
agreed to forgive it, with no money moving. For a budget with more than two
members, fully resolving a month's imbalances may require multiple
`Settlement` rows, each still tying exactly one debtor to one creditor.

Like `Commitment` and `EnvelopeAllocation`, a `Settlement` row is
**immutable and undeletable** once created — it isn't versioned the same
way (there's no "latest applicable row" lookup for it), but for the same
underlying reason: `balance(user, M)` sums every `Settlement` with
`month <= M`, so a silently-editable `amount`/`from_user_id`/`to_user_id`,
or a deleted row, would retroactively rewrite every later `balance`
figure. To correct a mistaken settlement, record a new offsetting
`Settlement` rather than editing or deleting the original.

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
3a. A user may not create a `BudgetAssignment` for a `Transaction` with
    `is_transfer = true` (see that field's note). This is a separate,
    unconditional restriction from rule 3 — it applies even to the
    transaction's own owner, on any budget — since a self-transfer isn't
    real spend for any budget to track.
4. **Assumption:** budget-membership-driven visibility is not retroactively
   revoked — rule 1's second clause checks membership as of the
   assignment's creation time, not live membership, so a member who later
   leaves a budget (`left_at` set) keeps seeing transactions already shared
   with them; only new assignments are affected. This principle is
   specific to `BudgetMembership`; it does not extend to `AccountOwnership`
   (see that entity's note above).
4a. This non-retroactive principle is about the *recipient's* membership
    changing — it says nothing about the *sharer* changing their mind.
    Deleting a `BudgetAssignment` (the only way to undo one, since a
    `0`-amount row is disallowed — see that entity's note) removes it from
    rule 1's existence check going forward: if it was the transaction's
    only qualifying assignment to that budget, visibility for that budget's
    members lapses. This is a deliberate, sender-initiated unsharing path,
    distinct from — and not in tension with — rule 4's recipient-side
    guarantee.

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
  too, not just that one month.) As with `running_balance` above, both
  terms of `monthly_delta` are `0` for any month before the user has an
  applicable `Commitment` or a solely-owned assignment on this budget, so
  the sum has no separate "start month" to track — only months with actual
  activity contribute.
- `Settlement.amount` isn't constrained by the data model to match the
  computed `balance` at recording time — the app layer is expected to
  suggest the computed figure, but recording a different amount is
  structurally legal and simply changes the running balance accordingly.
- **Important consequence:** a `transfer` or `accept` always moves
  `balance(from_user_id, M)` up by `amount` and `balance(to_user_id, M)`
  down by the same `amount`. Every other member's balance is untouched, so
  this redistributes without changing the *sum* of all members' balances —
  true for any number of members, not just two. That sum, cumulatively, is
  `sum over members of sum over m<=M of monthly_delta(user, m)` — **not**
  cumulative `total_variance`. The two are different quantities:
  `total_variance` also includes joint-account spend (via `total_spend`),
  which `monthly_delta` excludes by construction; they coincide only in
  periods with zero joint-account spend assigned to the budget. So the
  residual a household can never eliminate through settlement alone is the
  cumulative sum of members' `monthly_delta`, not `total_variance` — it can
  only shrink via future months running the other way, or by raising
  commitments.

### Example (from the design conversation)

Household budget, commitments: you $1000/month, partner $1200/month
(total committed $2200). This is the first month this budget has any
history, so the cumulative and single-month figures coincide. Actual spend
this month (call it `M0`): you $1450 (personal-account purchases assigned
to Household), partner $1000 — no joint-account spend was assigned to the
budget this month, so `total_spend(M0)` equals the sum of personal spend
here; if it had, `total_variance(M0)` would include it while
`monthly_delta(user, M0)` still would not.

- `monthly_delta(you, M0)` = 1450 − 1000 = **+450**
- `monthly_delta(partner, M0)` = 1000 − 1200 = **−200**
- `total_spend(M0)` = 1450 + 1000 = **2450**
- `total_variance(M0)` = 2450 − 2200 = **+250** (household overspent by
  $250 this month)
- With no prior history, `balance(you, M0) = +450` and
  `balance(partner, M0) = −200` going into settlement.

Say you record a `transfer` (`from_user_id`: partner, `to_user_id`: you,
`amount`: $200) with `month = M0` (partner pays you). That brings
`balance(partner, M0)` to `−200 + 200 = 0`, and `balance(you, M0)` to
`450 − 200 = 250` — **not** zero: partner's shortfall against *their own*
commitment is now fully covered, but your $250 remaining balance is the
sum of both members' `monthly_delta(user, M0)` (`450 + (−200) = 250`),
which a two-party transfer structurally cannot erase (see "Important
consequence" above). It happens to equal `total_variance(M0)` ($250) too
in this example only because no joint-account spend was assigned to the
budget that month — in general the two are different figures (see
"Important consequence"). With partner's balance already at `0`, there's
no debtor left to record an `accept` against, either — `Settlement` always
requires a genuine debtor/creditor pair. In practice your $250 simply
carries forward as `balance(you, M)` for every later `M` until a future
month's underspend offsets it, or you two renegotiate commitments upward;
the data model doesn't provide a way for one member to unilaterally write
off their own residual balance.

## Envelope tracking (rollover budgeting)

Envelopes track spend against a category allocation the way YNAB does:
unspent money rolls forward as available balance, and overspending leaves
the envelope negative until future allocations (or a manual fix) cover it.
This is independent of, and not reconciled against, the budget's own
per-member `Commitment` totals — deliberately so, not an oversight: an
envelope answers "did we spend what we planned in this category," a
household-level question with no owner, since who actually makes a given
Groceries or Insurance purchase varies transaction to transaction.
`Commitment`/`Settlement` answer a different question — "who fronted the
money, and are we square" — which is inherently per-member. Requiring the
two to reconcile (YNAB's "give every dollar a job") would conflate them;
keeping them decoupled lets "we're on track for Groceries this month" and
"you owe me $80" be tracked, and read, independently.

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

### Pattern: a "To Review" triage envelope

No schema addition is needed to support an inbox-style workflow for
transactions that need joint attention before being properly categorized —
it falls directly out of the primitives already defined:

- A joint-account transaction is already visible to both owners the moment
  it exists (Visibility rule 1's ownership clause) — no `BudgetAssignment`
  is needed for either of them to see it.
- Either owner can provisionally assign it to the shared budget under an
  ordinary, household-chosen envelope named e.g. "To Review" — this is
  just a normal `Envelope` row, not a special system category.
- Because `envelope_id` is mutable and `amount` can be edited or split
  into additional rows (subject to the usual invariants), re-categorizing
  out of "To Review" into Groceries, Insurance, etc. — or splitting it
  across several — is already fully supported without further design.
- This pattern is expected to matter mainly for joint-account
  transactions. Personal-account transactions are self-assigned directly
  by their owner, with no shared review step — visibility to the other
  member only happens once (and because) they've chosen a real budget/
  envelope for it, not an interim "needs review" state.

What actually populates "To Review" for a new joint-account transaction
(manual entry vs. future automatic bank-sync import) is outside this
document's scope — see the bank-sync/import design mentioned under Scope.

## Open questions / explicit assumptions

- Single currency per household is assumed for v1. This is a convention,
  not an enforced constraint: `currency` lives on `Account`, nothing
  requires all of a household's accounts to share one, and every aggregate
  in this doc (`actual`, `total_spend`, `total_variance`, envelope
  `spend(m)`) sums raw amounts with no currency check. Mixing currencies
  within a household would silently produce meaningless totals rather than
  an error. Acceptable for v1; real multi-currency support is out of scope.
- Who may edit/delete a `BudgetAssignment` after creation (only its
  `created_by`, or any budget member?), who may create a `Commitment` or
  `EnvelopeAllocation` (any budget member, or is it restricted?), and what
  special rights (if any) a `Budget`'s `created_by` has over other members
  — e.g. sole authority to delete the budget or change its settlement
  rules — are not yet decided — flagged for the implementation plan. (The
  `created_by` field needed to support a "creator-only" policy is present
  on `BudgetAssignment`, `Commitment`, `EnvelopeAllocation`, and `Budget`
  itself; only the policy is undecided.)
- Who may add or remove `BudgetMembership` rows at all (invite/remove
  members from a budget) is not addressed anywhere in this document — a
  more basic gap than the edit-policy questions above, since without it
  there's no stated way a budget gains its second member in the first
  place.
- `AccountOwnership.user_id` and `BudgetMembership.user_id` are not
  structurally required to hold a `HouseholdMembership` (current or past)
  in the household that owns the `Account`/`Budget` — nothing prevents a
  user outside the household from owning an account or belonging to a
  budget inside it. Same class of gap as the currency assumption above:
  a convention the data model doesn't enforce, acceptable for v1.
- Settlement UX (how a $250 combined-overspend gets divided into individual
  `accept`/`transfer` actions) is a product/UI decision, not a data-model
  one; this doc only defines the record types involved.
- What happens when a user is removed from a `Household`
  (`HouseholdMembership.left_at` set) — their solely-owned accounts, budget
  memberships, and commitments would become orphaned — is explicitly
  deferred. Not addressed in v1; worth its own design pass if/when the app
  needs to support it.
