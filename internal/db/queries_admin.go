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

// ---------- Tags (first-class taxonomy) ----------

const tagSelectCols = `t.id, t.slug, t.name, t.description, t.sort_order,
  COALESCE((SELECT COUNT(*) FROM dish_tags dt WHERE dt.tag_id = t.id), 0) AS dish_count,
  t.created_at, t.updated_at`

func scanTag(row pgx.Row) (*Tag, error) {
	var t Tag
	if err := row.Scan(&t.ID, &t.Slug, &t.Name, &t.Description, &t.SortOrder,
		&t.DishCount, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

// ListTags trả tags. `q` (substring case-insensitive) lọc theo name hoặc slug.
func (q *Queries) ListTags(ctx context.Context, search string, limit int) ([]Tag, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	args := []any{limit}
	where := ""
	if search = strings.TrimSpace(search); search != "" {
		args = append(args, "%"+strings.ToLower(search)+"%")
		where = "WHERE LOWER(t.name) LIKE $2 OR LOWER(t.slug) LIKE $2"
	}
	sql := fmt.Sprintf(`SELECT %s FROM tags t %s ORDER BY t.sort_order, t.name LIMIT $1`,
		tagSelectCols, where)

	rows, err := q.db.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Tag{}
	for rows.Next() {
		t, err := scanTag(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (q *Queries) GetTag(ctx context.Context, id int64) (*Tag, error) {
	sql := fmt.Sprintf(`SELECT %s FROM tags t WHERE t.id = $1`, tagSelectCols)
	t, err := scanTag(q.db.Pool.QueryRow(ctx, sql, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return t, nil
}

func (q *Queries) CreateTag(ctx context.Context, t Tag) (*Tag, error) {
	const sql = `
INSERT INTO tags (slug, name, description, sort_order)
VALUES ($1, $2, $3, COALESCE(NULLIF($4, 0), 100))
RETURNING id`
	var id int64
	if err := q.db.Pool.QueryRow(ctx, sql,
		t.Slug, t.Name, t.Description, t.SortOrder).Scan(&id); err != nil {
		return nil, err
	}
	return q.GetTag(ctx, id)
}

func (q *Queries) UpdateTag(ctx context.Context, id int64, t Tag) (*Tag, error) {
	const sql = `
UPDATE tags SET
  name = $2, description = $3, sort_order = $4, updated_at = NOW()
WHERE id = $1`
	tag, err := q.db.Pool.Exec(ctx, sql, id, t.Name, t.Description, t.SortOrder)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return q.GetTag(ctx, id)
}

func (q *Queries) DeleteTag(ctx context.Context, id int64) error {
	tag, err := q.db.Pool.Exec(ctx, `DELETE FROM tags WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MergeTags chuyển toàn bộ dish gắn `sourceID` sang `targetID` rồi xoá source.
// Idempotent qua `ON CONFLICT DO NOTHING` ở junction.
func (q *Queries) MergeTags(ctx context.Context, sourceID, targetID int64) error {
	if sourceID == targetID {
		return errors.New("source and target must differ")
	}
	tx, err := q.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
INSERT INTO dish_tags (dish_id, tag_id)
SELECT dt.dish_id, $2 FROM dish_tags dt WHERE dt.tag_id = $1
ON CONFLICT DO NOTHING`, sourceID, targetID); err != nil {
		return fmt.Errorf("merge tags: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM dish_tags WHERE tag_id = $1`, sourceID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM tags WHERE id = $1`, sourceID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SetDishTags replaces toàn bộ tag của dish.
func (q *Queries) SetDishTags(ctx context.Context, dishID int64, tagIDs []int64) error {
	tx, err := q.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM dish_tags WHERE dish_id = $1`, dishID); err != nil {
		return err
	}
	for _, tid := range tagIDs {
		if _, err := tx.Exec(ctx,
			`INSERT INTO dish_tags (dish_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			dishID, tid); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ListTagsForDish — thường dùng trong dish detail response.
func (q *Queries) ListTagsForDish(ctx context.Context, dishID int64) ([]Tag, error) {
	sql := fmt.Sprintf(`SELECT %s FROM tags t
JOIN dish_tags dt ON dt.tag_id = t.id
WHERE dt.dish_id = $1
ORDER BY t.sort_order, t.name`, tagSelectCols)
	rows, err := q.db.Pool.Query(ctx, sql, dishID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Tag{}
	for rows.Next() {
		t, err := scanTag(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// ---------- Dishes admin (paginated + full update + soft delete) ----------

type DishListAdminFilter struct {
	Q        string
	Status   string
	Category string // category slug
	Region   string
	Sort     string // created_desc | created_asc | updated_desc | name_asc
	Page     int
	PageSize int
}

type DishListPage struct {
	Items    []Dish `json:"items"`
	Total    int    `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

func (q *Queries) ListDishesAdmin(ctx context.Context, f DishListAdminFilter) (*DishListPage, error) {
	if f.PageSize <= 0 || f.PageSize > 100 {
		f.PageSize = 20
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	offset := (f.Page - 1) * f.PageSize

	var (
		conds []string
		args  []any
	)

	if s := strings.TrimSpace(f.Q); s != "" {
		args = append(args, "%"+strings.ToLower(s)+"%")
		conds = append(conds, fmt.Sprintf("(LOWER(d.name_vi) LIKE $%d OR LOWER(d.slug) LIKE $%d)", len(args), len(args)))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		conds = append(conds, fmt.Sprintf("d.status = $%d", len(args)))
	}
	if f.Region != "" {
		args = append(args, f.Region)
		conds = append(conds, fmt.Sprintf("d.region = $%d", len(args)))
	}
	if f.Category != "" {
		args = append(args, f.Category)
		conds = append(conds, fmt.Sprintf(`EXISTS (
  SELECT 1 FROM dish_categories dc JOIN categories c ON c.id = dc.category_id
  WHERE dc.dish_id = d.id AND c.slug = $%d
)`, len(args)))
	}

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	orderBy := "d.created_at DESC"
	switch f.Sort {
	case "created_asc":
		orderBy = "d.created_at ASC"
	case "name_asc":
		orderBy = "d.name_vi ASC"
	case "updated_desc":
		// Dish chưa có updated_at — fallback dùng MAX(events) cho từng dish quá tốn.
		// Tạm dùng id DESC làm proxy cho "thay đổi gần đây".
		orderBy = "d.id DESC"
	}

	// Count tổng
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM dishes d %s", where)
	var total int
	if err := q.db.Pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, err
	}

	// List
	listArgs := append(append([]any{}, args...), f.PageSize, offset)
	listSQL := fmt.Sprintf(`SELECT %s FROM dishes d %s ORDER BY %s LIMIT $%d OFFSET $%d`,
		dishSelectCols, where, orderBy, len(args)+1, len(args)+2)

	rows, err := q.db.Pool.Query(ctx, listSQL, listArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []Dish{}
	for rows.Next() {
		d, err := scanDish(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &DishListPage{
		Items: items, Total: total, Page: f.Page, PageSize: f.PageSize,
	}, nil
}

// DishUpdate là partial update payload — chỉ field non-nil được update.
type DishUpdate struct {
	NameVi   *string  `json:"name_vi,omitempty"`
	NameEn   *string  `json:"name_en,omitempty"`
	Slug     *string  `json:"slug,omitempty"`
	Region   *string  `json:"region,omitempty"`
	Keywords []string `json:"keywords,omitempty"`
	Status   *string  `json:"status,omitempty"`
}

func (q *Queries) UpdateDish(ctx context.Context, id int64, u DishUpdate) (*Dish, error) {
	const sql = `
UPDATE dishes SET
  name_vi  = COALESCE($2, name_vi),
  name_en  = COALESCE($3, name_en),
  slug     = COALESCE($4, slug),
  region   = COALESCE($5, region),
  keywords = COALESCE($6, keywords),
  status   = COALESCE($7, status)
WHERE id = $1`
	tag, err := q.db.Pool.Exec(ctx, sql, id, u.NameVi, u.NameEn, u.Slug, u.Region, u.Keywords, u.Status)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return q.GetDish(ctx, id)
}

// SoftDeleteDish set status = archived (giữ row + recipe để khôi phục).
func (q *Queries) SoftDeleteDish(ctx context.Context, id int64) error {
	tag, err := q.db.Pool.Exec(ctx,
		`UPDATE dishes SET status = $2, locked_by = NULL, locked_until = NULL WHERE id = $1`,
		id, DishStatusArchived)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RetryDish reset dish về pending và đánh recipe (nếu có) thành discarded.
func (q *Queries) RetryDish(ctx context.Context, id int64) error {
	tx, err := q.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
UPDATE dishes SET
  status = $2, locked_by = NULL, locked_until = NULL,
  last_error_code = NULL, last_error_message = NULL,
  last_error_stage = NULL, last_error_at = NULL
WHERE id = $1`, id, DishStatusPending)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	if _, err := tx.Exec(ctx,
		`UPDATE recipes SET status = $2, published_at = NULL, updated_at = NOW()
WHERE dish_id = $1 AND status <> $2`, id, RecipeStatusDiscarded); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ---------- Recipes admin ----------

type RecipeListAdminFilter struct {
	Status   string
	DishID   *int64
	Page     int
	PageSize int
}

type RecipeAdminSummary struct {
	ID             int64      `json:"id"`
	DishID         int64      `json:"dish_id"`
	Slug           string     `json:"slug"`
	Title          string     `json:"title"`
	Status         string     `json:"status"`
	HeroImageURL   *string    `json:"hero_image_url,omitempty"`
	ComposerModel  *string    `json:"composer_model,omitempty"`
	ComposerCost   *float64   `json:"composer_cost_usd,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
	PublishedAt    *time.Time `json:"published_at,omitempty"`
}

type RecipeListAdminPage struct {
	Items    []RecipeAdminSummary `json:"items"`
	Total    int                  `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
}

func (q *Queries) ListRecipesAdmin(ctx context.Context, f RecipeListAdminFilter) (*RecipeListAdminPage, error) {
	if f.PageSize <= 0 || f.PageSize > 100 {
		f.PageSize = 20
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	offset := (f.Page - 1) * f.PageSize

	var (
		conds []string
		args  []any
	)
	if f.Status != "" {
		args = append(args, f.Status)
		conds = append(conds, fmt.Sprintf("r.status = $%d", len(args)))
	}
	if f.DishID != nil {
		args = append(args, *f.DishID)
		conds = append(conds, fmt.Sprintf("r.dish_id = $%d", len(args)))
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	var total int
	if err := q.db.Pool.QueryRow(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM recipes r %s", where), args...).Scan(&total); err != nil {
		return nil, err
	}

	listArgs := append(append([]any{}, args...), f.PageSize, offset)
	listSQL := fmt.Sprintf(`
SELECT r.id, r.dish_id, d.slug, r.title, r.status, r.hero_image_url,
       r.composer_model, r.composer_cost_usd, r.updated_at, r.published_at
FROM recipes r JOIN dishes d ON d.id = r.dish_id
%s
ORDER BY r.updated_at DESC
LIMIT $%d OFFSET $%d`, where, len(args)+1, len(args)+2)

	rows, err := q.db.Pool.Query(ctx, listSQL, listArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []RecipeAdminSummary{}
	for rows.Next() {
		var s RecipeAdminSummary
		if err := rows.Scan(&s.ID, &s.DishID, &s.Slug, &s.Title, &s.Status, &s.HeroImageURL,
			&s.ComposerModel, &s.ComposerCost, &s.UpdatedAt, &s.PublishedAt); err != nil {
			return nil, err
		}
		items = append(items, s)
	}
	return &RecipeListAdminPage{Items: items, Total: total, Page: f.Page, PageSize: f.PageSize}, nil
}

const recipeAdminSelectCols = `r.id, d.slug, r.title, r.description, d.category, d.keywords,
r.prep_time_min, r.cook_time_min, r.total_time_min, r.servings,
r.difficulty, r.calories, r.intro_md, r.tips_md, r.variations_md,
r.hero_image_url, r.seo_title, r.seo_description, r.published_at, r.updated_at,
r.status, r.rejection_reason`

// GetRecipeAdmin — load full recipe + ingredients + steps + source videos +
// original_recipe (bản AI gốc lúc upsert) cho admin review page.
func (q *Queries) GetRecipeAdmin(ctx context.Context, id int64) (*RecipeAdmin, error) {
	sql := fmt.Sprintf(`SELECT %s, r.original_recipe
FROM recipes r JOIN dishes d ON d.id = r.dish_id
WHERE r.id = $1`, recipeAdminSelectCols)

	var r Recipe
	var originalRaw *string
	if err := q.db.Pool.QueryRow(ctx, sql, id).Scan(
		&r.ID, &r.Slug, &r.Title, &r.Description, &r.Category, &r.Keywords,
		&r.PrepTimeMin, &r.CookTimeMin, &r.TotalTimeMin, &r.Servings,
		&r.Difficulty, &r.Calories, &r.IntroMD, &r.TipsMD, &r.VariationsMD,
		&r.HeroImageURL, &r.SEOTitle, &r.SEODescription, &r.PublishedAt, &r.UpdatedAt,
		&r.Status, &r.RejectionReason, &originalRaw,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if ings, err := q.listIngredients(ctx, r.ID); err == nil {
		r.Ingredients = ings
	}
	if steps, err := q.listSteps(ctx, r.ID); err == nil {
		r.Steps = steps
	}

	// dish_id từ recipe để load videos.
	dishID, _ := q.dishIDFromRecipe(ctx, r.ID)
	var videos []Video
	if dishID > 0 {
		if vs, err := q.ListVideosByDish(ctx, dishID); err == nil {
			videos = vs
		}
	}

	out := &RecipeAdmin{Recipe: r, SourceVideos: videos}
	if originalRaw != nil && *originalRaw != "" {
		var orig ComposedRecipe
		if err := json.Unmarshal([]byte(*originalRaw), &orig); err == nil {
			out.OriginalRecipe = &orig
		}
	}
	return out, nil
}

// RecipeFieldUpdate — partial fields admin có thể edit.
type RecipeFieldUpdate struct {
	Title         *string  `json:"title,omitempty"`
	Description   *string  `json:"description,omitempty"`
	IntroMD       *string  `json:"intro_md,omitempty"`
	TipsMD        *string  `json:"tips_md,omitempty"`
	VariationsMD  *string  `json:"variations_md,omitempty"`
	PrepTimeMin   *int     `json:"prep_time_min,omitempty"`
	CookTimeMin   *int     `json:"cook_time_min,omitempty"`
	TotalTimeMin  *int     `json:"total_time_min,omitempty"`
	Servings      *int     `json:"servings,omitempty"`
	Difficulty    *string  `json:"difficulty,omitempty"`
	Calories      *int     `json:"calories,omitempty"`
	HeroImageURL  *string  `json:"hero_image_url,omitempty"`
	SEOTitle      *string  `json:"seo_title,omitempty"`
	SEODesc       *string  `json:"seo_description,omitempty"`
	SEOKeywords   []string `json:"seo_keywords,omitempty"`
	// Note: ingredients/steps replace toàn bộ nếu non-nil.
	Ingredients []Ingredient `json:"ingredients,omitempty"`
	Steps       []Step       `json:"steps,omitempty"`
}

func (q *Queries) PatchRecipe(ctx context.Context, id int64, u RecipeFieldUpdate) error {
	tx, err := q.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	const sql = `
UPDATE recipes SET
  title          = COALESCE($2,  title),
  description    = COALESCE($3,  description),
  intro_md       = COALESCE($4,  intro_md),
  tips_md        = COALESCE($5,  tips_md),
  variations_md  = COALESCE($6,  variations_md),
  prep_time_min  = COALESCE($7,  prep_time_min),
  cook_time_min  = COALESCE($8,  cook_time_min),
  total_time_min = COALESCE($9,  total_time_min),
  servings       = COALESCE($10, servings),
  difficulty     = COALESCE($11, difficulty),
  calories       = COALESCE($12, calories),
  hero_image_url = COALESCE($13, hero_image_url),
  seo_title      = COALESCE($14, seo_title),
  seo_description= COALESCE($15, seo_description),
  seo_keywords   = COALESCE($16, seo_keywords),
  updated_at     = NOW()
WHERE id = $1`
	tag, err := tx.Exec(ctx, sql, id,
		u.Title, u.Description, u.IntroMD, u.TipsMD, u.VariationsMD,
		u.PrepTimeMin, u.CookTimeMin, u.TotalTimeMin, u.Servings, u.Difficulty,
		u.Calories, u.HeroImageURL, u.SEOTitle, u.SEODesc, u.SEOKeywords,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	if u.Ingredients != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM recipe_ingredients WHERE recipe_id=$1`, id); err != nil {
			return err
		}
		for i, ing := range u.Ingredients {
			if _, err := tx.Exec(ctx,
				`INSERT INTO recipe_ingredients (recipe_id, position, name, quantity, unit, note)
VALUES ($1,$2,$3,$4,$5,$6)`,
				id, i, ing.Name, ing.Quantity, ing.Unit, ing.Note); err != nil {
				return err
			}
		}
	}
	if u.Steps != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM recipe_steps WHERE recipe_id=$1`, id); err != nil {
			return err
		}
		for i, st := range u.Steps {
			if _, err := tx.Exec(ctx,
				`INSERT INTO recipe_steps (recipe_id, position, instruction, image_url, duration_s, tip)
VALUES ($1,$2,$3,$4,$5,$6)`,
				id, i, st.Instruction, st.ImageURL, st.DurationS, st.Tip); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

// PublishRecipeAdmin set recipe.status = published, set published_at = NOW(),
// và đẩy dish.status = published. Trả về dish_id + slug để caller trigger ISR.
func (q *Queries) PublishRecipeAdmin(ctx context.Context, recipeID int64) (dishID int64, slug string, err error) {
	tx, err := q.db.Pool.Begin(ctx)
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `
UPDATE recipes SET
  status = $2,
  published_at = COALESCE(published_at, NOW()),
  updated_at = NOW()
WHERE id = $1
RETURNING dish_id`, recipeID, RecipeStatusPublished)

	if err = row.Scan(&dishID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, "", ErrNotFound
		}
		return 0, "", err
	}

	if err = tx.QueryRow(ctx, `
UPDATE dishes SET status = $2 WHERE id = $1 RETURNING slug`,
		dishID, DishStatusPublished).Scan(&slug); err != nil {
		return 0, "", err
	}

	if err = tx.Commit(ctx); err != nil {
		return 0, "", err
	}
	return dishID, slug, nil
}

// UnpublishRecipeAdmin đảo lại publish: status → ready_for_review, published_at NULL,
// dish trở về ready_for_review.
func (q *Queries) UnpublishRecipeAdmin(ctx context.Context, recipeID int64) error {
	tx, err := q.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var dishID int64
	if err := tx.QueryRow(ctx, `
UPDATE recipes SET
  status = $2, published_at = NULL, updated_at = NOW()
WHERE id = $1
RETURNING dish_id`, recipeID, RecipeStatusReadyForReview).Scan(&dishID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE dishes SET status = $2 WHERE id = $1`, dishID, DishStatusReadyForReview); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RejectRecipe set recipe.status=rejected + dish.status=failed.
func (q *Queries) RejectRecipe(ctx context.Context, recipeID int64, reason string) error {
	tx, err := q.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var dishID int64
	if err := tx.QueryRow(ctx, `
UPDATE recipes SET
  status = $2, rejection_reason = NULLIF($3, ''), updated_at = NOW()
WHERE id = $1
RETURNING dish_id`, recipeID, RecipeStatusRejected, reason).Scan(&dishID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE dishes SET status = $2 WHERE id = $1`, dishID, DishStatusFailed); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ---------- Pipeline events (admin read) ----------

type EventListFilter struct {
	Kind   string
	DishID *int64
	From   *time.Time
	To     *time.Time
	Limit  int
}

type PipelineEventRow struct {
	ID            int64           `json:"id"`
	Kind          string          `json:"kind"`
	DishID        *int64          `json:"dish_id,omitempty"`
	Stage         *int            `json:"stage,omitempty"`
	ErrorCode     *string         `json:"error_code,omitempty"`
	ErrorMessage  *string         `json:"error_message,omitempty"`
	ComposeRunID  *string         `json:"compose_run_id,omitempty"`
	WorkerID      *string         `json:"worker_id,omitempty"`
	Details       json.RawMessage `json:"details,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
}

func (q *Queries) ListPipelineEvents(ctx context.Context, f EventListFilter) ([]PipelineEventRow, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	var (
		conds []string
		args  []any
	)
	if f.Kind != "" {
		args = append(args, f.Kind)
		conds = append(conds, fmt.Sprintf("kind = $%d", len(args)))
	}
	if f.DishID != nil {
		args = append(args, *f.DishID)
		conds = append(conds, fmt.Sprintf("dish_id = $%d", len(args)))
	}
	if f.From != nil {
		args = append(args, *f.From)
		conds = append(conds, fmt.Sprintf("created_at >= $%d", len(args)))
	}
	if f.To != nil {
		args = append(args, *f.To)
		conds = append(conds, fmt.Sprintf("created_at <= $%d", len(args)))
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	args = append(args, f.Limit)

	sql := fmt.Sprintf(`SELECT id, kind, dish_id, stage, error_code, error_message,
  compose_run_id, worker_id, details::text, created_at
FROM pipeline_events %s ORDER BY created_at DESC LIMIT $%d`, where, len(args))

	rows, err := q.db.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PipelineEventRow{}
	for rows.Next() {
		var ev PipelineEventRow
		var details *string
		if err := rows.Scan(&ev.ID, &ev.Kind, &ev.DishID, &ev.Stage,
			&ev.ErrorCode, &ev.ErrorMessage, &ev.ComposeRunID, &ev.WorkerID,
			&details, &ev.CreatedAt); err != nil {
			return nil, err
		}
		if details != nil {
			ev.Details = json.RawMessage(*details)
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// ---------- Stats ----------

type StatsOverview struct {
	DishesTotal           int `json:"dishes_total"`
	DishesPending         int `json:"dishes_pending"`
	DishesProcessing      int `json:"dishes_processing"`
	DishesReadyForReview  int `json:"dishes_ready_for_review"`
	DishesPublished       int `json:"dishes_published"`
	DishesFailed7Days     int `json:"dishes_failed_7d"`
	RecipesPublished      int `json:"recipes_published"`
}

func (q *Queries) StatsOverview(ctx context.Context) (*StatsOverview, error) {
	const sql = `
SELECT
  COUNT(*) FILTER (WHERE status <> 'archived') AS dishes_total,
  COUNT(*) FILTER (WHERE status = 'pending')          AS dishes_pending,
  COUNT(*) FILTER (WHERE status = 'processing')       AS dishes_processing,
  COUNT(*) FILTER (WHERE status = 'ready_for_review') AS dishes_ready_for_review,
  COUNT(*) FILTER (WHERE status = 'published')        AS dishes_published,
  COUNT(*) FILTER (WHERE status = 'failed' AND last_error_at >= NOW() - INTERVAL '7 days')
                                                       AS dishes_failed_7d
FROM dishes`

	var s StatsOverview
	if err := q.db.Pool.QueryRow(ctx, sql).Scan(
		&s.DishesTotal, &s.DishesPending, &s.DishesProcessing,
		&s.DishesReadyForReview, &s.DishesPublished, &s.DishesFailed7Days,
	); err != nil {
		return nil, err
	}
	if err := q.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM recipes WHERE status = 'published'`).Scan(&s.RecipesPublished); err != nil {
		return nil, err
	}
	return &s, nil
}

type PipelineDayBucket struct {
	Date    string `json:"date"` // YYYY-MM-DD
	Started int    `json:"started"`
	Failed  int    `json:"failed"`
}

func (q *Queries) StatsPipeline7Days(ctx context.Context) ([]PipelineDayBucket, error) {
	const sql = `
WITH days AS (
  SELECT generate_series(
    DATE_TRUNC('day', NOW() - INTERVAL '6 days'),
    DATE_TRUNC('day', NOW()),
    INTERVAL '1 day'
  )::date AS d
)
SELECT to_char(days.d, 'YYYY-MM-DD') AS date,
  COALESCE(SUM(CASE WHEN ev.kind = 'stage_started' THEN 1 ELSE 0 END), 0) AS started,
  COALESCE(SUM(CASE WHEN ev.kind = 'stage_failed'  THEN 1 ELSE 0 END), 0) AS failed
FROM days
LEFT JOIN pipeline_events ev ON DATE_TRUNC('day', ev.created_at)::date = days.d
GROUP BY days.d
ORDER BY days.d`

	rows, err := q.db.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PipelineDayBucket{}
	for rows.Next() {
		var b PipelineDayBucket
		if err := rows.Scan(&b.Date, &b.Started, &b.Failed); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

type CostStats struct {
	MonthToDateUSD float64 `json:"month_to_date_usd"`
	LastMonthUSD   float64 `json:"last_month_usd"`
	RecipesCounted int     `json:"recipes_counted"`
}

func (q *Queries) StatsCost(ctx context.Context) (*CostStats, error) {
	const sql = `
SELECT
  COALESCE(SUM(composer_cost_usd) FILTER (
    WHERE updated_at >= DATE_TRUNC('month', NOW())
  ), 0)::float8 AS month_to_date,
  COALESCE(SUM(composer_cost_usd) FILTER (
    WHERE updated_at >= DATE_TRUNC('month', NOW() - INTERVAL '1 month')
      AND updated_at <  DATE_TRUNC('month', NOW())
  ), 0)::float8 AS last_month,
  COUNT(*) FILTER (WHERE composer_cost_usd IS NOT NULL) AS counted
FROM recipes`

	var s CostStats
	if err := q.db.Pool.QueryRow(ctx, sql).Scan(
		&s.MonthToDateUSD, &s.LastMonthUSD, &s.RecipesCounted,
	); err != nil {
		return nil, err
	}
	return &s, nil
}
