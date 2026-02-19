package schedule

import (
	"bytes"
	"fmt"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers"
)

// --- Color Palette (Tailwind CSS v3 values) ---
var (
	ColorBg       = canvas.Hex("#F8FAFC") // slate-50
	ColorCardBg   = canvas.Hex("#FFFFFF")
	ColorTextMain = canvas.Hex("#1E293B") // slate-800
	ColorTextSec  = canvas.Hex("#64748B") // slate-500
	ColorGrid     = canvas.Hex("#E2E8F0") // slate-200
	ColorGridWeak = canvas.Hex("#F1F5F9") // slate-100
	ColorRed      = canvas.Hex("#EF4444") // red-500
)

type EventStyle struct {
	Bg     color.RGBA
	Text   color.RGBA
	Border color.RGBA
}

var tailwindPalette = []EventStyle{
	{Bg: canvas.Hex("#BFDBFE"), Text: canvas.Hex("#1E3A8A"), Border: canvas.Hex("#93C5FD")}, // Blue
	{Bg: canvas.Hex("#C7D2FE"), Text: canvas.Hex("#312E81"), Border: canvas.Hex("#A5B4FC")}, // Indigo
	{Bg: canvas.Hex("#E9D5FF"), Text: canvas.Hex("#581C87"), Border: canvas.Hex("#D8B4FE")}, // Purple
	{Bg: canvas.Hex("#F5D0FE"), Text: canvas.Hex("#701A75"), Border: canvas.Hex("#F0ABFC")}, // Fuchsia
	{Bg: canvas.Hex("#FBCFE8"), Text: canvas.Hex("#831843"), Border: canvas.Hex("#F9A8D4")}, // Pink
	{Bg: canvas.Hex("#FECDD3"), Text: canvas.Hex("#881337"), Border: canvas.Hex("#FDA4AF")}, // Rose
	{Bg: canvas.Hex("#FED7AA"), Text: canvas.Hex("#7C2D12"), Border: canvas.Hex("#FDBA74")}, // Orange
	{Bg: canvas.Hex("#99F6E4"), Text: canvas.Hex("#134E4A"), Border: canvas.Hex("#5EEAD4")}, // Teal
	{Bg: canvas.Hex("#A5F3FC"), Text: canvas.Hex("#164E63"), Border: canvas.Hex("#67E8F9")}, // Cyan
}

const (
	SizeTitle  = 24.0
	SizeDate   = 14.0
	SizeHeader = 12.0
	SizeRoom   = 16.0
	SizeCap    = 11.0
	SizeEvent  = 13.0
	SizeDesc   = 11.0

	RowHeight    = 75.0
	HeaderHeight = 55.0
	TitleArea    = 80.0
	FooterArea   = 40.0
	SidebarWidth = 200.0
	HourWidth    = 90.0
	Padding      = 30.0
)

func getStringColor(str string) EventStyle {
	var hash int32 = 0
	for _, c := range str {
		hash = int32(c) + ((hash << 5) - hash)
	}
	idx := int(math.Abs(float64(hash))) % len(tailwindPalette)
	return tailwindPalette[idx]
}

func mm(v float64) float64 { return v * 0.3527 }

func formatDateRussian(t time.Time) string {
	days := map[time.Weekday]string{
		time.Monday: "Понедельник", time.Tuesday: "Вторник", time.Wednesday: "Среда",
		time.Thursday: "Четверг", time.Friday: "Пятница", time.Saturday: "Суббота", time.Sunday: "Воскресенье",
	}
	months := map[time.Month]string{
		time.January: "января", time.February: "февраля", time.March: "марта",
		time.April: "апреля", time.May: "мая", time.June: "июня",
		time.July: "июля", time.August: "августа", time.September: "сентября",
		time.October: "октября", time.November: "ноября", time.December: "декабря",
	}
	return fmt.Sprintf("%s, %d %s %d", days[t.Weekday()], t.Day(), months[t.Month()], t.Year())
}

// GenerateScheduleImage creates a PNG image of the schedule.
func GenerateScheduleImage(campusName string, currentTime time.Time, rooms []Room, outputPath string) error {
	startView := time.Date(currentTime.Year(), currentTime.Month(), currentTime.Day(), currentTime.Hour()-2, 0, 0, 0, currentTime.Location())
	numHours := 16

	tableHeight := HeaderHeight + (float64(len(rooms)) * RowHeight)
	totalW := SidebarWidth + (float64(numHours) * HourWidth) + Padding*2
	totalH := TitleArea + tableHeight + FooterArea + Padding*2

	c := canvas.New(mm(totalW), mm(totalH))
	ctx := canvas.NewContext(c)

	ff := canvas.NewFontFamily("sans")
	fontCandidates := []string{"Arial", "Helvetica", "DejaVu Sans", "Roboto", "Segoe UI", "Liberation Sans"}
	found := false
	for _, fName := range fontCandidates {
		if err := ff.LoadSystemFont(fName, canvas.FontRegular); err == nil {
			_ = ff.LoadSystemFont(fName, canvas.FontBold)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("could not find any system fonts")
	}

	fTitle := ff.Face(SizeTitle, ColorTextMain, canvas.FontBold)
	fDate := ff.Face(SizeDate, ColorTextSec, canvas.FontRegular)
	fHeader := ff.Face(SizeHeader, ColorTextSec, canvas.FontBold)
	fRoom := ff.Face(SizeRoom, ColorTextMain, canvas.FontBold)
	fCap := ff.Face(SizeCap, ColorTextSec, canvas.FontRegular)

	originX := mm(Padding)
	tableBottomY := mm(Padding + FooterArea)
	tableTopY := tableBottomY + mm(tableHeight)
	headerBottomY := tableTopY - mm(HeaderHeight)

	// Background
	ctx.SetFillColor(ColorBg)
	ctx.DrawPath(0, 0, canvas.Rectangle(mm(totalW), mm(totalH)))

	// Table Card
	ctx.SetFillColor(ColorCardBg)
	ctx.SetStrokeColor(ColorGrid)
	ctx.SetStrokeWidth(mm(0.5))
	ctx.DrawPath(originX, tableBottomY, canvas.RoundedRectangle(mm(totalW-Padding*2), mm(tableHeight), mm(5)))
	ctx.FillStroke()

	// Page Headers
	ctx.DrawText(originX, tableTopY+mm(32), canvas.NewTextLine(fTitle, "Переговорки "+campusName, canvas.Left))
	ctx.DrawText(originX, tableTopY+mm(12), canvas.NewTextLine(fDate, formatDateRussian(currentTime), canvas.Left))

	// Time Grid
	gridDash := []float64{mm(1.5), mm(1.5)}
	hourWidthMM := mm(HourWidth)

	for i := 0; i <= numHours; i++ {
		tickTime := startView.Add(time.Duration(i) * time.Hour)
		xOffset := SidebarWidth + float64(i)*HourWidth
		drawX := originX + mm(xOffset)

		timeStr := tickTime.Format("15:04")
		if tickTime.Hour() == 0 {
			timeStr = tickTime.Format("02.01")
		}

		// Center text on grid line
		ctx.DrawText(drawX, headerBottomY+mm(12), canvas.NewTextLine(fHeader, timeStr, canvas.Center))
		ctx.DrawText(drawX, tableBottomY-mm(15), canvas.NewTextLine(fHeader, timeStr, canvas.Center))

		// Main grid line
		ctx.SetStrokeColor(ColorGrid)
		ctx.SetStrokeWidth(mm(0.8))
		ctx.SetDashes(0.0)
		ctx.MoveTo(drawX, headerBottomY)
		ctx.LineTo(drawX, tableBottomY)
		ctx.Stroke()

		// Tick marks
		ctx.SetStrokeColor(ColorTextSec)
		ctx.SetStrokeWidth(mm(0.4))
		// Top
		ctx.MoveTo(drawX, headerBottomY)
		ctx.LineTo(drawX, headerBottomY+mm(6))
		ctx.Stroke()
		// Bottom
		ctx.MoveTo(drawX, tableBottomY)
		ctx.LineTo(drawX, tableBottomY-mm(6))
		ctx.Stroke()

		if i < numHours {
			midX := drawX + hourWidthMM/2
			// Dashed line for 30 min
			ctx.SetStrokeColor(ColorGrid)
			ctx.SetStrokeWidth(mm(0.4))
			ctx.SetDashes(gridDash[0], gridDash[1])
			ctx.MoveTo(midX, headerBottomY)
			ctx.LineTo(midX, tableBottomY)
			ctx.Stroke()

			// 30 min tick marks
			ctx.SetDashes(0.0)
			ctx.SetStrokeColor(ColorTextSec)
			ctx.SetStrokeWidth(mm(0.5))
			// Top (short)
			ctx.MoveTo(midX, headerBottomY)
			ctx.LineTo(midX, headerBottomY+mm(3))
			ctx.Stroke()
			// Bottom (short)
			ctx.MoveTo(midX, tableBottomY)
			ctx.LineTo(midX, tableBottomY-mm(3))
			ctx.Stroke()
		}
	}
	ctx.SetDashes(0.0)

	// Room List (Sidebar)
	for i, room := range rooms {
		rowTop := headerBottomY - mm(float64(i)*RowHeight)
		rowMid := rowTop - mm(RowHeight/2)
		rowBottom := rowTop - mm(RowHeight)

		if i < len(rooms)-1 {
			ctx.SetStrokeColor(ColorGridWeak)
			ctx.SetStrokeWidth(mm(0.5))
			ctx.MoveTo(originX, rowBottom)
			ctx.LineTo(originX+mm(totalW-Padding*2), rowBottom)
			ctx.Stroke()
		}

		ctx.DrawText(originX+mm(15), rowMid+mm(5), canvas.NewTextLine(fRoom, room.Name, canvas.Left))
		ctx.DrawText(originX+mm(15), rowMid-mm(8), canvas.NewTextLine(fCap, room.Capacity, canvas.Left))
	}

	// Draw Bookings
	minuteWidthMM := hourWidthMM / 60.0
	for i, room := range rooms {
		rowTop := headerBottomY - mm(float64(i)*RowHeight)
		rowMid := rowTop - mm(RowHeight/2)

		for _, b := range room.Bookings {
			startDiff := b.Start.Sub(startView).Minutes()
			endDiff := b.End.Sub(startView).Minutes()
			viewDurationMin := float64(numHours * 60)

			if endDiff <= 0 || startDiff >= viewDurationMin {
				continue
			}

			if startDiff < 0 {
				startDiff = 0
			}
			if endDiff > viewDurationMin {
				endDiff = viewDurationMin
			}

			durMin := endDiff - startDiff
			xPos := originX + mm(SidebarWidth) + (startDiff * minuteWidthMM)
			w := (durMin * minuteWidthMM) - mm(1)
			if w < mm(2) {
				w = mm(2)
			}

			hBlock := RowHeight * 0.70
			yBlock := rowMid - mm(hBlock/2)

			style := getStringColor(b.Nickname)
			ctx.SetFillColor(style.Bg)
			ctx.SetStrokeColor(style.Border)
			ctx.SetStrokeWidth(mm(0.4))
			ctx.DrawPath(xPos, yBlock, canvas.RoundedRectangle(w, mm(hBlock), mm(6)))
			ctx.FillStroke()

			if w > mm(15) {
				fEvBold := ff.Face(SizeEvent, style.Text, canvas.FontBold)
				fEvReg := ff.Face(SizeDesc, style.Text, canvas.FontRegular)
				ctx.DrawText(xPos+w/2, rowMid+mm(4), canvas.NewTextLine(fEvBold, b.Nickname, canvas.Center))
				ctx.DrawText(xPos+w/2, rowMid-mm(7), canvas.NewTextLine(fEvReg, b.Description, canvas.Center))
			}
		}
	}

	// Current Time Indicator
	nowDiff := currentTime.Sub(startView).Minutes()
	if nowDiff >= 0 && nowDiff <= float64(numHours*60) {
		nowX := originX + mm(SidebarWidth) + (nowDiff * minuteWidthMM)
		ctx.SetStrokeColor(ColorRed)
		ctx.SetStrokeWidth(mm(1.0))
		ctx.MoveTo(nowX, headerBottomY)
		ctx.LineTo(nowX, tableBottomY)
		ctx.Stroke()
		ctx.SetFillColor(ColorRed)
		ctx.DrawPath(nowX, headerBottomY, canvas.Circle(mm(2.5)))
		ctx.DrawPath(nowX, tableBottomY, canvas.Circle(mm(2.5)))
	}

	// Ensure output directory exists
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	var buf bytes.Buffer
	pngRenderer := renderers.PNG(canvas.DPI(72.0))
	if err := pngRenderer(&buf, c); err != nil {
		return err
	}
	return os.WriteFile(outputPath, buf.Bytes(), 0644)
}
