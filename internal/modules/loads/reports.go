package loads

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgtype"
	"zhonghui-api/internal/database/db"
	"zhonghui-api/internal/modules/auth"
)

const dateLayout = "2006-01-02"

type reportRow struct {
	ID           pgtype.UUID `json:"id"`
	LocationID   pgtype.UUID `json:"locationId"`
	BusinessDate string      `json:"businessDate"`
	CreatedAt    time.Time   `json:"createdAt"`
	OrderNumber  int32       `json:"orderNumber"`
	Status       string      `json:"status"`
	Registrar    string      `json:"registrar"`
	LocationName string      `json:"locationName"`
	RUC          string      `json:"ruc"`
	LegalName    string      `json:"legalName"`
	DocumentType string      `json:"documentType"`
	Document     string      `json:"documentNumber"`
	Driver       string      `json:"driver"`
	License      string      `json:"licenseNumber"`
	Phone        string      `json:"phone"`
	Tractor      string      `json:"tractorPlate"`
	Trailer      string      `json:"trailerPlate"`
	Unit         string      `json:"unitDescription"`
}

func reportRows(rows []db.SearchLoadRecordsRow) ([]reportRow, error) {
	data := make([]reportRow, 0, len(rows))
	for _, row := range rows {
		var fields snapshot
		if err := json.Unmarshal(row.Fields, &fields); err != nil {
			return nil, err
		}
		data = append(data, reportRow{
			ID: row.ID, LocationID: row.LoadingLocationID,
			BusinessDate: row.BusinessDate.Time.Format(dateLayout), CreatedAt: row.CreatedAt.Time.UTC(),
			OrderNumber: row.OrderNumber, Status: row.Status, Registrar: row.Registrar,
			LocationName: fields.LocationName, RUC: fields.RUC, LegalName: fields.LegalName,
			DocumentType: fields.DocumentType, Document: fields.DocumentNumber,
			Driver:  strings.TrimSpace(fields.FirstNames + " " + fields.LastNames),
			License: fields.LicenseNumber, Phone: fields.Phone, Tractor: fields.TractorPlate,
			Trailer: fields.TrailerPlate, Unit: fields.UnitDescription,
		})
	}
	return data, nil
}

func reportFilters(c fiber.Ctx) (db.SearchLoadRecordsParams, error) {
	user := auth.CurrentUser(c)
	filter := db.SearchLoadRecordsParams{
		AssignedOnly: user.Role == "OPERATOR", ViewerID: user.ID, LoadIds: []pgtype.UUID{},
		Sort: c.Query("sort", "date"), Ruc: strings.TrimSpace(c.Query("ruc")),
		Driver: strings.TrimSpace(c.Query("driver")), Registrar: strings.TrimSpace(c.Query("registrar")),
	}
	var err error
	filter.DateFrom, filter.DateTo, err = readDateRange(c)
	if err != nil {
		return filter, err
	}
	switch filter.Sort {
	case "date", "order", "location":
	default:
		return filter, fiber.ErrBadRequest
	}
	switch c.Query("direction", "desc") {
	case "desc":
		filter.Descending = true
	case "asc":
	default:
		return filter, fiber.ErrBadRequest
	}
	if raw := c.Query("locationId"); raw != "" {
		id, valid := readUUID(raw)
		if !valid {
			return filter, fiber.ErrBadRequest
		}
		filter.LocationID = id
	}
	if raw := c.Query("orderNumber"); raw != "" {
		number, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || number < 1 {
			return filter, fiber.ErrBadRequest
		}
		filter.OrderNumber = pgtype.Int4{Int32: int32(number), Valid: true}
	}
	normalizer := strings.NewReplacer(" ", "", "-", "")
	filter.Tractor = normalizer.Replace(strings.ToUpper(strings.TrimSpace(c.Query("tractorPlate"))))
	filter.Trailer = normalizer.Replace(strings.ToUpper(strings.TrimSpace(c.Query("trailerPlate"))))
	for _, value := range []string{filter.Ruc, filter.Driver, filter.Registrar, filter.Tractor, filter.Trailer} {
		if !within(value, 120) {
			return filter, fiber.ErrBadRequest
		}
	}
	return filter, nil
}

func readDateRange(c fiber.Ctx) (pgtype.Date, pgtype.Date, error) {
	parse := func(raw string) (pgtype.Date, error) {
		if raw == "" {
			return pgtype.Date{}, nil
		}
		date, err := time.Parse(dateLayout, raw)
		if err != nil || date.Format(dateLayout) != raw || date.Year() < 1 {
			return pgtype.Date{}, fiber.ErrBadRequest
		}
		return pgtype.Date{Time: date, Valid: true}, nil
	}
	from, err := parse(c.Query("from"))
	if err != nil {
		return pgtype.Date{}, pgtype.Date{}, err
	}
	to, err := parse(c.Query("to"))
	if err != nil || (from.Valid && to.Valid && to.Time.Before(from.Time)) {
		return pgtype.Date{}, pgtype.Date{}, fiber.ErrBadRequest
	}
	return from, to, nil
}

func readPagination(c fiber.Ctx) (int32, int32, error) {
	limit, limitErr := strconv.ParseInt(c.Query("limit", "25"), 10, 32)
	offset, offsetErr := strconv.ParseInt(c.Query("offset", "0"), 10, 32)
	if limitErr != nil || offsetErr != nil || limit < 1 || limit > 100 || offset < 0 {
		return 0, 0, fiber.ErrBadRequest
	}
	return int32(limit), int32(offset), nil
}

func paged[T any](rows []T, limit int32) fiber.Map {
	hasNext := len(rows) > int(limit)
	if hasNext {
		rows = rows[:limit]
	}
	return fiber.Map{"items": rows, "hasNext": hasNext}
}

func (s service) list(c fiber.Ctx) error {
	filter, err := reportFilters(c)
	if err != nil {
		return err
	}
	limit, offset, err := readPagination(c)
	if err != nil {
		return err
	}
	filter.RowLimit, filter.RowOffset = limit+1, offset
	rows, err := s.db.SearchLoadRecords(c.Context(), filter)
	if err != nil {
		return err
	}
	data, err := reportRows(rows)
	if err != nil {
		return err
	}
	return c.JSON(paged(data, limit))
}

func (s service) dashboard(c fiber.Ctx) error {
	now := time.Now().In(s.lima)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	from := pgtype.Date{Time: today.AddDate(0, 0, -6), Valid: true}
	to := pgtype.Date{Time: today, Valid: true}
	user := auth.CurrentUser(c)
	summary, err := s.db.LoadDashboardSummary(c.Context(), db.LoadDashboardSummaryParams{
		DateFrom: from, DateTo: to, AssignedOnly: user.Role == "OPERATOR", ViewerID: user.ID,
	})
	if err != nil {
		return err
	}
	rows, err := s.db.SearchLoadRecords(c.Context(), db.SearchLoadRecordsParams{
		DateFrom: from, DateTo: to, AssignedOnly: user.Role == "OPERATOR", ViewerID: user.ID,
		LoadIds: []pgtype.UUID{}, Sort: "date", Descending: true, RowLimit: 8,
	})
	if err != nil {
		return err
	}
	recent, err := reportRows(rows)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{
		"from": from.Time.Format(dateLayout), "to": to.Time.Format(dateLayout),
		"total": summary.Total, "registered": summary.Registered, "cancelled": summary.Cancelled,
		"locationsWithActivity": summary.ActiveLocations, "recent": recent,
	})
}

var auditFilter = regexp.MustCompile(`^[A-Za-z_]{0,64}$`)

func (s service) auditEntries(c fiber.Ctx) error {
	from, to, err := readDateRange(c)
	if err != nil {
		return err
	}
	limit, offset, err := readPagination(c)
	if err != nil {
		return err
	}
	filter := db.SearchAuditEntriesParams{
		ActionFilter: strings.TrimSpace(c.Query("action")), EntityFilter: strings.TrimSpace(c.Query("entity")),
		RowLimit: limit + 1, RowOffset: offset,
	}
	if !auditFilter.MatchString(filter.ActionFilter) || !auditFilter.MatchString(filter.EntityFilter) {
		return fiber.ErrBadRequest
	}
	limaMidnight := func(date time.Time) time.Time {
		return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, s.lima)
	}
	if from.Valid {
		filter.StartAt = pgtype.Timestamptz{Time: limaMidnight(from.Time), Valid: true}
	}
	if to.Valid {
		filter.EndAt = pgtype.Timestamptz{Time: limaMidnight(to.Time).AddDate(0, 0, 1), Valid: true}
	}
	rows, err := s.db.SearchAuditEntries(c.Context(), filter)
	if err != nil {
		return err
	}
	items := make([]fiber.Map, 0, len(rows))
	for _, row := range rows {
		items = append(items, fiber.Map{
			"id": row.ID, "actorId": row.ActorUserID, "actorName": row.ActorName,
			"entity": row.EntityType, "entityId": row.EntityID, "action": row.Action,
			"requestId": row.RequestID, "createdAt": row.CreatedAt.Time.UTC(),
		})
	}
	return c.JSON(paged(items, limit))
}
