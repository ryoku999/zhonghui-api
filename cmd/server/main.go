package main

import (
	"bufio"
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
	"github.com/jackc/pgx/v5/pgxpool"

	"zhonghui-api/internal/modules/auth"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return errors.New("invalid DATABASE_URL")
	}
	config.ConnConfig.ConnectTimeout = 3 * time.Second
	config.ConnConfig.RuntimeParams["timezone"] = "UTC"
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return errors.New("could not initialize PostgreSQL pool")
	}
	defer pool.Close()

	if len(os.Args) > 1 {
		if len(os.Args) != 4 || os.Args[1] != "bootstrap-admin" {
			return errors.New("usage: bootstrap-admin <username> <display-name>")
		}
		password := bufio.NewScanner(os.Stdin)
		if !password.Scan() {
			return errors.New("administrator password is required on stdin")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := auth.BootstrapAdmin(ctx, pool, os.Args[2], os.Args[3], password.Text()); err != nil {
			return err
		}
		logger.Info("first administrator created")
		return nil
	}

	authConfig, err := auth.ConfigFromEnv()
	if err != nil {
		return err
	}
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer listener.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app := newApp(pool, logger, authConfig)
	logger.Info("starting HTTP server", "address", listener.Addr(), "environment", os.Getenv("APP_ENV"))
	return app.Listener(listener, fiber.ListenConfig{DisableStartupMessage: true, GracefulContext: ctx})
}

func newApp(pool *pgxpool.Pool, logger *slog.Logger, authConfig auth.Config) *fiber.App {
	app := fiber.New(fiber.Config{
		BodyLimit:    1024 * 1024,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
		ErrorHandler: func(c fiber.Ctx, err error) error {
			status := fiber.StatusInternalServerError
			var fiberError *fiber.Error
			if errors.As(err, &fiberError) {
				status = fiberError.Code
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
				code, message = "CONFLICT", "El usuario ya existe o el cambio dejaría la aplicación sin un administrador activo."
			case fiber.StatusTooManyRequests:
				code, message = "RATE_LIMITED", "Demasiados intentos. Intenta nuevamente en un minuto."
			case fiber.StatusNotFound:
				code = "NOT_FOUND"
			case fiber.StatusServiceUnavailable:
				code, message = "DATABASE_UNAVAILABLE", "La base de datos no está disponible."
			case fiber.StatusInternalServerError:
				code = "INTERNAL_ERROR"
			}
			if status >= 500 {
				logger.Error("request failed", "requestId", requestid.FromContext(c), "status", status, "code", code)
			}
			c.Set(fiber.HeaderCacheControl, "no-store")
			return c.Status(status).JSON(fiber.Map{
				"code": code, "message": message, "requestId": requestid.FromContext(c),
			})
		},
	})
	app.Use(func(c fiber.Ctx) error {
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
		app.Use(cors.New(cors.Config{AllowOrigins: origins, AllowCredentials: true}))
	}
	auth.Register(app, pool, authConfig, origins)

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
	return app
}
