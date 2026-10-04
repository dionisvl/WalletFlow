-- version 1
CREATE TABLE addresses (
    id         INTEGER PRIMARY KEY,
    family     TEXT    NOT NULL,          -- evm | tron
    address    TEXT    NOT NULL UNIQUE,   -- evm stored lowercase
    name       TEXT    NOT NULL DEFAULT '',
    kind       TEXT    NOT NULL,          -- mine | exchange | external
    color      TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);

CREATE TABLE assets (
    id       INTEGER PRIMARY KEY,
    chain    TEXT    NOT NULL,
    contract TEXT    NOT NULL DEFAULT '',  -- '' = native coin
    symbol   TEXT    NOT NULL,
    decimals INTEGER NOT NULL,
    is_spam  INTEGER NOT NULL DEFAULT 0,
    UNIQUE (chain, contract)
);

CREATE TABLE transfers (
    id         INTEGER PRIMARY KEY,
    uid        TEXT    NOT NULL UNIQUE,    -- chain:hash:stream:index
    chain      TEXT    NOT NULL,
    tx_hash    TEXT    NOT NULL,
    ts         INTEGER NOT NULL,
    from_addr  TEXT    NOT NULL,
    to_addr    TEXT    NOT NULL,
    asset_id   INTEGER NOT NULL REFERENCES assets (id),
    amount_raw TEXT    NOT NULL,
    fee_raw    TEXT    NOT NULL DEFAULT '0',
    class      TEXT    NOT NULL DEFAULT 'unknown',
    category   TEXT,                       -- NULL = not reviewed
    comment    TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX transfers_ts ON transfers (ts);
CREATE INDEX transfers_from ON transfers (from_addr);
CREATE INDEX transfers_to ON transfers (to_addr);
CREATE INDEX transfers_hash ON transfers (tx_hash);

CREATE TABLE rules (
    id           INTEGER PRIMARY KEY,
    counterparty TEXT NOT NULL,
    class        TEXT NOT NULL,            -- inflow | outflow
    category     TEXT NOT NULL,
    UNIQUE (counterparty, class)
);

CREATE TABLE sync_cursors (
    chain   TEXT    NOT NULL,
    address TEXT    NOT NULL,
    stream  TEXT    NOT NULL,              -- native | token | internal | trc20
    cursor  INTEGER NOT NULL,
    PRIMARY KEY (chain, address, stream)
);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
