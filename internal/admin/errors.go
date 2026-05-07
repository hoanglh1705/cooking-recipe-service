package admin

import (
	"errors"
	"net/http"

	"github.com/cooking-recipe/cooking-recipe-service/internal/db"

	"github.com/labstack/echo/v4"
)

// adminError trả error theo schema thống nhất ở docs/admin_architech.md §6.7:
//
//	{"error": {"code": "...", "message": "...", "fields": {"slug": "must be unique"}}}
//
// `fields` chỉ xuất hiện khi đó là validation lỗi, FE map vào field tương ứng.
func adminError(c echo.Context, status int, code, message string, fields map[string]string) error {
	body := echo.Map{"code": code, "message": message}
	if len(fields) > 0 {
		body["fields"] = fields
	}
	return c.JSON(status, echo.Map{"error": body})
}

// dbErr translates các sentinel error từ db layer sang adminError chuẩn.
func dbErr(c echo.Context, err error) error {
	switch {
	case errors.Is(err, db.ErrNotFound):
		return adminError(c, http.StatusNotFound, "NOT_FOUND", "resource does not exist", nil)
	default:
		return adminError(c, http.StatusInternalServerError, "DB_ERROR", err.Error(), nil)
	}
}
