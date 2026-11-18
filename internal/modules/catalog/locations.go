package catalog

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"zhonghui-api/internal/database/db"
	"zhonghui-api/internal/modules/auth"
)

type location struct {
	ID       pgtype.UUID `json:"id"`
	Name     string      `json:"name"`
	IsActive bool        `json:"isActive"`
}

func (s catalog) listLocations(c fiber.Ctx) error {
	p, err := readPage(c)
	if err != nil {
		return err
	}
	user := auth.CurrentUser(c)
	rows, err := s.db.ListLoadingLocations(c.Context(), db.ListLoadingLocationsParams{
		IsAdmin: user.Role == "ADMIN", OnlyAssigned: user.Role == "OPERATOR", UserID: user.ID,
		Search: p.search, PageLimit: p.limit, PageOffset: p.offset,
	})
	if err != nil {
		return err
	}
	data := make([]location, 0, len(rows))
	for _, row := range rows {
		data = append(data, location{row.ID, row.Name, row.IsActive})
	}
	return c.JSON(data)
}

func (s catalog) createLocation(c fiber.Ctx) error {
	var input struct {
		Name string `json:"name"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return fiber.ErrBadRequest
	}
	name := strings.TrimSpace(input.Name)
	if !validName(name) {
		return fiber.ErrBadRequest
	}
	ctx := c.Context()
	var row db.LoadingLocation
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.db.WithTx(tx)
		created, err := q.CreateLoadingLocation(ctx, name)
		if err != nil {
			return err
		}
		row = created
		return auditLocation(c, q, row.ID, "CREATE_LOCATION")
	})
	if err != nil {
		return conflictOnDuplicate(err)
	}
	return c.Status(fiber.StatusCreated).JSON(location{row.ID, row.Name, row.IsActive})
}

func (s catalog) updateLocation(c fiber.Ctx) error {
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	var input struct {
		Name     *string `json:"name"`
		IsActive *bool   `json:"isActive"`
	}
	if err := c.Bind().JSON(&input); err != nil || (input.Name == nil && input.IsActive == nil) {
		return fiber.ErrBadRequest
	}
	params := db.UpdateLoadingLocationParams{ID: id}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if !validName(name) {
			return fiber.ErrBadRequest
		}
		params.Name = pgtype.Text{String: name, Valid: true}
	}
	if input.IsActive != nil {
		params.IsActive = pgtype.Bool{Bool: *input.IsActive, Valid: true}
	}
	ctx := c.Context()
	var row db.LoadingLocation
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.db.WithTx(tx)
		updated, err := q.UpdateLoadingLocation(ctx, params)
		if err != nil {
			return err
		}
		row = updated
		return auditLocation(c, q, id, "UPDATE_LOCATION")
	})
	if err != nil {
		return conflictOnDuplicate(notFound(err))
	}
	return c.JSON(location{row.ID, row.Name, row.IsActive})
}

func (s catalog) listLocationUsers(c fiber.Ctx) error {
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	p, err := readPage(c)
	if err != nil {
		return err
	}
	if _, err := s.db.GetLoadingLocation(c.Context(), id); err != nil {
		return notFound(err)
	}
	rows, err := s.db.ListLocationUsers(c.Context(), db.ListLocationUsersParams{
		LocationID: id, PageLimit: p.limit, PageOffset: p.offset,
	})
	if err != nil {
		return err
	}
	data := make([]fiber.Map, 0, len(rows))
	for _, row := range rows {
		data = append(data, fiber.Map{"id": row.ID, "username": row.Username, "displayName": row.DisplayName,
			"role": row.Role, "isActive": row.IsActive})
	}
	return c.JSON(data)
}

func (s catalog) assignUser(c fiber.Ctx) error {
	return s.changeAssignment(c, true)
}

func (s catalog) removeUser(c fiber.Ctx) error {
	return s.changeAssignment(c, false)
}

func (s catalog) changeAssignment(c fiber.Ctx, assign bool) error {
	locationID, err := pathID(c, "id")
	if err != nil {
		return err
	}
	userID, err := pathID(c, "userId")
	if err != nil {
		return err
	}
	ctx := c.Context()
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.db.WithTx(tx)
		if !assign {
			changed, err := q.RemoveLocationUser(ctx, db.RemoveLocationUserParams{LoadingLocationID: locationID, UserID: userID})
			if err != nil || changed == 0 {
				return err
			}
			return auditLocation(c, q, locationID, "REMOVE_OPERATOR")
		}
		point, err := q.LockLoadingLocation(ctx, locationID)
		if err != nil {
			return err
		}
		user, err := q.LockUserForSession(ctx, userID)
		if err != nil {
			return err
		}
		if !point.IsActive || !user.IsActive || user.Role != "OPERATOR" {
			return fiber.ErrConflict
		}
		changed, err := q.AssignLocationUser(ctx, db.AssignLocationUserParams{LoadingLocationID: locationID, UserID: userID})
		if err != nil || changed == 0 {
			return err
		}
		return auditLocation(c, q, locationID, "ASSIGN_OPERATOR")
	})
	if err != nil {
		return notFound(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func auditLocation(c fiber.Ctx, q *db.Queries, id pgtype.UUID, action string) error {
	return q.AuditLocationChange(c.Context(), db.AuditLocationChangeParams{
		ActorUserID: auth.CurrentUser(c).ID, EntityID: id, Action: action,
		RequestID: pgtype.Text{String: requestid.FromContext(c), Valid: true},
	})
}

func pathID(c fiber.Ctx, key string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(c.Params(key)); err != nil {
		return id, fiber.ErrBadRequest
	}
	return id, nil
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.ErrNotFound
	}
	return err
}

func validName(name string) bool {
	return name != "" && utf8.RuneCountInString(name) <= 120
}
