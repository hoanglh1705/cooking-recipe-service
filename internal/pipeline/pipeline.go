// Package pipeline contains the AI pipeline stages, each implemented as an
// asynq task handler. Stages are chained: each stage enqueues the next on
// success. This makes individual stages retryable in isolation.
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/cooking-recipe/cooking-recipe-service/internal/clients/llm"
	"github.com/cooking-recipe/cooking-recipe-service/internal/clients/tiktok"
	"github.com/cooking-recipe/cooking-recipe-service/internal/clients/whisper"
	"github.com/cooking-recipe/cooking-recipe-service/internal/clients/youtube"
	"github.com/cooking-recipe/cooking-recipe-service/internal/db"
	"github.com/cooking-recipe/cooking-recipe-service/internal/queue"

	"github.com/hibiken/asynq"
)

type Deps struct {
	Q        *db.Queries
	Client   *queue.Client // for enqueuing the next stage
	YouTube  *youtube.Client
	TikTok   *tiktok.Client
	LLM      llm.Composer
	Whisper  whisper.Transcriber
	YTDLP    string // path to yt-dlp binary
	AudioDir string // tmp dir for downloaded audio
	Logger   *slog.Logger

	// PublishHook is called after a recipe is composed so the frontend can
	// revalidate its ISR cache. Nil = skip.
	PublishHook PublishHook
}

// PublishHook lets cmd wire in a Next.js revalidate webhook without
// pulling http into this package's API.
type PublishHook func(ctx context.Context, slug string) error

// Register attaches all pipeline handlers to the asynq mux.
func (d *Deps) Register(mux *asynq.ServeMux) {
	mux.HandleFunc(queue.TaskSearchVideos, d.handleSearch)
	mux.HandleFunc(queue.TaskDownloadAudio, d.handleDownload)
	mux.HandleFunc(queue.TaskTranscribeVideo, d.handleTranscribe)
	mux.HandleFunc(queue.TaskComposeRecipe, d.handleCompose)
	mux.HandleFunc(queue.TaskPublishRecipe, d.handlePublish)
}

// ---------- Stage 1: Search ----------

func (d *Deps) handleSearch(ctx context.Context, t *asynq.Task) error {
	var p queue.SearchVideosPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	dish, err := d.Q.GetDish(ctx, p.DishID)
	if err != nil {
		return fmt.Errorf("get dish: %w", err)
	}
	d.Logger.Info("pipeline: search", "dish_id", dish.ID, "name", dish.NameVi)

	query := dish.NameVi + " cách nấu"
	results, err := d.YouTube.SearchTopVideos(ctx, query, 5)
	if err != nil {
		return fmt.Errorf("youtube search: %w", err)
	}
	results, _ = d.YouTube.FetchDurations(ctx, results)

	if len(results) == 0 {
		d.Logger.Warn("no videos found, marking dish failed", "dish_id", dish.ID)
		_ = d.Q.UpdateDishStatus(ctx, dish.ID, "failed_no_videos")
		return nil
	}

	for _, r := range results {
		dur := int(r.Duration.Seconds())
		v, err := d.Q.UpsertVideo(ctx, db.Video{
			DishID:     dish.ID,
			Platform:   "youtube",
			ExternalID: r.VideoID,
			URL:        r.URL,
			Title:      strPtr(r.Title),
			Channel:    strPtr(r.Channel),
			DurationS:  intPtrIfPos(dur),
		})
		if err != nil {
			d.Logger.Error("upsert video failed", "err", err, "video_id", r.VideoID)
			continue
		}
		if err := d.Client.EnqueueDownloadAudio(queue.DownloadAudioPayload{
			DishID: dish.ID, VideoID: v.ID,
		}); err != nil {
			d.Logger.Error("enqueue download failed", "err", err)
		}
	}
	return nil
}

// ---------- Stage 2: Download ----------

func (d *Deps) handleDownload(ctx context.Context, t *asynq.Task) error {
	var p queue.DownloadAudioPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return err
	}
	d.Logger.Info("pipeline: download", "video_id", p.VideoID)

	vids, err := d.Q.ListVideosByDish(ctx, p.DishID)
	if err != nil {
		return err
	}
	var url string
	for _, v := range vids {
		if v.ID == p.VideoID {
			url = v.URL
			break
		}
	}
	if url == "" {
		return fmt.Errorf("video %d not found", p.VideoID)
	}

	audioPath, err := DownloadAudio(ctx, d.YTDLP, d.AudioDir, p.VideoID, url)
	if err != nil {
		return fmt.Errorf("download audio: %w", err)
	}

	return d.Client.EnqueueTranscribeVideo(queue.TranscribeVideoPayload{
		DishID: p.DishID, VideoID: p.VideoID, AudioPath: audioPath,
	})
}

// ---------- Stage 3: Transcribe ----------

func (d *Deps) handleTranscribe(ctx context.Context, t *asynq.Task) error {
	var p queue.TranscribeVideoPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return err
	}
	d.Logger.Info("pipeline: transcribe", "video_id", p.VideoID, "audio", p.AudioPath)

	transcript, err := d.Whisper.Transcribe(ctx, p.AudioPath)
	if err != nil {
		return fmt.Errorf("whisper: %w", err)
	}
	if err := d.Q.SetVideoTranscript(ctx, p.VideoID, transcript); err != nil {
		return fmt.Errorf("save transcript: %w", err)
	}

	// Compose only when *all* videos for this dish have transcripts.
	vids, err := d.Q.ListVideosByDish(ctx, p.DishID)
	if err != nil {
		return err
	}
	for _, v := range vids {
		if v.Transcript == nil || *v.Transcript == "" {
			d.Logger.Info("waiting for more transcripts", "dish_id", p.DishID)
			return nil
		}
	}

	return d.Client.EnqueueComposeRecipe(queue.ComposeRecipePayload{DishID: p.DishID})
}

// ---------- Stage 4: Compose ----------

func (d *Deps) handleCompose(ctx context.Context, t *asynq.Task) error {
	var p queue.ComposeRecipePayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return err
	}
	dish, err := d.Q.GetDish(ctx, p.DishID)
	if err != nil {
		return err
	}
	vids, err := d.Q.ListVideosByDish(ctx, p.DishID)
	if err != nil {
		return err
	}
	d.Logger.Info("pipeline: compose", "dish_id", dish.ID, "videos", len(vids))

	system, user := BuildComposePrompt(*dish, vids)
	raw, err := d.LLM.ComposeJSON(ctx, system, user)
	if err != nil {
		return fmt.Errorf("llm: %w", err)
	}
	composed, err := ParseComposed(raw)
	if err != nil {
		return fmt.Errorf("parse llm output: %w", err)
	}

	sourceIDs := make([]int64, 0, len(vids))
	for _, v := range vids {
		sourceIDs = append(sourceIDs, v.ID)
	}
	var heroURL *string
	if len(vids) > 0 {
		// Default hero image: YouTube thumbnail of the first source.
		thumb := fmt.Sprintf("https://i.ytimg.com/vi/%s/hqdefault.jpg", vids[0].ExternalID)
		heroURL = &thumb
	}

	recipeID, err := d.Q.UpsertRecipe(ctx, dish.ID, *composed, sourceIDs, heroURL, nil)
	if err != nil {
		return fmt.Errorf("upsert recipe: %w", err)
	}

	return d.Client.EnqueuePublishRecipe(queue.PublishRecipePayload{
		DishID: dish.ID, RecipeID: recipeID, Slug: dish.Slug,
	})
}

// ---------- Stage 5: Publish ----------

func (d *Deps) handlePublish(ctx context.Context, t *asynq.Task) error {
	var p queue.PublishRecipePayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return err
	}
	d.Logger.Info("pipeline: publish", "slug", p.Slug, "recipe_id", p.RecipeID)

	if err := d.Q.MarkPublished(ctx, p.RecipeID); err != nil {
		return err
	}
	if err := d.Q.UpdateDishStatus(ctx, p.DishID, db.DishStatusPublished); err != nil {
		return err
	}
	if d.PublishHook != nil {
		if err := d.PublishHook(ctx, p.Slug); err != nil {
			// Don't fail the task — revalidate is best-effort.
			d.Logger.Warn("revalidate hook failed", "err", err, "slug", p.Slug)
		}
	}
	return nil
}

// ---------- helpers ----------

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
func intPtrIfPos(n int) *int {
	if n <= 0 {
		return nil
	}
	return &n
}
