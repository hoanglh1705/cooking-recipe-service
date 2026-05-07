package admin

import (
	"net/http"
	"strings"

	"github.com/cooking-recipe/cooking-recipe-service/internal/db"

	"github.com/labstack/echo/v4"
)

// categoryReq accepts cả `name_vi` (legacy schema-aligned) lẫn `name` (theo
// spec admin §6.4) — admin SPA gửi field nào cũng được.
type categoryReq struct {
	Slug         string   `json:"slug"`
	NameVi       string   `json:"name_vi"`
	Name         string   `json:"name"` // alias cho name_vi
	NameEn       *string  `json:"name_en,omitempty"`
	Description  *string  `json:"description,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Icon         *string  `json:"icon,omitempty"`
	SortOrder    int      `json:"sort_order"`
	DisplayOrder int      `json:"display_order"` // alias cho sort_order
	Visible      *bool    `json:"visible,omitempty"`
	ShowOnHome   *bool    `json:"show_on_home,omitempty"`
}

func (r categoryReq) resolvedName() string {
	if r.NameVi != "" {
		return r.NameVi
	}
	return r.Name
}

func (r categoryReq) resolvedSort() int {
	if r.SortOrder != 0 {
		return r.SortOrder
	}
	return r.DisplayOrder
}

func (r categoryReq) toModel(existing *db.Category) db.Category {
	c := db.Category{
		Slug:        r.Slug,
		NameVi:      r.resolvedName(),
		NameEn:      r.NameEn,
		Description: r.Description,
		Tags:        r.Tags,
		Icon:        r.Icon,
		SortOrder:   r.resolvedSort(),
		Visible:     true,
		ShowOnHome:  false,
	}
	if r.Visible != nil {
		c.Visible = *r.Visible
	} else if existing != nil {
		c.Visible = existing.Visible
	}
	if r.ShowOnHome != nil {
		c.ShowOnHome = *r.ShowOnHome
	} else if existing != nil {
		c.ShowOnHome = existing.ShowOnHome
	}
	return c
}

// GET /api/admin/categories
func (h *Handler) listCategoriesAdmin(c echo.Context) error {
	out, err := h.Q.ListCategories(c.Request().Context(), db.CategoryFilter{})
	if err != nil {
		return dbErr(c, err)
	}
	return c.JSON(http.StatusOK, out)
}

// POST /api/admin/categories
func (h *Handler) createCategory(c echo.Context) error {
	var req categoryReq
	if err := c.Bind(&req); err != nil {
		return adminError(c, http.StatusBadRequest, "INVALID_BODY", "expected JSON body", nil)
	}
	fields := map[string]string{}
	if strings.TrimSpace(req.Slug) == "" {
		fields["slug"] = "is required"
	}
	if strings.TrimSpace(req.resolvedName()) == "" {
		fields["name"] = "is required"
	}
	if len(fields) > 0 {
		return adminError(c, http.StatusBadRequest, "VALIDATION_FAILED", "missing required fields", fields)
	}
	cat, err := h.Q.UpsertCategory(c.Request().Context(), req.toModel(nil))
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique") {
			return adminError(c, http.StatusConflict, "VALIDATION_FAILED",
				"slug already exists", map[string]string{"slug": "must be unique"})
		}
		return dbErr(c, err)
	}
	return c.JSON(http.StatusCreated, cat)
}

// GET /api/admin/categories/:id
func (h *Handler) getCategoryAdmin(c echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return err
	}
	cat, err := h.Q.GetCategory(c.Request().Context(), id)
	if err != nil {
		return dbErr(c, err)
	}
	return c.JSON(http.StatusOK, cat)
}

// PATCH /api/admin/categories/:id — slug bất biến (giữ permalink ổn định).
func (h *Handler) updateCategory(c echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return err
	}
	existing, err := h.Q.GetCategory(c.Request().Context(), id)
	if err != nil {
		return dbErr(c, err)
	}

	var req categoryReq
	if err := c.Bind(&req); err != nil {
		return adminError(c, http.StatusBadRequest, "INVALID_BODY", "expected JSON body", nil)
	}
	if strings.TrimSpace(req.resolvedName()) == "" {
		req.NameVi = existing.NameVi
	}
	req.Slug = existing.Slug

	cat, err := h.Q.UpdateCategory(c.Request().Context(), id, req.toModel(existing))
	if err != nil {
		return dbErr(c, err)
	}
	return c.JSON(http.StatusOK, cat)
}

// DELETE /api/admin/categories/:id
func (h *Handler) deleteCategory(c echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return err
	}
	if err := h.Q.DeleteCategory(c.Request().Context(), id); err != nil {
		return dbErr(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

type reorderReq struct {
	OrderedIDs []int64  `json:"ordered_ids"` // spec §6.4 (canonical)
	Slugs      []string `json:"slugs"`       // legacy alias
}

// POST /api/admin/categories/reorder — body `{ordered_ids: [1,2,3]}`.
func (h *Handler) reorderCategories(c echo.Context) error {
	var req reorderReq
	if err := c.Bind(&req); err != nil {
		return adminError(c, http.StatusBadRequest, "INVALID_BODY", "expected JSON body", nil)
	}
	switch {
	case len(req.OrderedIDs) > 0:
		if err := h.Q.ReorderCategoriesByIDs(c.Request().Context(), req.OrderedIDs); err != nil {
			return dbErr(c, err)
		}
	case len(req.Slugs) > 0:
		if err := h.Q.ReorderCategories(c.Request().Context(), req.Slugs); err != nil {
			return dbErr(c, err)
		}
	default:
		return adminError(c, http.StatusBadRequest, "VALIDATION_FAILED",
			"either ordered_ids or slugs is required", nil)
	}
	return c.NoContent(http.StatusNoContent)
}

// ---------- Dish ↔ Category ----------

func (h *Handler) listDishCategories(c echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return err
	}
	cats, err := h.Q.ListCategoriesForDish(c.Request().Context(), id)
	if err != nil {
		return dbErr(c, err)
	}
	return c.JSON(http.StatusOK, cats)
}

type setDishCategoriesReq struct {
	CategoryIDs []int64 `json:"category_ids"`
}

func (h *Handler) setDishCategories(c echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return err
	}
	var req setDishCategoriesReq
	if err := c.Bind(&req); err != nil {
		return adminError(c, http.StatusBadRequest, "INVALID_BODY", "expected JSON body", nil)
	}
	if err := h.Q.AssignDishCategories(c.Request().Context(), id, req.CategoryIDs); err != nil {
		return dbErr(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}
