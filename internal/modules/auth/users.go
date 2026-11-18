package auth

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"zhonghui-api/internal/database/db"
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{2,63}$`)
var errLastAdmin = errors.New("at least one active administrator must remain")

type newUser struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Password    string `json:"password"`
	Role        string `json:"role"`
}

func (input newUser) params() (db.CreateUserParams, error) {
	name := normalizeUsername(input.Username)
	display := strings.TrimSpace(input.DisplayName)
	if !usernamePattern.MatchString(name) || !validDisplayName(display) || !validRole(input.Role) ||
		len(input.Password) > 256 || utf8.RuneCountInString(input.Password) < 12 {
		return db.CreateUserParams{}, errors.New("invalid username, display name, role or password (minimum 12 characters)")
	}
	return db.CreateUserParams{
		Username: name, DisplayName: display, PasswordHash: hashPassword(input.Password), Role: input.Role,
	}, nil
}

func (s *service) listUsers(c fiber.Ctx) error {
	limit, limitErr := strconv.ParseInt(c.Query("limit", "50"), 10, 32)
	offset, offsetErr := strconv.ParseInt(c.Query("offset", "0"), 10, 32)
	active, activeErr := strconv.ParseBool(c.Query("onlyActive", "false"))
	role := c.Query("role")
	search := strings.TrimSpace(c.Query("q"))
	if limitErr != nil || offsetErr != nil || activeErr != nil || limit < 1 || limit > 100 ||
		offset < 0 || len(search) > 200 || (role != "" && !validRole(role)) {
		return fiber.ErrBadRequest
	}
	rows, err := s.db.ListUsers(c.Context(), db.ListUsersParams{
		PageLimit: int32(limit), PageOffset: int32(offset), RoleFilter: role, OnlyActive: active, Search: search,
	})
	if err != nil {
		return err
	}
	users := make([]userResponse, 0, len(rows))
	for _, u := range rows {
		users = append(users, userResponse{u.ID.String(), u.Username, u.DisplayName, u.Role, u.IsActive})
	}
	return c.JSON(users)
}

func (s *service) createUser(c fiber.Ctx) error {
	var input newUser
	if err := c.Bind().JSON(&input); err != nil {
		return fiber.ErrBadRequest
	}
	params, err := input.params()
	if err != nil {
		return fiber.ErrBadRequest
	}
	ctx := c.Context()
	var user db.User
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.db.WithTx(tx)
		user, err = q.CreateUser(ctx, params)
		if err != nil {
			return err
		}
		return audit(c, q, user.ID, "CREATE_USER")
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fiber.ErrConflict
	}
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(publicUser(user))
}

func (s *service) updateUser(c fiber.Ctx) error {
	var id pgtype.UUID
	if err := id.Scan(c.Params("id")); err != nil {
		return fiber.ErrBadRequest
	}
	var input struct {
		DisplayName *string `json:"displayName"`
		Role        *string `json:"role"`
		IsActive    *bool   `json:"isActive"`
	}
	if err := c.Bind().JSON(&input); err != nil || (input.DisplayName == nil && input.Role == nil && input.IsActive == nil) {
		return fiber.ErrBadRequest
	}
	params := db.UpdateUserParams{ID: id}
	if input.DisplayName != nil {
		display := strings.TrimSpace(*input.DisplayName)
		if !validDisplayName(display) {
			return fiber.ErrBadRequest
		}
		params.DisplayName = pgtype.Text{String: display, Valid: true}
	}
	if input.Role != nil {
		if !validRole(*input.Role) {
			return fiber.ErrBadRequest
		}
		params.Role = pgtype.Text{String: *input.Role, Valid: true}
	}
	if input.IsActive != nil {
		params.IsActive = pgtype.Bool{Bool: *input.IsActive, Valid: true}
	}
	ctx := c.Context()
	var user db.User
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.db.WithTx(tx)
		if err := q.LockUserManagement(ctx); err != nil {
			return err
		}
		var err error
		user, err = q.UpdateUser(ctx, params)
		if err != nil {
			return err
		}
		adminCount, err := q.CountActiveAdmins(ctx)
		if err != nil {
			return err
		}
		if adminCount == 0 {
			return errLastAdmin
		}
		if !user.IsActive {
			if err := q.RevokeUserSessions(ctx, user.ID); err != nil {
				return err
			}
		}
		return audit(c, q, user.ID, "UPDATE_USER")
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return fiber.ErrNotFound
	case errors.Is(err, errLastAdmin):
		return fiber.ErrConflict
	case err != nil:
		return err
	default:
		return c.JSON(publicUser(user))
	}
}

func audit(c fiber.Ctx, q *db.Queries, userID pgtype.UUID, action string) error {
	actor := fiber.Locals[db.User](c, authenticatedUserKey{})
	return q.AuditUserChange(c.Context(), db.AuditUserChangeParams{
		ActorUserID: actor.ID, EntityID: userID, Action: action,
		RequestID: pgtype.Text{String: requestid.FromContext(c), Valid: true},
	})
}

func validRole(role string) bool {
	return role == "ADMIN" || role == "OPERATOR" || role == "DOCUMENTATION"
}

func validDisplayName(name string) bool {
	return name != "" && utf8.RuneCountInString(name) <= 120
}
