package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"time"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

var (
	ErrInvalidCredentials = errors.New("service: invalid credentials")
	ErrTokenReused        = errors.New("service: refresh token reused")
	ErrTokenExpired       = errors.New("service: token expired")
	ErrUnauthorized       = errors.New("service: unauthorized")
	ErrEmailTaken         = errors.New("service: email already registered")
	ErrEmptyPatch         = errors.New("service: empty patch")
	ErrAddressLimit       = errors.New("service: address limit reached")
)

const maxAddressesPerUser = 10

type Config struct {
	AccessTTL  time.Duration
	RefreshTTL time.Duration
}

type AuthService struct {
	users      domain.UserRepository
	refresh    domain.RefreshTokenRepository
	addresses  domain.AddressRepository
	audit      domain.AuditLog
	lockout    domain.LockoutStore
	hasher     domain.PasswordHasher
	signer     domain.TokenSigner
	clock      domain.Clock
	ids        domain.IDGenerator
	rawTokens  domain.RawTokenGenerator
	accessTTL  time.Duration
	refreshTTL time.Duration
	dummyHash  string
}

func NewAuthService(
	users domain.UserRepository,
	refresh domain.RefreshTokenRepository,
	addresses domain.AddressRepository,
	audit domain.AuditLog,
	lockout domain.LockoutStore,
	hasher domain.PasswordHasher,
	signer domain.TokenSigner,
	clock domain.Clock,
	ids domain.IDGenerator,
	rawTokens domain.RawTokenGenerator,
	cfg Config,
) (*AuthService, error) {
	if cfg.AccessTTL <= 0 || cfg.RefreshTTL <= 0 {
		return nil, errors.New("service: TTLs must be positive")
	}
	dummy, err := hasher.Hash("constant-time-dummy-not-a-real-password")
	if err != nil {
		return nil, fmt.Errorf("service: dummy hash: %w", err)
	}
	return &AuthService{
		users:      users,
		refresh:    refresh,
		addresses:  addresses,
		audit:      audit,
		lockout:    lockout,
		hasher:     hasher,
		signer:     signer,
		clock:      clock,
		ids:        ids,
		rawTokens:  rawTokens,
		accessTTL:  cfg.AccessTTL,
		refreshTTL: cfg.RefreshTTL,
		dummyHash:  dummy,
	}, nil
}

type LoginResult struct {
	AccessToken  string
	RefreshToken []byte
	User         domain.User
}

type SessionInfo struct {
	ID        domain.ID
	UserAgent string
	IP        string
	CreatedAt time.Time
	ExpiresAt time.Time
}

func (s *AuthService) Register(ctx context.Context, email, password, name, ip string) (domain.User, error) {
	if err := domain.ValidatePassword(password); err != nil {
		return domain.User{}, err
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return domain.User{}, fmt.Errorf("register: hash: %w", err)
	}
	u, err := domain.NewUser(s.ids.NewID(), email, hash, name, domain.RoleCustomer, "", s.clock.Now())
	if err != nil {
		return domain.User{}, err
	}
	if err := s.users.Create(ctx, u); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return domain.User{}, ErrEmailTaken
		}
		return domain.User{}, fmt.Errorf("register: create: %w", err)
	}
	s.auditEvent(ctx, u.ID(), "user_registered", "user", u.ID().String(), ip, nil, nil)
	return u, nil
}

// Login returns ErrInvalidCredentials for every authentication failure; the
// reason is recorded only in the audit log. If the lockout store is
// unavailable it fails closed (§13.2).
func (s *AuthService) Login(ctx context.Context, email, password, ip, userAgent string) (LoginResult, error) {
	now := s.clock.Now()

	normalized, err := domain.NormalizeEmail(email)
	if err != nil {
		s.dummyVerify(password)
		return LoginResult{}, ErrInvalidCredentials
	}

	u, err := s.users.FindByEmail(ctx, normalized)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			s.dummyVerify(password)
			return LoginResult{}, ErrInvalidCredentials
		}
		return LoginResult{}, fmt.Errorf("login: find: %w", err)
	}
	if !u.IsActive() {
		s.dummyVerify(password)
		s.auditEvent(ctx, u.ID(), "login_inactive_account", "user", u.ID().String(), ip, nil, nil)
		return LoginResult{}, ErrInvalidCredentials
	}

	prefix := ipPrefix(parseIP(ip))
	wait, err := s.lockout.Backoff(ctx, u.ID(), prefix)
	if err != nil {
		return LoginResult{}, fmt.Errorf("login: lockout: %w", err)
	}
	if wait > 0 {
		s.dummyVerify(password)
		s.auditEvent(ctx, u.ID(), "login_locked", "user", u.ID().String(), ip, nil, nil)
		return LoginResult{}, ErrInvalidCredentials
	}

	match, needsRehash, err := s.hasher.Verify(password, u.PasswordHash())
	if err != nil {
		return LoginResult{}, fmt.Errorf("login: verify: %w", err)
	}
	if !match {
		if _, err := s.lockout.RecordFailedAttempt(ctx, u.ID(), prefix); err != nil {
			logErr(ctx, "record failed login attempt", err)
		}
		s.auditEvent(ctx, u.ID(), "login_failed", "user", u.ID().String(), ip, nil, nil)
		return LoginResult{}, ErrInvalidCredentials
	}

	if err := s.lockout.Reset(ctx, u.ID(), prefix); err != nil {
		logErr(ctx, "reset login lockout", err)
	}
	if needsRehash {
		s.rehash(ctx, u.ID(), password, now)
	}
	return s.issueTokens(ctx, u, ip, userAgent, now)
}

func (s *AuthService) rehash(ctx context.Context, userID domain.ID, password string, now time.Time) {
	hash, err := s.hasher.Hash(password)
	if err != nil {
		logErr(ctx, "rehash password", err)
		return
	}
	if err := s.users.UpdatePasswordHash(ctx, userID, hash, now); err != nil {
		logErr(ctx, "store rehashed password", err)
	}
}

func (s *AuthService) issueTokens(ctx context.Context, u domain.User, ip, userAgent string, now time.Time) (LoginResult, error) {
	access, err := s.signer.Sign(u.ID(), u.Role(), s.accessTTL)
	if err != nil {
		return LoginResult{}, fmt.Errorf("issue tokens: sign: %w", err)
	}
	raw, err := s.rawTokens.NewRawToken()
	if err != nil {
		return LoginResult{}, fmt.Errorf("issue tokens: raw: %w", err)
	}
	rt, err := domain.NewRefreshToken(
		s.ids.NewID(), u.ID(), s.ids.NewID(),
		domain.HashRefreshToken(raw),
		now.Add(s.refreshTTL), now, userAgent, ip,
	)
	if err != nil {
		return LoginResult{}, fmt.Errorf("issue tokens: refresh: %w", err)
	}
	if err := s.refresh.Create(ctx, rt); err != nil {
		return LoginResult{}, fmt.Errorf("issue tokens: create refresh: %w", err)
	}
	s.auditEvent(ctx, u.ID(), "login_success", "user", u.ID().String(), ip, nil, nil)
	return LoginResult{AccessToken: access, RefreshToken: raw, User: u}, nil
}

// Refresh rotates a refresh token. A revoked parent, or a concurrent rotation
// (ErrConflict from the repository), is treated as reuse and revokes the
// whole family. Sign happens before Rotate so a signing failure cannot strand
// the client with a revoked token.
func (s *AuthService) Refresh(ctx context.Context, raw []byte, ip, userAgent string) (LoginResult, error) {
	now := s.clock.Now()
	if len(raw) == 0 {
		return LoginResult{}, ErrInvalidCredentials
	}
	parent, err := s.refresh.FindByHash(ctx, domain.HashRefreshToken(raw))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return LoginResult{}, ErrInvalidCredentials
		}
		return LoginResult{}, fmt.Errorf("refresh: find: %w", err)
	}
	if parent.IsRevoked() {
		return LoginResult{}, s.handleReuse(ctx, parent, ip, now)
	}
	if parent.IsExpired(now) {
		return LoginResult{}, ErrTokenExpired
	}
	u, err := s.users.FindByID(ctx, parent.UserID())
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return LoginResult{}, ErrInvalidCredentials
		}
		return LoginResult{}, fmt.Errorf("refresh: find user: %w", err)
	}
	if !u.IsActive() {
		return LoginResult{}, ErrInvalidCredentials
	}

	access, err := s.signer.Sign(u.ID(), u.Role(), s.accessTTL)
	if err != nil {
		return LoginResult{}, fmt.Errorf("refresh: sign: %w", err)
	}
	newRaw, err := s.rawTokens.NewRawToken()
	if err != nil {
		return LoginResult{}, fmt.Errorf("refresh: raw: %w", err)
	}
	parentAfter, child, err := parent.Rotate(
		s.ids.NewID(),
		domain.HashRefreshToken(newRaw),
		now.Add(s.refreshTTL),
		now,
	)
	if err != nil {
		return LoginResult{}, fmt.Errorf("refresh: rotate: %w", err)
	}
	if err := s.refresh.Rotate(ctx, parentAfter, child); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return LoginResult{}, s.handleReuse(ctx, parent, ip, now)
		}
		return LoginResult{}, fmt.Errorf("refresh: persist: %w", err)
	}
	return LoginResult{AccessToken: access, RefreshToken: newRaw, User: u}, nil
}

func (s *AuthService) handleReuse(ctx context.Context, parent domain.RefreshToken, ip string, now time.Time) error {
	if _, err := s.refresh.RevokeFamily(ctx, parent.FamilyID(), now); err != nil {
		logErr(ctx, "revoke token family", err)
	}
	s.auditEvent(ctx, parent.UserID(), "refresh_reuse_detected", "refresh_token", parent.ID().String(), ip, nil, nil)
	return ErrTokenReused
}

// Logout revokes the presented token's whole family (§7), so a stale token
// cannot leave its live successor valid.
func (s *AuthService) Logout(ctx context.Context, raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	t, err := s.refresh.FindByHash(ctx, domain.HashRefreshToken(raw))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("logout: find: %w", err)
	}
	if _, err := s.refresh.RevokeFamily(ctx, t.FamilyID(), s.clock.Now()); err != nil {
		return fmt.Errorf("logout: revoke: %w", err)
	}
	return nil
}

func (s *AuthService) GetMe(ctx context.Context, userID domain.ID) (domain.User, error) {
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.User{}, ErrUnauthorized
		}
		return domain.User{}, err
	}
	return u, nil
}

type UpdateMePatch struct {
	Name  *string
	Phone *string
}

func (s *AuthService) UpdateMe(ctx context.Context, userID domain.ID, patch UpdateMePatch) (domain.User, error) {
	if patch.Name == nil && patch.Phone == nil {
		return domain.User{}, ErrEmptyPatch
	}
	u, err := s.GetMe(ctx, userID)
	if err != nil {
		return domain.User{}, err
	}
	now := s.clock.Now()

	// Persist validated values, not raw input.
	var name, phone *string
	if patch.Name != nil {
		if u, err = u.WithName(*patch.Name, now); err != nil {
			return domain.User{}, err
		}
		v := u.Name()
		name = &v
	}
	if patch.Phone != nil {
		if u, err = u.WithPhone(*patch.Phone, now); err != nil {
			return domain.User{}, err
		}
		v := u.PhoneE164()
		phone = &v
	}
	if err := s.users.UpdateProfile(ctx, userID, name, phone, now); err != nil {
		return domain.User{}, fmt.Errorf("update me: persist: %w", err)
	}
	// Audit records which fields changed, not their values (§13.3).
	s.auditEvent(ctx, userID, "user_updated", "user", userID.String(), "", nil, fieldChanges(patch))
	return u, nil
}

func fieldChanges(p UpdateMePatch) []byte {
	m := map[string]bool{}
	if p.Name != nil {
		m["name_changed"] = true
	}
	if p.Phone != nil {
		m["phone_changed"] = true
	}
	b, _ := json.Marshal(m)
	return b
}

func (s *AuthService) ListAddresses(ctx context.Context, userID domain.ID) ([]domain.Address, error) {
	return s.addresses.ListByUser(ctx, userID)
}

type CreateAddressInput struct {
	Recipient string
	Phone     string
	Line1     string
	Line2     string
	Town      string
	County    string
	Notes     string
}

func (s *AuthService) CreateAddress(ctx context.Context, userID domain.ID, in CreateAddressInput) (domain.Address, error) {
	existing, err := s.addresses.ListByUser(ctx, userID)
	if err != nil {
		return domain.Address{}, fmt.Errorf("create address: list: %w", err)
	}
	if len(existing) >= maxAddressesPerUser {
		return domain.Address{}, ErrAddressLimit
	}
	a, err := domain.NewAddress(
		s.ids.NewID(), userID,
		in.Recipient, in.Phone, in.Line1, in.Line2, in.Town, in.County, in.Notes,
		s.clock.Now(),
	)
	if err != nil {
		return domain.Address{}, err
	}
	if err := s.addresses.Create(ctx, a); err != nil {
		return domain.Address{}, fmt.Errorf("create address: %w", err)
	}
	s.auditEvent(ctx, userID, "address_created", "address", a.ID().String(), "", nil, nil)
	return a, nil
}

func (s *AuthService) ListSessions(ctx context.Context, userID domain.ID) ([]SessionInfo, error) {
	all, err := s.refresh.ListByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	now := s.clock.Now()
	out := make([]SessionInfo, 0, len(all))
	for _, t := range all {
		if !t.IsActive(now) {
			continue
		}
		out = append(out, SessionInfo{
			ID:        t.ID(),
			UserAgent: t.UserAgent(),
			IP:        t.IP(),
			CreatedAt: t.CreatedAt(),
			ExpiresAt: t.ExpiresAt(),
		})
	}
	return out, nil
}

func (s *AuthService) RevokeSession(ctx context.Context, userID, tokenID domain.ID) error {
	return s.refresh.RevokeByUser(ctx, userID, tokenID, s.clock.Now())
}

func (s *AuthService) dummyVerify(password string) {
	_, _, _ = s.hasher.Verify(password, s.dummyHash)
}

func (s *AuthService) auditEvent(ctx context.Context, actor domain.ID, action, entityType, entityID, ip string, before, after []byte) {
	err := s.audit.Append(ctx, domain.AuditEntry{
		Actor:      actor,
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		Before:     before,
		After:      after,
		IP:         parseIP(ip),
		At:         s.clock.Now(),
	})
	if err != nil {
		logErr(ctx, "audit append", err, "action", action)
	}
}

func logErr(ctx context.Context, msg string, err error, attrs ...any) {
	slog.ErrorContext(ctx, msg, append(attrs, "error", err)...)
}

func parseIP(s string) netip.Addr {
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}
	}
	return a.Unmap()
}

func ipPrefix(a netip.Addr) string {
	if !a.IsValid() {
		return "unknown"
	}
	bits := 64
	if a.Is4() {
		bits = 24
	}
	p, err := a.Prefix(bits)
	if err != nil {
		return "unknown"
	}
	return p.String()
}
