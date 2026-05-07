package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrNotFound = errors.New("not found")

type Queries struct {
	db *DB
}

func NewQueries(d *DB) *Queries {
	return &Queries{db: d}
}

// ---------- Dishes ----------

const dishSelectCols = `id, slug, name_vi, name_en, category, region, keywords, status, created_at,
  locked_by, locked_until, last_error_code, last_error_message, last_error_stage, last_error_at`

func scanDish(row pgx.Row) (*Dish, error) {
	var d Dish
	if err := row.Scan(
		&d.ID, &d.Slug, &d.NameVi, &d.NameEn, &d.Category, &d.Region, &d.Keywords,
		&d.Status, &d.CreatedAt,
		&d.LockedBy, &d.LockedUntil,
		&d.LastErrorCode, &d.LastErrorMessage, &d.LastErrorStage, &d.LastErrorAt,
	); err != nil {
		return nil, err
	}
	return &d, nil
}

func (q *Queries) CreateDish(ctx context.Context, d Dish) (*Dish, error) {
	sql := fmt.Sprintf(`
INSERT INTO dishes (slug, name_vi, name_en, category, region, keywords, status)
VALUES ($1,$2,$3,$4,$5,$6, COALESCE(NULLIF($7,''), 'pending'))
RETURNING %s`, dishSelectCols)

	out, err := scanDish(q.db.Pool.QueryRow(ctx, sql,
		d.Slug, d.NameVi, d.NameEn, d.Category, d.Region, d.Keywords, d.Status))
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (q *Queries) UpdateDishStatus(ctx context.Context, id int64, status string) error {
	_, err := q.db.Pool.Exec(ctx, `UPDATE dishes SET status=$1 WHERE id=$2`, status, id)
	return err
}

func (q *Queries) GetDish(ctx context.Context, id int64) (*Dish, error) {
	sql := fmt.Sprintf(`SELECT %s FROM dishes WHERE id=$1`, dishSelectCols)
	d, err := scanDish(q.db.Pool.QueryRow(ctx, sql, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return d, nil
}

func (q *Queries) GetDishBySlug(ctx context.Context, slug string) (*Dish, error) {
	sql := fmt.Sprintf(`SELECT %s FROM dishes WHERE slug=$1`, dishSelectCols)
	d, err := scanDish(q.db.Pool.QueryRow(ctx, sql, slug))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return d, nil
}

func (q *Queries) ListDishes(ctx context.Context, status string, limit int) ([]Dish, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	args := []any{limit}
	where := ""
	if status != "" {
		where = "WHERE status = $2"
		args = append(args, status)
	}
	sql := fmt.Sprintf(`SELECT %s FROM dishes %s ORDER BY created_at DESC LIMIT $1`,
		dishSelectCols, where)
	rows, err := q.db.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Dish
	for rows.Next() {
		d, err := scanDish(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (q *Queries) DeleteDish(ctx context.Context, id int64) error {
	_, err := q.db.Pool.Exec(ctx, `DELETE FROM dishes WHERE id=$1`, id)
	return err
}

// ---------- Crawler-facing dish state ----------

// ErrDishLocked được trả khi LockDish phát hiện dish đang bị worker khác giữ.
var ErrDishLocked = errors.New("dish locked by another worker")

// ListPendingDishes trả về danh sách dish trong trạng thái pending, oldest-first.
// Trước khi list, ta tự reclaim các lock đã hết hạn (worker chết giữa chừng).
func (q *Queries) ListPendingDishes(ctx context.Context, limit int) ([]Dish, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if _, err := q.db.Pool.Exec(ctx, `
UPDATE dishes
SET status = 'pending', locked_by = NULL, locked_until = NULL
WHERE status = 'processing' AND locked_until IS NOT NULL AND locked_until < NOW()`); err != nil {
		return nil, fmt.Errorf("reclaim expired locks: %w", err)
	}

	sql := fmt.Sprintf(`SELECT %s FROM dishes WHERE status='pending' ORDER BY created_at ASC LIMIT $1`,
		dishSelectCols)
	rows, err := q.db.Pool.Query(ctx, sql, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Dish{}
	for rows.Next() {
		d, err := scanDish(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// LockDish acquires a TTL-bounded lock cho worker.
// Trả ErrDishLocked nếu worker khác đang giữ lock và TTL chưa hết.
// Cùng worker gọi lại → renew TTL và trả OK (idempotent).
func (q *Queries) LockDish(ctx context.Context, dishID int64, workerID string, ttl time.Duration) (*Dish, error) {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	const sql = `
UPDATE dishes
SET locked_by = $2,
    locked_until = NOW() + ($3::bigint || ' seconds')::interval,
    status = 'processing'
WHERE id = $1
  AND (locked_until IS NULL OR locked_until < NOW() OR locked_by = $2)
RETURNING ` + `id, slug, name_vi, name_en, category, region, keywords, status, created_at,
  locked_by, locked_until, last_error_code, last_error_message, last_error_stage, last_error_at`

	d, err := scanDish(q.db.Pool.QueryRow(ctx, sql, dishID, workerID, int64(ttl.Seconds())))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Hoặc dish không tồn tại, hoặc đang locked bởi worker khác.
			// Phân biệt bằng 1 query phụ.
			if _, gerr := q.GetDish(ctx, dishID); errors.Is(gerr, ErrNotFound) {
				return nil, ErrNotFound
			}
			return nil, ErrDishLocked
		}
		return nil, err
	}
	return d, nil
}

// UnlockDish clears lock và set final status. Nếu finalStatus == "failed",
// ghi luôn last_error_*.
func (q *Queries) UnlockDish(ctx context.Context, dishID int64, finalStatus string,
	errorCode, errorMessage *string, stage *int) error {

	if finalStatus == "" {
		finalStatus = DishStatusPending
	}
	const sql = `
UPDATE dishes
SET locked_by = NULL,
    locked_until = NULL,
    status = $2,
    last_error_code    = COALESCE($3, last_error_code),
    last_error_message = COALESCE($4, last_error_message),
    last_error_stage   = COALESCE($5, last_error_stage),
    last_error_at      = CASE WHEN $3 IS NOT NULL THEN NOW() ELSE last_error_at END
WHERE id = $1`
	tag, err := q.db.Pool.Exec(ctx, sql, dishID, finalStatus, errorCode, errorMessage, stage)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- Idempotency ----------

// IdempotencyHit là response đã ghi cho một Idempotency-Key trước đó.
type IdempotencyHit struct {
	StatusCode int
	Response   []byte
}

func (q *Queries) GetIdempotency(ctx context.Context, key string) (*IdempotencyHit, error) {
	if key == "" {
		return nil, nil
	}
	const sql = `SELECT status_code, response::text FROM idempotency_keys
WHERE key = $1 AND expires_at > NOW()`
	var hit IdempotencyHit
	var raw string
	if err := q.db.Pool.QueryRow(ctx, sql, key).Scan(&hit.StatusCode, &raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	hit.Response = []byte(raw)
	return &hit, nil
}

// SaveIdempotency ghi phản hồi mới. Nếu key đã tồn tại, giữ nguyên (DO NOTHING)
// để bảo toàn response gốc — đúng semantics idempotency.
func (q *Queries) SaveIdempotency(ctx context.Context, key string, statusCode int, response []byte) error {
	if key == "" {
		return nil
	}
	const sql = `
INSERT INTO idempotency_keys (key, status_code, response)
VALUES ($1, $2, $3::jsonb)
ON CONFLICT (key) DO NOTHING`
	_, err := q.db.Pool.Exec(ctx, sql, key, statusCode, string(response))
	return err
}

// ---------- Pipeline events ----------

// PipelineEvent matches POST /api/internal/events body.
type PipelineEvent struct {
	Kind         string         `json:"kind"`
	DishID       *int64         `json:"dish_id,omitempty"`
	Stage        *int           `json:"stage,omitempty"`
	ErrorCode    *string        `json:"error_code,omitempty"`
	ErrorMessage *string        `json:"error_message,omitempty"`
	ComposeRunID *string        `json:"compose_run_id,omitempty"`
	WorkerID     *string        `json:"-"`
	Details      map[string]any `json:"details,omitempty"`
}

func (q *Queries) RecordPipelineEvent(ctx context.Context, ev PipelineEvent) (int64, error) {
	const sql = `
INSERT INTO pipeline_events
  (kind, dish_id, stage, error_code, error_message, compose_run_id, worker_id, details)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)
RETURNING id`

	var detailsJSON any
	if ev.Details != nil {
		b, err := json.Marshal(ev.Details)
		if err != nil {
			return 0, err
		}
		detailsJSON = string(b)
	}
	var id int64
	if err := q.db.Pool.QueryRow(ctx, sql,
		ev.Kind, ev.DishID, ev.Stage, ev.ErrorCode, ev.ErrorMessage, ev.ComposeRunID, ev.WorkerID, detailsJSON,
	).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// ---------- Public dish reads ----------

// nilIfEmpty trả về nil nếu chuỗi rỗng — giúp các filter SQL kiểu
// `($1::text IS NULL OR ...)` hoạt động đúng khi caller không truyền giá trị.
func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

const listDishesPublicSQL = `
SELECT
    d.slug, d.name_vi, d.name_en, d.region,
    COALESCE(d.keywords, '{}'::text[]) AS keywords,
    EXISTS (SELECT 1 FROM recipes r WHERE r.dish_id = d.id AND r.published_at IS NOT NULL) AS has_recipe
FROM dishes d
WHERE
  ($1::text IS NULL OR EXISTS (
    SELECT 1 FROM dish_categories dc
    JOIN categories c ON c.id = dc.category_id
    WHERE dc.dish_id = d.id AND c.slug = $1
  ))
  AND ($2::text IS NULL OR
    EXISTS (SELECT 1 FROM unnest(COALESCE(d.keywords, '{}'::text[])) k WHERE LOWER(k) = LOWER($2))
    OR EXISTS (
      SELECT 1 FROM dish_categories dc
      JOIN categories c ON c.id = dc.category_id
      WHERE dc.dish_id = d.id AND EXISTS (
        SELECT 1 FROM unnest(c.tags) t WHERE LOWER(t) = LOWER($2)
      )
    )
  )
  AND ($3::text IS NULL OR d.region = $3)
  AND ($4::boolean IS NULL OR EXISTS (
        SELECT 1 FROM recipes r WHERE r.dish_id = d.id AND r.published_at IS NOT NULL
      ) = $4)
ORDER BY d.name_vi
LIMIT $5 OFFSET $6`

func (q *Queries) ListDishesPublic(ctx context.Context, f DishFilter) ([]DishPublic, error) {
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	rows, err := q.db.Pool.Query(ctx, listDishesPublicSQL,
		nilIfEmpty(f.CategorySlug),
		nilIfEmpty(f.Tag),
		nilIfEmpty(f.Region),
		f.HasRecipe,
		limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []DishPublic{}
	for rows.Next() {
		var d DishPublic
		if err := rows.Scan(&d.Slug, &d.NameVi, &d.NameEn, &d.Region, &d.Keywords, &d.HasRecipe); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (q *Queries) GetDishPublic(ctx context.Context, slug string) (*DishPublic, error) {
	const sql = `
SELECT
    d.id, d.slug, d.name_vi, d.name_en, d.region,
    COALESCE(d.keywords, '{}'::text[]) AS keywords,
    EXISTS (SELECT 1 FROM recipes r WHERE r.dish_id = d.id AND r.published_at IS NOT NULL) AS has_recipe
FROM dishes d
WHERE d.slug = $1`

	var dishID int64
	var d DishPublic
	if err := q.db.Pool.QueryRow(ctx, sql, slug).Scan(
		&dishID, &d.Slug, &d.NameVi, &d.NameEn, &d.Region, &d.Keywords, &d.HasRecipe,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if cats, err := q.ListCategoriesForDish(ctx, dishID); err == nil {
		d.Categories = cats
	}
	return &d, nil
}

// ListAllTags trả về union các giá trị (case-insensitive distinct) từ
// `dishes.keywords` + `categories.tags` (chỉ category visible). Dùng cho
// trang "tag cloud" hoặc dropdown filter ở public.
func (q *Queries) ListAllTags(ctx context.Context) ([]string, error) {
	const sql = `
SELECT t FROM (
  SELECT DISTINCT ON (LOWER(t)) t FROM (
    SELECT unnest(COALESCE(d.keywords, '{}'::text[])) AS t
    FROM dishes d
    UNION ALL
    SELECT unnest(c.tags) AS t
    FROM categories c WHERE c.visible = TRUE
  ) tags
  WHERE t IS NOT NULL AND TRIM(t) <> ''
  ORDER BY LOWER(t), t
) deduped
ORDER BY t`
	rows, err := q.db.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ---------- Videos ----------

func (q *Queries) UpsertVideo(ctx context.Context, v Video) (*Video, error) {
	const sql = `
INSERT INTO videos (dish_id, platform, external_id, url, title, channel, duration_s, transcript, fetched_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (platform, external_id) DO UPDATE SET
  url=EXCLUDED.url,
  title=COALESCE(EXCLUDED.title, videos.title),
  channel=COALESCE(EXCLUDED.channel, videos.channel),
  duration_s=COALESCE(EXCLUDED.duration_s, videos.duration_s),
  transcript=COALESCE(EXCLUDED.transcript, videos.transcript),
  fetched_at=COALESCE(EXCLUDED.fetched_at, videos.fetched_at)
RETURNING id`
	var id int64
	if err := q.db.Pool.QueryRow(ctx, sql,
		v.DishID, v.Platform, v.ExternalID, v.URL, v.Title, v.Channel,
		v.DurationS, v.Transcript, v.FetchedAt).Scan(&id); err != nil {
		return nil, err
	}
	v.ID = id
	return &v, nil
}

// UpsertSourceVideos upserts a batch of crawler-supplied videos and returns
// their DB IDs in input order — used to populate `recipes.source_video_ids`.
func (q *Queries) UpsertSourceVideos(ctx context.Context, dishID int64, videos []SourceVideo) ([]int64, error) {
	out := make([]int64, 0, len(videos))
	for _, v := range videos {
		if v.Platform == "" || v.ExternalID == "" {
			continue
		}
		now := time.Now()
		dst, err := q.UpsertVideo(ctx, Video{
			DishID:     dishID,
			Platform:   v.Platform,
			ExternalID: v.ExternalID,
			URL:        v.URL,
			Title:      v.Title,
			Channel:    v.Channel,
			DurationS:  v.DurationS,
			Transcript: v.Transcript,
			FetchedAt:  &now,
		})
		if err != nil {
			return nil, fmt.Errorf("upsert video %s/%s: %w", v.Platform, v.ExternalID, err)
		}
		out = append(out, dst.ID)
	}
	return out, nil
}

func (q *Queries) SetVideoTranscript(ctx context.Context, id int64, transcript string) error {
	_, err := q.db.Pool.Exec(ctx,
		`UPDATE videos SET transcript=$1, fetched_at=NOW() WHERE id=$2`, transcript, id)
	return err
}

func (q *Queries) ListVideosByDish(ctx context.Context, dishID int64) ([]Video, error) {
	const sql = `SELECT id, dish_id, platform, external_id, url, title, channel, duration_s, transcript, fetched_at
FROM videos WHERE dish_id=$1 ORDER BY id`
	rows, err := q.db.Pool.Query(ctx, sql, dishID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Video
	for rows.Next() {
		var v Video
		if err := rows.Scan(&v.ID, &v.DishID, &v.Platform, &v.ExternalID, &v.URL,
			&v.Title, &v.Channel, &v.DurationS, &v.Transcript, &v.FetchedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ---------- Recipes ----------

// UpsertRecipe inserts or updates a recipe + replaces its ingredients/steps in one transaction.
// UpsertRecipe — KHÔNG tự set `published_at` nữa (so với version trước).
// Caller phải gọi MarkPublished() khi muốn publish: internal pipeline tự gọi
// sau Stage 5; crawler thì để admin duyệt rồi publish qua admin API.
func (q *Queries) UpsertRecipe(
	ctx context.Context,
	dishID int64,
	r ComposedRecipe,
	sourceVideoIDs []int64,
	heroImageURL *string,
	composer *ComposerMeta,
) (int64, error) {
	tx, err := q.db.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	const upsert = `
INSERT INTO recipes (
  dish_id, title, description, prep_time_min, cook_time_min, total_time_min,
  servings, difficulty, calories, intro_md, tips_md, variations_md,
  source_video_ids, seo_title, seo_description, seo_keywords, hero_image_url,
  composer_model, composer_tokens_in, composer_tokens_out, composer_cost_usd, compose_run_id,
  updated_at
) VALUES (
  $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,
  $18,$19,$20,$21,$22,
  NOW()
)
ON CONFLICT (dish_id) DO UPDATE SET
  title=EXCLUDED.title,
  description=EXCLUDED.description,
  prep_time_min=EXCLUDED.prep_time_min,
  cook_time_min=EXCLUDED.cook_time_min,
  total_time_min=EXCLUDED.total_time_min,
  servings=EXCLUDED.servings,
  difficulty=EXCLUDED.difficulty,
  calories=EXCLUDED.calories,
  intro_md=EXCLUDED.intro_md,
  tips_md=EXCLUDED.tips_md,
  variations_md=EXCLUDED.variations_md,
  source_video_ids=EXCLUDED.source_video_ids,
  seo_title=EXCLUDED.seo_title,
  seo_description=EXCLUDED.seo_description,
  seo_keywords=EXCLUDED.seo_keywords,
  hero_image_url=COALESCE(EXCLUDED.hero_image_url, recipes.hero_image_url),
  composer_model=EXCLUDED.composer_model,
  composer_tokens_in=EXCLUDED.composer_tokens_in,
  composer_tokens_out=EXCLUDED.composer_tokens_out,
  composer_cost_usd=EXCLUDED.composer_cost_usd,
  compose_run_id=EXCLUDED.compose_run_id,
  updated_at=NOW()
RETURNING id`

	var (
		composerModel    any
		composerTokensIn any
		composerTokensOu any
		composerCost     any
		composerRunID    any
	)
	if composer != nil {
		composerModel = nullableStr(composer.Model, 100)
		composerTokensIn = nullInt(composer.TokensIn)
		composerTokensOu = nullInt(composer.TokensOut)
		if composer.CostUSD > 0 {
			composerCost = composer.CostUSD
		}
		composerRunID = nullableStr(composer.ComposeRunID, 100)
	}

	calories := 0
	if r.CaloriesPerServing != nil {
		calories = *r.CaloriesPerServing
	}

	var recipeID int64
	if err := tx.QueryRow(ctx, upsert,
		dishID,
		nullableStr(r.Title, 300),
		r.Description,
		nullInt(r.PrepTimeMin),
		nullInt(r.CookTimeMin),
		nullInt(r.TotalTimeMin),
		nullInt(r.Servings),
		nullableStr(r.Difficulty, 20),
		nullInt(calories),
		nullableStr(r.IntroMD, 0),
		toMarkdownList(r.Tips),
		toMarkdownList(r.Variations),
		sourceVideoIDs,
		nullableStr(r.SEOTitle, 60),
		seoDescriptionFrom(r),
		stringsOrEmpty(r.SEOKeywords),
		heroImageURL,
		composerModel,
		composerTokensIn,
		composerTokensOu,
		composerCost,
		composerRunID,
	).Scan(&recipeID); err != nil {
		return 0, fmt.Errorf("upsert recipe: %w", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM recipe_ingredients WHERE recipe_id=$1`, recipeID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM recipe_steps WHERE recipe_id=$1`, recipeID); err != nil {
		return 0, err
	}

	for i, ing := range r.Ingredients {
		if _, err := tx.Exec(ctx,
			`INSERT INTO recipe_ingredients (recipe_id, position, name, quantity, unit, note)
VALUES ($1,$2,$3,$4,$5,$6)`,
			recipeID, i, ing.Name, ing.Quantity, ing.Unit, ing.Note); err != nil {
			return 0, fmt.Errorf("insert ingredient %d: %w", i, err)
		}
	}

	for i, st := range r.Steps {
		if _, err := tx.Exec(ctx,
			`INSERT INTO recipe_steps (recipe_id, position, instruction, image_url, duration_s, tip)
VALUES ($1,$2,$3,$4,$5,$6)`,
			recipeID, i, st.Instruction, st.ImageURL, st.DurationS, st.Tip); err != nil {
			return 0, fmt.Errorf("insert step %d: %w", i, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return recipeID, nil
}

// ---------- Public reads ----------

const recipeSelectCols = `
r.id, d.slug, r.title, r.description, d.category, d.keywords,
r.prep_time_min, r.cook_time_min, r.total_time_min, r.servings,
r.difficulty, r.calories, r.intro_md, r.tips_md, r.variations_md,
r.hero_image_url, r.seo_title, r.seo_description, r.published_at, r.updated_at`

func (q *Queries) scanRecipeRow(row pgx.Row) (*Recipe, error) {
	var r Recipe
	if err := row.Scan(
		&r.ID, &r.Slug, &r.Title, &r.Description, &r.Category, &r.Keywords,
		&r.PrepTimeMin, &r.CookTimeMin, &r.TotalTimeMin, &r.Servings,
		&r.Difficulty, &r.Calories, &r.IntroMD, &r.TipsMD, &r.VariationsMD,
		&r.HeroImageURL, &r.SEOTitle, &r.SEODescription, &r.PublishedAt, &r.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &r, nil
}

func (q *Queries) GetRecipeBySlug(ctx context.Context, slug string) (*Recipe, error) {
	sql := fmt.Sprintf(`SELECT %s FROM recipes r
JOIN dishes d ON d.id = r.dish_id
WHERE d.slug = $1 AND r.published_at IS NOT NULL`, recipeSelectCols)
	r, err := q.scanRecipeRow(q.db.Pool.QueryRow(ctx, sql, slug))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	ings, err := q.listIngredients(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	r.Ingredients = ings

	steps, err := q.listSteps(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	r.Steps = steps

	// Attach minimal video info (no transcript) for credit & embed,
	// plus the dish's categories so the recipe page can render breadcrumbs.
	dishID, err := q.dishIDFromRecipe(ctx, r.ID)
	if err == nil && dishID > 0 {
		vids, _ := q.ListVideosByDish(ctx, dishID)
		for i := range vids {
			vids[i].Transcript = nil
		}
		r.Videos = vids

		if cats, err := q.ListCategoriesForDish(ctx, dishID); err == nil {
			r.Categories = cats
		}
	}

	return r, nil
}

func (q *Queries) dishIDFromRecipe(ctx context.Context, recipeID int64) (int64, error) {
	var id int64
	err := q.db.Pool.QueryRow(ctx, `SELECT dish_id FROM recipes WHERE id=$1`, recipeID).Scan(&id)
	return id, err
}

func (q *Queries) listIngredients(ctx context.Context, recipeID int64) ([]Ingredient, error) {
	rows, err := q.db.Pool.Query(ctx,
		`SELECT position, name, quantity, unit, note FROM recipe_ingredients
WHERE recipe_id=$1 ORDER BY position`, recipeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Ingredient
	for rows.Next() {
		var ing Ingredient
		if err := rows.Scan(&ing.Position, &ing.Name, &ing.Quantity, &ing.Unit, &ing.Note); err != nil {
			return nil, err
		}
		out = append(out, ing)
	}
	return out, rows.Err()
}

func (q *Queries) listSteps(ctx context.Context, recipeID int64) ([]Step, error) {
	rows, err := q.db.Pool.Query(ctx,
		`SELECT position, instruction, image_url, duration_s FROM recipe_steps
WHERE recipe_id=$1 ORDER BY position`, recipeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Step
	for rows.Next() {
		var s Step
		if err := rows.Scan(&s.Position, &s.Instruction, &s.ImageURL, &s.DurationS); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (q *Queries) ListPublishedSlugs(ctx context.Context) ([]string, error) {
	rows, err := q.db.Pool.Query(ctx, `
SELECT d.slug FROM recipes r
JOIN dishes d ON d.id = r.dish_id
WHERE r.published_at IS NOT NULL
ORDER BY r.published_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListRecipeSummaries lists published recipes, optionally filtered by a
// category slug (matched through the dish_categories join table).
func (q *Queries) ListRecipeSummaries(ctx context.Context, categorySlug string, limit int) ([]RecipeSummary, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	if categorySlug == "" {
		const sql = `
SELECT d.slug, r.title, r.description, r.hero_image_url, r.total_time_min, r.servings, d.keywords
FROM recipes r
JOIN dishes d ON d.id = r.dish_id
WHERE r.published_at IS NOT NULL
ORDER BY r.published_at DESC NULLS LAST
LIMIT $1`
		return q.scanSummaries(ctx, sql, limit)
	}

	const sql = `
SELECT d.slug, r.title, r.description, r.hero_image_url, r.total_time_min, r.servings, d.keywords
FROM recipes r
JOIN dishes d ON d.id = r.dish_id
JOIN dish_categories dc ON dc.dish_id = d.id
JOIN categories c ON c.id = dc.category_id
WHERE r.published_at IS NOT NULL AND c.slug = $2
ORDER BY r.published_at DESC NULLS LAST
LIMIT $1`
	return q.scanSummaries(ctx, sql, limit, categorySlug)
}

func (q *Queries) scanSummaries(ctx context.Context, sql string, args ...any) ([]RecipeSummary, error) {
	rows, err := q.db.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecipeSummary{}
	for rows.Next() {
		var s RecipeSummary
		if err := rows.Scan(&s.Slug, &s.Title, &s.Description, &s.HeroImageURL,
			&s.TotalTimeMin, &s.Servings, &s.Tags); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ---------- Categories ----------

const categorySelectCols = `c.id, c.slug, c.name_vi, c.name_en, c.description, c.tags,
  c.icon, c.sort_order, c.visible, c.show_on_home, c.created_at, c.updated_at,
  COALESCE((
    SELECT COUNT(*) FROM dish_categories dc
    JOIN recipes r ON r.dish_id = dc.dish_id
    WHERE dc.category_id = c.id AND r.published_at IS NOT NULL
  ), 0) AS recipe_count`

func scanCategory(row pgx.Row) (*Category, error) {
	var c Category
	if err := row.Scan(&c.ID, &c.Slug, &c.NameVi, &c.NameEn, &c.Description, &c.Tags,
		&c.Icon, &c.SortOrder, &c.Visible, &c.ShowOnHome, &c.CreatedAt, &c.UpdatedAt,
		&c.RecipeCount); err != nil {
		return nil, err
	}
	return &c, nil
}

// CategoryFilter controls which categories ListCategories returns.
type CategoryFilter struct {
	VisibleOnly bool
	HomeOnly    bool
}

func (q *Queries) ListCategories(ctx context.Context, f CategoryFilter) ([]Category, error) {
	where := []string{}
	if f.VisibleOnly {
		where = append(where, "c.visible = TRUE")
	}
	if f.HomeOnly {
		where = append(where, "c.show_on_home = TRUE")
	}
	clause := ""
	if len(where) > 0 {
		clause = "WHERE " + strings.Join(where, " AND ")
	}
	sql := fmt.Sprintf(`SELECT %s FROM categories c %s ORDER BY c.sort_order, c.slug`, categorySelectCols, clause)
	rows, err := q.db.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Category{}
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (q *Queries) GetCategory(ctx context.Context, id int64) (*Category, error) {
	sql := fmt.Sprintf(`SELECT %s FROM categories c WHERE c.id = $1`, categorySelectCols)
	c, err := scanCategory(q.db.Pool.QueryRow(ctx, sql, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return c, nil
}

func (q *Queries) GetCategoryBySlug(ctx context.Context, slug string) (*Category, error) {
	sql := fmt.Sprintf(`SELECT %s FROM categories c WHERE c.slug = $1`, categorySelectCols)
	c, err := scanCategory(q.db.Pool.QueryRow(ctx, sql, slug))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return c, nil
}

func (q *Queries) UpsertCategory(ctx context.Context, c Category) (*Category, error) {
	const sql = `
INSERT INTO categories (slug, name_vi, name_en, description, tags, icon, sort_order, visible, show_on_home)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (slug) DO UPDATE SET
  name_vi=EXCLUDED.name_vi,
  name_en=EXCLUDED.name_en,
  description=EXCLUDED.description,
  tags=EXCLUDED.tags,
  icon=EXCLUDED.icon,
  sort_order=EXCLUDED.sort_order,
  visible=EXCLUDED.visible,
  show_on_home=EXCLUDED.show_on_home,
  updated_at=NOW()
RETURNING id`
	var id int64
	if err := q.db.Pool.QueryRow(ctx, sql,
		c.Slug, c.NameVi, c.NameEn, c.Description, c.Tags, c.Icon,
		c.SortOrder, c.Visible, c.ShowOnHome,
	).Scan(&id); err != nil {
		return nil, err
	}
	return q.GetCategory(ctx, id)
}

func (q *Queries) UpdateCategory(ctx context.Context, id int64, c Category) (*Category, error) {
	const sql = `
UPDATE categories SET
  name_vi=$2, name_en=$3, description=$4, tags=$5, icon=$6,
  sort_order=$7, visible=$8, show_on_home=$9, updated_at=NOW()
WHERE id=$1`
	tag, err := q.db.Pool.Exec(ctx, sql,
		id, c.NameVi, c.NameEn, c.Description, c.Tags, c.Icon,
		c.SortOrder, c.Visible, c.ShowOnHome)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return q.GetCategory(ctx, id)
}

func (q *Queries) DeleteCategory(ctx context.Context, id int64) error {
	tag, err := q.db.Pool.Exec(ctx, `DELETE FROM categories WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ReorderCategories sets sort_order based on the slug ordering provided.
// Slugs not present keep their current sort_order.
func (q *Queries) ReorderCategories(ctx context.Context, orderedSlugs []string) error {
	tx, err := q.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for i, slug := range orderedSlugs {
		if _, err := tx.Exec(ctx,
			`UPDATE categories SET sort_order=$1, updated_at=NOW() WHERE slug=$2`,
			i*10, slug); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ReorderCategoriesByIDs — variant cho admin doc body `{ordered_ids: int[]}`.
func (q *Queries) ReorderCategoriesByIDs(ctx context.Context, orderedIDs []int64) error {
	tx, err := q.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for i, id := range orderedIDs {
		if _, err := tx.Exec(ctx,
			`UPDATE categories SET sort_order=$1, updated_at=NOW() WHERE id=$2`,
			i*10, id); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ---------- Dish ↔ Category ----------

func (q *Queries) AssignDishCategories(ctx context.Context, dishID int64, categoryIDs []int64) error {
	tx, err := q.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM dish_categories WHERE dish_id=$1`, dishID); err != nil {
		return err
	}
	for _, cid := range categoryIDs {
		if _, err := tx.Exec(ctx,
			`INSERT INTO dish_categories (dish_id, category_id) VALUES ($1,$2)
ON CONFLICT DO NOTHING`, dishID, cid); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (q *Queries) AddDishCategoryBySlug(ctx context.Context, dishID int64, categorySlug string) error {
	_, err := q.db.Pool.Exec(ctx, `
INSERT INTO dish_categories (dish_id, category_id)
SELECT $1, c.id FROM categories c WHERE c.slug = $2
ON CONFLICT DO NOTHING`, dishID, categorySlug)
	return err
}

func (q *Queries) ListCategoriesForDish(ctx context.Context, dishID int64) ([]Category, error) {
	sql := fmt.Sprintf(`SELECT %s FROM categories c
JOIN dish_categories dc ON dc.category_id = c.id
WHERE dc.dish_id = $1
ORDER BY c.sort_order, c.slug`, categorySelectCols)
	rows, err := q.db.Pool.Query(ctx, sql, dishID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Category{}
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// ListHomeSections returns each home category along with up to `perSection`
// of its most-recent published recipes. One round-trip per section keeps
// the SQL simple; with ~10 sections this is cheap.
func (q *Queries) ListHomeSections(ctx context.Context, perSection int) ([]HomeSection, error) {
	if perSection <= 0 || perSection > 24 {
		perSection = 8
	}
	cats, err := q.ListCategories(ctx, CategoryFilter{VisibleOnly: true, HomeOnly: true})
	if err != nil {
		return nil, err
	}
	out := make([]HomeSection, 0, len(cats))
	for _, c := range cats {
		recipes, err := q.ListRecipeSummaries(ctx, c.Slug, perSection)
		if err != nil {
			return nil, err
		}
		out = append(out, HomeSection{Category: c, Recipes: recipes})
	}
	return out, nil
}

func (q *Queries) SearchRecipes(ctx context.Context, query string, limit int) ([]RecipeSummary, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []RecipeSummary{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	pat := "%" + strings.ToLower(query) + "%"
	rows, err := q.db.Pool.Query(ctx, `
SELECT d.slug, r.title, r.description, r.hero_image_url, r.total_time_min, r.servings, d.keywords
FROM recipes r
JOIN dishes d ON d.id = r.dish_id
WHERE r.published_at IS NOT NULL
  AND (
    LOWER(d.name_vi) LIKE $1 OR
    LOWER(r.title) LIKE $1 OR
    LOWER(COALESCE(r.description, '')) LIKE $1 OR
    EXISTS (SELECT 1 FROM unnest(COALESCE(d.keywords,'{}'::text[])) k WHERE LOWER(k) LIKE $1)
  )
ORDER BY r.published_at DESC NULLS LAST
LIMIT $2`, pat, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecipeSummary{}
	for rows.Next() {
		var s RecipeSummary
		if err := rows.Scan(&s.Slug, &s.Title, &s.Description, &s.HeroImageURL,
			&s.TotalTimeMin, &s.Servings, &s.Tags); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ---------- helpers ----------

func nullableStr(s string, max int) any {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if max > 0 && len(s) > max {
		s = s[:max]
	}
	return s
}

func nullInt(n int) any {
	if n <= 0 {
		return nil
	}
	return n
}

// seoDescriptionFrom — `description` của ComposedRecipe vốn đã là chuỗi ≤ 160
// chars dùng cho meta description (xem prompt). Crawler không gửi field
// `seo_description` riêng, nên ta tự rút từ `description`.
func seoDescriptionFrom(r ComposedRecipe) any {
	d := strings.TrimSpace(r.Description)
	if d == "" {
		return nil
	}
	if len(d) > 160 {
		d = d[:160]
	}
	return d
}

// stringsOrEmpty đảm bảo cột TEXT[] NOT NULL DEFAULT '{}' nhận đúng kiểu
// thay vì NULL khi caller truyền slice nil.
func stringsOrEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func toMarkdownList(items []string) any {
	cleaned := make([]string, 0, len(items))
	for _, it := range items {
		it = strings.TrimSpace(it)
		if it == "" {
			continue
		}
		cleaned = append(cleaned, "- "+it)
	}
	if len(cleaned) == 0 {
		return nil
	}
	return strings.Join(cleaned, "\n")
}

// Used by the publish stage to update timestamps after revalidate.
func (q *Queries) MarkPublished(ctx context.Context, recipeID int64) error {
	_, err := q.db.Pool.Exec(ctx,
		`UPDATE recipes SET published_at=COALESCE(published_at, NOW()), updated_at=NOW() WHERE id=$1`, recipeID)
	return err
}

// ListAllPublishedRecipesUpdatedSince is used by the sitemap endpoint.
func (q *Queries) ListPublishedSummariesUpdatedSince(ctx context.Context, since time.Time) ([]RecipeSummary, error) {
	rows, err := q.db.Pool.Query(ctx, `
SELECT d.slug, r.title, r.description, r.hero_image_url, r.total_time_min, r.servings, d.keywords
FROM recipes r
JOIN dishes d ON d.id = r.dish_id
WHERE r.published_at IS NOT NULL AND r.updated_at >= $1
ORDER BY r.updated_at DESC`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecipeSummary{}
	for rows.Next() {
		var s RecipeSummary
		if err := rows.Scan(&s.Slug, &s.Title, &s.Description, &s.HeroImageURL,
			&s.TotalTimeMin, &s.Servings, &s.Tags); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
