package loads

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
	"zhonghui-api/internal/modules/lookups"
)

var (
	rucPattern      = regexp.MustCompile(`^[0-9]{11}$`)
	dniPattern      = regexp.MustCompile(`^[0-9]{8}$`)
	documentPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9.-]{1,39}$`)
	typePattern     = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,19}$`)
	platePattern    = regexp.MustCompile(`^[A-Z0-9]{4,12}$`)
)

type createInput struct {
	LocationID          string   `json:"locationId"`
	RUC                 string   `json:"ruc"`
	LegalName           string   `json:"legalName"`
	DocumentType        string   `json:"documentType"`
	DocumentNumber      string   `json:"documentNumber"`
	FirstNames          string   `json:"firstNames"`
	LastNames           string   `json:"lastNames"`
	LicenseNumber       string   `json:"licenseNumber"`
	LicenseID           string   `json:"licenseId,omitempty"`
	Phone               string   `json:"phone"`
	TractorPlate        string   `json:"tractorPlate"`
	TrailerPlate        string   `json:"trailerPlate"`
	UnitDescription     string   `json:"unitDescription"`
	Notes               string   `json:"notes"`
	ExternalFields      []string `json:"externalFields,omitempty"`
	LicenseVerification string   `json:"licenseVerification,omitempty"`
}

type snapshot struct {
	createInput
	LocationName   string                  `json:"locationName"`
	TrailerType    string                  `json:"trailerType"`
	LicenseDetails *lookups.LicenseDetails `json:"licenseDetails,omitempty"`
}

func (in *createInput) normalize() (pgtype.UUID, bool) {
	id, valid := readUUID(in.LocationID)
	in.LocationID = id.String()
	in.RUC = strings.TrimSpace(in.RUC)
	in.LegalName = strings.TrimSpace(in.LegalName)
	in.DocumentType = strings.ToUpper(strings.TrimSpace(in.DocumentType))
	in.DocumentNumber = strings.ToUpper(strings.TrimSpace(in.DocumentNumber))
	in.FirstNames = strings.TrimSpace(in.FirstNames)
	in.LastNames = strings.TrimSpace(in.LastNames)
	in.LicenseNumber = strings.ToUpper(strings.TrimSpace(in.LicenseNumber))
	if in.LicenseID != "" {
		licenseID, parsed := readUUID(in.LicenseID)
		valid = valid && parsed && in.LicenseNumber != ""
		in.LicenseID = licenseID.String()
	}
	in.Phone = strings.TrimSpace(in.Phone)
	plate := strings.NewReplacer(" ", "", "-", "")
	hasTrailer := strings.TrimSpace(in.TrailerPlate) != ""
	in.TractorPlate = plate.Replace(strings.ToUpper(strings.TrimSpace(in.TractorPlate)))
	in.TrailerPlate = plate.Replace(strings.ToUpper(strings.TrimSpace(in.TrailerPlate)))
	in.UnitDescription = strings.TrimSpace(in.UnitDescription)
	in.Notes = strings.TrimSpace(in.Notes)
	slices.Sort(in.ExternalFields)
	in.ExternalFields = slices.Compact(in.ExternalFields)
	for _, field := range in.ExternalFields {
		switch field {
		case "ruc", "legalName", "firstNames", "lastNames", "licenseNumber", "tractorPlate", "trailerPlate":
		default:
			valid = false
		}
	}
	valid = valid && rucPattern.MatchString(in.RUC) &&
		in.LegalName != "" && within(in.LegalName, 200) &&
		typePattern.MatchString(in.DocumentType) && documentPattern.MatchString(in.DocumentNumber) &&
		(in.DocumentType != "DNI" || dniPattern.MatchString(in.DocumentNumber)) &&
		in.FirstNames != "" && within(in.FirstNames, 120) &&
		in.LastNames != "" && within(in.LastNames, 120) &&
		within(in.LicenseNumber, 60) && within(in.Phone, 40) &&
		platePattern.MatchString(in.TractorPlate) &&
		(!hasTrailer || (platePattern.MatchString(in.TrailerPlate) && in.TractorPlate != in.TrailerPlate)) &&
		within(in.UnitDescription, 200) && within(in.Notes, 1000)
	return id, valid
}

func readUUID(raw string) (pgtype.UUID, bool) {
	var id pgtype.UUID
	valid := id.Scan(strings.TrimSpace(raw)) == nil && id.Valid && id.Bytes != [16]byte{}
	return id, valid
}

func within(value string, limit int) bool {
	return !strings.ContainsRune(value, 0) && utf8.RuneCountInString(value) <= limit
}

func optionalText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}
