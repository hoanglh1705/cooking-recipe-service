package admin

import (
	"context"
	"net/http"
	"strconv"

	"github.com/cooking-recipe/cooking-recipe-service/internal/db"
	"github.com/cooking-recipe/cooking-recipe-service/internal/pipeline"

	"github.com/labstack/echo/v4"
)

// GET /api/admin/recipes?status=&dish_id=&page=&page_size=
func (h *Handler) listRecipes(c echo.Context) error {
	page, _ := strconv.Atoi(c.QueryParam("page"))
	pageSize, _ := strconv.Atoi(c.QueryParam("page_size"))

	f := db.RecipeListAdminFilter{
		Status:   c.QueryParam("status"),
		Page:     page,
		PageSize: pageSize,
	}
	if dishStr := c.QueryParam("dish_id"); dishStr != "" {
		if did, err := strconv.ParseInt(dishStr, 10, 64); err == nil {
			f.DishID = &did
		}
	}

	out, err := h.Q.ListRecipesAdmin(c.Request().Context(), f)
	if err != nil {
		return dbErr(c, err)
	}
	return c.JSON(http.StatusOK, out)
}

// GET /api/admin/recipes/:id — full review payload.
func (h *Handler) getRecipe(c echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return err
	}
	r, err := h.Q.GetRecipeAdmin(c.Request().Context(), id)
	if err != nil {
		return dbErr(c, err)
	}
	return c.JSON(http.StatusOK, r)
}

// PATCH /api/admin/recipes/:id — partial fields update (admin edit).
func (h *Handler) patchRecipe(c echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return err
	}
	var u db.RecipeFieldUpdate
	if err := c.Bind(&u); err != nil {
		return adminError(c, http.StatusBadRequest, "INVALID_BODY", "expected JSON body", nil)
	}
	if err := h.Q.PatchRecipe(c.Request().Context(), id, u); err != nil {
		return dbErr(c, err)
	}
	r, err := h.Q.GetRecipeAdmin(c.Request().Context(), id)
	if err != nil {
		return dbErr(c, err)
	}
	return c.JSON(http.StatusOK, r)
}

// POST /api/admin/recipes/:id/publish — set published + trigger Next.js revalidate.
func (h *Handler) publishRecipe(c echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return err
	}
	dishID, slug, err := h.Q.PublishRecipeAdmin(c.Request().Context(), id)
	if err != nil {
		return dbErr(c, err)
	}

	// Best-effort revalidate webhook ở Next.js — không fail nếu lỗi.
	if hook := h.revalidateHook(); hook != nil {
		go func(s string) {
			_ = hook(context.Background(), s)
		}(slug)
	}

	return c.JSON(http.StatusOK, echo.Map{
		"recipe_id":  id,
		"dish_id":    dishID,
		"slug":       slug,
		"status":     db.RecipeStatusPublished,
		"revalidate": h.RevalidateURL != "",
	})
}

// POST /api/admin/recipes/:id/unpublish
func (h *Handler) unpublishRecipe(c echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return err
	}
	if err := h.Q.UnpublishRecipeAdmin(c.Request().Context(), id); err != nil {
		return dbErr(c, err)
	}
	return c.JSON(http.StatusOK, echo.Map{"recipe_id": id, "status": db.RecipeStatusReadyForReview})
}

type rejectReq struct {
	Reason string `json:"reason"`
}

// POST /api/admin/recipes/:id/reject
func (h *Handler) rejectRecipe(c echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return err
	}
	var req rejectReq
	_ = c.Bind(&req) // reason là optional
	if err := h.Q.RejectRecipe(c.Request().Context(), id, req.Reason); err != nil {
		return dbErr(c, err)
	}
	return c.JSON(http.StatusOK, echo.Map{"recipe_id": id, "status": db.RecipeStatusRejected})
}

// revalidateHook builds Next.js ISR revalidate hook from handler config.
// Trả nil nếu chưa cấu hình (env trống).
func (h *Handler) revalidateHook() pipeline.PublishHook {
	if h.RevalidateURL == "" {
		return nil
	}
	return pipeline.RevalidateNext(h.RevalidateURL, h.RevalidateSecret)
}
