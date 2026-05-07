package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/cooking-recipe/cooking-recipe-service/internal/db"

	"github.com/labstack/echo/v4"
)

// GET /api/dishes?category=&tag=&region=&has_recipe=&limit=&offset=
//
// `category` = category slug (exact).
// `tag`      = case-insensitive match với cả `dishes.keywords` lẫn
//              `categories.tags` của các category mà dish đang nằm trong.
// `region`   = exact match (`mien-bac`, `mien-trung`, `mien-nam`).
// `has_recipe=true|false` lọc theo việc dish đã có recipe publish hay chưa.
func (h *Handler) listDishes(c echo.Context) error {
	f := db.DishFilter{
		CategorySlug: c.QueryParam("category"),
		Tag:          c.QueryParam("tag"),
		Region:       c.QueryParam("region"),
		Limit:        atoiDefault(c.QueryParam("limit"), 50),
		Offset:       atoiDefault(c.QueryParam("offset"), 0),
	}
	if hr := strings.TrimSpace(c.QueryParam("has_recipe")); hr != "" {
		if b, err := strconv.ParseBool(hr); err == nil {
			f.HasRecipe = &b
		}
	}

	out, err := h.Q.ListDishesPublic(c.Request().Context(), f)
	if err != nil {
		return errInternal(err)
	}
	return c.JSON(http.StatusOK, out)
}

// GET /api/dishes/:slug
func (h *Handler) getDish(c echo.Context) error {
	slug := c.Param("slug")
	d, err := h.Q.GetDishPublic(c.Request().Context(), slug)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "dish not found")
		}
		return errInternal(err)
	}
	return c.JSON(http.StatusOK, d)
}

// GET /api/tags — union các tag từ dishes.keywords + categories.tags.
func (h *Handler) listTags(c echo.Context) error {
	tags, err := h.Q.ListAllTags(c.Request().Context())
	if err != nil {
		return errInternal(err)
	}
	return c.JSON(http.StatusOK, tags)
}
