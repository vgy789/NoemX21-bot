package library

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

// Register registers library-related actions.
func Register(registry *fsm.LogicRegistry, queries db.Querier, aliasRegistrar func(alias, target string)) {
	if aliasRegistrar != nil {
		aliasRegistrar("LIBRARY_MENU", "library.yaml/AUTO_SYNC_USER_STATS")
	}

	// Helper to get stats
	getLibraryStats := func(ctx context.Context, userID int64, payload map[string]any) (map[string]any, error) {
		campusUUID := robustScanUUID(payload["campus_id"])

		// Defaults
		vars := map[string]any{
			"total_books_count":     "0",
			"available_count":       "0",
			"my_active_loans_count": "0",
			"active_loans_count":    "0", // Alias
			"overdue_count":         "0",
			"user_firstname":        "Student",
			"campus_name":           "Campus",
			"user_status_message":   "Нет долгов",
		}

		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err == nil {
			// Get profile for name
			if p, err := queries.GetMyProfile(ctx, acc.S21Login); err == nil {
				vars["user_firstname"] = acc.S21Login
				if !campusUUID.Valid {
					campusUUID = p.CampusID
				}
			}

			// My active loans
			if myCount, err := queries.GetUserActiveLoanCount(ctx, acc.ID); err == nil {
				vars["my_active_loans_count"] = fmt.Sprintf("%d", myCount)
				vars["active_loans_count"] = fmt.Sprintf("%d", myCount)
			}

			if loans, err := queries.GetUserBookLoans(ctx, acc.ID); err == nil {
				overdue := 0
				for _, l := range loans {
					// Logic for overdue: if DueAt < Now
					if l.DueAt.Valid && l.DueAt.Time.Before(time.Now()) {
						overdue++
					}
				}
				vars["overdue_count"] = fmt.Sprintf("%d", overdue)
			}
		}

		if campusUUID.Valid {
			vars["campus_id"] = campusUUID // Pass back as UUID object/string for context stability

			// Try to get real campus name
			if c, err := queries.GetCampusByID(ctx, campusUUID); err == nil {
				vars["campus_name"] = c.ShortName
			}

			if counts, err := queries.CountBooksByCampus(ctx, campusUUID); err == nil {
				vars["total_books_count"] = fmt.Sprintf("%d", counts.TotalBooks)
				vars["available_count"] = fmt.Sprintf("%d", counts.AvailableBooks)
			}
		}

		return vars, nil
	}

	registry.Register("get_library_stats", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		vars, err := getLibraryStats(ctx, userID, payload)
		return "", vars, err
	})

	registry.Register("get_user_summary", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		vars, err := getLibraryStats(ctx, userID, payload)
		return "", vars, err
	})

	registry.Register("search_books", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		campusUUID := robustScanUUID(payload["campus_id"])
		if !campusUUID.Valid {
			// Try to resolve campus from profile if missing
			acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
				Platform:   db.EnumPlatformTelegram,
				ExternalID: fmt.Sprintf("%d", userID),
			})
			if err == nil {
				if p, err := queries.GetMyProfile(ctx, acc.S21Login); err == nil {
					campusUUID = p.CampusID
				}
			}
		}

		search, _ := payload["query"].(string)
		search = strings.TrimSpace(search)
		// Only clear if it is EXACTLY "search" (engine artifact)
		if strings.ToLower(search) == "search" {
			search = ""
		}

		page := max(toInt(payload["page"]), 1)
		limit := toInt(payload["limit"])
		if limit < 1 {
			limit = 5
		}
		offset := (page - 1) * limit

		onlyAvailable, _ := payload["only_available"].(bool)

		type BookRow struct {
			ID             int16
			Title          string
			Author         string
			AvailableStock int32
		}
		var resultBooks []BookRow

		// Determine query
		if search != "" {
			bs, err := queries.SearchBooks(ctx, db.SearchBooksParams{
				CampusID: campusUUID,
				Column2:  pgtype.Text{String: search, Valid: true},
				Limit:    100, // Fetch more to filter by availability in Go if needed, or just for pagination
				Offset:   int32(offset),
			})
			if err == nil {
				for _, b := range bs {
					if onlyAvailable && b.AvailableStock <= 0 {
						continue
					}
					resultBooks = append(resultBooks, BookRow{ID: b.ID, Title: b.Title, Author: b.Author, AvailableStock: b.AvailableStock})
					if len(resultBooks) > limit {
						break
					}
				}
			}
		} else if cat, ok := payload["category_id"].(string); ok && cat != "" {
			bs, err := queries.GetBooksByCampusAndCategory(ctx, db.GetBooksByCampusAndCategoryParams{
				CampusID: campusUUID,
				Category: cat,
				Limit:    100,
				Offset:   int32(offset),
			})
			if err == nil {
				for _, b := range bs {
					if onlyAvailable && b.AvailableStock <= 0 {
						continue
					}
					resultBooks = append(resultBooks, BookRow{ID: b.ID, Title: b.Title, Author: b.Author, AvailableStock: b.AvailableStock})
					if len(resultBooks) > limit {
						break
					}
				}
			}
		} else {
			// Default: list all
			bs, err := queries.GetBooksByCampus(ctx, db.GetBooksByCampusParams{
				CampusID: campusUUID,
				Limit:    100,
				Offset:   int32(offset),
			})
			if err == nil {
				for _, b := range bs {
					if onlyAvailable && b.AvailableStock <= 0 {
						continue
					}
					resultBooks = append(resultBooks, BookRow{ID: b.ID, Title: b.Title, Author: b.Author, AvailableStock: b.AvailableStock})
					if len(resultBooks) > limit {
						break
					}
				}
			}
		}

		vars := make(map[string]any)
		vars["page"] = page

		var totalCount int32
		if search != "" {
			totalCount, _ = queries.CountSearchBooks(ctx, db.CountSearchBooksParams{
				CampusID: campusUUID,
				Column2:  pgtype.Text{String: search, Valid: true},
			})
		} else if cat, ok := payload["category_id"].(string); ok && cat != "" {
			totalCount, _ = queries.CountBooksByCategory(ctx, db.CountBooksByCategoryParams{
				CampusID: campusUUID,
				Category: cat,
			})
		} else {
			counts, _ := queries.CountBooksByCampus(ctx, campusUUID)
			totalCount = counts.TotalBooks
		}
		vars["total_count"] = totalCount

		hasNext := len(resultBooks) > limit
		if hasNext {
			resultBooks = resultBooks[:limit]
		}

		totalPages := int(totalCount / int32(limit))
		if totalCount%int32(limit) > 0 {
			totalPages++
		}

		if totalPages < 1 {
			totalPages = 1
		}
		vars["total_pages"] = totalPages

		if onlyAvailable {
			vars["filter_status_text_ru"] = "Только доступные"
			vars["filter_status_text_en"] = "Available only"
			vars["toggle_btn_label_ru"] = "Показать все"
			vars["toggle_btn_label_en"] = "Show all"
		} else {
			vars["filter_status_text_ru"] = "Все книги"
			vars["filter_status_text_en"] = "All books"
			vars["toggle_btn_label_ru"] = "Только доступные"
			vars["toggle_btn_label_en"] = "Available only"
		}

		var sb strings.Builder
		if len(resultBooks) == 0 {
			sb.WriteString("Ничего не найдено.")
		}

		for i, b := range resultBooks {
			icon := "🟢" // Available
			if b.AvailableStock <= 0 {
				icon = "🔴" // Taken
			}
			// Use offset from context if we want global numbering, but here we use page-local
			sb.WriteString(fmt.Sprintf("*%d.* %s *%s* (%s)\n", i+1, icon, fsm.EscapeMarkdown(b.Title), fsm.EscapeMarkdown(b.Author)))

			if i < 7 {
				kID := fmt.Sprintf("book_id_%d", i+1)
				vars[kID] = fmt.Sprintf("book_%d", b.ID)
				vars[fmt.Sprintf("short_title_%d", i+1)] = b.Title
			}
		}
		vars["formatted_book_list_with_icons"] = sb.String()

		for i := len(resultBooks) + 1; i <= 7; i++ {
			vars[fmt.Sprintf("book_id_%d", i)] = ""
			vars[fmt.Sprintf("short_title_%d", i)] = ""
		}

		return "", vars, nil
	})

	registry.Register("set_context", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return "", payload, nil
	})

	registry.Register("toggle_boolean_context", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		key, _ := payload["key"].(string)
		if key == "" {
			return "", nil, nil
		}

		val, _ := payload[key].(bool) // Should be in payload due to merge
		newVal := !val

		ret := map[string]any{key: newVal}
		if reset, ok := payload["reset_pagination"].(bool); ok && reset {
			ret["page"] = 1
		}
		return "", ret, nil
	})

	registry.Register("increment_context", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		key, _ := payload["key"].(string)
		val, _ := payload["value"].(float64)
		if val == 0 {
			if v, ok := payload["value"].(int); ok {
				val = float64(v)
			}
		}

		current, _ := payload[key].(float64) // Should be in payload due to merge
		if current == 0 {
			if v, ok := payload[key].(int); ok {
				current = float64(v)
			}
		}

		newVal := current + val
		if newVal < 1 && key == "page" {
			newVal = 1
		}
		return "", map[string]any{key: int(newVal)}, nil
	})

	registry.Register("get_book_details", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		campusUUID := robustScanUUID(payload["campus_id"])

		bookID := toInt16(payload["book_id"])
		if bookID == 0 {
			if lastInput, ok := payload["last_input"].(string); ok {
				bookID = extractBookID(lastInput)
			}
		}

		if bookID == 0 {
			return "", map[string]any{"title": "Книга не найдена", "status_text_ru": "Ошибка", "is_available": false}, nil
		}

		book, err := queries.GetBookByID(ctx, db.GetBookByIDParams{CampusID: campusUUID, ID: bookID})
		if err != nil {
			return "", map[string]any{"title": "Книга не найдена", "status_text_ru": "Ошибка загрузки", "is_available": false}, nil
		}

		isAvailable := book.AvailableStock > 0
		statusEmoji := "✅"
		statusTextRu := "Доступна"
		if !isAvailable {
			statusEmoji = "❌"
			statusTextRu = "На руках"
		}

		desc := ""
		if book.Description.Valid {
			desc = bTrunc(book.Description.String, 150)
		}

		return "", map[string]any{
			"title": book.Title, "author": book.Author, "category": book.Category,
			"status_emoji": statusEmoji, "status_text_ru": statusTextRu, "status_text_en": statusTextRu,
			"description_snippet": desc, "shelf_number": "—", "is_available": isAvailable,
			"selected_book_id": bookID,
		}, nil
	})

	registry.Register("borrow_book", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		campusUUID := robustScanUUID(payload["campus_id"])

		bookID := toInt16(payload["book_id"])
		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform: db.EnumPlatformTelegram, ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			return "", map[string]any{"success": false}, err
		}

		// Resolve timezone
		loc := getUserTimezoneForLibrary(ctx, queries, userID, campusUUID)
		due := time.Now().In(loc).AddDate(0, 0, 14)

		loan, err := queries.CreateBookLoan(ctx, db.CreateBookLoanParams{
			CampusID: campusUUID, BookID: bookID, UserID: acc.ID,
			DueAt: pgtype.Timestamptz{Time: due, Valid: true},
		})
		if err != nil {
			return "", map[string]any{"success": false}, err
		}

		dueDate := "—"
		if loan.DueAt.Valid {
			dueDate = loan.DueAt.Time.In(loc).Format("02.01.2006")
		}

		ret := map[string]any{"success": true, "due_date": dueDate}
		if title, ok := payload["title"].(string); ok {
			ret["title"] = title
		}
		return "", ret, nil
	})

	registry.Register("get_user_loans", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		acc, _ := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform: db.EnumPlatformTelegram, ExternalID: fmt.Sprintf("%d", userID),
		})
		loans, _ := queries.GetUserBookLoans(ctx, acc.ID)

		var loc *time.Location = time.UTC
		if len(loans) > 0 {
			// Try to resolve timezone using the first loan's campus
			loc = getUserTimezoneForLibrary(ctx, queries, userID, loans[0].CampusID)
		}

		var sb strings.Builder
		if len(loans) == 0 {
			sb.WriteString("📭 Не найдено активных книг.")
		}
		for i, l := range loans {
			due := "—"
			if l.DueAt.Valid {
				due = l.DueAt.Time.In(loc).Format("02.01.2006")
			}
			sb.WriteString(fmt.Sprintf("*%d.* *%s* (%s)\n   🗓 До: `%s`\n", i+1, fsm.EscapeMarkdown(l.BookTitle), fsm.EscapeMarkdown(l.BookAuthor), due))
		}
		return "", map[string]any{"loans_list_formatted": sb.String(), "my_books_list_formatted": sb.String()}, nil
	})

	registry.Register("get_book_categories", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		campusUUID := robustScanUUID(payload["campus_id"])

		vars := make(map[string]any)
		cats, _ := queries.GetBookCategories(ctx, campusUUID)
		for i, c := range cats {
			if i >= 15 {
				break
			}
			vars[fmt.Sprintf("category_%d", i+1)] = c
		}
		return "", vars, nil
	})

	registry.Register("get_book_authors", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		campusUUID := robustScanUUID(payload["campus_id"])

		vars := make(map[string]any)
		authors, _ := queries.GetBookAuthors(ctx, campusUUID)
		for i, a := range authors {
			if i >= 10 {
				break
			}
			vars[fmt.Sprintf("author_id_%d", i+1)] = a
			vars[fmt.Sprintf("author_name_%d", i+1)] = a
		}
		return "", vars, nil
	})
}

func toInt(v any) int {
	switch val := v.(type) {
	case int:
		return val
	case int32:
		return int(val)
	case int64:
		return int(val)
	case float64:
		return int(val)
	case string:
		var n int
		fmt.Sscanf(val, "%d", &n)
		return n
	}
	return 0
}

func toInt16(v any) int16 {
	switch val := v.(type) {
	case int:
		return int16(val)
	case int16:
		return val
	case int32:
		return int16(val)
	case int64:
		return int16(val)
	case float64:
		return int16(val)
	case string:
		var n int16
		fmt.Sscanf(val, "%d", &n)
		return n
	}
	return 0
}

func extractBookID(s string) int16 {
	s = strings.TrimPrefix(s, "book_")
	var id int16
	fmt.Sscanf(s, "%d", &id)
	return id
}

func getUserTimezoneForLibrary(ctx context.Context, queries db.Querier, userID int64, campusUUID pgtype.UUID) *time.Location {
	defaultLoc := time.UTC
	if moscow, err := time.LoadLocation("Europe/Moscow"); err == nil {
		defaultLoc = moscow
	}

	acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: fmt.Sprintf("%d", userID),
	})
	if err == nil {
		if u, err := queries.GetRegisteredUserByS21Login(ctx, acc.S21Login); err == nil {
			if u.Timezone != "" {
				if loc, err := time.LoadLocation(u.Timezone); err == nil {
					return loc
				}
			}
		}
	}

	if campusUUID.Valid {
		if c, err := queries.GetCampusByID(ctx, campusUUID); err == nil {
			if c.Timezone.Valid && c.Timezone.String != "" {
				if loc, err := time.LoadLocation(c.Timezone.String); err == nil {
					return loc
				}
			}
		}
	}

	return defaultLoc
}

func bTrunc(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

func robustScanUUID(v any) pgtype.UUID {
	var uuid pgtype.UUID
	if v == nil {
		return uuid
	}
	switch val := v.(type) {
	case string:
		if strings.HasPrefix(val, "$context.") {
			return uuid
		}
		_ = uuid.Scan(val)
	case [16]byte:
		uuid.Bytes = val
		uuid.Valid = true
	case []byte:
		if len(val) == 16 {
			copy(uuid.Bytes[:], val)
			uuid.Valid = true
		} else {
			_ = uuid.Scan(string(val))
		}
	case pgtype.UUID:
		return val
	}
	return uuid
}
