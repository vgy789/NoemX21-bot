package common

import (
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
)

func campusNameString(name any) string {
	switch v := name.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case pgtype.Text:
		if v.Valid {
			return v.String
		}
		return ""
	case interface{ String() string }:
		return v.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}
