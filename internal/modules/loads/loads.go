package loads

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"zhonghui-api/internal/database/db"
	"zhonghui-api/internal/modules/auth"
	"zhonghui-api/internal/modules/lookups"
)

type service struct {
	pool    *pgxpool.Pool
	db      *db.Queries
	lima    *time.Location
	lookups *lookups.Service
}

func Register(app *fiber.App, pool *pgxpool.Pool, protected fiber.Handler, external *lookups.Service) error {
	lima, err := time.LoadLocation("America/Lima")
	if err != nil {
		return err
	}
	s := service{pool: pool, db: db.New(pool), lima: lima, lookups: external}
	routes := app.Group("/api/v1/loads")
	routes.Post("/", auth.RequestContextWithTimeout(8*time.Second), protected, s.create)
	routes.Get("/", auth.RequestContext, protected, s.list)
	routes.Get("/:id", auth.RequestContext, protected, s.detail)
	routes.Patch("/:id", auth.RequestContext, protected, s.update)
	app.Get("/api/v1/dashboard", auth.RequestContext, protected, s.dashboard)
	app.Get("/api/v1/audit", auth.RequestContext, protected, auth.RequireAdmin, s.auditEntries)
	app.Get("/api/v1/reports/loads.xlsx", auth.RequestContext, protected, s.export)
	return nil
}

func (s service) create(c fiber.Ctx) error {
	actor := auth.CurrentUser(c)
	if !canWrite(actor) {
		return fiber.ErrForbidden
	}
	key, valid := readUUID(c.Get("Idempotency-Key"))
	if !valid {
		return fiber.ErrBadRequest
	}
	var input createInput
	if err := c.Bind().JSON(&input); err != nil {
		return fiber.ErrBadRequest
	}
	locationID, valid := input.normalize()
	if !valid {
		return fiber.ErrBadRequest
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(encoded)
	ctx := c.Context()
	dni := ""
	if input.DocumentType == "DNI" {
		dni = input.DocumentNumber
	}
	external := make(map[string]bool, len(input.ExternalFields))
	externalValues := map[string]string{
		"ruc": input.RUC, "legalName": input.LegalName, "firstNames": input.FirstNames, "lastNames": input.LastNames,
		"licenseNumber": input.LicenseNumber, "tractorPlate": input.TractorPlate, "trailerPlate": input.TrailerPlate,
	}
	for _, field := range input.ExternalFields {
		external[field] = s.lookups.MatchesCached(input.RUC, dni, field, externalValues[field])
	}
	var result db.GetLoadResultRow
	replayed := false
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.db.WithTx(tx)
		if err := q.LockLoadRequest(ctx, actor.ID.String()+":"+key.String()); err != nil {
			return err
		}
		point, err := q.LockLoadingLocation(ctx, locationID)
		if err != nil {
			return notFound(err)
		}
		if err := lockAssignment(c, q, actor, locationID); err != nil {
			return err
		}
		prior, err := q.FindLoadRequest(ctx, db.FindLoadRequestParams{UserID: actor.ID, IdempotencyKey: key})
		if err == nil {
			if !bytes.Equal(prior.RequestHash, digest[:]) {
				return fiber.ErrConflict
			}
			replayed = true
			result, err = q.GetLoadResult(ctx, db.GetLoadResultParams{ID: prior.LoadRecordID, Original: true})
			result.Status = "REGISTERED"
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if !point.IsActive {
			return fiber.ErrConflict
		}

		data := snapshot{createInput: input, LocationName: point.Name}
		data.ExternalFields = nil
		record := db.CreateLoadRecordParams{
			LoadingLocationID: locationID, CreatedBy: actor.ID,
			UnitDescription: optionalText(input.UnitDescription), Notes: optionalText(input.Notes),
		}
		history, err := resolveParties(ctx, q, &data, &record)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		year, month, day := now.In(s.lima).Date()
		record.BusinessDate = pgtype.Date{Time: time.Date(year, month, day, 0, 0, 0, 0, time.UTC), Valid: true}
		record.RecordedAt = pgtype.Timestamptz{Time: now, Valid: true}
		record.OrderNumber, err = q.NextLoadOrder(ctx, db.NextLoadOrderParams{
			LoadingLocationID: locationID, BusinessDate: record.BusinessDate,
		})
		if err != nil {
			return err
		}
		id, err := q.CreateLoadRecord(ctx, record)
		if err != nil {
			return err
		}
		if err := saveSnapshot(c, q, id, 1, data); err != nil {
			return err
		}
		history["locationId"], history["locationName"] = "HISTORY", "HISTORY"
		fields := []string{
			"locationId", "locationName", "ruc", "legalName", "documentType", "documentNumber",
			"firstNames", "lastNames", "licenseNumber", "phone", "tractorPlate",
			"unitDescription", "notes",
		}
		if input.TrailerPlate != "" {
			fields = append(fields, "trailerPlate", "trailerType")
		}
		provenance := db.WriteLoadProvenanceParams{
			LoadRecordID: id, Version: 1, FieldNames: fields,
			Sources: make([]string, len(fields)), VerificationStatuses: make([]string, len(fields)),
		}
		for i, field := range fields {
			source := history[field]
			verification := "UNVERIFIED"
			if source == "" {
				source = "MANUAL"
			}
			if external[field] {
				source, verification = "EXTERNAL_API", "VERIFIED"
			}
			provenance.Sources[i] = source
			provenance.VerificationStatuses[i] = verification
		}
		if err := q.WriteLoadProvenance(ctx, provenance); err != nil {
			return err
		}
		if err := q.SaveLoadRequest(ctx, db.SaveLoadRequestParams{
			UserID: actor.ID, IdempotencyKey: key, LoadRecordID: id, RequestHash: digest[:],
		}); err != nil {
			return err
		}
		if err := audit(c, q, actor.ID, id, "CREATE_LOAD"); err != nil {
			return err
		}
		result, err = q.GetLoadResult(ctx, db.GetLoadResultParams{ID: id})
		return err
	})
	if err != nil {
		return err
	}
	if !replayed {
		c.Status(fiber.StatusCreated)
	}
	s.lookups.EnrichFromCache(ctx, input.RUC, dni, input.TractorPlate, input.TrailerPlate)
	return respond(c, result)
}

func (s service) detail(c fiber.Ctx) error {
	id, valid := readUUID(c.Params("id"))
	if !valid {
		return fiber.ErrBadRequest
	}
	row, err := s.db.GetLoadResult(c.Context(), db.GetLoadResultParams{ID: id})
	if err != nil {
		return notFound(err)
	}
	actor := auth.CurrentUser(c)
	if actor.Role == "OPERATOR" {
		assigned, err := s.db.HasLoadAssignment(c.Context(), db.HasLoadAssignmentParams{UserID: actor.ID, LoadingLocationID: row.LoadingLocationID})
		if err != nil {
			return err
		}
		if !assigned {
			return fiber.ErrNotFound
		}
	}
	return respond(c, row)
}

func (s service) update(c fiber.Ctx) error {
	actor := auth.CurrentUser(c)
	if !canWrite(actor) {
		return fiber.ErrForbidden
	}
	id, valid := readUUID(c.Params("id"))
	if !valid {
		return fiber.ErrBadRequest
	}
	var input struct {
		Version         int32   `json:"version"`
		UnitDescription *string `json:"unitDescription"`
		Notes           *string `json:"notes"`
		Status          *string `json:"status"`
	}
	if err := c.Bind().JSON(&input); err != nil || input.Version < 1 ||
		(input.UnitDescription == nil && input.Notes == nil && input.Status == nil) ||
		(input.Status != nil && *input.Status != "CANCELLED") {
		return fiber.ErrBadRequest
	}
	ctx := c.Context()
	var result db.GetLoadResultRow
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.db.WithTx(tx)
		row, err := q.LockLoadRecord(ctx, id)
		if err != nil {
			return notFound(err)
		}
		if actor.Role == "OPERATOR" && actor.ID != row.CreatedBy {
			return fiber.ErrForbidden
		}
		if err := lockAssignment(c, q, actor, row.LoadingLocationID); err != nil {
			return err
		}
		if row.Version != input.Version || row.Status == "CANCELLED" {
			return fiber.ErrConflict
		}
		current, err := q.GetLoadResult(ctx, db.GetLoadResultParams{ID: id})
		if err != nil {
			return err
		}
		var data snapshot
		if err := json.Unmarshal(current.Fields, &data); err != nil {
			return err
		}
		if input.UnitDescription != nil {
			data.UnitDescription = strings.TrimSpace(*input.UnitDescription)
		}
		if input.Notes != nil {
			data.Notes = strings.TrimSpace(*input.Notes)
		}
		if !within(data.UnitDescription, 200) || !within(data.Notes, 1000) {
			return fiber.ErrBadRequest
		}
		status, action := row.Status, "UPDATE_LOAD"
		if input.Status != nil {
			status, action = "CANCELLED", "CANCEL_LOAD"
		}
		version, err := q.UpdateLoadRecord(ctx, db.UpdateLoadRecordParams{
			ID: id, Status: status, UnitDescription: optionalText(data.UnitDescription), Notes: optionalText(data.Notes),
		})
		if err != nil {
			return err
		}
		if err := saveSnapshot(c, q, id, version, data); err != nil {
			return err
		}
		if err := q.CopyLoadProvenance(ctx, db.CopyLoadProvenanceParams{
			LoadID: id, PreviousVersion: row.Version, NextVersion: version,
		}); err != nil {
			return err
		}
		if err := audit(c, q, actor.ID, id, action); err != nil {
			return err
		}
		result, err = q.GetLoadResult(ctx, db.GetLoadResultParams{ID: id})
		return err
	})
	if err != nil {
		return err
	}
	return respond(c, result)
}

func lockAssignment(c fiber.Ctx, q *db.Queries, actor db.User, locationID pgtype.UUID) error {
	if actor.Role == "ADMIN" {
		return nil
	}
	_, err := q.LockLoadAssignment(c.Context(), db.LockLoadAssignmentParams{UserID: actor.ID, LoadingLocationID: locationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.ErrForbidden
	}
	return err
}

func canWrite(actor db.User) bool {
	return actor.Role == "ADMIN" || actor.Role == "OPERATOR"
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.ErrNotFound
	}
	return err
}

func saveSnapshot(c fiber.Ctx, q *db.Queries, id pgtype.UUID, version int32, data snapshot) error {
	fields, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return q.WriteLoadSnapshot(c.Context(), db.WriteLoadSnapshotParams{LoadRecordID: id, Version: version, Fields: fields})
}

func audit(c fiber.Ctx, q *db.Queries, actor, id pgtype.UUID, action string) error {
	return q.AuditLoadChange(c.Context(), db.AuditLoadChangeParams{
		ActorUserID: actor, EntityID: id, Action: action,
		RequestID: pgtype.Text{String: requestid.FromContext(c), Valid: true},
	})
}

func respond(c fiber.Ctx, row db.GetLoadResultRow) error {
	return c.JSON(fiber.Map{
		"id": row.ID, "locationId": row.LoadingLocationID, "businessDate": row.BusinessDate,
		"orderNumber": row.OrderNumber, "createdAt": row.CreatedAt.Time.UTC(), "createdBy": row.CreatedBy,
		"status": row.Status, "version": row.Version, "snapshot": json.RawMessage(row.Fields),
	})
}
