package store

import (
	"context"
	"errors"

	"walletflow/internal/ledger"
)

func (s *Store) Rules(ctx context.Context) ([]ledger.Rule, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, counterparty, class, category FROM rules ORDER BY category, counterparty")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ledger.Rule
	for rows.Next() {
		var r ledger.Rule
		if err := rows.Scan(&r.ID, &r.Counterparty, &r.Class, &r.Category); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AddRule saves a rule and applies it to the whole history.
func (s *Store) AddRule(ctx context.Context, r ledger.Rule) error {
	if r.Class != ledger.ClassInflow && r.Class != ledger.ClassOutflow {
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
	const match = `r.class = transfers.class
		AND r.counterparty = CASE transfers.class WHEN 'inflow' THEN transfers.from_addr ELSE transfers.to_addr END`
	_, err := s.db.ExecContext(ctx, `
		UPDATE transfers SET category = (SELECT r.category FROM rules r WHERE `+match+`)
		WHERE category IS NULL AND class IN ('inflow', 'outflow')
		  AND EXISTS (SELECT 1 FROM rules r WHERE `+match+`)`)
	return err
}
