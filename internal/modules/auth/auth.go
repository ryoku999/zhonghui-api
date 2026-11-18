package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"slices"
	"strings"
	"time"

	jwtware "github.com/gofiber/contrib/v3/jwt"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"zhonghui-api/internal/database/db"
)

const refreshCookie = "zhonghui_refresh"
const cookiePath = "/api/v1/auth"

type service struct {
	config Config
	pool   *pgxpool.Pool
	db     *db.Queries
	dummy  string
}

type userResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
	IsActive    bool   `json:"isActive"`
}

func publicUser(u db.User) userResponse {
	return userResponse{u.ID.String(), u.Username, u.DisplayName, u.Role, u.IsActive}
}

type authenticatedUserKey struct{}

func Register(app *fiber.App, pool *pgxpool.Pool, config Config, origins []string) {
	s := &service{config: config, pool: pool, db: db.New(pool), dummy: hashPassword(uuid.NewString())}
	auth := app.Group(cookiePath, requestContext)
	protected := jwtware.New(jwtware.Config{
		SigningKey:    jwtware.SigningKey{Key: config.accessKey, JWTAlg: "HS256"},
		Claims:        claims{},
		ParserOptions: config.parserOptions(),
		ErrorHandler: func(_ fiber.Ctx, _ error) error {
			return fiber.ErrUnauthorized
		},
		SuccessHandler: s.requireSession,
	})
	checkOrigin := func(c fiber.Ctx) error {
		if c.Get("Sec-Fetch-Site") == "cross-site" ||
			(c.Get("Origin") != "" && !slices.Contains(origins, c.Get("Origin"))) {
			return fiber.ErrForbidden
		}
		return c.Next()
	}
	auth.Post("/login", checkOrigin, limiter.New(limiter.Config{
		Max: 10, Expiration: time.Minute, SkipSuccessfulRequests: true,
		LimitReached: func(fiber.Ctx) error { return fiber.ErrTooManyRequests },
	}), s.login)
	auth.Post("/refresh", checkOrigin, s.refresh)
	auth.Post("/logout", checkOrigin, s.logout)
	auth.Get("/me", protected, func(c fiber.Ctx) error {
		return c.JSON(publicUser(fiber.Locals[db.User](c, authenticatedUserKey{})))
	})
	users := app.Group("/api/v1/users", requestContext, protected, requireAdmin)
	users.Get("/", s.listUsers)
	users.Post("/", s.createUser)
	users.Patch("/:id", s.updateUser)
}

func requestContext(c fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	c.SetContext(ctx)
	return c.Next()
}

func (s *service) requireSession(c fiber.Ctx) error {
	token := jwtware.FromContext(c)
	claims, ok := token.Claims.(*claims)
	if !ok {
		return fiber.ErrUnauthorized
	}
	userID, sessionID, ok := claims.ids("access")
	if !ok || claims.Role == "" {
		return fiber.ErrUnauthorized
	}
	u, err := s.db.GetActiveSessionUser(c.Context(), db.GetActiveSessionUserParams{SessionID: sessionID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.ErrUnauthorized
	}
	if err != nil {
		return err
	}
	c.Locals(authenticatedUserKey{}, u)
	return c.Next()
}

func requireAdmin(c fiber.Ctx) error {
	if fiber.Locals[db.User](c, authenticatedUserKey{}).Role != "ADMIN" {
		return fiber.ErrForbidden
	}
	return c.Next()
}

func (s *service) login(c fiber.Ctx) error {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return fiber.ErrBadRequest
	}
	input.Username = normalizeUsername(input.Username)
	if len(input.Username) == 0 || len(input.Username) > 64 || len(input.Password) == 0 || len(input.Password) > 256 {
		return fiber.ErrBadRequest
	}

	ctx := c.Context()
	u, err := s.db.FindUserByUsername(ctx, input.Username)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	hash := u.PasswordHash
	if err != nil {
		hash = s.dummy
	}
	if !checkPassword(input.Password, hash) || !u.IsActive {
		return fiber.ErrUnauthorized
	}

	sessionID := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	now := time.Now().UTC().Truncate(time.Second)
	expiry := now.Add(sessionLifetime)
	var access, refresh string
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.db.WithTx(tx)
		current, err := q.LockUserForSession(ctx, u.ID)
		if err != nil {
			return missingToUnauthorized(err)
		}
		if !current.IsActive || current.PasswordHash != u.PasswordHash {
			return fiber.ErrUnauthorized
		}
		u = current
		access, refresh, err = s.tokens(u, sessionID, now, expiry)
		if err != nil {
			return err
		}
		digest := sha256.Sum256([]byte(refresh))
		return q.CreateSession(ctx, db.CreateSessionParams{
			ID: sessionID, UserID: u.ID, RefreshTokenHash: digest[:],
			ExpiresAt: pgtype.Timestamptz{Time: expiry, Valid: true},
		})
	})
	if err != nil {
		return err
	}
	return s.sessionResponse(c, u, access, refresh, expiry)
}

func (s *service) refresh(c fiber.Ctx) error {
	raw := c.Cookies(refreshCookie)
	token, err := s.config.parseRefresh(raw)
	userID, sessionID, valid := token.ids("refresh")
	if err != nil || !valid {
		s.setCookie(c, "", time.Time{})
		return fiber.ErrUnauthorized
	}

	ctx := c.Context()
	var u db.User
	var access, refresh string
	var expiry time.Time
	var reused bool
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.db.WithTx(tx)
		u, err = q.LockUserForSession(ctx, userID)
		if err != nil {
			return missingToUnauthorized(err)
		}
		if !u.IsActive {
			return fiber.ErrUnauthorized
		}
		row, err := q.LockSession(ctx, db.LockSessionParams{ID: sessionID, UserID: userID})
		if err != nil {
			return missingToUnauthorized(err)
		}
		expiry = row.ExpiresAt.Time
		now := time.Now().UTC().Truncate(time.Second)
		if row.RevokedAt.Valid || !expiry.After(now) {
			return fiber.ErrUnauthorized
		}
		digest := sha256.Sum256([]byte(raw))
		if subtle.ConstantTimeCompare(digest[:], row.RefreshTokenHash) != 1 {
			reused = true
			return q.RevokeSession(ctx, db.RevokeSessionParams{ID: sessionID, UserID: userID})
		}
		access, refresh, err = s.tokens(u, sessionID, now, expiry)
		if err != nil {
			return err
		}
		nextDigest := sha256.Sum256([]byte(refresh))
		return q.RotateSession(ctx, db.RotateSessionParams{ID: sessionID, RefreshTokenHash: nextDigest[:]})
	})
	if err == nil && reused {
		err = fiber.ErrUnauthorized
	}
	if err != nil {
		if errors.Is(err, fiber.ErrUnauthorized) {
			s.setCookie(c, "", time.Time{})
		}
		return err
	}
	return s.sessionResponse(c, u, access, refresh, expiry)
}

func (s *service) logout(c fiber.Ctx) error {
	token, err := s.config.parseRefresh(c.Cookies(refreshCookie))
	userID, sessionID, valid := token.ids("refresh")
	if err == nil && valid {
		if err := s.db.RevokeSession(c.Context(), db.RevokeSessionParams{ID: sessionID, UserID: userID}); err != nil {
			return err
		}
	}
	s.setCookie(c, "", time.Time{})
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *service) tokens(u db.User, sessionID pgtype.UUID, now, expiry time.Time) (string, string, error) {
	accessExpiry := now.Add(accessLifetime)
	if expiry.Before(accessExpiry) {
		accessExpiry = expiry
	}
	access, err := s.config.sign("access", u.Role, u.ID, sessionID, now, accessExpiry)
	if err != nil {
		return "", "", err
	}
	refresh, err := s.config.sign("refresh", "", u.ID, sessionID, now, expiry)
	return access, refresh, err
}

func (s *service) sessionResponse(c fiber.Ctx, u db.User, access, refresh string, expiry time.Time) error {
	s.setCookie(c, refresh, expiry)
	return c.JSON(fiber.Map{"accessToken": access, "user": publicUser(u)})
}

func (s *service) setCookie(c fiber.Ctx, value string, expiry time.Time) {
	maxAge := -1
	if value != "" {
		maxAge = max(1, int(time.Until(expiry).Seconds()))
	} else {
		expiry = time.Unix(0, 0).UTC()
	}
	c.Cookie(&fiber.Cookie{
		Name: refreshCookie, Value: value, Path: cookiePath, Expires: expiry, MaxAge: maxAge,
		HTTPOnly: true, Secure: s.config.secureCookie, SameSite: fiber.CookieSameSiteStrictMode,
	})
}

func missingToUnauthorized(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.ErrUnauthorized
	}
	return err
}

func normalizeUsername(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
