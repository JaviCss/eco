package store

import (
	"database/sql"
	"errors"
	"strings"
)

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, sql.ErrNoRows) {
		return true
	}
	return strings.Contains(err.Error(), "no rows in result set")
}