package main

type seedCategory struct {
	Slug        string
	NameVi      string
	NameEn      string
	Description string
	Tags        []string
	Icon        string
	SortOrder   int
	ShowOnHome  bool
}

// 11 category đầu — phủ 3 trục: bữa, kiểu món, vùng miền.
// sort_order tăng dần = xuất hiện từ trên xuống ở trang chủ.
var seedCategories = []seedCategory{
	{
		Slug: "bua-sang", NameVi: "Bữa sáng", NameEn: "Breakfast",
		Description: "Món gọn, nhanh, đủ no cho buổi sáng — phở, bún, hủ tiếu…",
		Tags:        []string{"sáng", "ăn sáng", "breakfast", "buổi sáng"},
		Icon:        "Sunrise", SortOrder: 10, ShowOnHome: true,
	},
	{
		Slug: "com", NameVi: "Cơm", NameEn: "Rice Dishes",
		Description: "Các món cơm: cơm tấm, cơm hến, cơm dĩa, cơm bụi.",
		Tags:        []string{"cơm", "rice", "cơm dĩa"},
		Icon:        "Bowl", SortOrder: 20, ShowOnHome: true,
	},
	{
		Slug: "pho-bun", NameVi: "Phở & Bún", NameEn: "Noodle Soups",
		Description: "Sợi nóng nước dùng — phở bò, bún bò Huế, mì Quảng, hủ tiếu…",
		Tags:        []string{"phở", "bún", "mì", "hủ tiếu", "noodle"},
		Icon:        "Soup", SortOrder: 30, ShowOnHome: true,
	},
	{
		Slug: "canh", NameVi: "Canh & Súp", NameEn: "Soups",
		Description: "Canh bữa cơm gia đình: canh chua, canh rau, canh cá.",
		Tags:        []string{"canh", "súp", "soup"},
		Icon:        "CookingPot", SortOrder: 40, ShowOnHome: true,
	},
	{
		Slug: "kho", NameVi: "Món kho", NameEn: "Braised Dishes",
		Description: "Kho mặn đậm vị — cá kho, thịt kho, gà kho, bò kho.",
		Tags:        []string{"kho", "rim", "braised"},
		Icon:        "Flame", SortOrder: 50, ShowOnHome: true,
	},
	{
		Slug: "nuong-chien", NameVi: "Nướng & Chiên", NameEn: "Grilled & Fried",
		Description: "Món nướng/chiên giòn rụm — chả cá, bánh xèo, chả giò, sườn nướng.",
		Tags:        []string{"nướng", "chiên", "rán", "grilled", "fried"},
		Icon:        "Flame", SortOrder: 60, ShowOnHome: true,
	},
	{
		Slug: "cuon-goi", NameVi: "Cuốn & Gỏi", NameEn: "Rolls & Salads",
		Description: "Mát, nhẹ, ăn kèm — gỏi cuốn, bò bía, gỏi gà.",
		Tags:        []string{"cuốn", "gỏi", "salad", "fresh roll"},
		Icon:        "Leaf", SortOrder: 70, ShowOnHome: true,
	},
	{
		Slug: "khai-vi", NameVi: "Khai vị", NameEn: "Appetizers",
		Description: "Mở đầu bữa ăn: chả giò, gỏi cuốn, súp.",
		Tags:        []string{"khai vị", "appetizer", "starter"},
		Icon:        "Utensils", SortOrder: 80, ShowOnHome: true,
	},
	{
		Slug: "mon-bac", NameVi: "Món Bắc", NameEn: "Northern Dishes",
		Description: "Phở Hà Nội, bún chả, bún đậu — vị thanh, ít cay.",
		Tags:        []string{"miền Bắc", "Hà Nội", "northern"},
		Icon:        "MapPin", SortOrder: 90, ShowOnHome: true,
	},
	{
		Slug: "mon-trung", NameVi: "Món Trung", NameEn: "Central Dishes",
		Description: "Bún bò Huế, mì Quảng, cao lầu — đậm đà, cay hơn.",
		Tags:        []string{"miền Trung", "Huế", "central"},
		Icon:        "MapPin", SortOrder: 100, ShowOnHome: true,
	},
	{
		Slug: "mon-nam", NameVi: "Món Nam", NameEn: "Southern Dishes",
		Description: "Hủ tiếu, bánh xèo, cá kho tộ — ngọt nhẹ, dùng nhiều dừa/đường.",
		Tags:        []string{"miền Nam", "Sài Gòn", "southern"},
		Icon:        "MapPin", SortOrder: 110, ShowOnHome: true,
	},
}

// dishCategories: dish slug → category slugs.
// Một dish được gắn vào nhiều category để xuất hiện ở nhiều section trang chủ.
var dishCategories = map[string][]string{
	// Bắc
	"pho-bo-ha-noi":   {"bua-sang", "pho-bun", "mon-bac"},
	"pho-ga-ha-noi":   {"bua-sang", "pho-bun", "mon-bac"},
	"bun-cha-ha-noi":  {"pho-bun", "nuong-chien", "mon-bac"},
	"bun-rieu-cua":    {"bua-sang", "pho-bun", "canh", "mon-bac"},
	"bun-dau-mam-tom": {"pho-bun", "mon-bac"},
	"cha-ca-la-vong":  {"nuong-chien", "mon-bac"},

	// Trung
	"bun-bo-hue":     {"bua-sang", "pho-bun", "mon-trung"},
	"mi-quang":       {"pho-bun", "mon-trung"},
	"cao-lau-hoi-an": {"pho-bun", "mon-trung"},
	"com-hen-hue":    {"com", "mon-trung"},

	// Nam
	"com-tam-suon-bi-cha": {"com", "nuong-chien", "mon-nam"},
	"hu-tieu-nam-vang":    {"bua-sang", "pho-bun", "mon-nam"},
	"banh-xeo-mien-tay":   {"nuong-chien", "mon-nam"},
	"canh-chua-ca-loc":    {"canh", "mon-nam"},
	"ca-kho-to":           {"kho", "com", "mon-nam"},
	"thit-kho-hot-vit":    {"kho", "com", "mon-nam"},

	// Phổ thông
	"ga-kho-gung":       {"kho", "com"},
	"bo-kho":            {"kho", "com"},
	"goi-cuon-tom-thit": {"cuon-goi", "khai-vi", "mon-nam"},
	"cha-gio":           {"khai-vi", "nuong-chien"},
}
