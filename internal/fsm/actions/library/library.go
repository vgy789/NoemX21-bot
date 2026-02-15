package library

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

// Register registers library-related actions.
func Register(registry *fsm.LogicRegistry, queries db.Querier, aliasRegistrar func(alias, target string)) {
	if aliasRegistrar != nil {
		aliasRegistrar("LIBRARY_MENU", "library.yaml/AUTO_SYNC_LIB")
	}

	registry.Register("get_library_stats", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		campusIDStr, _ := payload["campus_id"].(string)
		if campusIDStr == "" || campusIDStr == "$context.campus_id" {
			return "", nil, fmt.Errorf("campus_id missing")
		}
		var campusUUID pgtype.UUID
		_ = campusUUID.Scan(campusIDStr)

		// Defaults
		vars := map[string]any{
			"total_books_count":     "0",
			"available_count":       "0",
			"my_active_loans_count": "0",
		}

		// Count books
		counts, err := queries.CountBooksByCampus(ctx, campusUUID)
		if err == nil {
			vars["total_books_count"] = fmt.Sprintf("%d", counts.TotalBooks)
			vars["available_count"] = fmt.Sprintf("%d", counts.AvailableBooks)
		}

		// Count my loans
		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err == nil {
			myCount, err := queries.GetUserActiveLoanCount(ctx, acc.ID)
			if err == nil {
				vars["my_active_loans_count"] = fmt.Sprintf("%d", myCount)
			}
		}

		return "", vars, nil
	})

	registry.Register("get_books", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		campusIDStr, _ := payload["campus_id"].(string)
		var campusUUID pgtype.UUID
		_ = campusUUID.Scan(campusIDStr)

		search, _ := payload["search"].(string)

		limit := 10
		offset := toInt(payload["offset"]) // 0 by default

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
				Limit:    int32(limit + 1),
				Offset:   int32(offset),
			})
			if err == nil {
				for i, b := range bs {
					if i >= limit {
						break
					}
					resultBooks = append(resultBooks, BookRow{ID: b.ID, Title: b.Title, Author: b.Author, AvailableStock: 1})
				}
			}
		} else if cat, ok := payload["category_id"].(string); ok && cat != "" {
			bs, err := queries.GetBooksByCampusAndCategory(ctx, db.GetBooksByCampusAndCategoryParams{
				CampusID: campusUUID,
				Category: cat,
				Limit:    int32(limit + 1),
				Offset:   int32(offset),
			})
			if err == nil {
				for i, b := range bs {
					if i >= limit {
						break
					}
					resultBooks = append(resultBooks, BookRow{ID: b.ID, Title: b.Title, Author: b.Author, AvailableStock: 1})
				}
			}
		} else if auth, ok := payload["author_id"].(string); ok && auth != "" {
			bs, err := queries.GetBooksByCampusAndAuthor(ctx, db.GetBooksByCampusAndAuthorParams{
				CampusID: campusUUID,
				Author:   auth,
				Limit:    int32(limit + 1),
				Offset:   int32(offset),
			})
			if err == nil {
				for i, b := range bs {
					if i >= limit {
						break
					}
					resultBooks = append(resultBooks, BookRow{ID: b.ID, Title: b.Title, Author: b.Author, AvailableStock: 1})
				}
			}
		} else {
			// Default GetBooksByCampus
			bs, err := queries.GetBooksByCampus(ctx, db.GetBooksByCampusParams{
				CampusID: campusUUID,
				Limit:    int32(limit + 1),
				Offset:   int32(offset),
			})
			if err == nil {
				for i, b := range bs {
					if i >= limit {
						break
					}
					resultBooks = append(resultBooks, BookRow{ID: b.ID, Title: b.Title, Author: b.Author, AvailableStock: 1})
				}
			}
		}

		vars := make(map[string]any)
		current_page := (offset / limit) + 1
		vars["current_page"] = current_page
		vars["total_pages"] = 99               // Mock total pages for now
		vars["total_count"] = len(resultBooks) // + offset?

		var sb strings.Builder
		if len(resultBooks) == 0 {
			sb.WriteString("Ничего не найдено.")
		}

		for i, b := range resultBooks {
			// Escape title and author since they will be wrapped in Markdown markers
			sb.WriteString(fmt.Sprintf("%d. *%s* (%s)\n", offset+i+1, fsm.EscapeMarkdown(b.Title), fsm.EscapeMarkdown(b.Author)))
			if i < 10 {
				kID := fmt.Sprintf("book_btn_id_%d", i+1)
				kLbl := fmt.Sprintf("book_btn_label_%d", i+1)
				vars[kID] = fmt.Sprintf("book_%d", b.ID)
				vars[kLbl] = fmt.Sprintf("%d. %s", offset+i+1, b.Title)
			}
		}
		vars["books_list_numbered"] = sb.String()

		for i := len(resultBooks); i < 10; i++ {
			vars[fmt.Sprintf("book_btn_id_%d", i+1)] = ""
			vars[fmt.Sprintf("book_btn_label_%d", i+1)] = ""
		}

		return "", vars, nil
	})

	registry.Register("update_page", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		offset := toInt(payload["current_offset"])
		delta := toInt(payload["delta"])
		limit := 10

		newOffset := max(offset+(delta*limit), 0)

		return "", map[string]any{"offset": newOffset}, nil
	})

	// get_book_details: fetch a single book by ID
	registry.Register("get_book_details", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		campusIDStr, _ := payload["campus_id"].(string)
		var campusUUID pgtype.UUID
		_ = campusUUID.Scan(campusIDStr)

		bookIDRaw := payload["book_id"]
		bookID := toInt16(bookIDRaw)

		// Also try last_input callback data: "book_3" -> 3
		if bookID == 0 {
			if lastInput, ok := payload["_last_input"].(string); ok {
				bookID = extractBookID(lastInput)
			}
		}
		if bookID == 0 {
			if lastInput, ok := payload["last_input"].(string); ok {
				bookID = extractBookID(lastInput)
			}
		}

		if bookID == 0 {
			return "", map[string]any{
				"book_title":         "Книга не найдена",
				"book_author":        "—",
				"book_category":      "—",
				"status_emoji":       "❓",
				"status_description": "Не удалось найти книгу",
				"loan_info":          "",
				"is_available":       false,
				"selected_book_id":   0,
			}, nil
		}

		book, err := queries.GetBookByID(ctx, db.GetBookByIDParams{
			CampusID: campusUUID,
			ID:       bookID,
		})
		if err != nil {
			return "", map[string]any{
				"book_title":         "Книга не найдена",
				"book_author":        "—",
				"book_category":      "—",
				"status_emoji":       "❓",
				"status_description": fmt.Sprintf("Ошибка: %v", err),
				"loan_info":          "",
				"is_available":       false,
				"selected_book_id":   bookID,
			}, nil
		}

		isAvailable := book.AvailableStock > 0
		statusEmoji := "✅"
		statusDesc := "Доступна"
		if !isAvailable {
			statusEmoji = "❌"
			statusDesc = "Все экземпляры на руках"
		}

		desc := ""
		if book.Description.Valid && book.Description.String != "" {
			// Do not use italics wrapped around dynamic content that may contain underscores
			desc = fmt.Sprintf("\n📝 %s", fsm.EscapeMarkdown(book.Description.String))
		}

		return "", map[string]any{
			"book_title":         book.Title,
			"book_author":        book.Author,
			"book_category":      book.Category,
			"status_emoji":       statusEmoji,
			"status_description": statusDesc,
			"loan_info":          desc,
			"is_available":       isAvailable,
			"selected_book_id":   bookID,
		}, nil
	})

	registry.Register("borrow_book", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		campusIDStr, _ := payload["campus_id"].(string)
		var campusUUID pgtype.UUID
		_ = campusUUID.Scan(campusIDStr)

		bookID := toInt16(payload["book_id"])
		if bookID == 0 {
			return "", map[string]any{"success": false}, fmt.Errorf("book_id missing")
		}

		// Get user account
		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			return "", map[string]any{"success": false}, fmt.Errorf("user not found: %w", err)
		}

		// Create loan
		loan, err := queries.CreateBookLoan(ctx, db.CreateBookLoanParams{
			CampusID: campusUUID,
			BookID:   bookID,
			UserID:   acc.ID,
		})
		if err != nil {
			return "", map[string]any{"success": false}, fmt.Errorf("failed to borrow: %w", err)
		}

		dueDate := "—"
		if loan.DueAt.Valid {
			dueDate = loan.DueAt.Time.Format("02.01.2006")
		}

		return "", map[string]any{
			"success":  true,
			"due_date": dueDate,
		}, nil
	})

	registry.Register("get_user_loans", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			return "", map[string]any{"my_books_list_formatted": "❌ Не удалось получить данные"}, nil
		}

		loans, err := queries.GetUserBookLoans(ctx, acc.ID)
		if err != nil {
			return "", map[string]any{"my_books_list_formatted": "❌ Ошибка при загрузке"}, nil
		}

		if len(loans) == 0 {
			return "", map[string]any{"my_books_list_formatted": "📭 У тебя пока нет книг на руках."}, nil
		}

		var sb strings.Builder
		for i, l := range loans {
			due := "—"
			if l.DueAt.Valid {
				due = l.DueAt.Time.Format("02.01.2006")
			}
			// Escape book title and author
			sb.WriteString(fmt.Sprintf("%d. *%s* (%s)\n   🗓 Вернуть до: `%s`\n", i+1, fsm.EscapeMarkdown(l.BookTitle), fsm.EscapeMarkdown(l.BookAuthor), due))
		}

		return "", map[string]any{"my_books_list_formatted": sb.String()}, nil
	})

	// get_categories: fetch unique book categories for the user's campus
	registry.Register("get_book_categories", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		campusIDStr, _ := payload["campus_id"].(string)
		if campusIDStr == "" || campusIDStr == "$context.campus_id" {
			// Try to resolve from DB
			acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
				Platform:   db.EnumPlatformTelegram,
				ExternalID: fmt.Sprintf("%d", userID),
			})
			if err == nil {
				profile, err := queries.GetMyProfile(ctx, acc.S21Login)
				if err == nil && profile.CampusID.Valid {
					b := profile.CampusID.Bytes
					campusIDStr = fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
				}
			}
		}

		var campusUUID pgtype.UUID
		_ = campusUUID.Scan(campusIDStr)

		vars := make(map[string]any)
		cats, err := queries.GetBookCategories(ctx, campusUUID)
		if err == nil {
			maxCats := 15
			for i, c := range cats {
				if i >= maxCats {
					break
				}
				vars[fmt.Sprintf("category_%d", i+1)] = c
			}
			// Clear remainder
			for i := len(cats) + 1; i <= 15; i++ {
				vars[fmt.Sprintf("category_%d", i)] = ""
			}
		}
		return "", vars, nil
	})

	// get_authors: fetch unique book authors for the user's campus
	registry.Register("get_book_authors", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		campusIDStr, _ := payload["campus_id"].(string)
		if campusIDStr == "" || campusIDStr == "$context.campus_id" {
			// Try to resolve from DB
			acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
				Platform:   db.EnumPlatformTelegram,
				ExternalID: fmt.Sprintf("%d", userID),
			})
			if err == nil {
				profile, err := queries.GetMyProfile(ctx, acc.S21Login)
				if err == nil && profile.CampusID.Valid {
					b := profile.CampusID.Bytes
					campusIDStr = fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
				}
			}
		}

		var campusUUID pgtype.UUID
		_ = campusUUID.Scan(campusIDStr)

		vars := make(map[string]any)
		authors, err := queries.GetBookAuthors(ctx, campusUUID)
		if err == nil {
			maxAuthors := 10
			for i, a := range authors {
				if i >= maxAuthors {
					break
				}
				num := i + 1
				vars[fmt.Sprintf("author_id_%d", num)] = a
				vars[fmt.Sprintf("author_name_%d", num)] = a
			}
			// Clear remainder
			for i := len(authors) + 1; i <= maxAuthors; i++ {
				vars[fmt.Sprintf("author_id_%d", i)] = ""
				vars[fmt.Sprintf("author_name_%d", i)] = ""
			}
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
	// Try format "book_3" -> 3
	s = strings.TrimPrefix(s, "book_")
	var id int16
	fmt.Sscanf(s, "%d", &id)
	return id
}
