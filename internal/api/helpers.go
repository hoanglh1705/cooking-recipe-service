package api

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
)

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// errInternal wraps a server error so it surfaces as a clean 500 response
// without leaking the raw message to the client.
func errInternal(err error) error {
	return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
}
