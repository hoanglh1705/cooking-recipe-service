package internalapi

import (
	"github.com/cooking-recipe/cooking-recipe-service/internal/db"

	"github.com/labstack/echo/v4"
)

type Handler struct {
	Q      *db.Queries
	Secret string
}

func New(q *db.Queries, secret string) *Handler {
	return &Handler{Q: q, Secret: secret}
}

// Mount /api/internal/* — chỉ dành cho crawler, bảo vệ bằng HMAC.
func (h *Handler) Mount(e *echo.Echo) {
	g := e.Group("/api/internal", HMACAuth(h.Secret))

	g.GET("/dishes/pending", h.listPending)
	g.GET("/dishes/:id", h.getDish)
	g.POST("/dishes/:id/lock", h.lockDish)
	g.POST("/dishes/:id/unlock", h.unlockDish)

	g.POST("/recipes", h.upsertRecipe)
	g.POST("/events", h.recordEvent)
}
