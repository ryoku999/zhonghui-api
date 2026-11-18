package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"zhonghui-api/internal/config"
	"zhonghui-api/internal/modules/auth"
	"zhonghui-api/internal/modules/catalog"
	"zhonghui-api/internal/modules/loads"
	"zhonghui-api/internal/modules/lookups"
)

type requestStartedKey struct{}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	if err := config.LoadEnv(); err != nil {
		return err
	}
	poolConfig, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		return errors.New("invalid DATABASE_URL")
	}
	poolConfig.ConnConfig.ConnectTimeout = 3 * time.Second
	poolConfig.ConnConfig.RuntimeParams["timezone"] = "UTC"
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		return errors.New("could not initialize PostgreSQL pool")
	}
	defer pool.Close()

	authConfig, err := auth.ConfigFromEnv()
	if err != nil {
		return err
	}
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
		if port := os.Getenv("PORT"); port != "" {
			addr = net.JoinHostPort("0.0.0.0", port)
		}
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer listener.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app, err := newApp(pool, logger, authConfig)
	if err != nil {
		return err
	}
	logger.Info("starting HTTP server", "address", listener.Addr(), "environment", os.Getenv("APP_ENV"))
	return app.Listener(listener, fiber.ListenConfig{DisableStartupMessage: true, GracefulContext: ctx})
}

func newApp(pool *pgxpool.Pool, logger *slog.Logger, authConfig auth.Config) (*fiber.App, error) {
	app := fiber.New(fiber.Config{
		BodyLimit:    1024 * 1024,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 40 * time.Second,
		IdleTimeout:  30 * time.Second,
		ErrorHandler: func(c fiber.Ctx, err error) error {
			status := fiber.StatusInternalServerError
			var fiberError *fiber.Error
			if errors.As(err, &fiberError) {
				status = fiberError.Code
			} else if errors.Is(err, context.DeadlineExceeded) || pgconn.Timeout(err) {
				status = fiber.StatusGatewayTimeout
			}
			code := "HTTP_ERROR"
			message := "No se pudo procesar la solicitud."
			switch status {
			case fiber.StatusBadRequest:
				code, message = "INVALID_REQUEST", "Revisa los datos enviados."
			case fiber.StatusUnauthorized:
				code, message = "UNAUTHORIZED", "Las credenciales o la sesión no son válidas."
			case fiber.StatusForbidden:
				code, message = "FORBIDDEN", "No tienes permiso para realizar esta acción."
			case fiber.StatusConflict:
				code, message = "CONFLICT", "El registro ya existe o su estado actual impide el cambio."
			case fiber.StatusUnprocessableEntity:
				code, message = "EXPORT_LIMIT", "La exportación supera los 5 000 registros. Acota los filtros."
			case fiber.StatusTooManyRequests:
				code, message = "RATE_LIMITED", "Demasiados intentos. Intenta nuevamente en un minuto."
			case fiber.StatusNotFound:
				code = "NOT_FOUND"
			case fiber.StatusServiceUnavailable:
				code, message = "DATABASE_UNAVAILABLE", "La base de datos no está disponible."
				if fiberError != fiber.ErrServiceUnavailable {
					code, message = "LOOKUP_UNAVAILABLE", fiberError.Message
				}
			case fiber.StatusBadGateway:
				code, message = "PROVIDER_ERROR", "No se pudo obtener una respuesta válida del proveedor."
			case fiber.StatusGatewayTimeout:
				code, message = "DEADLINE_EXCEEDED", "El servidor tardó demasiado en responder. Puedes reintentar."
			case fiber.StatusInternalServerError:
				code = "INTERNAL_ERROR"
			}
			if status >= 500 {
				fields := []any{"requestId", requestid.FromContext(c), "status", status, "code", code,
					"method", c.Method(), "route", c.Route().Path}
				if started, ok := c.Locals(requestStartedKey{}).(time.Time); ok {
					fields = append(fields, "durationMs", time.Since(started).Milliseconds())
				}
				var databaseError *pgconn.PgError
				if errors.As(err, &databaseError) {
					fields = append(fields, "sqlState", databaseError.Code, "constraint", databaseError.ConstraintName)
				}
				logger.Error("request failed", fields...)
			}
			c.Set(fiber.HeaderCacheControl, "no-store")
			return c.Status(status).JSON(fiber.Map{
				"code": code, "message": message, "requestId": requestid.FromContext(c),
			})
		},
	})
	app.Use(func(c fiber.Ctx) error {
		c.Locals(requestStartedKey{}, time.Now())
		// Keep request IDs in the logs under server control.
		c.Request().Header.Del(fiber.HeaderXRequestID)
		return c.Next()
	})
	app.Use(requestid.New())
	app.Use(recover.New())

	var origins []string
	for origin := range strings.SplitSeq(os.Getenv("CORS_ALLOWED_ORIGINS"), ",") {
		origin = strings.TrimSpace(origin)
		if strings.HasPrefix(origin, "http://") || strings.HasPrefix(origin, "https://") {
			if !strings.Contains(origin, "*") {
				origins = append(origins, origin)
			}
		}
	}
	if len(origins) > 0 {
		app.Use(cors.New(cors.Config{
			AllowOrigins: origins, AllowCredentials: true,
			AllowHeaders: []string{fiber.HeaderContentType, fiber.HeaderAuthorization, "Idempotency-Key"},
		}))
	}
	protected := auth.Register(app, pool, authConfig, origins)
	catalog.Register(app, pool, protected)
	external, err := lookups.Register(app, pool, protected, logger)
	if err != nil {
		return nil, err
	}
	if err := loads.Register(app, pool, protected, external); err != nil {
		return nil, err
	}

	health := app.Group("/api/v1/health")
	health.Get("/live", func(c fiber.Ctx) error {
		c.Set(fiber.HeaderCacheControl, "no-store")
		return c.JSON(fiber.Map{"status": "ok"})
	})
	health.Get("/ready", func(c fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			return fiber.ErrServiceUnavailable
		}
		c.Set(fiber.HeaderCacheControl, "no-store")
		return c.JSON(fiber.Map{"status": "ready"})
	})
	return app, nil
}
