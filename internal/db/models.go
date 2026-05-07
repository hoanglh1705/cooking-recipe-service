package db

import "time"

type Dish struct {
	ID        int64     `json:"id"`
	Slug      string    `json:"slug"`
	NameVi    string    `json:"name_vi"`
	NameEn    *string   `json:"name_en,omitempty"`
	Category  *string   `json:"category,omitempty"`
	Region    *string   `json:"region,omitempty"`
	Keywords  []string  `json:"keywords,omitempty"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`

	// Lock state — đặt bởi crawler qua /api/internal/dishes/:id/lock.
	LockedBy    *string    `json:"locked_by,omitempty"`
	LockedUntil *time.Time `json:"locked_until,omitempty"`

	// Last failure — admin xem để retry.
	LastErrorCode    *string    `json:"last_error_code,omitempty"`
	LastErrorMessage *string    `json:"last_error_message,omitempty"`
	LastErrorStage   *int       `json:"last_error_stage,omitempty"`
	LastErrorAt      *time.Time `json:"last_error_at,omitempty"`
}

// Dish status state machine
//
//	pending           — chưa xử lý
//	processing        — crawler đã lock, đang chạy pipeline
//	ready_for_review  — recipe đã được upsert, chờ admin duyệt
//	published         — admin đã publish (set recipes.published_at)
//	failed            — pipeline báo lỗi (xem last_error_*)
//	archived          — admin soft-delete; không hiển thị public, không trigger lại
const (
	DishStatusPending        = "pending"
	DishStatusProcessing     = "processing"
	DishStatusReadyForReview = "ready_for_review"
	DishStatusPublished      = "published"
	DishStatusFailed         = "failed"
	DishStatusArchived       = "archived"

	// Deprecated: dùng DishStatusPublished. Giữ alias cho dữ liệu cũ.
	DishStatusDone = DishStatusPublished
)

// Recipe status — lifecycle riêng cho admin review:
//
//	ready_for_review  — crawler vừa upsert
//	published         — admin click Publish
//	rejected          — admin click Reject (kèm rejection_reason)
//	discarded         — admin "Trả lại pending" để regenerate
const (
	RecipeStatusReadyForReview = "ready_for_review"
	RecipeStatusPublished      = "published"
	RecipeStatusRejected       = "rejected"
	RecipeStatusDiscarded      = "discarded"
)

// Tag — taxonomy curated cho admin. Khác `categories.tags TEXT[]`: tag là entity
// có id + slug + sort_order; admin có thể merge 2 tag, dish có nhiều tag.
type Tag struct {
	ID          int64     `json:"id"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
	SortOrder   int       `json:"sort_order"`
	DishCount   int       `json:"dish_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// DishPublic là phiên bản dish phơi ra public API: ẩn `status` (chi tiết
// pipeline nội bộ) và đính kèm cờ `has_recipe` cho FE biết món đã có công
// thức publish hay đang "coming soon".
type DishPublic struct {
	Slug       string     `json:"slug"`
	NameVi     string     `json:"name_vi"`
	NameEn     *string    `json:"name_en,omitempty"`
	Region     *string    `json:"region,omitempty"`
	Keywords   []string   `json:"keywords"`
	HasRecipe  bool       `json:"has_recipe"`
	Categories []Category `json:"categories,omitempty"`
}

// DishFilter cho ListDishesPublic. Tag được match case-insensitive với cả
// `dishes.keywords` và `categories.tags` của các category mà dish thuộc về.
type DishFilter struct {
	CategorySlug string
	Tag          string
	Region       string
	HasRecipe    *bool // nil = không filter
	Limit        int
	Offset       int
}

type Video struct {
	ID         int64      `json:"id"`
	DishID     int64      `json:"dish_id"`
	Platform   string     `json:"platform"`
	ExternalID string     `json:"external_id"`
	URL        string     `json:"url"`
	Title      *string    `json:"title,omitempty"`
	Channel    *string    `json:"channel,omitempty"`
	DurationS  *int       `json:"duration_s,omitempty"`
	Transcript *string    `json:"transcript,omitempty"`
	FetchedAt  *time.Time `json:"fetched_at,omitempty"`
}

type Recipe struct {
	ID             int64      `json:"id"`
	DishID         int64      `json:"-"`
	Slug           string     `json:"slug"`
	Title          string     `json:"title"`
	Description    string     `json:"description"`
	Category       *string    `json:"category,omitempty"`
	Keywords       []string   `json:"keywords,omitempty"`
	PrepTimeMin    *int       `json:"prep_time_min,omitempty"`
	CookTimeMin    *int       `json:"cook_time_min,omitempty"`
	TotalTimeMin   *int       `json:"total_time_min,omitempty"`
	Servings       *int       `json:"servings,omitempty"`
	Difficulty     *string    `json:"difficulty,omitempty"`
	Calories       *int       `json:"calories,omitempty"`
	IntroMD        *string    `json:"intro_md,omitempty"`
	TipsMD         *string    `json:"tips_md,omitempty"`
	VariationsMD   *string    `json:"variations_md,omitempty"`
	HeroImageURL   *string    `json:"hero_image_url,omitempty"`
	SEOTitle       *string    `json:"seo_title,omitempty"`
	SEODescription *string    `json:"seo_description,omitempty"`
	PublishedAt    *time.Time `json:"published_at,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
	Ingredients    []Ingredient `json:"ingredients"`
	Steps          []Step       `json:"steps"`
	Videos         []Video      `json:"videos,omitempty"`
	Categories     []Category   `json:"categories,omitempty"`

	// Admin lifecycle fields — public reads vẫn hiển thị nhưng FE thường ignore.
	Status           string  `json:"status,omitempty"`
	RejectionReason  *string `json:"rejection_reason,omitempty"`
}

// RecipeAdmin = Recipe + bản AI gốc + source_videos đầy đủ (kèm transcript)
// dùng cho admin review page. Public API KHÔNG trả shape này.
type RecipeAdmin struct {
	Recipe          Recipe          `json:"recipe"`
	OriginalRecipe  *ComposedRecipe `json:"original_recipe,omitempty"`
	SourceVideos    []Video         `json:"source_videos"`
}

type RecipeSummary struct {
	Slug         string   `json:"slug"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	HeroImageURL *string  `json:"hero_image_url,omitempty"`
	TotalTimeMin *int     `json:"total_time_min,omitempty"`
	Servings     *int     `json:"servings,omitempty"`
	Tags         []string `json:"tags,omitempty"`
}

// Category nhóm các dish theo nhiều chiều (bữa sáng, kho, nướng, miền Bắc…).
// Tags là alias/keywords riêng của category (dùng cho SEO + alternate matching).
type Category struct {
	ID          int64     `json:"id"`
	Slug        string    `json:"slug"`
	NameVi      string    `json:"name_vi"`
	NameEn      *string   `json:"name_en,omitempty"`
	Description *string   `json:"description,omitempty"`
	Tags        []string  `json:"tags"`
	Icon        *string   `json:"icon,omitempty"`
	SortOrder   int       `json:"sort_order"`
	Visible     bool      `json:"visible"`
	ShowOnHome  bool      `json:"show_on_home"`
	RecipeCount int       `json:"recipe_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// HomeSection là một block trên trang chủ: 1 category + vài recipe nổi bật.
type HomeSection struct {
	Category Category        `json:"category"`
	Recipes  []RecipeSummary `json:"recipes"`
}

type Ingredient struct {
	Position int      `json:"position"`
	Name     string   `json:"name"`
	Quantity *float64 `json:"quantity,omitempty"`
	Unit     *string  `json:"unit,omitempty"`
	Note     *string  `json:"note,omitempty"`
}

type Step struct {
	Position    int     `json:"position"`
	Instruction string  `json:"instruction"`
	ImageURL    *string `json:"image_url,omitempty"`
	DurationS   *int    `json:"duration_s,omitempty"`
	Tip         *string `json:"tip,omitempty"`
}

// ComposedRecipe — payload mà LLM/crawler trả về.
// Field naming KHỚP với pydantic ComposedRecipe trong crawler service
// (xem docs/crawler_architech.md §5.4).
type ComposedRecipe struct {
	Title              string       `json:"title"`
	Description        string       `json:"description"`
	IntroMD            string       `json:"intro_md"`
	PrepTimeMin        int          `json:"prep_time_min"`
	CookTimeMin        int          `json:"cook_time_min"`
	TotalTimeMin       int          `json:"total_time_min"`
	Servings           int          `json:"servings"`
	Difficulty         string       `json:"difficulty"`
	CaloriesPerServing *int         `json:"calories_per_serving,omitempty"`
	Ingredients        []Ingredient `json:"ingredients"`
	Steps              []Step       `json:"steps"`
	Tips               []string     `json:"tips"`
	Variations         []string     `json:"variations"`
	FAQs               []FAQ        `json:"faqs"`
	SEOTitle           string       `json:"seo_title"`
	SEOKeywords        []string     `json:"seo_keywords"`
}

type FAQ struct {
	Q string `json:"q"`
	A string `json:"a"`
}

// ComposerMeta — metadata về lần gọi LLM (gắn vào upsert payload).
// Crawler service set; internal Go pipeline có thể bỏ qua (nil).
type ComposerMeta struct {
	Model        string  `json:"model"`
	TokensIn     int     `json:"tokens_in,omitempty"`
	TokensOut    int     `json:"tokens_out,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
	ComposeRunID string  `json:"compose_run_id"`
}

// SourceVideo — video gốc gửi kèm trong upsert recipe.
type SourceVideo struct {
	Platform         string  `json:"platform"`           // youtube | tiktok
	ExternalID       string  `json:"external_id"`
	URL              string  `json:"url"`
	Title            *string `json:"title,omitempty"`
	Channel          *string `json:"channel,omitempty"`
	DurationS        *int    `json:"duration_s,omitempty"`
	Transcript       *string `json:"transcript,omitempty"`
	TranscriptSource *string `json:"transcript_source,omitempty"` // caption | whisper
}
