# so-budget — Design Spec

Date: 2026-09-11
Status: approved

## 1. Purpose

A household budgeting app for two people who each hold personal chequing, savings,
and credit card accounts at TD Canada Trust and have no joint account. Today the
household ledger is a Google Sheet filled by copying transactions from the TD app
by hand, which is tedious, error-prone, and easy to forget.

Goals, in priority order:

1. Know who spends what on the household, to keep contributions balanced.
   Balanced is defined by the couple, not as equal.
2. Know where household money goes.
3. Know the month's margin of manoeuvre given agreed contributions and
   already-planned expenses.
4. Reporting beyond that is left open.

Hard constraint: **each person's raw bank transactions are private**. Only
transactions a person explicitly flags as household become visible to the other.

## 2. Decisions taken

| Topic | Decision |
|---|---|
| Bank data source | SimpleFIN Bridge. Two Bridge accounts, one per person ($15/yr each). Fallback to TD CSV export if TD is unsupported; only the sync adapter changes. |
| Product shape | Hosted web app replaces the sheet. Sheet history imported once. |
| Privacy model | Per-user encryption at rest, key unwrapped at login and held in session memory only. Sync only runs while the owner is logged in. |
| Accepted limits | An operator who alters the running server or dumps process memory during a session could read private data. Client-side crypto was rejected as incompatible with server-side sync. Lost password plus lost recovery code means private data is unrecoverable; household data survives. |
| Partial amounts | Supported: a flagged transaction may contribute less than its full amount. |
| Contributions | Fixed monthly amount per person, stored per month. |
| Planned expenses | Count as spent from the 1st, matched to real transactions when they appear; unmatched at month end are reported as "did not occur" and excluded from actuals. |
| Stack | Single Go binary, SQLite, server-rendered HTML with htmx, mobile-friendly. Strict layering so a JSON API and dedicated frontend can be added later without touching business logic. |
| Pending transactions | Skipped during sync. |
| First pull | Full 90 days (SimpleFIN maximum); optional start-date override. |

## 3. Architecture

One Go module, one binary. Three layers, dependencies point inward only.

- **`domain`** — pure types and rules: `Transaction`, `HouseholdEntry`, `Rule`,
  `PlannedExpense`, `Contribution`, `Category`, month arithmetic,
  planned-expense matching, transfer detection, rule evaluation. No I/O, no
  crypto, no HTTP. Fully unit-tested.
- **`service`** — one use case per user action (`LinkSimpleFIN`, `Sync`,
  `ListInbox`, `FlagTransaction`, `FlagPartial`, `DismissTransaction`,
  `UnflagTransaction`, `CreateRule`, `MonthReport`, `ImportCSV`, …). Depends on
  storage and bank-client interfaces. Owns encryption of private records. Inputs
  and outputs are plain structs; no HTTP or template types leak in or out. This
  is the boundary a future API or frontend calls.
- **`adapters`** — replaceable edges:
  - `sqlite`: storage implementation, embedded SQL migrations.
  - `simplefin`: HTTP client for claim and `/accounts`.
  - `web`: handlers, sessions, embedded `html/template` views, htmx
    interactions, static CSS.
  - future `api`: JSON handlers over the same service, added without changing
    `web`.

The `web` adapter owns sessions and holds each session's unwrapped data key in
memory. It passes the key into the service on every call, so the service is
stateless and the same call shape works for a bearer-token API later.

## 4. Data model

### 4.1 Private tables (encrypted per user)

Each row: `user_id`, a plain lookup column where needed, and one `blob` holding
the record as JSON encrypted with AES-256-GCM under the user's data key. The
table name and row id are supplied as additional authenticated data so a blob
cannot be moved to another row.

| Table | Plain columns | Encrypted content |
|---|---|---|
| `credentials` | `user_id` | SimpleFIN access URL |
| `bank_accounts` | `user_id`, `id_hash` | SimpleFIN account id, display name, institution, currency, balance, balance date |
| `transactions` | `user_id`, `id_hash`, `state` (`new` / `flagged` / `dismissed`), `posted_month` | SimpleFIN transaction id, account, posted date, amount (signed cents), payee text, original amount if partially flagged, suggested category, suggested household flag, linked household entry id |
| `private_rules` | `user_id` | payee pattern → dismiss |
| `sync_state` | `user_id`, `last_success_at` | — |

`id_hash` = HMAC-SHA256 over the SimpleFIN id keyed by a per-user secret that is
itself stored inside the user's encrypted key material, combined with a
server-side pepper. It allows duplicate detection without decryption.
`state` and `posted_month` are plain so inbox counts and month filters work
before decryption; they reveal nothing about content.

### 4.2 Household tables (plain, shared)

| Table | Columns |
|---|---|
| `users` | id, name, wrapped data key (password), wrapped data key (recovery), Argon2id parameters and salts, created_at |
| `sessions` | id, user_id, created_at, last_seen_at (data key is in memory only, never here) |
| `categories` | id, name, active |
| `household_entries` | id, payer_user_id, date, amount (signed cents), category_id, note, source (`sync` / `import` / `manual`), source_transaction_ref (nullable, opaque), planned_expense_id (nullable), created_at, updated_at |
| `household_rules` | id, payee pattern, category_id, suggest_household (bool) |
| `planned_expenses` | id, name, amount, category_id, payer_user_id, day_of_month, recurring (bool), single_month (nullable), active |
| `contributions` | user_id, month, amount |

`source_transaction_ref` lets the owner's inbox be updated when a synced entry is
deleted. It is an opaque id and reveals nothing to the other user.

### 4.3 Keys

- Per user: random 256-bit data key, generated at signup.
- Wrapped twice: with a key derived from the password via Argon2id, and with a
  key derived from a one-time recovery code shown at signup.
- Login: derive from password, unwrap, keep in session memory.
- Password change: re-wrap only; data untouched.
- Recovery: unwrap with recovery code, set new password, re-wrap, issue a new
  recovery code.

## 5. Sync

**Linking.** User pastes a SimpleFIN setup token in settings. Service claims it
(one POST, single use), receives the access URL, stores it encrypted. Accounts
are listed and the user names each. All accounts under a token belong to that
user. An optional start date can be set to avoid overlap with imported history.

**Window.** First pull: 90 days back. Subsequent pulls:
`max(start_date, last_success_at − 7 days)` to now. Incoming transaction ids are
hashed and compared against `id_hash`; known ones are dropped without decryption.
Pending transactions are excluded (`pending=0`). Balances refresh every sync.

**Trigger.** On login; then at most once per hour while a session is active and a
page is loaded; a "sync now" button respects the same limit. No background job
ever touches private data.

**Failure.** The SimpleFIN `errors` list is shown as an inbox banner linking to
the SimpleFIN dashboard; it does not abort the sync. Network or parse failure
leaves `last_success_at` untouched. A sync commits as one database transaction.

**Amounts.** Signed integer cents, negative for money out, in the account
currency. Refunds and deposits appear in the inbox; a flagged refund becomes a
negative household entry.

## 6. Rules

Rules run at sync on each new transaction and produce a **suggestion**, never a
final state, except built-in dismissals. Three layers, evaluated in order, first
match wins; within a layer, the longest matching pattern wins. Patterns are
case-insensitive substring matches on payee text.

1. **Built-in dismiss** → state `dismissed` directly. Credit card payments
   (TD payee patterns) and transfers between the user's own accounts (equal
   amount, opposite sign, two of the user's accounts, within three days).
2. **Private rules** (encrypted, per user): payee pattern → dismiss. Created
   from the inbox via "always dismiss this payee".
3. **Household rules** (shared): payee pattern → category, plus a
   "suggest household" flag. Created from the inbox via "always suggest this
   for this payee". Either user may edit them.

Unmatched transactions arrive with no suggestion and sort first in the inbox. A
"re-apply rules" action re-evaluates all `new` transactions.

## 7. Inbox and ledger

**Inbox** (private, landing page). Lists `new` transactions, unsuggested first,
then date descending. Per-row actions via htmx:

- **Flag**: creates a household entry for the full amount with the suggested or
  chosen category and optional note; transaction → `flagged`.
- **Flag partial**: inline amount field; entry gets the partial amount, the
  transaction keeps its original.
- **Dismiss**: transaction → `dismissed`, no entry.
- "Always…" shortcuts create rules.

Top bar: "accept all suggestions" (skips suggestions without a category),
"sync now", last sync time, SimpleFIN error banner if any.

**Private history** (private). All of the user's transactions with state,
filter by account and month, search by payee. Flag a dismissed transaction,
unflag a flagged one (deletes its entry), edit amount or category of a flagged
one (edits its entry).

**Household ledger** (shared). Entries by month with payer, date, category,
amount, note, source marker. Either user may edit category, note, or amount of
any entry. Editing the amount of a synced entry does not touch the source
transaction. Deleting a synced entry resets its source transaction to `new` in
the owner's inbox.

**Manual entries**: payer, date, amount, category, note; source `manual`.

**CSV import** (settings, one-time): columns payer, date, payee, amount,
description, type. Preview, map `type` values to categories, then commit as
entries with source `import`.

## 8. Planning

**Contributions.** Per user, per month. Setting a new amount applies from a
chosen month forward; earlier months are never rewritten. Either user may edit
both.

**Planned expenses.** Name, expected amount, category, expected payer, day of
month, recurring or single month, active flag.

**Matching.** On flagging, look for an active unmatched planned expense in the
same month with the same payer and category whose amount is within 10% or $5,
whichever is larger. Exactly one match → link automatically. Otherwise the entry
offers a "match to planned" picker. Matches can be undone. Matching never alters
the planned amount.

**Month arithmetic** (in `domain`):

```
committed  = Σ household entries this month
           + Σ active planned expenses this month still unmatched
margin     = Σ contributions this month − committed

per person:
  spent     = Σ their household entries this month
  committed = spent + Σ their unmatched planned expenses this month
  balance   = their contribution − committed
```

**Month close.** Implicit. After a month ends, its unmatched planned expenses are
reported as "did not occur" and excluded from that month's actuals. A planned
expense unmatched for two consecutive closed months gets a "consider
deactivating" badge on the planning page.

## 9. Reports

One page, month picker, current month by default. All numbers come from
`service.MonthReport`, which returns plain structs.

- **Margin card**: contributions, committed (split spent / still planned), margin.
- **Balance per person**: contribution, spent, committed, balance.
- **Spend by category**: this month beside previous month, table with bars;
  click-through to entries.
- **Planned vs actual**: each planned expense with expected and matched amount,
  or "did not occur".
- **Trend**: six months of total household spend and each person's share.
- **CSV export** of any month's ledger.

## 10. Testing

- **Domain**: table-driven unit tests for month arithmetic, planned matching and
  tolerances, transfer detection, rule precedence, partial amounts.
- **Crypto**: wrap/unwrap round trips for password and recovery code; wrong
  password fails; blob moved to another row fails; password change keeps data
  readable.
- **Service**: real SQLite in a temp file plus a fake SimpleFIN server serving
  fixtures. Covers first 90-day sync, overlap sync producing no duplicates,
  `errors` surfacing without abort, failed sync leaving state untouched,
  flag/unflag consistency, ledger delete returning the transaction to the inbox,
  CSV import mapping.
- **Privacy invariant**: log in as A, sync, then read the raw database and assert
  no payee text or amount from A's transactions appears in plain; assert B's
  session cannot reach any of A's private rows through any service call.
- **Web**: `httptest` for login, session expiry, and private routes rejecting
  requests without a session. Templates parsed at startup so a broken template
  fails the build.

Development is test-first; the implementation plan breaks work into units that
each start with a failing test.

## 11. Deployment and operations

- **Build**: `go build` → one static binary; templates and CSS via `embed`;
  pure-Go SQLite driver, no cgo.
- **Run**: Docker image, mounted volume for the database file. Environment:
  database path, listen address, session idle timeout, server-side pepper,
  development flag.
- **HTTPS**: reverse proxy (Caddy recommended). Secure cookies required unless
  the development flag is set.
- **Users**: no public signup. CLI subcommand creates the first user and prints
  an invitation link; the second user sets a password and receives a recovery
  code on first login.
- **Sessions**: idle timeout 30 min, absolute limit 12 h. Restart clears
  sessions and keys from memory.
- **Backups**: SQLite online backup to a second file on a schedule; copying it
  off-box is left to the operator. Private data is encrypted in the backup.
- **Migrations**: embedded SQL, applied at startup.
- **Local development**: same binary, development flag, fake SimpleFIN server
  from the test suite.

## 12. Out of scope for the first version

- Push notifications or any background sync.
- More than one household or more than two users.
- Client-side encryption.
- Native mobile apps (the layering keeps this possible).
- Multi-currency reporting beyond storing the account currency.
- Automatic flagging without review.
