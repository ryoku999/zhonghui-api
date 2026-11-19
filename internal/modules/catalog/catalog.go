package catalog

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"

	"zhonghui-api/internal/database/db"
	"zhonghui-api/internal/modules/auth"
	"zhonghui-api/internal/modules/lookups"
)

type catalog struct {
	pool *pgxpool.Pool
	db   *db.Queries
}

func Register(app *fiber.App, pool *pgxpool.Pool, protected fiber.Handler) {
	s := catalog{pool: pool, db: db.New(pool)}
	locations := app.Group("/api/v1/locations", auth.RequestContext, protected)
	locations.Get("/", s.listLocations)
	locations.Post("/", auth.RequireAdmin, s.createLocation)
	locations.Patch("/:id", auth.RequireAdmin, s.updateLocation)
	locations.Get("/:id/users", auth.RequireAdmin, s.listLocationUsers)
	locations.Put("/:id/users/:userId", auth.RequireAdmin, s.assignUser)
	locations.Delete("/:id/users/:userId", auth.RequireAdmin, s.removeUser)
	app.Get("/api/v1/companies", auth.RequestContext, protected, s.companies)
	app.Get("/api/v1/drivers", auth.RequestContext, protected, s.drivers)
	app.Get("/api/v1/licenses", auth.RequestContext, protected, s.licenses)
	app.Get("/api/v1/licenses/:id", auth.RequestContext, protected, s.licenseDetails)
	app.Get("/api/v1/vehicles", auth.RequestContext, protected, s.vehicles)
	app.Get("/api/v1/vehicles/:id", auth.RequestContext, protected, s.vehicleDetails)
}

type page struct {
	limit, offset int32
	search        string
}

func readPage(c fiber.Ctx) (page, error) {
	limit, limitErr := strconv.ParseInt(c.Query("limit", "25"), 10, 32)
	offset, offsetErr := strconv.ParseInt(c.Query("offset", "0"), 10, 32)
	search := strings.TrimSpace(c.Query("q"))
	if limitErr != nil || offsetErr != nil || limit < 1 || limit > 100 || offset < 0 || len(search) > 200 {
		return page{}, fiber.ErrBadRequest
	}
	return page{limit: int32(limit), offset: int32(offset), search: search}, nil
}

func (s catalog) companies(c fiber.Ctx) error {
	p, err := readPage(c)
	if err != nil {
		return err
	}
	rows, err := s.db.SearchCompanies(c.Context(), db.SearchCompaniesParams{
		Search: p.search, PageLimit: p.limit, PageOffset: p.offset,
	})
	if err != nil {
		return err
	}
	data := make([]fiber.Map, 0, len(rows))
	for _, row := range rows {
		data = append(data, fiber.Map{"id": row.ID, "ruc": row.Ruc, "legalName": row.LegalName})
	}
	return c.JSON(data)
}

func (s catalog) drivers(c fiber.Ctx) error {
	p, err := readPage(c)
	if err != nil {
		return err
	}
	rows, err := s.db.SearchDrivers(c.Context(), db.SearchDriversParams{
		Search: p.search, PageLimit: p.limit, PageOffset: p.offset,
	})
	if err != nil {
		return err
	}
	data := make([]fiber.Map, 0, len(rows))
	for _, row := range rows {
		data = append(data, fiber.Map{"id": row.ID, "documentType": row.DocumentType,
			"documentNumber": row.DocumentNumber, "firstNames": row.FirstNames, "lastNames": row.LastNames, "phone": row.Phone})
	}
	return c.JSON(data)
}

func (s catalog) licenses(c fiber.Ctx) error {
	p, err := readPage(c)
	if err != nil {
		return err
	}
	input := db.SearchLicensesParams{Search: p.search, PageLimit: p.limit, PageOffset: p.offset}
	if driverID := c.Query("driverId"); driverID != "" {
		if err := input.DriverID.Scan(driverID); err != nil {
			return fiber.ErrBadRequest
		}
	}
	rows, err := s.db.SearchLicenses(c.Context(), input)
	if err != nil {
		return err
	}
	data := make([]fiber.Map, 0, len(rows))
	for _, row := range rows {
		data = append(data, fiber.Map{"id": row.ID, "driverId": row.DriverID, "licenseNumber": row.LicenseNumber,
			"licenseClass": row.LicenseClass, "category": row.Category, "expiresOn": row.ExpiresOn, "issuingCountry": row.IssuingCountry,
			"licenseStatus": row.LicenseStatus, "issuedOn": row.IssuedOn, "revalidationOn": row.RevalidationOn,
			"driverDocument": row.DriverDocument, "driverDocumentType": row.DriverDocumentType})
	}
	return c.JSON(data)
}

func (s catalog) licenseDetails(c fiber.Ctx) error {
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	row, err := s.db.ReadLicenseDetails(c.Context(), id)
	if err != nil {
		return notFound(err)
	}
	return c.JSON(lookups.DetailOf(row))
}

func (s catalog) vehicles(c fiber.Ctx) error {
	p, err := readPage(c)
	if err != nil {
		return err
	}
	vehicleTypes := []string{}
	if raw := strings.TrimSpace(c.Query("type")); raw != "" {
		for value := range strings.SplitSeq(raw, ",") {
			vehicleType := strings.ToUpper(strings.TrimSpace(value))
			switch vehicleType {
			case "TRACTOR", "TRAILER", "SEMITRAILER":
				vehicleTypes = append(vehicleTypes, vehicleType)
			default:
				return fiber.ErrBadRequest
			}
		}
	}
	plate := strings.NewReplacer(" ", "", "-", "").Replace(strings.ToUpper(p.search))
	rows, err := s.db.SearchVehicles(c.Context(), db.SearchVehiclesParams{
		Search: plate, VehicleTypes: vehicleTypes, PageLimit: p.limit, PageOffset: p.offset,
	})
	if err != nil {
		return err
	}
	data := make([]fiber.Map, 0, len(rows))
	for _, row := range rows {
		data = append(data, fiber.Map{"id": row.ID, "plate": row.Plate, "vehicleType": row.VehicleType, "make": row.Make,
			"model": row.Model, "modelYear": row.ModelYear, "color": row.Color, "registrationStatus": row.RegistrationStatus})
	}
	return c.JSON(data)
}

func conflictOnDuplicate(err error) error {
	var dbError interface{ SQLState() string }
	if errors.As(err, &dbError) && dbError.SQLState() == "23505" {
		return fiber.ErrConflict
	}
	return err
}

func (s catalog) vehicleDetails(c fiber.Ctx) error {
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	row, err := s.db.ReadVehicleDetails(c.Context(), id)
	if err != nil {
		return notFound(err)
	}
	details, err := lookups.DetailOfVehicle(row)
	if err != nil {
		return err
	}
	return c.JSON(details)
}
