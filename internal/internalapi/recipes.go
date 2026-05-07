package internalapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cooking-recipe/cooking-recipe-service/internal/db"

	"github.com/labstack/echo/v4"
)

// HeaderIdempotency: client gửi key này; cùng key trong 24h → trả response cũ.
const HeaderIdempotency = "Idempotency-Key"

// upsertRecipeReq khớp với body docs/crawler_architech.md §5.5.
type upsertRecipeReq struct {
	DishID        int64             `json:"dish_id"`
	Recipe        db.ComposedRecipe `json:"recipe"`
	SourceVideos  []db.SourceVideo  `json:"source_videos"`
	ComposerMeta  *db.ComposerMeta  `json:"composer_meta,omitempty"`
}

// POST /api/internal/recipes
//
// Idempotency: client SHOULD gửi `Idempotency-Key` (recommended:
// `{dish_id}-{compose_run_id}`). Server lưu (status, response) trong 24h —
// retry với cùng key trả lại response cũ thay vì insert lần 2.
//
// Sau upsert thành công, server set `dishes.status='ready_for_review'`.
func (h *Handler) upsertRecipe(c echo.Context) error {
	ctx := c.Request().Context()
	idempKey := strings.TrimSpace(c.Request().Header.Get(HeaderIdempotency))

	// 1) Hit cache idempotency.
	if idempKey != "" {
		hit, err := h.Q.GetIdempotency(ctx, idempKey)
		if err != nil {
			return errorJSON(c, http.StatusInternalServerError, "DB_ERROR", err.Error(), nil)
		}
		if hit != nil {
			c.Response().Header().Set("Content-Type", "application/json")
			c.Response().Header().Set("X-Idempotent-Replay", "1")
			return c.Blob(hit.StatusCode, "application/json", hit.Response)
		}
	}

	// 2) Bind + validate.
	var req upsertRecipeReq
	if err := c.Bind(&req); err != nil {
		return errorJSON(c, http.StatusBadRequest, "INVALID_BODY", "expected JSON body", nil)
	}
	if req.DishID == 0 {
		return errorJSON(c, http.StatusBadRequest, "INVALID_DISH_ID", "dish_id is required", nil)
	}
	if req.Recipe.Title == "" || len(req.Recipe.Steps) == 0 {
		return errorJSON(c, http.StatusBadRequest, "INVALID_RECIPE",
			"recipe.title and at least one recipe.steps are required", nil)
	}
	if req.Recipe.TotalTimeMin == 0 {
		req.Recipe.TotalTimeMin = req.Recipe.PrepTimeMin + req.Recipe.CookTimeMin
	}

	// 3) Upsert source videos → có IDs để gắn vào recipes.source_video_ids.
	tx, err := h.Q.UpsertSourceVideos(ctx, req.DishID, req.SourceVideos)
	if err != nil {
		return errorJSON(c, http.StatusInternalServerError, "VIDEO_UPSERT_FAILED", err.Error(), nil)
	}
	heroURL := pickHeroURL(req.SourceVideos)

	// 4) Upsert recipe + steps + ingredients (transactional bên trong queries).
	recipeID, err := h.Q.UpsertRecipe(ctx, req.DishID, req.Recipe, tx, heroURL, req.ComposerMeta)
	if err != nil {
		return errorJSON(c, http.StatusInternalServerError, "RECIPE_UPSERT_FAILED", err.Error(), nil)
	}

	// 5) Set dish.status = ready_for_review (admin sẽ duyệt rồi publish).
	if err := h.Q.UpdateDishStatus(ctx, req.DishID, db.DishStatusReadyForReview); err != nil {
		return errorJSON(c, http.StatusInternalServerError, "STATUS_UPDATE_FAILED", err.Error(), nil)
	}

	// 6) Log event.
	dishID := req.DishID
	_, _ = h.Q.RecordPipelineEvent(ctx, db.PipelineEvent{
		Kind:         "recipe_upserted",
		DishID:       &dishID,
		ComposeRunID: composeRunIDOf(req.ComposerMeta),
		WorkerID:     ptrIfNotEmpty(WorkerIDOf(c)),
		Details: map[string]any{
			"recipe_id":     recipeID,
			"video_count":   len(req.SourceVideos),
			"received_at":   time.Now().UTC().Format(time.RFC3339),
		},
	})

	resp := echo.Map{"recipe_id": recipeID, "status": db.DishStatusReadyForReview}

	// 7) Save response cho idempotency replay.
	if idempKey != "" {
		body, _ := json.Marshal(resp)
		if err := h.Q.SaveIdempotency(ctx, idempKey, http.StatusOK, body); err != nil {
			// Không fail request — lần retry sẽ chạy lại upsert (vẫn idempotent ở DB level).
			c.Logger().Warnf("save idempotency failed: %v", err)
		}
	}

	return c.JSON(http.StatusOK, resp)
}

func composeRunIDOf(m *db.ComposerMeta) *string {
	if m == nil || m.ComposeRunID == "" {
		return nil
	}
	id := m.ComposeRunID
	return &id
}

// pickHeroURL — fallback hero image lấy từ thumbnail video YouTube đầu tiên.
func pickHeroURL(videos []db.SourceVideo) *string {
	for _, v := range videos {
		if v.Platform == "youtube" && v.ExternalID != "" {
			u := fmt.Sprintf("https://i.ytimg.com/vi/%s/hqdefault.jpg", v.ExternalID)
			return &u
		}
	}
	return nil
}
