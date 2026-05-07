// Seed script — bơm dữ liệu khởi đầu cho `cooking-recipe-service`:
//   - ~20 món Việt phổ biến (bảng dishes)
//   - 11 category dùng cho trang chủ (bảng categories)
//   - mapping dish ↔ category (bảng dish_categories)
//
// Idempotent — chạy lại không nhân bản:
//   - dishes: ON CONFLICT (slug) DO NOTHING
//   - categories: ON CONFLICT (slug) DO UPDATE  (cập nhật metadata khi sửa seed)
//   - dish_categories: ON CONFLICT DO NOTHING
//
// Usage:
//   make run-seed   # hoặc: go run ./cmd/seed
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/cooking-recipe/cooking-recipe-service/internal/config"
	"github.com/cooking-recipe/cooking-recipe-service/internal/db"
)

type seedDish struct {
	Slug     string
	NameVi   string
	NameEn   string
	Category string // mon-chinh | khai-vi | trang-mieng | mon-an-vat
	Region   string // mien-bac | mien-trung | mien-nam | "" nếu phổ thông
	Keywords []string
}

// 20 món được chọn theo độ phổ biến + đa dạng vùng miền + đa dạng category.
// Keywords là các long-tail SEO chính: "{tên}", "cách nấu {tên}", "công thức {tên}"...
var dishes = []seedDish{
	// --- Miền Bắc ---
	{
		Slug: "pho-bo-ha-noi", NameVi: "Phở bò Hà Nội", NameEn: "Hanoi Beef Pho",
		Category: "mon-chinh", Region: "mien-bac",
		Keywords: []string{"phở bò", "cách nấu phở bò", "công thức phở bò Hà Nội", "phở bò truyền thống"},
	},
	{
		Slug: "pho-ga-ha-noi", NameVi: "Phở gà Hà Nội", NameEn: "Hanoi Chicken Pho",
		Category: "mon-chinh", Region: "mien-bac",
		Keywords: []string{"phở gà", "cách nấu phở gà", "phở gà Hà Nội"},
	},
	{
		Slug: "bun-cha-ha-noi", NameVi: "Bún chả Hà Nội", NameEn: "Hanoi Grilled Pork with Vermicelli",
		Category: "mon-chinh", Region: "mien-bac",
		Keywords: []string{"bún chả", "cách làm bún chả", "công thức bún chả Hà Nội"},
	},
	{
		Slug: "bun-rieu-cua", NameVi: "Bún riêu cua", NameEn: "Crab Tomato Vermicelli Soup",
		Category: "mon-chinh", Region: "mien-bac",
		Keywords: []string{"bún riêu", "cách nấu bún riêu cua", "công thức bún riêu"},
	},
	{
		Slug: "bun-dau-mam-tom", NameVi: "Bún đậu mắm tôm", NameEn: "Vermicelli with Tofu and Shrimp Paste",
		Category: "mon-chinh", Region: "mien-bac",
		Keywords: []string{"bún đậu", "mắm tôm", "bún đậu mắm tôm Hà Nội"},
	},
	{
		Slug: "cha-ca-la-vong", NameVi: "Chả cá Lã Vọng", NameEn: "La Vong Turmeric Fish",
		Category: "mon-chinh", Region: "mien-bac",
		Keywords: []string{"chả cá", "chả cá Lã Vọng", "cách làm chả cá Hà Nội"},
	},

	// --- Miền Trung ---
	{
		Slug: "bun-bo-hue", NameVi: "Bún bò Huế", NameEn: "Hue Beef Vermicelli Soup",
		Category: "mon-chinh", Region: "mien-trung",
		Keywords: []string{"bún bò Huế", "cách nấu bún bò Huế", "công thức bún bò Huế"},
	},
	{
		Slug: "mi-quang", NameVi: "Mì Quảng", NameEn: "Quang Style Noodles",
		Category: "mon-chinh", Region: "mien-trung",
		Keywords: []string{"mì Quảng", "cách nấu mì Quảng", "mì Quảng tôm thịt"},
	},
	{
		Slug: "cao-lau-hoi-an", NameVi: "Cao lầu Hội An", NameEn: "Hoi An Cao Lau",
		Category: "mon-chinh", Region: "mien-trung",
		Keywords: []string{"cao lầu", "cao lầu Hội An", "cách nấu cao lầu"},
	},
	{
		Slug: "com-hen-hue", NameVi: "Cơm hến Huế", NameEn: "Hue Baby Clam Rice",
		Category: "mon-chinh", Region: "mien-trung",
		Keywords: []string{"cơm hến", "cơm hến Huế", "cách làm cơm hến"},
	},

	// --- Miền Nam ---
	{
		Slug: "com-tam-suon-bi-cha", NameVi: "Cơm tấm sườn bì chả", NameEn: "Broken Rice with Pork Chop",
		Category: "mon-chinh", Region: "mien-nam",
		Keywords: []string{"cơm tấm", "cơm tấm sườn", "cách làm cơm tấm Sài Gòn"},
	},
	{
		Slug: "hu-tieu-nam-vang", NameVi: "Hủ tiếu Nam Vang", NameEn: "Phnom Penh Style Noodle Soup",
		Category: "mon-chinh", Region: "mien-nam",
		Keywords: []string{"hủ tiếu", "hủ tiếu Nam Vang", "cách nấu hủ tiếu"},
	},
	{
		Slug: "banh-xeo-mien-tay", NameVi: "Bánh xèo miền Tây", NameEn: "Mekong Crispy Crepe",
		Category: "mon-chinh", Region: "mien-nam",
		Keywords: []string{"bánh xèo", "bánh xèo miền Tây", "cách làm bánh xèo giòn"},
	},
	{
		Slug: "canh-chua-ca-loc", NameVi: "Canh chua cá lóc", NameEn: "Sour Snakehead Fish Soup",
		Category: "mon-chinh", Region: "mien-nam",
		Keywords: []string{"canh chua", "canh chua cá lóc", "cách nấu canh chua"},
	},
	{
		Slug: "ca-kho-to", NameVi: "Cá kho tộ", NameEn: "Caramelized Clay Pot Fish",
		Category: "mon-chinh", Region: "mien-nam",
		Keywords: []string{"cá kho tộ", "cách kho cá", "cá kho tộ miền Nam"},
	},
	{
		Slug: "thit-kho-hot-vit", NameVi: "Thịt kho hột vịt", NameEn: "Caramelized Pork with Eggs",
		Category: "mon-chinh", Region: "mien-nam",
		Keywords: []string{"thịt kho hột vịt", "thịt kho tàu", "cách kho thịt ngày Tết"},
	},

	// --- Phổ thông (không gắn vùng cụ thể) ---
	{
		Slug: "ga-kho-gung", NameVi: "Gà kho gừng", NameEn: "Ginger Braised Chicken",
		Category: "mon-chinh",
		Keywords: []string{"gà kho gừng", "cách kho gà", "gà kho gừng đậm đà"},
	},
	{
		Slug: "bo-kho", NameVi: "Bò kho", NameEn: "Vietnamese Beef Stew",
		Category: "mon-chinh",
		Keywords: []string{"bò kho", "cách nấu bò kho", "bò kho bánh mì"},
	},
	{
		Slug: "goi-cuon-tom-thit", NameVi: "Gỏi cuốn tôm thịt", NameEn: "Fresh Spring Rolls with Shrimp",
		Category: "khai-vi", Region: "mien-nam",
		Keywords: []string{"gỏi cuốn", "gỏi cuốn tôm thịt", "cách làm gỏi cuốn"},
	},
	{
		Slug: "cha-gio", NameVi: "Chả giò", NameEn: "Crispy Spring Rolls",
		Category: "khai-vi",
		Keywords: []string{"chả giò", "nem rán", "cách làm chả giò giòn"},
	},
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", "err", err)
		os.Exit(1)
	}

	ctx := context.Background()
	database, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("open db", "err", err)
		os.Exit(1)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		logger.Error("migrate", "err", err)
		os.Exit(1)
	}

	dInserted, dSkipped := seedDishes(ctx, database, logger)
	cInserted, cUpdated := seedCategoriesTable(ctx, database, logger)
	links := seedDishCategoryLinks(ctx, database, logger)

	fmt.Printf("\n--- Seed xong ---\n")
	fmt.Printf("Dishes     : %d mới, %d đã tồn tại  (tổng seed: %d)\n", dInserted, dSkipped, len(dishes))
	fmt.Printf("Categories : %d mới, %d cập nhật    (tổng seed: %d)\n", cInserted, cUpdated, len(seedCategories))
	fmt.Printf("Dish↔Category links: %d entries (mới hoặc đã tồn tại)\n", links)
}

func seedDishes(ctx context.Context, database *db.DB, logger *slog.Logger) (inserted, skipped int) {
	const sql = `
INSERT INTO dishes (slug, name_vi, name_en, category, region, keywords, status)
VALUES ($1, $2, NULLIF($3,''), NULLIF($4,''), NULLIF($5,''), $6, 'pending')
ON CONFLICT (slug) DO NOTHING`

	for _, d := range dishes {
		tag, err := database.Pool.Exec(ctx, sql,
			d.Slug, d.NameVi, d.NameEn, d.Category, d.Region, d.Keywords)
		if err != nil {
			logger.Error("insert dish", "slug", d.Slug, "err", err)
			os.Exit(1)
		}
		if tag.RowsAffected() == 1 {
			inserted++
			logger.Info("dish inserted", "slug", d.Slug, "name", d.NameVi)
		} else {
			skipped++
		}
	}
	return
}

func seedCategoriesTable(ctx context.Context, database *db.DB, logger *slog.Logger) (inserted, updated int) {
	// xmax = 0 sau RETURNING là cách đơn giản phân biệt INSERT vs UPDATE
	// trong upsert của Postgres.
	const sql = `
INSERT INTO categories (slug, name_vi, name_en, description, tags, icon, sort_order, visible, show_on_home)
VALUES ($1, $2, NULLIF($3,''), NULLIF($4,''), $5, NULLIF($6,''), $7, TRUE, $8)
ON CONFLICT (slug) DO UPDATE SET
  name_vi=EXCLUDED.name_vi,
  name_en=EXCLUDED.name_en,
  description=EXCLUDED.description,
  tags=EXCLUDED.tags,
  icon=EXCLUDED.icon,
  sort_order=EXCLUDED.sort_order,
  show_on_home=EXCLUDED.show_on_home,
  updated_at=NOW()
RETURNING (xmax = 0) AS inserted`

	for _, c := range seedCategories {
		var wasInserted bool
		err := database.Pool.QueryRow(ctx, sql,
			c.Slug, c.NameVi, c.NameEn, c.Description, c.Tags, c.Icon, c.SortOrder, c.ShowOnHome,
		).Scan(&wasInserted)
		if err != nil {
			logger.Error("upsert category", "slug", c.Slug, "err", err)
			os.Exit(1)
		}
		if wasInserted {
			inserted++
			logger.Info("category inserted", "slug", c.Slug, "name", c.NameVi)
		} else {
			updated++
		}
	}
	return
}

func seedDishCategoryLinks(ctx context.Context, database *db.DB, logger *slog.Logger) int {
	const sql = `
INSERT INTO dish_categories (dish_id, category_id)
SELECT d.id, c.id FROM dishes d, categories c
WHERE d.slug = $1 AND c.slug = $2
ON CONFLICT DO NOTHING`

	count := 0
	for dishSlug, catSlugs := range dishCategories {
		for _, catSlug := range catSlugs {
			if _, err := database.Pool.Exec(ctx, sql, dishSlug, catSlug); err != nil {
				logger.Error("link dish↔category", "dish", dishSlug, "category", catSlug, "err", err)
				os.Exit(1)
			}
			count++
		}
	}
	return count
}
