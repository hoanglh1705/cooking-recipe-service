package internalapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/cooking-recipe/cooking-recipe-service/internal/db"

	"github.com/labstack/echo/v4"
)

// GET /api/internal/dishes/pending?limit=N — oldest pending dishes first.
// Trước khi list, query layer tự reclaim các lock hết hạn.
func (h *Handler) listPending(c echo.Context) error {
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit <= 0 {
		limit = 20
	}
	out, err := h.Q.ListPendingDishes(c.Request().Context(), limit)
	if err != nil {
		return errorJSON(c, http.StatusInternalServerError, "DB_ERROR", err.Error(), nil)
	}
	return c.JSON(http.StatusOK, out)
}

// GET /api/internal/dishes/:id — dùng cho retry hoặc khi worker mất state.
func (h *Handler) getDish(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return errorJSON(c, http.StatusBadRequest, "INVALID_DISH_ID", "id must be integer", nil)
	}
	d, err := h.Q.GetDish(c.Request().Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return errorJSON(c, http.StatusNotFound, "DISH_NOT_FOUND", "dish does not exist", nil)
		}
		return errorJSON(c, http.StatusInternalServerError, "DB_ERROR", err.Error(), nil)
	}
	return c.JSON(http.StatusOK, d)
}

type lockReq struct {
	WorkerID   string `json:"worker_id"`
	TTLSeconds int    `json:"ttl_seconds"`
}

// POST /api/internal/dishes/:id/lock
//   200 OK   — lock acquired (mới hoặc gia hạn cho cùng worker)
//   404      — dish không tồn tại
//   409      — locked bởi worker khác và TTL chưa hết
func (h *Handler) lockDish(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return errorJSON(c, http.StatusBadRequest, "INVALID_DISH_ID", "id must be integer", nil)
	}
	var req lockReq
	if err := c.Bind(&req); err != nil {
		return errorJSON(c, http.StatusBadRequest, "INVALID_BODY", "expected JSON body", nil)
	}
	if req.WorkerID == "" {
		req.WorkerID = WorkerIDOf(c)
	}
	if req.WorkerID == "" {
		return errorJSON(c, http.StatusBadRequest, "MISSING_WORKER_ID",
			"worker_id is required (body or X-Crawler-Worker)", nil)
	}
	ttl := time.Duration(req.TTLSeconds) * time.Second

	d, err := h.Q.LockDish(c.Request().Context(), id, req.WorkerID, ttl)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrNotFound):
			return errorJSON(c, http.StatusNotFound, "DISH_NOT_FOUND", "dish does not exist", nil)
		case errors.Is(err, db.ErrDishLocked):
			// Best-effort: get current lock holder để trả về retry_after.
			retry := 600
			if cur, gerr := h.Q.GetDish(c.Request().Context(), id); gerr == nil && cur.LockedUntil != nil {
				if remain := int(time.Until(*cur.LockedUntil).Seconds()); remain > 0 {
					retry = remain
				}
			}
			return errorJSON(c, http.StatusConflict, "DISH_ALREADY_LOCKED",
				"dish is locked by another worker", &retry)
		default:
			return errorJSON(c, http.StatusInternalServerError, "DB_ERROR", err.Error(), nil)
		}
	}

	return c.JSON(http.StatusOK, echo.Map{
		"dish_id":      d.ID,
		"locked_by":    d.LockedBy,
		"locked_until": d.LockedUntil,
		"status":       d.Status,
	})
}

type unlockReq struct {
	FinalStatus  string  `json:"final_status"` // ready_for_review | failed | pending
	ErrorCode    *string `json:"error_code,omitempty"`
	ErrorMessage *string `json:"error_message,omitempty"`
	Stage        *int    `json:"stage,omitempty"`
	ComposeRunID *string `json:"compose_run_id,omitempty"`
}

// validUnlockStatuses định nghĩa các trạng thái cuối hợp lệ mà crawler được set.
// Admin-only như `done` không được set qua endpoint này.
var validUnlockStatuses = map[string]bool{
	db.DishStatusReadyForReview: true,
	db.DishStatusFailed:         true,
	db.DishStatusPending:        true, // crawler abort/skip → quay lại hàng đợi
}

// POST /api/internal/dishes/:id/unlock — clear lock + set final status.
func (h *Handler) unlockDish(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return errorJSON(c, http.StatusBadRequest, "INVALID_DISH_ID", "id must be integer", nil)
	}
	var req unlockReq
	if err := c.Bind(&req); err != nil {
		return errorJSON(c, http.StatusBadRequest, "INVALID_BODY", "expected JSON body", nil)
	}
	if !validUnlockStatuses[req.FinalStatus] {
		return errorJSON(c, http.StatusBadRequest, "INVALID_FINAL_STATUS",
			"final_status must be ready_for_review | failed | pending", nil)
	}

	if err := h.Q.UnlockDish(c.Request().Context(), id, req.FinalStatus,
		req.ErrorCode, req.ErrorMessage, req.Stage); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return errorJSON(c, http.StatusNotFound, "DISH_NOT_FOUND", "dish does not exist", nil)
		}
		return errorJSON(c, http.StatusInternalServerError, "DB_ERROR", err.Error(), nil)
	}

	// Log event cho admin theo dõi.
	_, _ = h.Q.RecordPipelineEvent(c.Request().Context(), db.PipelineEvent{
		Kind:         "dish_unlocked",
		DishID:       &id,
		Stage:        req.Stage,
		ErrorCode:    req.ErrorCode,
		ErrorMessage: req.ErrorMessage,
		ComposeRunID: req.ComposeRunID,
		WorkerID:     ptrIfNotEmpty(WorkerIDOf(c)),
		Details:      map[string]any{"final_status": req.FinalStatus},
	})

	return c.JSON(http.StatusOK, echo.Map{"status": req.FinalStatus})
}

func ptrIfNotEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
