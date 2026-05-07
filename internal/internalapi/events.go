package internalapi

import (
	"net/http"

	"github.com/cooking-recipe/cooking-recipe-service/internal/db"

	"github.com/labstack/echo/v4"
)

// POST /api/internal/events — crawler push event log (stage_started, stage_failed, …).
// Server lưu vào pipeline_events; admin nhìn vào để debug khi job fail.
func (h *Handler) recordEvent(c echo.Context) error {
	var ev db.PipelineEvent
	if err := c.Bind(&ev); err != nil {
		return errorJSON(c, http.StatusBadRequest, "INVALID_BODY", "expected JSON body", nil)
	}
	if ev.Kind == "" {
		return errorJSON(c, http.StatusBadRequest, "MISSING_KIND", "event.kind is required", nil)
	}

	ev.WorkerID = ptrIfNotEmpty(WorkerIDOf(c))

	id, err := h.Q.RecordPipelineEvent(c.Request().Context(), ev)
	if err != nil {
		return errorJSON(c, http.StatusInternalServerError, "DB_ERROR", err.Error(), nil)
	}
	return c.JSON(http.StatusCreated, echo.Map{"event_id": id})
}
