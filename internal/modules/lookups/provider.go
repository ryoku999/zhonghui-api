package lookups

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const providerTimeout = 33 * time.Second
const bodyLimit = 12 << 20

type provider struct {
	baseURL, token string
	http           *http.Client
}

func providerFromEnv() (provider, error) {
	token := strings.TrimSpace(os.Getenv("APISTED_TOKEN"))
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("APISTED_BASE_URL")), "/")
	if base == "" {
		base = "https://apisted.com/api"
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return provider{}, errors.New("APISTED_BASE_URL must be an HTTPS base URL")
	}
	client := &http.Client{
		Timeout:       providerTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return provider{baseURL: base, token: token, http: client}, nil
}

type providerError struct {
	status     int
	retryAfter string
}

func (e providerError) Error() string { return "ApisTED returned an unexpected status" }

func (p provider) post(ctx context.Context, endpoint string, body map[string]string, output any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		retry := res.Header.Get("Retry-After")
		if seconds, err := strconv.Atoi(retry); err != nil || seconds < 0 {
			retry = ""
		}
		return providerError{status: res.StatusCode, retryAfter: retry}
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, bodyLimit+1))
	if err != nil {
		return err
	}
	if len(data) > bodyLimit {
		return errors.New("ApisTED response exceeds the size limit")
	}
	return json.Unmarshal(data, output)
}

type Company struct {
	RUC          string `json:"ruc"`
	LegalName    string `json:"legalName"`
	TaxStatus    string `json:"taxStatus,omitempty"`
	TaxCondition string `json:"taxCondition,omitempty"`
	Address      string `json:"fiscalAddress,omitempty"`
}

type License struct {
	Number         string   `json:"licenseNumber"`
	Category       string   `json:"category,omitempty"`
	Status         string   `json:"licenseStatus,omitempty"`
	IssuedOn       string   `json:"issuedOn,omitempty"`
	RevalidationOn string   `json:"revalidationOn,omitempty"`
	Restrictions   []string `json:"restrictions"`
	IssuingCenter  string   `json:"issuingCenter,omitempty"`
	IsElectronic   *bool    `json:"isElectronic,omitempty"`
}

type Driver struct {
	DNI        string    `json:"dni"`
	FirstNames string    `json:"firstNames"`
	LastNames  string    `json:"lastNames"`
	Licenses   []License `json:"licenses"`
	Incomplete bool      `json:"incomplete"`
	CheckedAt  time.Time `json:"checkedAt"`
}

type Vehicle struct {
	Plate              string `json:"plate"`
	Make               string `json:"make,omitempty"`
	Model              string `json:"model,omitempty"`
	ModelYear          int16  `json:"modelYear,omitempty"`
	Color              string `json:"color,omitempty"`
	RegistrationStatus string `json:"registrationStatus,omitempty"`
}

func (p provider) fetch(ctx context.Context, kind, key string) (entry, error) {
	switch kind {
	case "RUC":
		var body struct {
			RUC       string `json:"ruc"`
			LegalName string `json:"razon_social"`
			State     string `json:"estado"`
			Condition string `json:"condicion"`
			Address   string `json:"domicilio_fiscal"`
		}
		err := p.post(ctx, "/sunat/consultar-contribuyente", map[string]string{"ruc": key}, &body)
		if err != nil {
			return entry{}, err
		}
		if body.RUC != key || strings.TrimSpace(body.LegalName) == "" {
			return entry{}, errors.New("unexpected SUNAT response")
		}
		return entry{data: Company{key, body.LegalName, body.State, body.Condition, body.Address}, ttl: time.Hour}, nil
	case "DNI":
		var body struct {
			DNI        string `json:"dni"`
			Found      *bool  `json:"encontrado"`
			Incomplete bool   `json:"incompleto"`
			CheckedAt  string `json:"fecha_consulta"`
			Driver     struct {
				Names  string `json:"nombres"`
				First  string `json:"apellido_paterno"`
				Second string `json:"apellido_materno"`
			} `json:"conductor"`
			Licenses []struct {
				Number       string   `json:"numero"`
				Category     string   `json:"categoria"`
				Status       string   `json:"estado"`
				Issued       string   `json:"fecha_expedicion"`
				Revalidate   string   `json:"fecha_revalidacion"`
				Restrictions []string `json:"restricciones"`
				Center       string   `json:"centro_emision"`
				Electronic   *bool    `json:"electronica"`
			} `json:"licencias"`
		}
		if err := p.post(ctx, "/mtc/licencia", map[string]string{"dni": key}, &body); err != nil {
			return entry{}, err
		}
		if body.DNI != key || body.Found == nil {
			return entry{}, errors.New("unexpected MTC response")
		}
		if !*body.Found {
			return entry{ttl: time.Minute}, nil
		}
		data := Driver{DNI: key, FirstNames: body.Driver.Names, LastNames: strings.TrimSpace(body.Driver.First + " " + body.Driver.Second),
			Incomplete: body.Incomplete, Licenses: []License{}, CheckedAt: mtcCheckedAt(body.CheckedAt)}
		for _, row := range body.Licenses {
			number := strings.ToUpper(strings.TrimSpace(row.Number))
			if number != "" {
				data.Licenses = append(data.Licenses, License{
					Number: number, Category: cleanText(row.Category, 80), Status: cleanText(row.Status, 80),
					IssuedOn: isoDate(row.Issued), RevalidationOn: isoDate(row.Revalidate),
					Restrictions: cleanRestrictions(row.Restrictions), IssuingCenter: cleanText(row.Center, 240),
					IsElectronic: row.Electronic,
				})
			}
		}
		ttl := 10 * time.Minute
		if body.Incomplete {
			ttl = time.Minute
		}
		return entry{data: data, ttl: ttl}, nil
	default:
		var body struct {
			Plate string `json:"placa"`
			Found *bool  `json:"encontrado"`
			Car   struct {
				Plate string          `json:"placa_vigente"`
				Make  string          `json:"marca"`
				Model string          `json:"modelo"`
				Year  json.RawMessage `json:"anio_modelo"`
				Color string          `json:"color"`
				State string          `json:"estado"`
			} `json:"vehiculo"`
		}
		if err := p.post(ctx, "/sunarp/consulta-vehicular", map[string]string{"placa": key}, &body); err != nil {
			return entry{}, err
		}
		if normalizePlate(body.Plate) != key || body.Found == nil {
			return entry{}, errors.New("unexpected SUNARP response")
		}
		if !*body.Found {
			return entry{ttl: time.Minute}, nil
		}
		plate := normalizePlate(body.Car.Plate)
		if plate == "" {
			plate = key
		}
		if !platePattern.MatchString(plate) {
			return entry{}, errors.New("unexpected plate in SUNARP response")
		}
		year, _ := strconv.ParseInt(strings.TrimSpace(strings.Trim(string(body.Car.Year), "\"")), 10, 16)
		if year < 1886 || year > 9999 {
			year = 0
		}
		return entry{data: Vehicle{plate, body.Car.Make, body.Car.Model, int16(year), body.Car.Color, body.Car.State}, ttl: 24 * time.Hour}, nil
	}
}

func isoDate(raw string) string {
	for _, layout := range []string{"02/01/2006", time.DateOnly} {
		if date, err := time.Parse(layout, strings.TrimSpace(raw)); err == nil && date.Year() >= 1900 {
			return date.Format(time.DateOnly)
		}
	}
	return ""
}
