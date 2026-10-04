package store

import (
	"context"
	"time"

	"github.com/dionisvl/walletflow/internal/ledger"
)

func (s *Store) Addresses(ctx context.Context) ([]ledger.Address, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, family, address, name, kind, color FROM addresses ORDER BY kind, name, address")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ledger.Address
	for rows.Next() {
		var a ledger.Address
		if err := rows.Scan(&a.ID, &a.Family, &a.Address, &a.Name, &a.Kind, &a.Color); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) Book(ctx context.Context) (ledger.Book, error) {
	list, err := s.Addresses(ctx)
	if err != nil {
		return nil, err
	}
	b := make(ledger.Book, len(list))
	for _, a := range list {
		b[a.Address] = a
	}
	return b, nil
}

// UpsertAddress adds an address or updates name, kind and color of an existing one.
func (s *Store) UpsertAddress(ctx context.Context, a ledger.Address) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO addresses (family, address, name, kind, color, created_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (address) DO UPDATE SET name = excluded.name, kind = excluded.kind, color = excluded.color`,
		a.Family, a.Address, a.Name, a.Kind, a.Color, time.Now().Unix())
	return err
}

func (s *Store) UpdateAddress(ctx context.Context, a ledger.Address) error {
	_, err := s.db.ExecContext(ctx, "UPDATE addresses SET name = ?, kind = ?, color = ? WHERE id = ?",
		a.Name, a.Kind, a.Color, a.ID)
	return err
}

func (s *Store) DeleteAddress(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM addresses WHERE id = ?", id)
	return err
}

// Assets lists assets that have at least one transfer.
func (s *Store) Assets(ctx context.Context) ([]ledger.Asset, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.chain, a.contract, a.symbol, a.decimals, a.is_spam FROM assets a
		WHERE EXISTS (SELECT 1 FROM transfers t WHERE t.asset_id = a.id)
		ORDER BY a.is_spam, a.symbol, a.chain`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ledger.Asset
	for rows.Next() {
		var a ledger.Asset
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

// IsSynced reports whether addr is in the book as a synced (mine or watched) address.
func (s *Store) IsSynced(ctx context.Context, addr string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM addresses WHERE address = ? AND kind IN ('mine', 'watch')", addr).Scan(&n)
	return n > 0, err
}
