package library

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/fsm/actions/common"
)

// Register registers library-related actions.
func Register(registry *fsm.LogicRegistry, queries db.Querier, aliasRegistrar func(alias, target string)) {
	if aliasRegistrar != nil {
		aliasRegistrar("LIBRARY_MENU", "library.yaml/AUTO_SYNC_USER_STATS")
	}

	registry.Register("get_user_summary", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return "", getUserSummary(ctx, queries, userID, payload), nil
	})

	registry.Register("prepare_search_context", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		return "", map[string]any{
			"search_query":           strings.TrimSpace(common.ToString(payload["last_input"])),
			"selected_category_id":   "",
			"page":                   1,
			"only_available":         false,
			"results_error_text":     "",
			"selected_book_id":       0,
			"shown_results_count":    0,
			"formatted_book_list":    "",
			"results_scope_label_ru": "",
			"results_scope_label_en": "",
		}, nil
	})

	registry.Register("search_books", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		return "", searchBooks(ctx, queries, userID, payload), nil
	})

	registry.Register("toggle_search_availability", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		onlyAvailable := common.ToBool(payload["only_available"])
		return "", map[string]any{
			"only_available":     !onlyAvailable,
			"page":               1,
			"results_error_text": "",
		}, nil
	})

	registry.Register("go_to_prev_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := common.ToInt(payload["page"])
		if page <= 1 {
			page = 1
		} else {
			page--
		}
		return "", map[string]any{"page": page, "results_error_text": ""}, nil
	})

	registry.Register("go_to_next_page", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		page := max(common.ToInt(payload["page"]), 1)
		totalPages := common.ToInt(payload["total_pages"])
		if totalPages <= 0 || page < totalPages {
			page++
		}
		return "", map[string]any{"page": page, "results_error_text": ""}, nil
	})

	registry.Register("prepare_category_search", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		id := strings.TrimSpace(common.ToString(payload["id"]))
		category := ""
		if after, ok := strings.CutPrefix(id, "cat_"); ok {
			category = strings.TrimSpace(common.ToString(payload["category_value_"+after]))
		}
		if category == "" && id != "" && id != "back" {
			// Backward compatibility for legacy callback data where category name was used as button ID.
			category = id
		}
		if category == "" {
			return "DISPLAY_CATEGORIES", nil, nil
		}
		return "", map[string]any{
			"search_query":         "",
			"selected_category_id": category,
			"page":                 1,
			"only_available":       false,
			"results_error_text":   "",
			"selected_book_id":     0,
		}, nil
	})

	registry.Register("select_book_by_number", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		raw := strings.TrimSpace(common.ToString(payload["last_input"]))
		num, err := strconv.Atoi(raw)
		if err != nil || num <= 0 {
			return "DISPLAY_RESULTS", map[string]any{"results_error_text": invalidBookNumberHint(payload)}, nil
		}

		bookID := common.ToInt16(payload[fmt.Sprintf("result_book_id_%d", num)])
		if bookID == 0 {
			return "DISPLAY_RESULTS", map[string]any{"results_error_text": invalidBookNumberHint(payload)}, nil
		}

		return "", map[string]any{
			"selected_book_id":   bookID,
			"results_error_text": "",
		}, nil
	})

	registry.Register("select_book_from_callback", func(_ context.Context, _ int64, payload map[string]any) (string, map[string]any, error) {
		raw := strings.TrimSpace(common.ToString(payload["id"]))
		if raw == "" {
			raw = strings.TrimSpace(common.ToString(payload["last_input"]))
		}
		bookID := extractBookID(raw)
		if bookID == 0 {
			return "DISPLAY_RESULTS", map[string]any{"results_error_text": invalidBookNumberHint(payload)}, nil
		}
		return "", map[string]any{
			"selected_book_id":   bookID,
			"results_error_text": "",
		}, nil
	})

	registry.Register("get_book_details", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		campusUUID := resolveCampusID(ctx, queries, userID, payload)
		bookID := resolveBookID(payload)
		if !campusUUID.Valid || bookID == 0 {
			return "", bookDetailsFallback(payload), nil
		}

		book, err := queries.GetBookByID(ctx, db.GetBookByIDParams{CampusID: campusUUID, ID: bookID})
		if err != nil {
			return "", bookDetailsFallback(payload), nil
		}

		isAvailable := book.AvailableStock > 0
		statusEmoji := "🔴"
		statusTextRU := "На руках"
		statusTextEN := "Borrowed"
		if isAvailable {
			statusEmoji = "🟢"
			statusTextRU = "Доступна"
			statusTextEN = "Available"
		}

		description := "Отсутствует"
		if lang := common.ToString(payload["language"]); lang == fsm.LangEn {
			description = "No yet"
		}
		if book.Description.Valid && strings.TrimSpace(book.Description.String) != "" && strings.TrimSpace(book.Description.String) != "-" {
			description = common.TrimRunes(strings.TrimSpace(book.Description.String), 300)
		}

		return "", map[string]any{
			"selected_book_id":    book.ID,
			"title":               book.Title,
			"author":              book.Author,
			"category":            book.Category,
			"status_emoji":        statusEmoji,
			"status_text_ru":      statusTextRU,
			"status_text_en":      statusTextEN,
			"description_snippet": description,
			"is_available":        isAvailable,
		}, nil
	})

	registry.Register("borrow_book", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		bookID := resolveBookID(payload)
		if bookID == 0 {
			return "", map[string]any{"success": false}, nil
		}

		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			return "", map[string]any{"success": false}, nil
		}

		campusUUID := resolveCampusID(ctx, queries, userID, payload)
		if !campusUUID.Valid {
			return "", map[string]any{"success": false}, nil
		}

		loc := getUserTimezoneForLibrary(ctx, queries, userID, campusUUID)
		dueAt := time.Now().In(loc).AddDate(0, 0, loanPeriodDays)

		loan, err := queries.CreateBookLoan(ctx, db.CreateBookLoanParams{
			CampusID: campusUUID,
			BookID:   bookID,
			UserID:   acc.ID,
			DueAt:    pgtype.Timestamptz{Time: dueAt, Valid: true},
		})
		if err != nil {
			return "", map[string]any{"success": false}, nil
		}

		title := strings.TrimSpace(common.ToString(payload["title"]))
		if title == "" {
			if book, err := queries.GetBookByID(ctx, db.GetBookByIDParams{CampusID: campusUUID, ID: bookID}); err == nil {
				title = book.Title
			}
		}
		if title == "" {
			title = "книгу"
		}

		dueDate := "—"
		if loan.DueAt.Valid {
			dueDate = loan.DueAt.Time.In(loc).Format("02.01.2006")
		}

		return "", map[string]any{
			"success":  true,
			"due_date": dueDate,
			"title":    title,
		}, nil
	})

	registry.Register("get_user_loans", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		vars := map[string]any{
			"my_loans_hint_ru":       "",
			"my_loans_hint_en":       "",
			"active_loans_count":     0,
			"overdue_count":          0,
			"user_status_message_ru": "📭 Книг пока не взято.",
			"user_status_message_en": "📭 No books have been taken yet.",
			"overdue_line_ru":        "",
			"overdue_line_en":        "",
		}
		for i := range maxLoanButtons {
			vars[fmt.Sprintf("loan_btn_id_%d", i+1)] = ""
			vars[fmt.Sprintf("loan_btn_label_%d", i+1)] = ""
		}

		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			vars["loans_list_formatted"] = noLoansText(payload)
			return "", vars, nil
		}

		loans, err := queries.GetUserBookLoans(ctx, acc.ID)
		if err != nil || len(loans) == 0 {
			vars["loans_list_formatted"] = noLoansText(payload)
			return "", vars, nil
		}

		loc := getUserTimezoneForLibrary(ctx, queries, userID, loans[0].CampusID)
		now := time.Now().In(loc)
		overdueCount := 0

		var sb strings.Builder
		btnCount := min(len(loans), maxLoanButtons)
		for i, loan := range loans {
			stateIcon := "✅"
			dueText := "—"
			if loan.DueAt.Valid {
				dueLocal := loan.DueAt.Time.In(loc)
				dueText = dueLocal.Format("02.01.2006")
				daysLeft := int(math.Ceil(dueLocal.Sub(now).Hours() / 24))
				if daysLeft <= 3 {
					stateIcon = "⚠️"
				}
				if daysLeft < 0 {
					stateIcon = "❗"
					overdueCount++
				}
			}
			sb.WriteString(fmt.Sprintf("%s *%d.* *%s* (%s)\n   🗓 До: `%s`\n\n", stateIcon, i+1, fsm.EscapeMarkdown(loan.BookTitle), fsm.EscapeMarkdown(loan.BookAuthor), dueText))
			if i < btnCount {
				vars[fmt.Sprintf("loan_btn_id_%d", i+1)] = fmt.Sprintf("return_%d", loan.ID)
				vars[fmt.Sprintf("loan_btn_label_%d", i+1)] = common.TrimRunes(strings.TrimSpace(loan.BookTitle), 28)
			}
		}

		if btnCount > 0 {
			vars["my_loans_hint_ru"] = "👇 Нажми на кнопку, чтобы вернуть:"
			vars["my_loans_hint_en"] = "👇 Click a button to return:"
		}
		vars["loans_list_formatted"] = sb.String()
		vars["active_loans_count"] = len(loans)
		vars["overdue_count"] = overdueCount
		if overdueCount > 0 {
			vars["user_status_message_ru"] = fmt.Sprintf("⚠️ Просрочено книг: %d", overdueCount)
			vars["user_status_message_en"] = fmt.Sprintf("⚠️ Overdue books: %d", overdueCount)
			vars["overdue_line_ru"] = fmt.Sprintf("⚠️ Пора вернуть: %d", overdueCount)
			vars["overdue_line_en"] = fmt.Sprintf("⚠️ Time to return: %d", overdueCount)
		} else {
			vars["user_status_message_ru"] = "✅ Все книги в срок."
			vars["user_status_message_en"] = "✅ All books are on time."
		}

		return "", vars, nil
	})

	registry.Register("return_book_loan", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		lastInput := strings.TrimSpace(common.ToString(payload["last_input"]))
		if lastInput == "" {
			lastInput = strings.TrimSpace(common.ToString(payload["id"]))
		}
		if lastInput == "" {
			return "FETCH_MY_BOOKS", nil, nil
		}

		loanID := int64(0)
		if after, ok := strings.CutPrefix(lastInput, "return_"); ok {
			loanID = common.ToInt64(after)
		}
		if loanID == 0 {
			return "FETCH_MY_BOOKS", nil, nil
		}

		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			return "FETCH_MY_BOOKS", nil, nil
		}

		_ = queries.ReturnBookLoan(ctx, db.ReturnBookLoanParams{
			ID:     loanID,
			UserID: acc.ID,
		})

		return "FETCH_MY_BOOKS", map[string]any{"success": true}, nil
	})

	registry.Register("get_book_categories", func(ctx context.Context, userID int64, payload map[string]any) (string, map[string]any, error) {
		campusUUID := resolveCampusID(ctx, queries, userID, payload)
		vars := make(map[string]any)
		for i := range maxCategoryButtons {
			vars[fmt.Sprintf("category_id_%d", i+1)] = ""
			vars[fmt.Sprintf("category_label_%d", i+1)] = ""
			vars[fmt.Sprintf("category_value_%d", i+1)] = ""
		}

		if !campusUUID.Valid {
			return "", vars, nil
		}

		cats, err := queries.GetBookCategories(ctx, campusUUID)
		if err != nil {
			return "", vars, nil
		}

		for i, category := range cats {
			if i >= maxCategoryButtons {
				break
			}
			vars[fmt.Sprintf("category_id_%d", i+1)] = fmt.Sprintf("cat_%d", i+1)
			vars[fmt.Sprintf("category_label_%d", i+1)] = common.TrimRunes(strings.TrimSpace(category), 28)
			vars[fmt.Sprintf("category_value_%d", i+1)] = strings.TrimSpace(category)
		}
		return "", vars, nil
	})
}

func getUserSummary(ctx context.Context, queries db.Querier, userID int64, payload map[string]any) map[string]any {
	vars := map[string]any{
		"campus_name":            "Campus",
		"active_loans_count":     0,
		"overdue_count":          0,
		"user_status_message_ru": "📭 На полке пока пусто.",
		"user_status_message_en": "📭 Your shelf is empty.",
		"overdue_line_ru":        "",
		"overdue_line_en":        "",
		"selected_category_id":   "",
		"search_query":           "",
		"only_available":         false,
		"page":                   1,
		"results_error_text":     "",
		"selected_book_id":       0,
		"shown_results_count":    0,
		"formatted_book_list":    "",
		"results_scope_label_ru": "",
		"results_scope_label_en": "",
		"filter_status_text_ru":  "Все книги",
		"filter_status_text_en":  "All books",
		"toggle_btn_label_ru":    "Только доступные",
		"toggle_btn_label_en":    "Available only",
		"page_caption_ru":        "Страница 1/1",
		"page_caption_en":        "Page 1/1",
		"has_prev_page":          false,
		"has_next_page":          false,
	}

	acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: fmt.Sprintf("%d", userID),
	})
	if err != nil {
		return vars
	}

	campusUUID := resolveCampusID(ctx, queries, userID, payload)
	if campusUUID.Valid {
		vars["campus_id"] = campusUUID
		if campus, err := queries.GetCampusByID(ctx, campusUUID); err == nil {
			vars["campus_name"] = campus.ShortName
		}
	}

	activeCount, _ := queries.GetUserActiveLoanCount(ctx, acc.ID)
	vars["active_loans_count"] = activeCount

	loans, _ := queries.GetUserBookLoans(ctx, acc.ID)
	overdue := 0
	loc := getUserTimezoneForLibrary(ctx, queries, userID, campusUUID)
	now := time.Now().In(loc)
	for _, loan := range loans {
		if loan.DueAt.Valid && loan.DueAt.Time.In(loc).Before(now) {
			overdue++
		}
	}
	vars["overdue_count"] = overdue

	switch {
	case overdue > 0:
		vars["user_status_message_ru"] = fmt.Sprintf("⚠️ Просрочено книг: %d", overdue)
		vars["user_status_message_en"] = fmt.Sprintf("⚠️ Overdue books: %d", overdue)
		vars["overdue_line_ru"] = fmt.Sprintf("⚠️ Пора вернуть: %d", overdue)
		vars["overdue_line_en"] = fmt.Sprintf("⚠️ Time to return: %d", overdue)
	case activeCount > 0:
		vars["user_status_message_ru"] = "✅ Все книги в срок"
		vars["user_status_message_en"] = "✅ All books are on time"
	default:
		vars["user_status_message_ru"] = "📭 Книг пока не взято"
		vars["user_status_message_en"] = "📭 No books have been taken yet"
	}

	return vars
}

func searchBooks(ctx context.Context, queries db.Querier, userID int64, payload map[string]any) map[string]any {
	query := strings.TrimSpace(common.ToString(payload["query"]))
	if strings.EqualFold(query, "search") {
		query = ""
	}
	category := strings.TrimSpace(common.ToString(payload["category_id"]))
	onlyAvailable := common.ToBool(payload["only_available"])
	page := max(common.ToInt(payload["page"]), 1)
	limit := common.ToInt(payload["limit"])
	if limit <= 0 {
		limit = searchPageLimit
	}

	vars := map[string]any{
		"search_query":         query,
		"selected_category_id": category,
		"only_available":       onlyAvailable,
		"results_error_text":   "",
	}

	campusUUID := resolveCampusID(ctx, queries, userID, payload)
	if !campusUUID.Valid {
		return fillSearchView(vars, nil, page, limit, query, category, onlyAvailable)
	}
	vars["campus_id"] = campusUUID

	books := make([]catalogBook, 0, 64)
	if query != "" {
		rows, err := queries.SearchBooks(ctx, db.SearchBooksParams{
			CampusID: campusUUID,
			Column2:  pgtype.Text{String: query, Valid: true},
			Limit:    1000,
			Offset:   0,
		})
		if err == nil {
			for _, row := range rows {
				books = append(books, catalogBook{
					ID:             row.ID,
					Title:          row.Title,
					Author:         row.Author,
					Category:       row.Category,
					AvailableStock: row.AvailableStock,
				})
			}
		}
	} else if category != "" {
		rows, err := queries.GetBooksByCampusAndCategory(ctx, db.GetBooksByCampusAndCategoryParams{
			CampusID: campusUUID,
			Category: category,
			Limit:    1000,
			Offset:   0,
		})
		if err == nil {
			for _, row := range rows {
				books = append(books, catalogBook{
					ID:             row.ID,
					Title:          row.Title,
					Author:         row.Author,
					Category:       row.Category,
					AvailableStock: row.AvailableStock,
				})
			}
		}
	} else {
		rows, err := queries.GetBooksByCampus(ctx, db.GetBooksByCampusParams{
			CampusID: campusUUID,
			Limit:    1000,
			Offset:   0,
		})
		if err == nil {
			for _, row := range rows {
				books = append(books, catalogBook{
					ID:             row.ID,
					Title:          row.Title,
					Author:         row.Author,
					Category:       row.Category,
					AvailableStock: row.AvailableStock,
				})
			}
		}
	}

	filtered := books
	if onlyAvailable {
		filtered = make([]catalogBook, 0, len(books))
		for _, b := range books {
			if b.AvailableStock > 0 {
				filtered = append(filtered, b)
			}
		}
	}

	return fillSearchView(vars, filtered, page, limit, query, category, onlyAvailable)
}

func fillSearchView(
	vars map[string]any,
	books []catalogBook,
	page, limit int,
	query, category string,
	onlyAvailable bool,
) map[string]any {
	if vars == nil {
		vars = make(map[string]any)
	}

	totalCount := len(books)
	totalPages := 1
	if totalCount > 0 {
		totalPages = int(math.Ceil(float64(totalCount) / float64(limit)))
	}
	if page > totalPages {
		page = totalPages
	}
	if page < 1 {
		page = 1
	}

	start := min((page-1)*limit, totalCount)
	end := min(start+limit, totalCount)
	pageBooks := books[start:end]

	vars["total_count"] = totalCount
	vars["total_pages"] = totalPages
	vars["page"] = page
	vars["has_prev_page"] = page > 1
	vars["has_next_page"] = page < totalPages
	vars["shown_results_count"] = len(pageBooks)
	vars["page_caption_ru"] = fmt.Sprintf("%d/%d", page, totalPages)
	vars["page_caption_en"] = fmt.Sprintf("%d/%d", page, totalPages)

	scopeRU := "весь каталог"
	scopeEN := "full catalog"
	switch {
	case query != "":
		scopeRU = fmt.Sprintf("поиск: «%s»", query)
		scopeEN = fmt.Sprintf("search: \"%s\"", query)
	case category != "":
		scopeRU = fmt.Sprintf("раздел: «%s»", category)
		scopeEN = fmt.Sprintf("category: \"%s\"", category)
	}
	vars["results_scope_label_ru"] = fsm.EscapeMarkdown(scopeRU)
	vars["results_scope_label_en"] = fsm.EscapeMarkdown(scopeEN)

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

	var listBuilder strings.Builder
	for i := range maxResultButtons {
		vars[fmt.Sprintf("book_id_%d", i+1)] = ""
		vars[fmt.Sprintf("book_btn_label_%d", i+1)] = ""
		vars[fmt.Sprintf("result_book_id_%d", i+1)] = 0
	}

	if len(pageBooks) == 0 {
		listBuilder.WriteString("Пусто по текущему запросу.")
	}

	for i, b := range pageBooks {
		if i >= maxResultButtons {
			break
		}
		num := i + 1
		icon := "🟢"
		if b.AvailableStock <= 0 {
			icon = "🔴"
		}
		listBuilder.WriteString(fmt.Sprintf("%s *%d.* *%s* (%s)\n", icon, num, fsm.EscapeMarkdown(b.Title), fsm.EscapeMarkdown(b.Author)))
		vars[fmt.Sprintf("book_id_%d", num)] = fmt.Sprintf("book_%d", b.ID)
		vars[fmt.Sprintf("book_btn_label_%d", num)] = fmt.Sprintf("%d. %s", num, b.Title)
		vars[fmt.Sprintf("result_book_id_%d", num)] = b.ID
	}

	vars["formatted_book_list_with_icons"] = listBuilder.String()
	return vars
}

func invalidBookNumberHint(payload map[string]any) string {
	maxNum := common.ToInt(payload["shown_results_count"])
	if maxNum <= 0 {
		maxNum = maxResultButtons
	}
	if common.ToString(payload["language"]) == fsm.LangEn {
		return fmt.Sprintf("⚠️ Enter a number from 1 to %d.", maxNum)
	}
	return fmt.Sprintf("⚠️ Введи номер от 1 до %d.", maxNum)
}

func noLoansText(payload map[string]any) string {
	if common.ToString(payload["language"]) == fsm.LangEn {
		return "📭 You have no active loans right now."
	}
	return "📭 Сейчас у тебя нет книг на руках."
}

func bookDetailsFallback(payload map[string]any) map[string]any {
	title := "Книга не найдена"
	desc := "Попробуй выбрать книгу из результатов поиска."
	status := "Ошибка"
	if common.ToString(payload["language"]) == fsm.LangEn {
		title = "Book not found"
		desc = "Try selecting a book from search results."
		status = "Error"
	}
	return map[string]any{
		"title":               title,
		"author":              "—",
		"category":            "—",
		"status_emoji":        "🔴",
		"status_text_ru":      status,
		"status_text_en":      status,
		"description_snippet": desc,
		"is_available":        false,
	}
}
