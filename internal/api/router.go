package api

import (
	"errors"
	"net/http"

	"github.com/cooking-recipe/cooking-recipe-service/internal/db"

	"github.com/labstack/echo/v4"
)

// Handler holds dependencies for the public API.
type Handler struct {
	Q *db.Queries
}

func New(q *db.Queries) *Handler {
	return &Handler{Q: q}
}

// Mount attaches routes under /api.
func (h *Handler) Mount(e *echo.Echo) {
	g := e.Group("/api")
	g.GET("/health", h.health)

	g.GET("/recipes", h.listRecipes)
	g.GET("/recipes/slugs", h.listSlugs)
	g.GET("/recipes/search", h.searchRecipes)
	g.GET("/recipes/:slug", h.getRecipe)

	g.GET("/dishes", h.listDishes)
	g.GET("/dishes/:slug", h.getDish)

	g.GET("/categories", h.listCategories)
	g.GET("/categories/:slug", h.getCategory)
	g.GET("/categories/:slug/recipes", h.listRecipesByCategory)

	g.GET("/tags", h.listTags)
	g.GET("/home", h.home)
}

func (h *Handler) health(c echo.Context) error {
	return c.JSON(http.StatusOK, echo.Map{"status": "ok"})
}

func (h *Handler) listSlugs(c echo.Context) error {
	slugs, err := h.Q.ListPublishedSlugs(c.Request().Context())
	if err != nil {
		return errInternal(err)
	}
	return c.JSON(http.StatusOK, slugs)
}

func (h *Handler) listRecipes(c echo.Context) error {
	limit := atoiDefault(c.QueryParam("limit"), 20)
	category := c.QueryParam("category")

	out, err := h.Q.ListRecipeSummaries(c.Request().Context(), category, limit)
	if err != nil {
		return errInternal(err)
	}
	return c.JSON(http.StatusOK, out)
}

func (h *Handler) listCategories(c echo.Context) error {
	out, err := h.Q.ListCategories(c.Request().Context(), db.CategoryFilter{VisibleOnly: true})
	if err != nil {
		return errInternal(err)
	}
	return c.JSON(http.StatusOK, out)
}

func (h *Handler) getCategory(c echo.Context) error {
	slug := c.Param("slug")
	cat, err := h.Q.GetCategoryBySlug(c.Request().Context(), slug)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "category not found")
		}
		return errInternal(err)
	}
	if !cat.Visible {
		return echo.NewHTTPError(http.StatusNotFound, "category not found")
	}
	return c.JSON(http.StatusOK, cat)
}

func (h *Handler) listRecipesByCategory(c echo.Context) error {
	slug := c.Param("slug")
	limit := atoiDefault(c.QueryParam("limit"), 24)
	out, err := h.Q.ListRecipeSummaries(c.Request().Context(), slug, limit)
	if err != nil {
		return errInternal(err)
	}
	return c.JSON(http.StatusOK, out)
}

func (h *Handler) home(c echo.Context) error {
	per := atoiDefault(c.QueryParam("per_section"), 8)
	sections, err := h.Q.ListHomeSections(c.Request().Context(), per)
	if err != nil {
		return errInternal(err)
	}
	return c.JSON(http.StatusOK, echo.Map{"sections": sections})
}

func (h *Handler) searchRecipes(c echo.Context) error {
	q := c.QueryParam("q")
	limit := atoiDefault(c.QueryParam("limit"), 20)
	out, err := h.Q.SearchRecipes(c.Request().Context(), q, limit)
	if err != nil {
		return errInternal(err)
	}
	return c.JSON(http.StatusOK, out)
}

func (h *Handler) getRecipe(c echo.Context) error {
	slug := c.Param("slug")
	r, err := h.Q.GetRecipeBySlug(c.Request().Context(), slug)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "recipe not found")
		}
		return errInternal(err)
	}
	return c.JSON(http.StatusOK, r)
}
