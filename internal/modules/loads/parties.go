package loads

import (
	"context"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgtype"

	"zhonghui-api/internal/database/db"
)

func resolveParties(ctx context.Context, q *db.Queries, data *snapshot, record *db.CreateLoadRecordParams) (map[string]string, error) {
	history := make(map[string]string)
	companyCount, err := q.EnsureLoadCompany(ctx, db.EnsureLoadCompanyParams{Ruc: data.RUC, LegalName: data.LegalName})
	if err != nil {
		return nil, err
	}
	company, err := q.FindLoadCompany(ctx, data.RUC)
	if err != nil {
		return nil, err
	}
	if !company.IsActive {
		return nil, fiber.ErrConflict
	}
	record.CompanyID = company.ID
	if companyCount == 0 {
		history["ruc"] = "HISTORY"
		if company.LegalName == data.LegalName {
			history["legalName"] = "HISTORY"
		}
	}

	driverCount, err := q.EnsureLoadDriver(ctx, db.EnsureLoadDriverParams{
		DocumentType: data.DocumentType, DocumentNumber: data.DocumentNumber,
		FirstNames: data.FirstNames, LastNames: data.LastNames, Phone: optionalText(data.Phone),
	})
	if err != nil {
		return nil, err
	}
	driver, err := q.FindLoadDriver(ctx, db.FindLoadDriverParams{DocumentType: data.DocumentType, DocumentNumber: data.DocumentNumber})
	if err != nil {
		return nil, err
	}
	if !driver.IsActive {
		return nil, fiber.ErrConflict
	}
	record.DriverID = driver.ID
	if driverCount == 0 {
		history["documentType"], history["documentNumber"] = "HISTORY", "HISTORY"
		if driver.FirstNames == data.FirstNames {
			history["firstNames"] = "HISTORY"
		}
		if driver.LastNames == data.LastNames {
			history["lastNames"] = "HISTORY"
		}
		if driver.Phone.Valid && driver.Phone.String == data.Phone {
			history["phone"] = "HISTORY"
		}
	}

	if data.LicenseNumber != "" {
		licenseID, _ := readUUID(data.LicenseID)
		input := db.FindLoadLicensesParams{DriverID: driver.ID, LicenseNumber: data.LicenseNumber, LicenseID: licenseID}
		ids, err := q.FindLoadLicenses(ctx, input)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 && !licenseID.Valid {
			count, err := q.EnsureLoadLicense(ctx, db.EnsureLoadLicenseParams{DriverID: driver.ID, LicenseNumber: data.LicenseNumber})
			if err != nil {
				return nil, err
			}
			if count == 0 {
				history["licenseNumber"] = "HISTORY"
			}
			ids, err = q.FindLoadLicenses(ctx, input)
			if err != nil {
				return nil, err
			}
		} else {
			history["licenseNumber"] = "HISTORY"
		}
		if len(ids) != 1 {
			return nil, fiber.ErrConflict
		}
		record.DriverLicenseID = ids[0]
		data.LicenseID = ids[0].String()
	}

	vehicles := []struct {
		field, plate, kind string
		id                 *pgtype.UUID
	}{
		{"tractorPlate", data.TractorPlate, "TRACTOR", &record.TractorID},
		{"trailerPlate", data.TrailerPlate, "TRAILER", &record.TrailerID},
	}
	// Acquire plate locks in the same order across concurrent registrations.
	if vehicles[0].plate > vehicles[1].plate {
		vehicles[0], vehicles[1] = vehicles[1], vehicles[0]
	}
	for _, vehicle := range vehicles {
		count, err := q.EnsureLoadVehicle(ctx, db.EnsureLoadVehicleParams{Plate: vehicle.plate, VehicleType: vehicle.kind})
		if err != nil {
			return nil, err
		}
		row, err := q.FindLoadVehicle(ctx, vehicle.plate)
		if err != nil {
			return nil, err
		}
		if !row.IsActive || (vehicle.kind == "TRACTOR" && row.VehicleType != "TRACTOR") ||
			(vehicle.kind == "TRAILER" && row.VehicleType != "TRAILER" && row.VehicleType != "SEMITRAILER") {
			return nil, fiber.ErrConflict
		}
		*vehicle.id = row.ID
		if vehicle.kind == "TRAILER" {
			data.TrailerType = row.VehicleType
		}
		if count == 0 {
			history[vehicle.field] = "HISTORY"
			if vehicle.kind == "TRAILER" {
				history["trailerType"] = "HISTORY"
			}
		}
	}
	return history, nil
}
