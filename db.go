package main

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaV1 string

// migrations[i] brings the schema to user_version i+1.
var migrations = []string{schemaV1}

type Store struct {
	db *sql.DB
}

func openStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	// One connection keeps SQLite locking trivial. Always drain rows before the next query.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return err
	}
	for i := v; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// --- settings ---

func (s *Store) Setting(ctx context.Context, key, def string) string {
	var v string
	if err := s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&v); err != nil {
		return def
	}
	return v
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
		key, value)
	return err
}

// --- addresses ---

type Address struct {
	ID      int64
	Family  string
	Address string
	Name    string
	Kind    Kind
	Color   string
}

func (a Address) Label() string {
	if a.Name != "" {
		return a.Name
	}
	return shortAddr(a.Address)
}

func (s *Store) Addresses(ctx context.Context) ([]Address, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, family, address, name, kind, color FROM addresses ORDER BY kind, name, address")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Address
	for rows.Next() {
		var a Address
		if err := rows.Scan(&a.ID, &a.Family, &a.Address, &a.Name, &a.Kind, &a.Color); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) AddressBook(ctx context.Context) (map[string]Address, error) {
	list, err := s.Addresses(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[string]Address, len(list))
	for _, a := range list {
		m[a.Address] = a
	}
	return m, nil
}

// UpsertAddress adds an address or updates name/kind/color of an existing one.
func (s *Store) UpsertAddress(ctx context.Context, a Address) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO addresses (family, address, name, kind, color, created_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (address) DO UPDATE SET name = excluded.name, kind = excluded.kind, color = excluded.color`,
		a.Family, a.Address, a.Name, a.Kind, a.Color, time.Now().Unix())
	return err
}

func (s *Store) UpdateAddress(ctx context.Context, a Address) error {
	_, err := s.db.ExecContext(ctx, "UPDATE addresses SET name = ?, kind = ?, color = ? WHERE id = ?",
		a.Name, a.Kind, a.Color, a.ID)
	return err
}

func (s *Store) DeleteAddress(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM addresses WHERE id = ?", id)
	return err
}

// --- assets ---

type Asset struct {
	ID       int64
	Chain    string
	Contract string
	Symbol   string
	Decimals int
	IsSpam   bool
}

// AssetID returns the id of an asset, creating it on first sight.
func (s *Store) AssetID(ctx context.Context, a Asset) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO assets (chain, contract, symbol, decimals) VALUES (?, ?, ?, ?)
		ON CONFLICT (chain, contract) DO UPDATE SET symbol = symbol
		RETURNING id`, a.Chain, a.Contract, a.Symbol, a.Decimals).Scan(&id)
	return id, err
}

func (s *Store) Assets(ctx context.Context) ([]Asset, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.chain, a.contract, a.symbol, a.decimals, a.is_spam FROM assets a
		WHERE EXISTS (SELECT 1 FROM transfers t WHERE t.asset_id = a.id)
		ORDER BY a.is_spam, a.symbol, a.chain`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Asset
	for rows.Next() {
		var a Asset
		if err := rows.Scan(&a.ID, &a.Chain, &a.Contract, &a.Symbol, &a.Decimals, &a.IsSpam); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) SetAssetSpam(ctx context.Context, id int64, spam bool) error {
	_, err := s.db.ExecContext(ctx, "UPDATE assets SET is_spam = ? WHERE id = ?", spam, id)
	return err
}

// --- transfers ---

type Transfer struct {
	ID        int64
	UID       string
	Chain     string
	TxHash    string
	TS        int64
	From      string
	To        string
	AssetID   int64
	AmountRaw string
	FeeRaw    string
	Class     Class
	Category  string
	Comment   string

	// Joined from assets.
	Symbol   string
	Decimals int
	IsSpam   bool
}

func (t Transfer) Time() time.Time { return time.Unix(t.TS, 0).UTC() }
func (t Transfer) Amount() string  { return formatAmount(t.AmountRaw, t.Decimals) }

// Counterparty is the side that is not the owner.
func (t Transfer) Counterparty() string { return counterparty(t.Class, t.From, t.To) }

// Incoming reports whether funds came to the owner.
func (t Transfer) Incoming() bool {
	return t.Class == ClassInflow || t.Class == ClassCEXWithdrawal
}

// InsertTransfers stores new transfers and skips ones already known.
func (s *Store) InsertTransfers(ctx context.Context, ts []Transfer) (int, error) {
	if len(ts) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO transfers (uid, chain, tx_hash, ts, from_addr, to_addr, asset_id, amount_raw, fee_raw)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (uid) DO NOTHING`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	added := 0
	for _, t := range ts {
		res, err := stmt.ExecContext(ctx, t.UID, t.Chain, t.TxHash, t.TS, t.From, t.To, t.AssetID, t.AmountRaw, t.FeeRaw)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		added += int(n)
	}
	return added, tx.Commit()
}

type Filter struct {
	From, To      int64 // unix, 0 = open
	Chain         string
	AssetID       int64
	Class         string
	Addresses     []string // either side
	Counterparty  string
	Category      string
	Uncategorized bool
	Inbox         bool
	HideSpam      bool
	HideZero      bool
	Limit, Offset int
}

func (f Filter) where() (string, []any) {
	var conds []string
	var args []any
	add := func(c string, a ...any) { conds = append(conds, c); args = append(args, a...) }
	if f.From > 0 {
		add("t.ts >= ?", f.From)
	}
	if f.To > 0 {
		add("t.ts < ?", f.To)
	}
	if f.Chain != "" {
		add("t.chain = ?", f.Chain)
	}
	if f.AssetID > 0 {
		add("t.asset_id = ?", f.AssetID)
	}
	if f.Class != "" {
		add("t.class = ?", f.Class)
	}
	if len(f.Addresses) > 0 {
		in := strings.Repeat(",?", len(f.Addresses))[1:]
		var a []any
		for _, x := range f.Addresses {
			a = append(a, x)
		}
		add("(t.from_addr IN ("+in+") OR t.to_addr IN ("+in+"))", append(a, a...)...)
	}
	if f.Counterparty != "" {
		add(`((t.class IN ('inflow', 'cex_withdrawal') AND t.from_addr = ?)
			OR (t.class NOT IN ('inflow', 'cex_withdrawal') AND t.to_addr = ?))`, f.Counterparty, f.Counterparty)
	}
	if f.Category != "" {
		add("t.category = ?", f.Category)
	}
	if f.Uncategorized {
		add("t.category IS NULL")
	}
	if f.Inbox {
		add("t.class IN ('inflow', 'outflow', 'unknown') AND t.category IS NULL AND t.amount_raw != '0'")
	}
	if f.HideZero {
		add("t.amount_raw != '0'")
	}
	if f.HideSpam || f.Inbox {
		add("a.is_spam = 0")
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

const transferCols = `t.id, t.uid, t.chain, t.tx_hash, t.ts, t.from_addr, t.to_addr, t.asset_id,
	t.amount_raw, t.fee_raw, t.class, COALESCE(t.category, ''), t.comment, a.symbol, a.decimals, a.is_spam`

func scanTransfer(sc interface{ Scan(...any) error }) (Transfer, error) {
	var t Transfer
	err := sc.Scan(&t.ID, &t.UID, &t.Chain, &t.TxHash, &t.TS, &t.From, &t.To, &t.AssetID,
		&t.AmountRaw, &t.FeeRaw, &t.Class, &t.Category, &t.Comment, &t.Symbol, &t.Decimals, &t.IsSpam)
	return t, err
}

func (s *Store) Transfers(ctx context.Context, f Filter) ([]Transfer, error) {
	where, args := f.where()
	q := "SELECT " + transferCols + " FROM transfers t JOIN assets a ON a.id = t.asset_id" + where +
		" ORDER BY t.ts DESC, t.tx_hash, t.id"
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d OFFSET %d", f.Limit, f.Offset)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Transfer
	for rows.Next() {
		t, err := scanTransfer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) CountTransfers(ctx context.Context, f Filter) (int, error) {
	where, args := f.where()
	var n int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM transfers t JOIN assets a ON a.id = t.asset_id"+where, args...).Scan(&n)
	return n, err
}

func (s *Store) Transfer(ctx context.Context, id int64) (Transfer, error) {
	return scanTransfer(s.db.QueryRowContext(ctx,
		"SELECT "+transferCols+" FROM transfers t JOIN assets a ON a.id = t.asset_id WHERE t.id = ?", id))
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Store) SetCategory(ctx context.Context, ids []int64, category string) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, "UPDATE transfers SET category = ? WHERE id = ?", nullIfEmpty(category), id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SetComment(ctx context.Context, id int64, comment string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE transfers SET comment = ? WHERE id = ?", comment, id)
	return err
}

// Reclassify recomputes classes from the address book, then applies rules.
func (s *Store) Reclassify(ctx context.Context) error {
	book, err := s.AddressBook(ctx)
	if err != nil {
		return err
	}
	kindOf := func(addr string) Kind {
		if a, ok := book[addr]; ok {
			return a.Kind
		}
		return KindExternal
	}
	type row struct {
		id       int64
		from, to string
		class    Class
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id, from_addr, to_addr, class FROM transfers")
	if err != nil {
		return err
	}
	var changed []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.from, &r.to, &r.class); err != nil {
			rows.Close()
			return err
		}
		if c := Classify(kindOf(r.from), kindOf(r.to)); c != r.class {
			r.class = c
			changed = append(changed, r)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(changed) > 0 {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, r := range changed {
			if _, err := tx.ExecContext(ctx, "UPDATE transfers SET class = ? WHERE id = ?", r.class, r.id); err != nil {
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return s.ApplyRules(ctx)
}

// --- rules ---

type Rule struct {
	ID           int64
	Counterparty string
	Class        Class
	Category     string
}

func (s *Store) Rules(ctx context.Context) ([]Rule, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, counterparty, class, category FROM rules ORDER BY category, counterparty")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		var r Rule
		if err := rows.Scan(&r.ID, &r.Counterparty, &r.Class, &r.Category); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) AddRule(ctx context.Context, r Rule) error {
	if r.Class != ClassInflow && r.Class != ClassOutflow {
		return errors.New("rules work for inflow and outflow only")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO rules (counterparty, class, category) VALUES (?, ?, ?)
		ON CONFLICT (counterparty, class) DO UPDATE SET category = excluded.category`,
		r.Counterparty, r.Class, r.Category)
	if err != nil {
		return err
	}
	return s.ApplyRules(ctx)
}

func (s *Store) DeleteRule(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM rules WHERE id = ?", id)
	return err
}

// ApplyRules fills the category of unreviewed transfers that match a rule.
func (s *Store) ApplyRules(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE transfers SET category = (
			SELECT r.category FROM rules r
			WHERE r.class = transfers.class
			  AND r.counterparty = CASE transfers.class WHEN 'inflow' THEN transfers.from_addr ELSE transfers.to_addr END)
		WHERE category IS NULL AND class IN ('inflow', 'outflow')
		  AND EXISTS (SELECT 1 FROM rules r
			WHERE r.class = transfers.class
			  AND r.counterparty = CASE transfers.class WHEN 'inflow' THEN transfers.from_addr ELSE transfers.to_addr END)`)
	return err
}

// --- sync cursors ---

func (s *Store) Cursor(ctx context.Context, chain, addr, stream string) int64 {
	var c int64
	s.db.QueryRowContext(ctx, "SELECT cursor FROM sync_cursors WHERE chain = ? AND address = ? AND stream = ?",
		chain, addr, stream).Scan(&c)
	return c
}

func (s *Store) SetCursor(ctx context.Context, chain, addr, stream string, c int64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sync_cursors (chain, address, stream, cursor) VALUES (?, ?, ?, ?)
		ON CONFLICT (chain, address, stream) DO UPDATE SET cursor = excluded.cursor`, chain, addr, stream, c)
	return err
}
