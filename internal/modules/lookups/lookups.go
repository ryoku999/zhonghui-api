package lookups

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/singleflight"

	"zhonghui-api/internal/database/db"
	"zhonghui-api/internal/modules/auth"
)

var rucPattern = regexp.MustCompile(`^[0-9]{11}$`)
var dniPattern = regexp.MustCompile(`^[0-9]{8}$`)
var platePattern = regexp.MustCompile(`^[A-Z0-9]{6}$`)

type entry struct {
	data       any
	ttl        time.Duration
	verifiedAt time.Time
	status     int
}

type Service struct {
	client     provider
	db         *db.Queries
	logger     *slog.Logger
	mu         sync.Mutex
	cache      map[string]entry
	flight     singleflight.Group
	licenseKey []byte
}

func Register(app *fiber.App, pool *pgxpool.Pool, protected fiber.Handler, logger *slog.Logger) (*Service, error) {
	client, err := providerFromEnv()
	if err != nil {
		return nil, err
	}
	s := &Service{client: client, db: db.New(pool), logger: logger, cache: make(map[string]entry), licenseKey: licenseVerificationKey()}
	routes := app.Group("/api/v1/lookups", func(c fiber.Ctx) error {
		c.Set(fiber.HeaderCacheControl, "no-store")
		ctx, cancel := context.WithTimeout(c.Context(), 37*time.Second)
		defer cancel()
		c.SetContext(ctx)
		return c.Next()
	}, protected, func(c fiber.Ctx) error {
		role := auth.CurrentUser(c).Role
		if role != "ADMIN" && role != "OPERATOR" {
			return fiber.ErrForbidden
		}
		return c.Next()
	}, limiter.New(limiter.Config{
		Max: 15, Expiration: time.Minute,
		KeyGenerator: func(c fiber.Ctx) string { return auth.CurrentUser(c).ID.String() },
		LimitReached: func(fiber.Ctx) error { return fiber.ErrTooManyRequests },
	}))
	routes.Post("/sunat", s.handle("RUC"))
	routes.Post("/mtc", s.handle("DNI"))
	routes.Post("/sunarp", s.handle("PLATE"))
	return s, nil
}

func (s *Service) handle(kind string) fiber.Handler {
	return func(c fiber.Ctx) error {
		var input struct {
			RUC   string `json:"ruc"`
			DNI   string `json:"dni"`
			Plate string `json:"plate"`
		}
		if c.Bind().JSON(&input) != nil {
			return fiber.ErrBadRequest
		}
		var key string
		switch kind {
		case "RUC":
			key = strings.TrimSpace(input.RUC)
			if !rucPattern.MatchString(key) {
				return fiber.ErrBadRequest
			}
		case "DNI":
			key = strings.TrimSpace(input.DNI)
			if !dniPattern.MatchString(key) || key == strings.Repeat(key[:1], 8) {
				return fiber.ErrBadRequest
			}
		case "PLATE":
			key = normalizePlate(input.Plate)
			if !platePattern.MatchString(key) {
				return fiber.ErrBadRequest
			}
		}
		if s.client.token == "" {
			return fiber.NewError(fiber.StatusServiceUnavailable, "La consulta externa no está configurada.")
		}
		ctx := c.Context()
		result, err := s.lookup(ctx, kind, key)
		status, outcome := http.StatusOK, "FOUND"
		if result.status != 0 {
			status = result.status
		}
		var providerFailure providerError
		if errors.As(err, &providerFailure) {
			status = providerFailure.status
		}
		if result.data == nil {
			outcome = "NOT_FOUND"
		}
		if err != nil {
			outcome = "ERROR"
			if status == http.StatusOK {
				status = 0
			}
		}
		if auditErr := s.db.RecordExternalLookup(ctx, db.RecordExternalLookupParams{
			RequestedBy: auth.CurrentUser(c).ID, LookupType: kind, Outcome: outcome,
			StatusCode: pgtype.Int4{Int32: int32(status), Valid: status != 0},
		}); auditErr != nil {
			s.logger.Warn("could not record external lookup", "kind", kind)
		}
		if err != nil {
			if status == http.StatusUnprocessableEntity {
				return fiber.ErrBadRequest
			}
			if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
				if providerFailure.retryAfter != "" {
					c.Set("Retry-After", providerFailure.retryAfter)
				}
				return fiber.NewError(fiber.StatusServiceUnavailable, "El proveedor está ocupado. Inténtalo más tarde.")
			}
			return fiber.ErrBadGateway
		}
		if result.data != nil {
			if err := s.enrich(ctx, result); err != nil {
				return err
			}
		}
		data := result.data
		if driver, ok := data.(Driver); ok {
			type option struct {
				License
				Verification string `json:"verification"`
			}
			licenses := make([]option, 0, len(driver.Licenses))
			for _, row := range driver.Licenses {
				proof, err := s.signLicense(licenseVerification{
					OwnerID: auth.CurrentUser(c).ID.String(), DNI: driver.DNI, CheckedAt: driver.CheckedAt,
					ExpiresAt: time.Now().Add(7 * 24 * time.Hour).Unix(), License: row,
				})
				if err != nil {
					return err
				}
				licenses = append(licenses, option{License: row, Verification: proof})
			}
			data = struct {
				Driver
				Licenses []option `json:"licenses"`
			}{Driver: driver, Licenses: licenses}
		}
		return c.JSON(fiber.Map{"found": data != nil, "source": "APISTED", "data": data})
	}
}

func (s *Service) lookup(ctx context.Context, kind, key string) (entry, error) {
	cacheKey := kind + ":" + key
	if result, ok := s.cached(cacheKey); ok {
		return result, nil
	}
	done := s.flight.DoChan(cacheKey, func() (any, error) {
		if result, ok := s.cached(cacheKey); ok {
			return result, nil
		}
		result, err := s.client.fetch(ctx, kind, key)
		var failure providerError
		if kind == "RUC" && errors.As(err, &failure) && failure.status == http.StatusNotFound {
			result, err = entry{ttl: time.Minute, status: http.StatusNotFound}, nil
		}
		if err != nil {
			return nil, err
		}
		result.verifiedAt = time.Now().UTC()
		s.store(cacheKey, result)
		if vehicle, ok := result.data.(Vehicle); ok && vehicle.Plate != key {
			s.store(kind+":"+vehicle.Plate, result)
		}
		return result, nil
	})
	select {
	case result := <-done:
		if result.Err != nil {
			return entry{}, result.Err
		}
		return result.Val.(entry), nil
	case <-ctx.Done():
		return entry{}, ctx.Err()
	}
}

func (s *Service) cached(key string) (entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, ok := s.cache[key]
	if ok && time.Since(result.verifiedAt) >= result.ttl {
		delete(s.cache, key)
		return entry{}, false
	}
	return result, ok
}

func (s *Service) store(key string, value entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cache) >= 512 {
		var oldest string
		for candidate, item := range s.cache {
			if oldest == "" || item.verifiedAt.Before(s.cache[oldest].verifiedAt) {
				oldest = candidate
			}
		}
		delete(s.cache, oldest)
	}
	s.cache[key] = value
}

func normalizePlate(text string) string {
	return strings.NewReplacer("-", "", " ", "").Replace(strings.ToUpper(strings.TrimSpace(text)))
}

func (s *Service) EnrichFromCache(ctx context.Context, ruc, tractor, trailer string) {
	for _, key := range []string{"RUC:" + ruc, "PLATE:" + tractor, "PLATE:" + trailer} {
		if item, ok := s.cached(key); ok {
			if err := s.enrich(ctx, item); err != nil {
				s.logger.Warn("could not save verified catalog fields")
			}
		}
	}
}

func (s *Service) MatchesCached(ruc, dni, field, value string) bool {
	if value == "" {
		return false
	}
	switch field {
	case "ruc", "legalName":
		item, _ := s.cached("RUC:" + ruc)
		company, ok := item.data.(Company)
		return ok && (field == "ruc" && company.RUC == value || field == "legalName" && strings.TrimSpace(company.LegalName) == value)
	case "firstNames", "lastNames", "licenseNumber":
		item, _ := s.cached("DNI:" + dni)
		driver, ok := item.data.(Driver)
		if !ok {
			return false
		}
		switch field {
		case "firstNames":
			return strings.TrimSpace(driver.FirstNames) == value
		case "lastNames":
			return driver.LastNames == value
		default:
			for _, license := range driver.Licenses {
				if license.Number == value {
					return true
				}
			}
		}
	case "tractorPlate", "trailerPlate":
		item, _ := s.cached("PLATE:" + value)
		vehicle, ok := item.data.(Vehicle)
		return ok && vehicle.Plate == value
	}
	return false
}

func (s *Service) enrich(ctx context.Context, result entry) error {
	switch value := result.data.(type) {
	case Company:
		return s.db.EnrichCompany(ctx, db.EnrichCompanyParams{
			Ruc: value.RUC, TaxStatus: text(value.TaxStatus), TaxCondition: text(value.TaxCondition),
			FiscalAddress: text(value.Address), VerifiedAt: pgtype.Timestamptz{Time: result.verifiedAt, Valid: true},
		})
	case Driver:
		for _, row := range value.Licenses {
			if err := s.db.EnrichLicense(ctx, LicenseUpdate(value.DNI, pgtype.UUID{}, row, value.CheckedAt)); err != nil {
				return err
			}
		}
	case Vehicle:
		return s.db.EnrichVehicle(ctx, db.EnrichVehicleParams{
			Plate: value.Plate, Make: text(value.Make), Model: text(value.Model), Color: text(value.Color),
			ModelYear:          pgtype.Int2{Int16: value.ModelYear, Valid: value.ModelYear != 0},
			RegistrationStatus: text(value.RegistrationStatus),
		})
	}
	return nil
}

func text(value string) pgtype.Text {
	value = strings.TrimSpace(value)
	return pgtype.Text{String: value, Valid: value != "" && !strings.ContainsRune(value, 0)}
}
