package gitsync

import "github.com/jackc/pgx/v5/pgtype"

func toText(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
}

func toBool(b *bool) pgtype.Bool {
	val := true
	if b != nil {
		val = *b
	}
	return pgtype.Bool{Bool: val, Valid: true}
}
