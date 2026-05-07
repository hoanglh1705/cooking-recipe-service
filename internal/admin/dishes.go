package admin

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/cooking-recipe/cooking-recipe-service/internal/db"
	"github.com/cooking-recipe/cooking-recipe-service/internal/queue"

	"github.com/labstack/echo/v4"
)

// GET /api/admin/dishes — list paginated với filter q/status/category/region/sort.
func (h *Handler) listDishes(c echo.Context) error {
	pageSize, _ := strconv.Atoi(c.QueryParam("page_size"))
	page, _ := strconv.Atoi(c.QueryParam("page"))

	out, err := h.Q.ListDishesAdmin(c.Request().Context(), db.DishListAdminFilter{
		Q:        c.QueryParam("q"),
		Status:   c.QueryParam("status"),
		Category: c.QueryParam("category"),
		Region:   c.QueryParam("region"),
		Sort:     c.QueryParam("sort"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		return dbErr(c, err)
	}
	return c.JSON(http.StatusOK, out)
}

type createDishReq struct {
	NameVi     string   `json:"name_vi"`
	NameEn     *string  `json:"name_en,omitempty"`
	Slug       string   `json:"slug"`
	CategoryID *int64   `json:"category_id,omitempty"`
	Region     *string  `json:"region,omitempty"`
	TagIDs     []int64  `json:"tag_ids,omitempty"`
	Keywords   []string `json:"keywords,omitempty"`
}

// POST /api/admin/dishes — body theo §6.2 admin doc.
func (h *Handler) createDish(c echo.Context) error {
	var req createDishReq
	if err := c.Bind(&req); err != nil {
		return adminError(c, http.StatusBadRequest, "INVALID_BODY", "expected JSON body", nil)
	}

	fields := map[string]string{}
	if strings.TrimSpace(req.Slug) == "" {
		fields["slug"] = "is required"
	}
	if strings.TrimSpace(req.NameVi) == "" {
		fields["name_vi"] = "is required"
	}
	if len(fields) > 0 {
		return adminError(c, http.StatusBadRequest, "VALIDATION_FAILED", "missing required fields", fields)
	}

	d, err := h.Q.CreateDish(c.Request().Context(), db.Dish{
		Slug: req.Slug, NameVi: req.NameVi, NameEn: req.NameEn,
		Region: req.Region, Keywords: req.Keywords,
	})
	if err != nil {
		// Heuristic: unique violation trên slug → trả VALIDATION_FAILED.
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique") {
			return adminError(c, http.StatusConflict, "VALIDATION_FAILED",
				"slug already exists", map[string]string{"slug": "must be unique"})
		}
		return dbErr(c, err)
	}

	if req.CategoryID != nil {
		if err := h.Q.AssignDishCategories(c.Request().Context(), d.ID, []int64{*req.CategoryID}); err != nil {
			return dbErr(c, err)
		}
	}
	if len(req.TagIDs) > 0 {
		if err := h.Q.SetDishTags(c.Request().Context(), d.ID, req.TagIDs); err != nil {
			return dbErr(c, err)
		}
	}
	return c.JSON(http.StatusCreated, echo.Map{"dish": d})
}

// GET /api/admin/dishes/:id — bundle dish + videos + events.
func (h *Handler) getDish(c echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return err
	}
	d, err := h.Q.GetDish(c.Request().Context(), id)
	if err != nil {
		return dbErr(c, err)
	}
	videos, _ := h.Q.ListVideosByDish(c.Request().Context(), id)
	dishID := id
	events, _ := h.Q.ListPipelineEvents(c.Request().Context(), db.EventListFilter{
		DishID: &dishID, Limit: 50,
	})
	cats, _ := h.Q.ListCategoriesForDish(c.Request().Context(), id)
	tags, _ := h.Q.ListTagsForDish(c.Request().Context(), id)
	return c.JSON(http.StatusOK, echo.Map{
		"dish":       d,
		"categories": cats,
		"tags":       tags,
		"videos":     videos,
		"events":     events,
	})
}

// PATCH /api/admin/dishes/:id — partial update theo DishUpdate.
func (h *Handler) patchDish(c echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return err
	}
	var u db.DishUpdate
	if err := c.Bind(&u); err != nil {
		return adminError(c, http.StatusBadRequest, "INVALID_BODY", "expected JSON body", nil)
	}
	d, err := h.Q.UpdateDish(c.Request().Context(), id, u)
	if err != nil {
		return dbErr(c, err)
	}
	return c.JSON(http.StatusOK, echo.Map{"dish": d})
}

// DELETE /api/admin/dishes/:id — soft delete (status=archived).
func (h *Handler) deleteDish(c echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return err
	}
	if err := h.Q.SoftDeleteDish(c.Request().Context(), id); err != nil {
		return dbErr(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// POST /api/admin/dishes/:id/trigger-crawler — push job vào asynq queue.
func (h *Handler) triggerCrawler(c echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return err
	}
	dish, err := h.Q.GetDish(c.Request().Context(), id)
	if err != nil {
		return dbErr(c, err)
	}
	if h.Queue == nil {
		return adminError(c, http.StatusServiceUnavailable, "QUEUE_NOT_CONFIGURED",
			"async queue is not configured (REDIS_ADDR missing?)", nil)
	}
	if err := h.Queue.EnqueueSearchVideos(queue.SearchVideosPayload{DishID: dish.ID}); err != nil {
		return adminError(c, http.StatusInternalServerError, "QUEUE_ERROR", err.Error(), nil)
	}
	if err := h.Q.UpdateDishStatus(c.Request().Context(), dish.ID, db.DishStatusProcessing); err != nil {
		return dbErr(c, err)
	}
	return c.JSON(http.StatusAccepted, echo.Map{
		"dish_id": dish.ID,
		"status":  db.DishStatusProcessing,
		"queued":  true,
	})
}

// POST /api/admin/dishes/:id/retry — admin force re-run pipeline.
func (h *Handler) retryDish(c echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return err
	}
	if err := h.Q.RetryDish(c.Request().Context(), id); err != nil {
		return dbErr(c, err)
	}
	d, err := h.Q.GetDish(c.Request().Context(), id)
	if err != nil {
		return dbErr(c, err)
	}
	return c.JSON(http.StatusOK, echo.Map{"dish": d})
}

// ---------- Dish ↔ Tag ----------

func (h *Handler) listDishTags(c echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return err
	}
	tags, err := h.Q.ListTagsForDish(c.Request().Context(), id)
	if err != nil {
		return dbErr(c, err)
	}
	return c.JSON(http.StatusOK, tags)
}

type setDishTagsReq struct {
	TagIDs []int64 `json:"tag_ids"`
}

func (h *Handler) setDishTags(c echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return err
	}
	var req setDishTagsReq
	if err := c.Bind(&req); err != nil {
		return adminError(c, http.StatusBadRequest, "INVALID_BODY", "expected JSON body", nil)
	}
	if err := h.Q.SetDishTags(c.Request().Context(), id, req.TagIDs); err != nil {
		return dbErr(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// ---------- helpers ----------

func parseIDParam(c echo.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, adminError(c, http.StatusBadRequest, "INVALID_ID", "id must be positive integer", nil)
	}
	return id, nil
}

// guard for unused imports when we trim later
var _ = errors.New
