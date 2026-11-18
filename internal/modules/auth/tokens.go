package auth

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	accessLifetime  = 15 * time.Minute
	sessionLifetime = 7 * 24 * time.Hour
)

type Config struct {
	issuer, audience string
	accessKey        []byte
	refreshKey       []byte
	secureCookie     bool
}

func ConfigFromEnv() (Config, error) {
	c := Config{issuer: os.Getenv("JWT_ISSUER"), audience: os.Getenv("JWT_AUDIENCE")}
	if c.issuer == "" || c.audience == "" {
		return Config{}, errors.New("JWT_ISSUER and JWT_AUDIENCE are required")
	}
	var err error
	c.accessKey, err = base64.StdEncoding.DecodeString(os.Getenv("JWT_ACCESS_SECRET"))
	if err != nil || len(c.accessKey) < 32 {
		return Config{}, errors.New("JWT_ACCESS_SECRET must encode at least 32 random bytes in Base64")
	}
	c.refreshKey, err = base64.StdEncoding.DecodeString(os.Getenv("JWT_REFRESH_SECRET"))
	if err != nil || len(c.refreshKey) < 32 || bytes.Equal(c.accessKey, c.refreshKey) {
		return Config{}, errors.New("JWT_REFRESH_SECRET must encode at least 32 random bytes and differ from JWT_ACCESS_SECRET")
	}
	c.secureCookie, err = strconv.ParseBool(os.Getenv("COOKIE_SECURE"))
	if err != nil || (!c.secureCookie && os.Getenv("APP_ENV") != "development") {
		return Config{}, errors.New("COOKIE_SECURE must be true outside development")
	}
	return c, nil
}

type claims struct {
	jwt.RegisteredClaims
	SessionID string `json:"sid"`
	Type      string `json:"type"`
	Role      string `json:"role,omitempty"`
}

func (c Config) parserOptions() []jwt.ParserOption {
	return []jwt.ParserOption{
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(c.issuer), jwt.WithAudience(c.audience),
		jwt.WithExpirationRequired(), jwt.WithIssuedAt(),
	}
}

func (c Config) sign(kind, role string, userID, sessionID pgtype.UUID, now, expiry time.Time) (string, error) {
	key := c.accessKey
	if kind == "refresh" {
		key = c.refreshKey
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: c.issuer, Audience: jwt.ClaimStrings{c.audience}, Subject: userID.String(),
			ID: uuid.NewString(), IssuedAt: jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(expiry),
		},
		SessionID: sessionID.String(), Type: kind, Role: role,
	}).SignedString(key)
}

func (c Config) parseRefresh(raw string) (*claims, error) {
	parsed := new(claims)
	_, err := jwt.ParseWithClaims(raw, parsed, func(*jwt.Token) (any, error) {
		return c.refreshKey, nil
	}, c.parserOptions()...)
	return parsed, err
}

func (c *claims) ids(kind string) (userID, sessionID pgtype.UUID, ok bool) {
	if c.Type != kind || c.IssuedAt == nil || c.NotBefore == nil || c.ID == "" {
		return userID, sessionID, false
	}
	return userID, sessionID, userID.Scan(c.Subject) == nil && sessionID.Scan(c.SessionID) == nil
}
