package gitsync

import "github.com/jackc/pgx/v5/pgtype"

func toText(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
}

func toBool(b bool) pgtype.Bool {
	return pgtype.Bool{Bool: b, Valid: true}
}
