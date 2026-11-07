package postgres

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

func toUUID(id domain.ID) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte(id), Valid: !id.IsZero()}
}

func fromUUID(u pgtype.UUID) domain.ID {
	if !u.Valid {
		return domain.ID{}
	}
	return domain.ID(u.Bytes)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func strOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func toNetip(a netip.Addr) *netip.Addr {
	if !a.IsValid() {
		return nil
	}
	return &a
}

// ---------- UserRepo ----------

type UserRepo struct {
	pool *pgxpool.Pool
}

func NewUserRepo(pool *pgxpool.Pool) *UserRepo {
	return &UserRepo{pool: pool}
}

const userSelect = `
	SELECT id, email, password_hash, name, role,
	       COALESCE(phone_e164, '') AS phone_e164,
	       is_active, email_verified_at, created_at, updated_at
	FROM users`

func (r *UserRepo) Create(ctx context.Context, u domain.User) error {
	var eva *time.Time
	if !u.EmailVerifiedAt().IsZero() {
		t := u.EmailVerifiedAt()
		eva = &t
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO users
		  (id, email, password_hash, name, role, phone_e164,
		   is_active, email_verified_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		toUUID(u.ID()), u.Email(), u.PasswordHash(), u.Name(), string(u.Role()),
		strOrNil(u.PhoneE164()), u.IsActive(), eva,
		u.CreatedAt(), u.UpdatedAt())
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrConflict
		}
		return fmt.Errorf("user create: %w", err)
	}
	return nil
}

func (r *UserRepo) FindByEmail(ctx context.Context, email string) (domain.User, error) {
	return r.findOne(ctx, userSelect+" WHERE email = $1", email)
}

func (r *UserRepo) FindByID(ctx context.Context, id domain.ID) (domain.User, error) {
	return r.findOne(ctx, userSelect+" WHERE id = $1", toUUID(id))
}

func (r *UserRepo) findOne(ctx context.Context, query string, arg any) (domain.User, error) {
	u, err := scanUser(r.pool.QueryRow(ctx, query, arg))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	return u, err
}

// UpdateProfile writes only the non-nil fields; untouched columns are not
// written, so a concurrent UpdatePasswordHash cannot be clobbered.
func (r *UserRepo) UpdateProfile(ctx context.Context, id domain.ID, name, phone *string, at time.Time) error {
	var nameVal string
	if name != nil {
		nameVal = *name
	}
	var phoneVal *string
	if phone != nil && *phone != "" {
		phoneVal = phone
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE users SET
		    name       = CASE WHEN $2::boolean THEN $3 ELSE name END,
		    phone_e164 = CASE WHEN $4::boolean THEN $5 ELSE phone_e164 END,
		    updated_at = $6
		WHERE id = $1`,
		toUUID(id),
		name != nil, nameVal,
		phone != nil, phoneVal,
		at)
	if err != nil {
		return fmt.Errorf("user update profile: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *UserRepo) UpdatePasswordHash(ctx context.Context, id domain.ID, hash string, at time.Time) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE users SET password_hash = $2, updated_at = $3 WHERE id = $1`,
		toUUID(id), hash, at)
	if err != nil {
		return fmt.Errorf("user update password hash: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func scanUser(row pgx.Row) (domain.User, error) {
	var (
		id              pgtype.UUID
		email           string
		passwordHash    string
		name            string
		role            string
		phoneE164       string
		isActive        bool
		emailVerifiedAt *time.Time
		createdAt       time.Time
		updatedAt       time.Time
	)
	if err := row.Scan(&id, &email, &passwordHash, &name, &role, &phoneE164,
		&isActive, &emailVerifiedAt, &createdAt, &updatedAt); err != nil {
		return domain.User{}, err
	}
	rec := domain.UserRecord{
		ID:           fromUUID(id),
		Email:        email,
		PasswordHash: passwordHash,
		Name:         name,
		Role:         domain.Role(role),
		PhoneE164:    phoneE164,
		IsActive:     isActive,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
	}
	if emailVerifiedAt != nil {
		rec.EmailVerifiedAt = *emailVerifiedAt
	}
	return domain.RehydrateUser(rec)
}

// ---------- RefreshTokenRepo ----------

type RefreshTokenRepo struct {
	pool *pgxpool.Pool
}

func NewRefreshTokenRepo(pool *pgxpool.Pool) *RefreshTokenRepo {
	return &RefreshTokenRepo{pool: pool}
}

const refreshSelect = `
	SELECT id, user_id, family_id, token_hash, expires_at, revoked_at,
	       replaced_by, user_agent, ip, created_at
	FROM refresh_tokens`

func (r *RefreshTokenRepo) Create(ctx context.Context, t domain.RefreshToken) error {
	var ip *netip.Addr
	if a, err := netip.ParseAddr(t.IP()); err == nil {
		ip = &a
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO refresh_tokens
		  (id, user_id, family_id, token_hash, expires_at, revoked_at,
		   replaced_by, user_agent, ip, created_at)
		VALUES ($1,$2,$3,$4,$5,NULL,NULL,$6,$7,$8)`,
		toUUID(t.ID()), toUUID(t.UserID()), toUUID(t.FamilyID()),
		t.TokenHash(), t.ExpiresAt(), strOrNil(t.UserAgent()), ip, t.CreatedAt())
	if err != nil {
		return fmt.Errorf("refresh token create: %w", err)
	}
	return nil
}

func (r *RefreshTokenRepo) FindByHash(ctx context.Context, hash []byte) (domain.RefreshToken, error) {
	t, err := scanRefreshToken(r.pool.QueryRow(ctx, refreshSelect+" WHERE token_hash = $1", hash))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RefreshToken{}, domain.ErrNotFound
	}
	return t, err
}

// Rotate runs the conditional parent-revoke and child-insert in one
// transaction. Zero rows on the UPDATE (parent already rotated by a
// concurrent request) returns ErrConflict — the service treats that as
// reuse and revokes the family.
func (r *RefreshTokenRepo) Rotate(ctx context.Context, parent, child domain.RefreshToken) error {
	if parent.RevokedAt().IsZero() || parent.ReplacedBy().IsZero() {
		return fmt.Errorf("refresh token rotate: parent not marked rotated")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("refresh token rotate begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		UPDATE refresh_tokens
		   SET revoked_at = $2, replaced_by = $3
		 WHERE id = $1 AND revoked_at IS NULL`,
		toUUID(parent.ID()), parent.RevokedAt(), toUUID(parent.ReplacedBy()))
	if err != nil {
		return fmt.Errorf("refresh token rotate update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}

	var ip *netip.Addr
	if a, err := netip.ParseAddr(child.IP()); err == nil {
		ip = &a
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO refresh_tokens
		  (id, user_id, family_id, token_hash, expires_at, revoked_at,
		   replaced_by, user_agent, ip, created_at)
		VALUES ($1,$2,$3,$4,$5,NULL,NULL,$6,$7,$8)`,
		toUUID(child.ID()), toUUID(child.UserID()), toUUID(child.FamilyID()),
		child.TokenHash(), child.ExpiresAt(), strOrNil(child.UserAgent()), ip, child.CreatedAt()); err != nil {
		return fmt.Errorf("refresh token rotate insert: %w", err)
	}
	return tx.Commit(ctx)
}

func (r *RefreshTokenRepo) RevokeFamily(ctx context.Context, familyID domain.ID, at time.Time) (int, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at = $2
		 WHERE family_id = $1 AND revoked_at IS NULL`,
		toUUID(familyID), at)
	if err != nil {
		return 0, fmt.Errorf("refresh token revoke family: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (r *RefreshTokenRepo) ListByUser(ctx context.Context, userID domain.ID) ([]domain.RefreshToken, error) {
	rows, err := r.pool.Query(ctx,
		refreshSelect+" WHERE user_id = $1 ORDER BY created_at DESC", toUUID(userID))
	if err != nil {
		return nil, fmt.Errorf("refresh token list: %w", err)
	}
	defer rows.Close()
	var out []domain.RefreshToken
	for rows.Next() {
		t, err := scanRefreshToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *RefreshTokenRepo) RevokeByUser(ctx context.Context, userID, tokenID domain.ID, at time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at = $3
		 WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`,
		toUUID(tokenID), toUUID(userID), at)
	if err != nil {
		return fmt.Errorf("refresh token revoke by user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func scanRefreshToken(row pgx.Row) (domain.RefreshToken, error) {
	var (
		id, userID, familyID, replacedBy pgtype.UUID
		tokenHash                        []byte
		expiresAt                        time.Time
		revokedAt                        *time.Time
		userAgent                        *string
		ip                               *netip.Addr
		createdAt                        time.Time
	)
	if err := row.Scan(&id, &userID, &familyID, &tokenHash, &expiresAt,
		&revokedAt, &replacedBy, &userAgent, &ip, &createdAt); err != nil {
		return domain.RefreshToken{}, err
	}
	rec := domain.RefreshTokenRecord{
		ID:         fromUUID(id),
		UserID:     fromUUID(userID),
		FamilyID:   fromUUID(familyID),
		TokenHash:  tokenHash,
		ExpiresAt:  expiresAt,
		ReplacedBy: fromUUID(replacedBy),
		CreatedAt:  createdAt,
	}
	if revokedAt != nil {
		rec.RevokedAt = *revokedAt
	}
	if userAgent != nil {
		rec.UserAgent = *userAgent
	}
	if ip != nil {
		rec.IP = ip.String()
	}
	return domain.RehydrateRefreshToken(rec)
}

// ---------- AddressRepo ----------

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

// ---------- AuditLog ----------

type AuditLog struct {
	pool *pgxpool.Pool
}

func NewAuditLog(pool *pgxpool.Pool) *AuditLog {
	return &AuditLog{pool: pool}
}

func (l *AuditLog) Append(ctx context.Context, e domain.AuditEntry) error {
	var before, after []byte
	if len(e.Before) > 0 {
		before = e.Before
	}
	if len(e.After) > 0 {
		after = e.After
	}
	_, err := l.pool.Exec(ctx, `
		INSERT INTO audit_log
		  (actor, action, entity_type, entity_id, before, after, ip, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		toUUID(e.Actor), e.Action, e.EntityType, e.EntityID,
		before, after, toNetip(e.IP), e.At)
	if err != nil {
		return fmt.Errorf("audit append: %w", err)
	}
	return nil
}
