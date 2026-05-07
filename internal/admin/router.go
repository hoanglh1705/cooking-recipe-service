// Package admin expose namespace `/api/admin/*` cho web admin SPA.
// Auth: Bearer token (xem ADMIN_TOKEN env). JWT cookie session sẽ thay thế ở
// phase sau — xem docs/admin_architech.md §7.
package admin

import (
	"net/http"
	"strings"

	"github.com/cooking-recipe/cooking-recipe-service/internal/db"
	"github.com/cooking-recipe/cooking-recipe-service/internal/queue"

	"github.com/labstack/echo/v4"
)

type Handler struct {
	Q                *db.Queries
	Queue            *queue.Client
	Token            string // bearer token required for admin endpoints
	RevalidateURL    string // Next.js ISR webhook URL (publish triggers it)
	RevalidateSecret string
}

// Config aggregates handler dependencies. Empty webhook fields = no-op.
type Config struct {
	AdminToken       string
	RevalidateURL    string
	RevalidateSecret string
}

func New(q *db.Queries, qc *queue.Client, cfg Config) *Handler {
	return &Handler{
		Q:                q,
		Queue:            qc,
		Token:            cfg.AdminToken,
		RevalidateURL:    cfg.RevalidateURL,
		RevalidateSecret: cfg.RevalidateSecret,
	}
}

// Mount /api/admin/* — match docs/admin_architech.md §6.
func (h *Handler) Mount(e *echo.Echo) {
	g := e.Group("/api/admin", h.authMiddleware)

	// §6.1 Auth
	g.GET("/me", h.me)

	// §6.2 Dishes
	g.GET("/dishes", h.listDishes)
	g.POST("/dishes", h.createDish)
	g.GET("/dishes/:id", h.getDish)
	g.PATCH("/dishes/:id", h.patchDish)
	g.DELETE("/dishes/:id", h.deleteDish)
	g.POST("/dishes/:id/trigger-crawler", h.triggerCrawler)
	g.POST("/dishes/:id/retry", h.retryDish)
	g.GET("/dishes/:id/categories", h.listDishCategories)
	g.PUT("/dishes/:id/categories", h.setDishCategories)
	g.GET("/dishes/:id/tags", h.listDishTags)
	g.PUT("/dishes/:id/tags", h.setDishTags)

	// §6.3 Recipes
	g.GET("/recipes", h.listRecipes)
	g.GET("/recipes/:id", h.getRecipe)
	g.PATCH("/recipes/:id", h.patchRecipe)
	g.POST("/recipes/:id/publish", h.publishRecipe)
	g.POST("/recipes/:id/unpublish", h.unpublishRecipe)
	g.POST("/recipes/:id/reject", h.rejectRecipe)

	// §6.4 Categories
	g.GET("/categories", h.listCategoriesAdmin)
	g.POST("/categories", h.createCategory)
	g.GET("/categories/:id", h.getCategoryAdmin)
	g.PATCH("/categories/:id", h.updateCategory)
	g.DELETE("/categories/:id", h.deleteCategory)
	g.POST("/categories/reorder", h.reorderCategories)

	// §6.4 Tags
	g.GET("/tags", h.listTags)
	g.POST("/tags", h.createTag)
	g.GET("/tags/:id", h.getTag)
	g.PATCH("/tags/:id", h.updateTag)
	g.DELETE("/tags/:id", h.deleteTag)
	g.POST("/tags/merge", h.mergeTags)

	// §6.5 Events & Stats
	g.GET("/events", h.listEvents)
	g.GET("/stats/overview", h.statsOverview)
	g.GET("/stats/pipeline", h.statsPipeline)
	g.GET("/stats/cost", h.statsCost)
}

func (h *Handler) authMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if h.Token == "" {
			return adminError(c, http.StatusServiceUnavailable, "ADMIN_AUTH_DISABLED",
				"admin token not configured", nil)
		}
		auth := c.Request().Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") != h.Token {
			return adminError(c, http.StatusUnauthorized, "UNAUTHORIZED", "invalid bearer token", nil)
		}
		return next(c)
	}
}

// §6.1 — GET /api/admin/me
//
// Bearer-token auth chỉ có 1 user (root admin). Trả 1 stub để FE biết đã login.
// Khi swap sang JWT cookie session sẽ trả thông tin user thật.
func (h *Handler) me(c echo.Context) error {
	return c.JSON(http.StatusOK, echo.Map{
		"user": echo.Map{
			"id":    "root",
			"email": "admin@local",
			"role":  "admin",
			"auth":  "bearer", // hint cho FE biết đang ở chế độ MVP
		},
	})
}
