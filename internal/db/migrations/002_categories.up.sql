-- Categories: linh hoạt như tag, mỗi category có "tags" (alias/keywords riêng).
-- Một dish có thể thuộc nhiều category (M-N).
-- show_on_home + sort_order điều khiển bố cục trang chủ.

CREATE TABLE IF NOT EXISTS categories (
    id           BIGSERIAL PRIMARY KEY,
    slug         VARCHAR(100) UNIQUE NOT NULL,
    name_vi      VARCHAR(200) NOT NULL,
    name_en      VARCHAR(200),
    description  TEXT,
    tags         TEXT[] NOT NULL DEFAULT '{}',
    icon         VARCHAR(100),                 -- lucide icon name hoặc emoji
    sort_order   INT NOT NULL DEFAULT 100,
    visible      BOOLEAN NOT NULL DEFAULT TRUE,
    show_on_home BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS dish_categories (
    dish_id     BIGINT NOT NULL REFERENCES dishes(id) ON DELETE CASCADE,
    category_id BIGINT NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    PRIMARY KEY (dish_id, category_id)
);

CREATE INDEX IF NOT EXISTS idx_categories_sort ON categories(sort_order, slug);
CREATE INDEX IF NOT EXISTS idx_categories_home ON categories(show_on_home, sort_order) WHERE show_on_home = TRUE;
CREATE INDEX IF NOT EXISTS idx_dish_categories_category ON dish_categories(category_id);
