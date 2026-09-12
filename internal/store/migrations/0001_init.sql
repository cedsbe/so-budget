CREATE TABLE users (
  id           INTEGER PRIMARY KEY,
  name         TEXT NOT NULL UNIQUE,
  invite_hash  TEXT,
  pw_salt      BLOB,
  pw_wrapped   BLOB,
  rc_salt      BLOB,
  rc_wrapped   BLOB,
  kdf_time     INTEGER,
  kdf_memory   INTEGER,
  kdf_threads  INTEGER,
  created_at   TEXT NOT NULL
);

CREATE TABLE credentials (
  user_id INTEGER PRIMARY KEY REFERENCES users(id),
  blob    BLOB NOT NULL
);

CREATE TABLE bank_accounts (
  user_id INTEGER NOT NULL REFERENCES users(id),
  id_hash TEXT NOT NULL,
  blob    BLOB NOT NULL,
  PRIMARY KEY (user_id, id_hash)
);

CREATE TABLE transactions (
  user_id      INTEGER NOT NULL REFERENCES users(id),
  id_hash      TEXT NOT NULL,
  state        TEXT NOT NULL CHECK (state IN ('new','flagged','dismissed')),
  posted_month TEXT NOT NULL,
  blob         BLOB NOT NULL,
  PRIMARY KEY (user_id, id_hash)
);
CREATE INDEX transactions_state ON transactions(user_id, state);
CREATE INDEX transactions_month ON transactions(user_id, posted_month);

CREATE TABLE private_rules (
  user_id INTEGER NOT NULL REFERENCES users(id),
  id      TEXT NOT NULL,
  blob    BLOB NOT NULL,
  PRIMARY KEY (user_id, id)
);

CREATE TABLE sync_state (
  user_id         INTEGER PRIMARY KEY REFERENCES users(id),
  last_success_at TEXT NOT NULL
);

CREATE TABLE categories (
  id     INTEGER PRIMARY KEY,
  name   TEXT NOT NULL UNIQUE,
  active INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE household_entries (
  id                 INTEGER PRIMARY KEY,
  payer_user_id      INTEGER NOT NULL REFERENCES users(id),
  date               TEXT NOT NULL,
  amount             INTEGER NOT NULL,
  category_id        INTEGER REFERENCES categories(id),
  note               TEXT NOT NULL DEFAULT '',
  source             TEXT NOT NULL CHECK (source IN ('sync','import','manual')),
  source_ref         TEXT,
  planned_expense_id INTEGER,
  created_at         TEXT NOT NULL,
  updated_at         TEXT NOT NULL
);
CREATE INDEX household_entries_date ON household_entries(date);
CREATE UNIQUE INDEX household_entries_source ON household_entries(payer_user_id, source_ref) WHERE source_ref IS NOT NULL;

CREATE TABLE household_rules (
  id                INTEGER PRIMARY KEY,
  pattern           TEXT NOT NULL,
  category_id       INTEGER NOT NULL REFERENCES categories(id),
  suggest_household INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE planned_expenses (
  id            INTEGER PRIMARY KEY,
  name          TEXT NOT NULL,
  amount        INTEGER NOT NULL,
  category_id   INTEGER NOT NULL REFERENCES categories(id),
  payer_user_id INTEGER NOT NULL REFERENCES users(id),
  day_of_month  INTEGER NOT NULL,
  recurring     INTEGER NOT NULL DEFAULT 1,
  single_month  TEXT,
  active        INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE contributions (
  user_id INTEGER NOT NULL REFERENCES users(id),
  month   TEXT NOT NULL,
  amount  INTEGER NOT NULL,
  PRIMARY KEY (user_id, month)
);
