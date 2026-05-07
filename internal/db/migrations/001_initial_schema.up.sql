-- Danh sách món gốc (seed)
CREATE TABLE IF NOT EXISTS dishes (
    id           BIGSERIAL PRIMARY KEY,
    slug         VARCHAR(200) UNIQUE NOT NULL,
    name_vi      VARCHAR(200) NOT NULL,
    name_en      VARCHAR(200),
    category     VARCHAR(50),
    region       VARCHAR(50),
    keywords     TEXT[],
    status       VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Video nguồn (provenance)
CREATE TABLE IF NOT EXISTS videos (
    id           BIGSERIAL PRIMARY KEY,
    dish_id      BIGINT NOT NULL REFERENCES dishes(id) ON DELETE CASCADE,
    platform     VARCHAR(20) NOT NULL,
    external_id  VARCHAR(100) NOT NULL,
    url          TEXT NOT NULL,
    title        TEXT,
    channel      VARCHAR(200),
    duration_s   INT,
    transcript   TEXT,
    fetched_at   TIMESTAMPTZ,
    UNIQUE (platform, external_id)
);

-- Công thức tổng hợp
CREATE TABLE IF NOT EXISTS recipes (
    id              BIGSERIAL PRIMARY KEY,
    dish_id         BIGINT NOT NULL REFERENCES dishes(id) ON DELETE CASCADE UNIQUE,
    title           VARCHAR(300) NOT NULL,
    description     TEXT,
    prep_time_min   INT,
    cook_time_min   INT,
    total_time_min  INT,
    servings        INT,
    difficulty      VARCHAR(20),
    calories        INT,
    intro_md        TEXT,
    tips_md         TEXT,
    variations_md   TEXT,
    source_video_ids BIGINT[],
    seo_title       VARCHAR(60),
    seo_description VARCHAR(160),
    hero_image_url  TEXT,
    published_at    TIMESTAMPTZ,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Nguyên liệu (tách bảng để search & filter)
CREATE TABLE IF NOT EXISTS recipe_ingredients (
    id          BIGSERIAL PRIMARY KEY,
    recipe_id   BIGINT NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    position    INT NOT NULL DEFAULT 0,
    name        VARCHAR(200) NOT NULL,
    quantity    NUMERIC,
    unit        VARCHAR(50),
    note        TEXT
);

-- Các bước
CREATE TABLE IF NOT EXISTS recipe_steps (
    id          BIGSERIAL PRIMARY KEY,
    recipe_id   BIGINT NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    position    INT NOT NULL DEFAULT 0,
    instruction TEXT NOT NULL,
    image_url   TEXT,
    duration_s  INT
);

CREATE INDEX IF NOT EXISTS idx_dishes_status ON dishes(status);
CREATE INDEX IF NOT EXISTS idx_dishes_category ON dishes(category);
CREATE INDEX IF NOT EXISTS idx_recipes_published ON recipes(published_at DESC);
CREATE INDEX IF NOT EXISTS idx_videos_dish ON videos(dish_id);
CREATE INDEX IF NOT EXISTS idx_recipe_ingredients_recipe ON recipe_ingredients(recipe_id, position);
CREATE INDEX IF NOT EXISTS idx_recipe_steps_recipe ON recipe_steps(recipe_id, position);
