package queue

import (
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"
)

// Task names used as the asynq Type. Each pipeline stage is a distinct task
// so it can be retried independently.
const (
	TaskSearchVideos     = "pipeline:search_videos"
	TaskDownloadAudio    = "pipeline:download_audio"
	TaskTranscribeVideo  = "pipeline:transcribe_video"
	TaskComposeRecipe    = "pipeline:compose_recipe"
	TaskPublishRecipe    = "pipeline:publish_recipe"
)

// Queue names — separate priority lanes.
const (
	QueueDefault = "default"
	QueueAI      = "ai"
)

// Payloads ------------------------------------------------------------

type SearchVideosPayload struct {
	DishID int64 `json:"dish_id"`
}

type DownloadAudioPayload struct {
	DishID  int64 `json:"dish_id"`
	VideoID int64 `json:"video_id"`
}

type TranscribeVideoPayload struct {
	DishID    int64  `json:"dish_id"`
	VideoID   int64  `json:"video_id"`
	AudioPath string `json:"audio_path"`
}

type ComposeRecipePayload struct {
	DishID int64 `json:"dish_id"`
}

type PublishRecipePayload struct {
	DishID   int64  `json:"dish_id"`
	RecipeID int64  `json:"recipe_id"`
	Slug     string `json:"slug"`
}

// Client wraps asynq.Client with helpers per task type.
type Client struct {
	C *asynq.Client
}

func NewClient(redisOpt asynq.RedisClientOpt) *Client {
	return &Client{C: asynq.NewClient(redisOpt)}
}

func (c *Client) Close() error {
	if c.C == nil {
		return nil
	}
	return c.C.Close()
}

func (c *Client) EnqueueSearchVideos(p SearchVideosPayload) error {
	return c.enqueue(TaskSearchVideos, p, QueueDefault)
}
func (c *Client) EnqueueDownloadAudio(p DownloadAudioPayload) error {
	return c.enqueue(TaskDownloadAudio, p, QueueDefault)
}
func (c *Client) EnqueueTranscribeVideo(p TranscribeVideoPayload) error {
	return c.enqueue(TaskTranscribeVideo, p, QueueAI)
}
func (c *Client) EnqueueComposeRecipe(p ComposeRecipePayload) error {
	return c.enqueue(TaskComposeRecipe, p, QueueAI)
}
func (c *Client) EnqueuePublishRecipe(p PublishRecipePayload) error {
	return c.enqueue(TaskPublishRecipe, p, QueueDefault)
}

func (c *Client) enqueue(taskType string, payload any, queue string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", taskType, err)
	}
	t := asynq.NewTask(taskType, body)
	if _, err := c.C.Enqueue(t,
		asynq.Queue(queue),
		asynq.MaxRetry(5),
	); err != nil {
		return fmt.Errorf("enqueue %s: %w", taskType, err)
	}
	return nil
}
