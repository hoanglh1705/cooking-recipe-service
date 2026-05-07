package admin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/cooking-recipe/cooking-recipe-service/internal/db"

	"github.com/labstack/echo/v4"
)

// GET /api/admin/tags?q=&limit=
func (h *Handler) listTags(c echo.Context) error {
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	tags, err := h.Q.ListTags(c.Request().Context(), c.QueryParam("q"), limit)
	if err != nil {
		return dbErr(c, err)
	}
	return c.JSON(http.StatusOK, tags)
}

type tagReq struct {
	Slug        string  `json:"slug"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	SortOrder   int     `json:"sort_order"`
}

// POST /api/admin/tags
func (h *Handler) createTag(c echo.Context) error {
	var req tagReq
	if err := c.Bind(&req); err != nil {
		return adminError(c, http.StatusBadRequest, "INVALID_BODY", "expected JSON body", nil)
	}
	fields := map[string]string{}
	if strings.TrimSpace(req.Name) == "" {
		fields["name"] = "is required"
	}
	if strings.TrimSpace(req.Slug) == "" {
		fields["slug"] = "is required"
	}
	if len(fields) > 0 {
		return adminError(c, http.StatusBadRequest, "VALIDATION_FAILED", "missing required fields", fields)
	}

	t, err := h.Q.CreateTag(c.Request().Context(), db.Tag{
		Slug: req.Slug, Name: req.Name, Description: req.Description, SortOrder: req.SortOrder,
	})
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique") {
			return adminError(c, http.StatusConflict, "VALIDATION_FAILED",
				"slug already exists", map[string]string{"slug": "must be unique"})
		}
		return dbErr(c, err)
	}
	return c.JSON(http.StatusCreated, t)
}

// GET /api/admin/tags/:id
func (h *Handler) getTag(c echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return err
	}
	t, err := h.Q.GetTag(c.Request().Context(), id)
	if err != nil {
		return dbErr(c, err)
	}
	return c.JSON(http.StatusOK, t)
}

// PATCH /api/admin/tags/:id — slug bất biến (giữ permalink ổn định).
func (h *Handler) updateTag(c echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return err
	}
	existing, err := h.Q.GetTag(c.Request().Context(), id)
	if err != nil {
		return dbErr(c, err)
	}
	var req tagReq
	if err := c.Bind(&req); err != nil {
		return adminError(c, http.StatusBadRequest, "INVALID_BODY", "expected JSON body", nil)
	}
	if strings.TrimSpace(req.Name) == "" {
		req.Name = existing.Name
	}
	t, err := h.Q.UpdateTag(c.Request().Context(), id, db.Tag{
		Name: req.Name, Description: req.Description, SortOrder: req.SortOrder,
	})
	if err != nil {
		return dbErr(c, err)
	}
	return c.JSON(http.StatusOK, t)
}

// DELETE /api/admin/tags/:id — cascade xóa từ dish_tags.
func (h *Handler) deleteTag(c echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return err
	}
	if err := h.Q.DeleteTag(c.Request().Context(), id); err != nil {
		return dbErr(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

type mergeTagsReq struct {
	SourceID int64 `json:"source_id"`
	TargetID int64 `json:"target_id"`
}

// POST /api/admin/tags/merge — chuyển toàn bộ dish của source tag sang target,
// rồi xóa source tag.
func (h *Handler) mergeTags(c echo.Context) error {
	var req mergeTagsReq
	if err := c.Bind(&req); err != nil {
		return adminError(c, http.StatusBadRequest, "INVALID_BODY", "expected JSON body", nil)
	}
	if req.SourceID == 0 || req.TargetID == 0 {
		return adminError(c, http.StatusBadRequest, "VALIDATION_FAILED",
			"source_id and target_id are required", nil)
	}
	if req.SourceID == req.TargetID {
		return adminError(c, http.StatusBadRequest, "VALIDATION_FAILED",
			"source_id must differ from target_id", nil)
	}
	if err := h.Q.MergeTags(c.Request().Context(), req.SourceID, req.TargetID); err != nil {
		return dbErr(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}
