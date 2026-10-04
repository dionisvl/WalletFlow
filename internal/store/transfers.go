package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/dionisvl/WalletFlow/internal/ledger"
)

// InsertTransfers stores new transfers, skips known ones and returns how many were added.
// Assets are created on first sight. It implements chain.Emit.
func (s *Store) InsertTransfers(ctx context.Context, ts []ledger.Transfer) (int, error) {
	if len(ts) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	assetIDs := map[[2]string]int64{}
	assetID := func(a ledger.Asset) (int64, error) {
		k := [2]string{a.Chain, a.Contract}
		if id, ok := assetIDs[k]; ok {
			return id, nil
		}
		var id int64
		err := tx.QueryRowContext(ctx, `
			INSERT INTO assets (chain, contract, symbol, decimals) VALUES (?, ?, ?, ?)
			ON CONFLICT (chain, contract) DO UPDATE SET symbol = symbol
			RETURNING id`, a.Chain, a.Contract, a.Symbol, a.Decimals).Scan(&id)
		assetIDs[k] = id
		return id, err
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO transfers (uid, chain, tx_hash, ts, from_addr, to_addr, asset_id, amount_raw, fee_raw)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (uid) DO NOTHING`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	added := 0
	for _, t := range ts {
		id, err := assetID(t.Asset)
		if err != nil {
			return 0, err
		}
		res, err := stmt.ExecContext(ctx, t.UID, t.Chain, t.TxHash, t.TS, t.From, t.To, id, t.AmountRaw, t.FeeRaw)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		added += int(n)
	}
	return added, tx.Commit()
}

// Filter selects transfers. Zero fields do not filter.
type Filter struct {
	From, To      int64 // unix seconds, To exclusive
	Chain         string
	AssetID       int64
	Class         ledger.Class
	Classes       []ledger.Class
	Addresses     []string // either side
	FromAddrs     []string
	ToAddrs       []string
	Category      string
	Uncategorized bool
	Inbox         bool
	HideSpam      bool
	HideZero      bool
	OnlyLedger    bool // skip "unknown": transfers of watched wallets with strangers
	Limit, Offset int
}

// in builds "col IN (?, …)". Spread its args: add(c, a...), never add(in(…)).
func in(col string, vals []string) (string, []any) {
	args := make([]any, len(vals))
	for i, v := range vals {
		args[i] = v
	}
	return col + " IN (" + strings.Repeat(",?", len(vals))[1:] + ")", args
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
	if len(f.Classes) > 0 {
		cs := make([]string, len(f.Classes))
		for i, c := range f.Classes {
			cs[i] = string(c)
		}
		c, a := in("t.class", cs)
		add(c, a...)
	}
	if len(f.Addresses) > 0 {
		c1, a1 := in("t.from_addr", f.Addresses)
		c2, a2 := in("t.to_addr", f.Addresses)
		add("("+c1+" OR "+c2+")", append(a1, a2...)...)
	}
	if len(f.FromAddrs) > 0 {
		c, a := in("t.from_addr", f.FromAddrs)
		add(c, a...)
	}
	if len(f.ToAddrs) > 0 {
		c, a := in("t.to_addr", f.ToAddrs)
		add(c, a...)
	}
	if f.Category != "" {
		add("t.category = ?", f.Category)
	}
	if f.Uncategorized {
		add("t.category IS NULL")
	}
	if f.Inbox {
		// Only transfers with an own side; "unknown" ones are not the owner's business.
		add("t.class IN ('inflow', 'outflow') AND t.category IS NULL AND t.amount_raw != '0'")
	}
	if f.HideZero {
		add("t.amount_raw != '0'")
	}
	if f.OnlyLedger {
		add("t.class != 'unknown'")
	}
	if f.HideSpam || f.Inbox {
		add("a.is_spam = 0")
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

const transferCols = `t.id, t.uid, t.chain, t.tx_hash, t.ts, t.from_addr, t.to_addr,
	t.amount_raw, t.fee_raw, t.class, COALESCE(t.category, ''), t.comment,
	a.id, a.chain, a.contract, a.symbol, a.decimals, a.is_spam`

func scanTransfer(sc interface{ Scan(...any) error }) (ledger.Transfer, error) {
	var t ledger.Transfer
	a := &t.Asset
	err := sc.Scan(&t.ID, &t.UID, &t.Chain, &t.TxHash, &t.TS, &t.From, &t.To,
		&t.AmountRaw, &t.FeeRaw, &t.Class, &t.Category, &t.Comment,
		&a.ID, &a.Chain, &a.Contract, &a.Symbol, &a.Decimals, &a.IsSpam)
	return t, err
}

// Transfers returns matching transfers, newest first.
func (s *Store) Transfers(ctx context.Context, f Filter) ([]ledger.Transfer, error) {
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
	var out []ledger.Transfer
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

func (s *Store) Transfer(ctx context.Context, id int64) (ledger.Transfer, error) {
	return scanTransfer(s.db.QueryRowContext(ctx,
		"SELECT "+transferCols+" FROM transfers t JOIN assets a ON a.id = t.asset_id WHERE t.id = ?", id))
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// SetCategory sets (or with "" clears) the category of transfers.
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
	book, err := s.Book(ctx)
	if err != nil {
		return err
	}
	type change struct {
		id    int64
		class ledger.Class
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id, from_addr, to_addr, class FROM transfers")
	if err != nil {
		return err
	}
	var changed []change
	for rows.Next() {
		var id int64
		var from, to string
		var class ledger.Class
		if err := rows.Scan(&id, &from, &to, &class); err != nil {
			rows.Close()
			return err
		}
		if c := ledger.Classify(book.KindOf(from), book.KindOf(to)); c != class {
			changed = append(changed, change{id, c})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(changed) > 0 {
		if err := s.inTx(ctx, func(tx *sql.Tx) error {
			for _, c := range changed {
				if _, err := tx.ExecContext(ctx, "UPDATE transfers SET class = ? WHERE id = ?", c.class, c.id); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return s.ApplyRules(ctx)
}

func (s *Store) inTx(ctx context.Context, f func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := f(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// PurgeOrphans deletes transfers with no synced side, mine or watched (left over after
// an address was removed or stopped being synced) and the sync cursors of such addresses,
// so a later re-add syncs from scratch. Reviewed transfers (category or comment) are kept.
func (s *Store) PurgeOrphans(ctx context.Context) (int64, error) {
	var n int64
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			DELETE FROM transfers
			WHERE category IS NULL AND comment = ''
			  AND from_addr NOT IN (SELECT address FROM addresses WHERE kind IN ('mine', 'watch'))
			  AND to_addr NOT IN (SELECT address FROM addresses WHERE kind IN ('mine', 'watch'))`)
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		_, err = tx.ExecContext(ctx, `
			DELETE FROM sync_cursors WHERE address NOT IN (SELECT address FROM addresses WHERE kind IN ('mine', 'watch'))`)
		return err
	})
	return n, err
}

// RefreshBook brings transfers in line with the address book after it changed:
// drops orphans, recomputes classes and applies rules.
func (s *Store) RefreshBook(ctx context.Context) error {
	if _, err := s.PurgeOrphans(ctx); err != nil {
		return err
	}
	return s.Reclassify(ctx)
}
