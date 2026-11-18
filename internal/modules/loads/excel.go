package loads

import (
	"context"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/xuri/excelize/v2"
)

const exportLimit = 5000

func (s service) export(c fiber.Ctx) error {
	filter, err := reportFilters(c)
	if err != nil {
		return err
	}
	if raw := c.Query("ids"); raw != "" {
		ids := strings.Split(raw, ",")
		if len(ids) > 100 {
			return fiber.ErrBadRequest
		}
		for _, value := range ids {
			id, valid := readUUID(value)
			if !valid {
				return fiber.ErrBadRequest
			}
			filter.LoadIds = append(filter.LoadIds, id)
		}
	}
	filter.RowLimit = exportLimit + 1
	rows, err := s.db.SearchLoadRecords(c.Context(), filter)
	if err != nil {
		return err
	}
	if len(rows) > exportLimit {
		return fiber.ErrUnprocessableEntity
	}
	data, err := reportRows(rows)
	if err != nil {
		return err
	}
	xlsx, err := buildWorkbook(c.Context(), data)
	if err != nil {
		return err
	}
	c.Set(fiber.HeaderContentType, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Set(fiber.HeaderContentDisposition, `attachment; filename="registros-zhonghui.xlsx"`)
	return c.Send(xlsx)
}

func buildWorkbook(ctx context.Context, data []reportRow) ([]byte, error) {
	book := excelize.NewFile()
	defer book.Close()
	const sheet = "Registros"
	if err := book.SetSheetName("Sheet1", sheet); err != nil {
		return nil, err
	}
	headings := []string{"Fecha", "Orden", "Punto de embarque", "RUC", "Razón social", "Tipo de documento",
		"Documento", "Conductor", "Licencia", "Celular", "Tracto", "Carreta", "Unidad", "Registrado por", "Estado"}
	widths := []float64{14, 10, 25, 16, 36, 20, 20, 32, 22, 18, 14, 14, 28, 26, 16}
	for i, heading := range headings {
		column, err := excelize.ColumnNumberToName(i + 1)
		if err != nil {
			return nil, err
		}
		if err := book.SetCellStr(sheet, column+"1", heading); err != nil {
			return nil, err
		}
		if err := book.SetColWidth(sheet, column, column, widths[i]); err != nil {
			return nil, err
		}
	}
	headerStyle, err := book.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"17324D"}},
	})
	if err != nil {
		return nil, err
	}
	if err := book.SetCellStyle(sheet, "A1", "O1", headerStyle); err != nil {
		return nil, err
	}
	dateFormat := "dd/mm/yyyy"
	dateStyle, err := book.NewStyle(&excelize.Style{CustomNumFmt: &dateFormat})
	if err != nil {
		return nil, err
	}
	for i, row := range data {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		number := i + 2
		date, err := time.Parse(dateLayout, row.BusinessDate)
		if err != nil {
			return nil, err
		}
		status := "Registrado"
		if row.Status == "CANCELLED" {
			status = "Anulado"
		}
		dateCell, _ := excelize.CoordinatesToCellName(1, number)
		orderCell, _ := excelize.CoordinatesToCellName(2, number)
		if err := book.SetCellValue(sheet, dateCell, date); err != nil {
			return nil, err
		}
		if err := book.SetCellStyle(sheet, dateCell, dateCell, dateStyle); err != nil {
			return nil, err
		}
		if err := book.SetCellInt(sheet, orderCell, int64(row.OrderNumber)); err != nil {
			return nil, err
		}
		text := []string{row.LocationName, row.RUC, row.LegalName, row.DocumentType, row.Document, row.Driver,
			row.License, row.Phone, row.Tractor, row.Trailer, row.Unit, row.Registrar, status}
		for column, value := range text {
			cell, _ := excelize.CoordinatesToCellName(column+3, number)
			if err := book.SetCellStr(sheet, cell, value); err != nil {
				return nil, err
			}
		}
	}
	buffer, err := book.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
