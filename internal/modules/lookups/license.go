package lookups

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"zhonghui-api/internal/database/db"
)

type licenseVerification struct {
	OwnerID   string    `json:"owner"`
	DNI       string    `json:"dni"`
	CheckedAt time.Time `json:"checkedAt"`
	ExpiresAt int64     `json:"expiresAt"`
	License   License   `json:"license"`
}

type LicenseDetails struct {
	License
	LicenseClass   pgtype.Text        `json:"licenseClass"`
	IssuingCountry pgtype.Text        `json:"issuingCountry"`
	ExpiresOn      pgtype.Date        `json:"expiresOn"`
	CheckedAt      pgtype.Timestamptz `json:"checkedAt"`
}

func DetailOf(row db.DriverLicense) LicenseDetails {
	license := License{
		Number: row.LicenseNumber, Category: row.Category.String, Status: row.LicenseStatus.String,
		Restrictions: row.Restrictions, IssuingCenter: row.IssuingCenter.String,
	}
	if row.IssuedOn.Valid {
		license.IssuedOn = row.IssuedOn.Time.Format(time.DateOnly)
	}
	if row.RevalidationOn.Valid {
		license.RevalidationOn = row.RevalidationOn.Time.Format(time.DateOnly)
	}
	if row.IsElectronic.Valid {
		license.IsElectronic = &row.IsElectronic.Bool
	}
	return LicenseDetails{License: license, LicenseClass: row.LicenseClass, IssuingCountry: row.IssuingCountry,
		ExpiresOn: row.ExpiresOn, CheckedAt: row.MtcCheckedAt}
}

func licenseVerificationKey() []byte {
	secret, _ := base64.StdEncoding.DecodeString(os.Getenv("JWT_ACCESS_SECRET"))
	sum := hmac.New(sha256.New, secret)
	sum.Write([]byte("zhonghui:mtc-license-verification:v1"))
	return sum.Sum(nil)
}

func (s *Service) signLicense(data licenseVerification) (string, error) {
	return s.signVerification("", data)
}

func (s *Service) signVerification(domain string, data any) (string, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(s.verificationMAC(domain, payload)), nil
}

func (s *Service) verificationMAC(domain string, payload []byte) []byte {
	sum := hmac.New(sha256.New, s.licenseKey)
	sum.Write([]byte(domain))
	sum.Write(payload)
	return sum.Sum(nil)
}

func (s *Service) readVerification(domain, proof string, maxSize int, decoded any) bool {
	payload, signature, found := strings.Cut(proof, ".")
	if !found || len(proof) > maxSize {
		return false
	}
	body, bodyErr := base64.RawURLEncoding.DecodeString(payload)
	mac, macErr := base64.RawURLEncoding.DecodeString(signature)
	return bodyErr == nil && macErr == nil && hmac.Equal(s.verificationMAC(domain, body), mac) &&
		json.Unmarshal(body, decoded) == nil
}

func (s *Service) SelectedLicense(proof, ownerID, dni, number string) (License, time.Time, bool) {
	var decoded licenseVerification
	if s.readVerification("", proof, 12_000, &decoded) && decoded.OwnerID == ownerID && decoded.DNI == dni &&
		decoded.License.Number == number && decoded.ExpiresAt > time.Now().Unix() {
		return decoded.License, decoded.CheckedAt, true
	}
	item, _ := s.cached("DNI:" + dni)
	if driver, ok := item.data.(Driver); ok {
		for _, license := range driver.Licenses {
			if license.Number == number {
				return license, driver.CheckedAt, true
			}
		}
	}
	return License{}, time.Time{}, false
}

func LicenseUpdate(dni string, id pgtype.UUID, license License, checkedAt time.Time) db.EnrichLicenseParams {
	toDate := func(value string) pgtype.Date {
		day, err := time.Parse(time.DateOnly, value)
		return pgtype.Date{Time: day, Valid: err == nil}
	}
	electronic := pgtype.Bool{}
	if license.IsElectronic != nil {
		electronic = pgtype.Bool{Bool: *license.IsElectronic, Valid: true}
	}
	return db.EnrichLicenseParams{
		Dni: dni, LicenseID: id, LicenseNumber: license.Number, Category: text(license.Category), LicenseStatus: text(license.Status),
		IssuedOn: toDate(license.IssuedOn), RevalidationOn: toDate(license.RevalidationOn), Restrictions: license.Restrictions,
		IssuingCenter: text(license.IssuingCenter), IsElectronic: electronic,
		CheckedAt: pgtype.Timestamptz{Time: checkedAt.UTC(), Valid: true},
	}
}

func providerCheckedAt(value string) time.Time {
	now := time.Now().UTC()
	if lima, err := time.LoadLocation("America/Lima"); err == nil {
		if day, err := time.ParseInLocation("02/01/2006 15:04:05", strings.TrimSpace(value), lima); err == nil && !day.After(now) {
			return day.UTC()
		}
	}
	return now
}

func cleanText(value string, max int) string {
	runes := []rune(strings.TrimSpace(strings.ReplaceAll(value, "\x00", "")))
	if len(runes) > max {
		runes = runes[:max]
	}
	return string(runes)
}

func cleanRestrictions(values []string) []string {
	if values == nil {
		return nil
	}
	result := make([]string, 0, min(len(values), 12))
	for _, value := range values {
		if value = cleanText(value, 120); value != "" {
			result = append(result, value)
			if len(result) == 12 {
				break
			}
		}
	}
	return result
}
