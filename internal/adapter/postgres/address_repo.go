package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type AddressRepo struct {
	pool *pgxpool.Pool
}

func NewAddressRepo(pool *pgxpool.Pool) *AddressRepo {
	return &AddressRepo{pool: pool}
}

const addressSelect = `
SELECT id, user_id, recipient, phone_e164, line1,
       COALESCE(line2, '') AS line2,
       town,
       COALESCE(county, '') AS county,
       COALESCE(notes, '') AS notes,
       created_at
FROM addresses`

func (r *AddressRepo) Create(ctx context.Context, a domain.Address) error {
	_, err := r.pool.Exec(ctx, `
INSERT INTO addresses
  (id, user_id, recipient, phone_e164, line1, line2, town, county, notes, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		toUUID(a.ID()), toUUID(a.UserID()),
		a.Recipient(), a.PhoneE164(), a.Line1(),
		strOrNil(a.Line2()), a.Town(), strOrNil(a.County()), strOrNil(a.Notes()),
		a.CreatedAt())
	if err != nil {
		return fmt.Errorf("address create: %w", err)
	}
	return nil
}

func (r *AddressRepo) ListByUser(ctx context.Context, userID domain.ID) ([]domain.Address, error) {
	rows, err := r.pool.Query(ctx,
		addressSelect+" WHERE user_id = $1 ORDER BY created_at DESC", toUUID(userID))
	if err != nil {
		return nil, fmt.Errorf("address list: %w", err)
	}
	defer rows.Close()
	var out []domain.Address
	for rows.Next() {
		a, err := scanAddress(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *AddressRepo) FindByID(ctx context.Context, id domain.ID) (domain.Address, error) {
	a, err := scanAddress(r.pool.QueryRow(ctx, addressSelect+" WHERE id = $1", toUUID(id)))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Address{}, domain.ErrNotFound
	}
	return a, err
}

func scanAddress(row pgx.Row) (domain.Address, error) {
	var (
		id, userID                  pgtype.UUID
		recipient, phoneE164, line1 string
		line2, town, county, notes  string
		createdAt                   time.Time
	)
	if err := row.Scan(&id, &userID, &recipient, &phoneE164, &line1,
		&line2, &town, &county, &notes, &createdAt); err != nil {
		return domain.Address{}, err
	}
	return domain.RehydrateAddress(domain.AddressRecord{
		ID:        fromUUID(id),
		UserID:    fromUUID(userID),
		Recipient: recipient,
		PhoneE164: phoneE164,
		Line1:     line1,
		Line2:     line2,
		Town:      town,
		County:    county,
		Notes:     notes,
		CreatedAt: createdAt,
	})
}
