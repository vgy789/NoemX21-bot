package campuslabel

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/database/db"
)

// Format renders one stable label from two campus name variants.
// Use Pick when the UI language is available.
func Format(nameEn, nameRu string) string {
	nameEn = strings.TrimSpace(nameEn)
	nameRu = strings.TrimSpace(nameRu)

	switch {
	case nameEn == "" && nameRu == "":
		return ""
	case nameEn == "":
		return nameRu
	case nameRu == "" || strings.EqualFold(nameEn, nameRu):
		return nameEn
	default:
		return nameRu
	}
}

// FromDB prefers localized campus names and falls back to legacy API names.
func FromDB(nameEn, nameRu, shortName, fullName string) string {
	if label := Format(nameEn, nameRu); label != "" {
		return label
	}
	return Format(shortName, fullName)
}

// Lookup resolves a campus label by id and falls back to the provided text.
func Lookup(ctx context.Context, queries db.Querier, campusID pgtype.UUID, fallback string) string {
	if queries != nil && campusID.Valid {
		if campus, err := queries.GetCampusByID(ctx, campusID); err == nil {
			if label := FromDB(campus.NameEn.String, campus.NameRu.String, campus.ShortName, campus.FullName); label != "" {
				return label
			}
		}
	}
	return strings.TrimSpace(fallback)
}

// Pick returns a single campus name variant for the requested language.
func Pick(nameEn, nameRu, shortName, fullName, lang string) string {
	nameEn = strings.TrimSpace(nameEn)
	nameRu = strings.TrimSpace(nameRu)
	shortName = strings.TrimSpace(shortName)
	fullName = strings.TrimSpace(fullName)

	if lang == "en" {
		switch {
		case nameEn != "":
			return nameEn
		case nameRu != "":
			return nameRu
		case shortName != "":
			return shortName
		default:
			return fullName
		}
	}

	switch {
	case nameRu != "":
		return nameRu
	case nameEn != "":
		return nameEn
	case fullName != "":
		return fullName
	default:
		return shortName
	}
}

// Localize converts a combined "en / ru" label into a single localized value.
func Localize(label, lang string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return ""
	}

	parts := strings.SplitN(label, " / ", 2)
	if len(parts) == 1 {
		return label
	}

	first := strings.TrimSpace(parts[0])
	second := strings.TrimSpace(parts[1])

	if lang == "en" {
		if first != "" {
			return first
		}
		return second
	}

	if second != "" {
		return second
	}
	return first
}
