package lookups

import (
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"zhonghui-api/internal/database/db"
)

const vehicleVerificationDomain = "sunarp-vehicle:v1:"

type RegistryEntry struct {
	Office     string `json:"office,omitempty"`
	Number     string `json:"number,omitempty"`
	Zone       string `json:"zone,omitempty"`
	OfficeCode string `json:"officeCode,omitempty"`
}

type Vehicle struct {
	Plate              string          `json:"plate"`
	Make               string          `json:"make,omitempty"`
	Model              string          `json:"model,omitempty"`
	ModelYear          int16           `json:"modelYear,omitempty"`
	Color              string          `json:"color,omitempty"`
	RegistrationStatus string          `json:"registrationStatus,omitempty"`
	SerialNumber       string          `json:"serialNumber,omitempty"`
	VIN                string          `json:"vin,omitempty"`
	EngineNumber       string          `json:"engineNumber,omitempty"`
	PreviousPlate      string          `json:"previousPlate,omitempty"`
	RegistryOffice     string          `json:"registryOffice,omitempty"`
	RegistryNotes      string          `json:"registryNotes,omitempty"`
	RegistryEntries    []RegistryEntry `json:"registryEntries"`
	TheftAlert         *string         `json:"theftAlert,omitempty"`
	CheckedAt          *time.Time      `json:"checkedAt,omitempty"`
}

type VehicleDetails struct {
	Vehicle
	ID          pgtype.UUID `json:"id"`
	VehicleType string      `json:"vehicleType"`
}

type vehicleVerification struct {
	OwnerID   string  `json:"owner"`
	ExpiresAt int64   `json:"expiresAt"`
	Vehicle   Vehicle `json:"vehicle"`
}

func (s *Service) SelectedVehicle(proof, ownerID, plate string) (Vehicle, bool) {
	if plate == "" {
		return Vehicle{}, false
	}
	var decoded vehicleVerification
	if s.readVerification(vehicleVerificationDomain, proof, 32_000, &decoded) &&
		decoded.OwnerID == ownerID && decoded.Vehicle.Plate == plate && decoded.Vehicle.CheckedAt != nil &&
		decoded.ExpiresAt > time.Now().Unix() {
		return decoded.Vehicle, true
	}
	item, _ := s.cached("PLATE:" + plate)
	vehicle, ok := item.data.(Vehicle)
	return vehicle, ok && vehicle.Plate == plate && vehicle.CheckedAt != nil
}

func VehicleUpdate(car Vehicle) (db.EnrichVehicleParams, error) {
	params := db.EnrichVehicleParams{
		Plate: car.Plate, Make: text(car.Make), Model: text(car.Model),
		ModelYear: pgtype.Int2{Int16: car.ModelYear, Valid: car.ModelYear != 0},
		Color:     text(car.Color), RegistrationStatus: text(car.RegistrationStatus), SerialNumber: text(car.SerialNumber),
		Vin: text(car.VIN), EngineNumber: text(car.EngineNumber), PreviousPlate: text(car.PreviousPlate),
		RegistryOffice: text(car.RegistryOffice), RegistryNotes: text(car.RegistryNotes),
	}
	if car.CheckedAt != nil {
		params.CheckedAt = pgtype.Timestamptz{Time: *car.CheckedAt, Valid: true}
	}
	if car.TheftAlert != nil {
		params.TheftAlert = pgtype.Text{String: *car.TheftAlert, Valid: true}
	}
	if car.RegistryEntries != nil {
		var err error
		params.RegistryEntries, err = json.Marshal(car.RegistryEntries)
		if err != nil {
			return params, err
		}
	}
	return params, nil
}

func DetailOfVehicle(row db.Vehicle) (VehicleDetails, error) {
	car := Vehicle{
		Plate: row.Plate, Make: row.Make.String, Model: row.Model.String, ModelYear: row.ModelYear.Int16,
		Color: row.Color.String, RegistrationStatus: row.RegistrationStatus.String,
		SerialNumber: row.SerialNumber.String, VIN: row.Vin.String, EngineNumber: row.EngineNumber.String,
		PreviousPlate: row.PreviousPlate.String, RegistryOffice: row.RegistryOffice.String, RegistryNotes: row.RegistryNotes.String,
	}
	if len(row.RegistryEntries) > 0 {
		if err := json.Unmarshal(row.RegistryEntries, &car.RegistryEntries); err != nil {
			return VehicleDetails{}, err
		}
	}
	if row.TheftAlert.Valid {
		car.TheftAlert = &row.TheftAlert.String
	}
	if row.SunarpCheckedAt.Valid {
		checked := row.SunarpCheckedAt.Time.UTC()
		car.CheckedAt = &checked
	}
	return VehicleDetails{Vehicle: car, ID: row.ID, VehicleType: row.VehicleType}, nil
}
