package admin

import (
	"net/http"
	"strconv"
	"time"

	"github.com/cooking-recipe/cooking-recipe-service/internal/db"

	"github.com/labstack/echo/v4"
)

// GET /api/admin/events?kind=&dish_id=&from=&to=&limit=
//
// `from`/`to` định dạng RFC3339 (`2026-05-06T00:00:00Z`).
func (h *Handler) listEvents(c echo.Context) error {
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	f := db.EventListFilter{
		Kind:  c.QueryParam("kind"),
		Limit: limit,
	}
	if dishStr := c.QueryParam("dish_id"); dishStr != "" {
		if did, err := strconv.ParseInt(dishStr, 10, 64); err == nil {
			f.DishID = &did
		}
	}
	if fromStr := c.QueryParam("from"); fromStr != "" {
		if t, err := time.Parse(time.RFC3339, fromStr); err == nil {
			f.From = &t
		}
	}
	if toStr := c.QueryParam("to"); toStr != "" {
		if t, err := time.Parse(time.RFC3339, toStr); err == nil {
			f.To = &t
		}
	}

	events, err := h.Q.ListPipelineEvents(c.Request().Context(), f)
	if err != nil {
		return dbErr(c, err)
	}
	return c.JSON(http.StatusOK, events)
}

// GET /api/admin/stats/overview — dashboard stat cards.
func (h *Handler) statsOverview(c echo.Context) error {
	s, err := h.Q.StatsOverview(c.Request().Context())
	if err != nil {
		return dbErr(c, err)
	}
	return c.JSON(http.StatusOK, s)
}

// GET /api/admin/stats/pipeline — 7 days pipeline runs (started/failed) per day.
func (h *Handler) statsPipeline(c echo.Context) error {
	days, err := h.Q.StatsPipeline7Days(c.Request().Context())
	if err != nil {
		return dbErr(c, err)
	}
	return c.JSON(http.StatusOK, days)
}

// GET /api/admin/stats/cost — month-to-date LLM cost.
func (h *Handler) statsCost(c echo.Context) error {
	s, err := h.Q.StatsCost(c.Request().Context())
	if err != nil {
		return dbErr(c, err)
	}
	return c.JSON(http.StatusOK, s)
}
