-- Tags như entity riêng (khác `categories.tags TEXT[]` là alias keywords cho category).
-- Tag = nhãn taxonomy curated, có slug + sort_order, dùng cho filter UI.
CREATE TABLE IF NOT EXISTS tags (
    id          BIGSERIAL PRIMARY KEY,
    slug        VARCHAR(100) UNIQUE NOT NULL,
    name        VARCHAR(200) NOT NULL,
    description TEXT,
    sort_order  INT NOT NULL DEFAULT 100,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_tags_sort ON tags(sort_order, name);

CREATE TABLE IF NOT EXISTS dish_tags (
    dish_id BIGINT NOT NULL REFERENCES dishes(id) ON DELETE CASCADE,
    tag_id  BIGINT NOT NULL REFERENCES tags(id)   ON DELETE CASCADE,
    PRIMARY KEY (dish_id, tag_id)
);
CREATE INDEX IF NOT EXISTS idx_dish_tags_tag ON dish_tags(tag_id);

-- Recipe lifecycle riêng so với dish:
--   ready_for_review  — crawler vừa upsert, chờ admin duyệt
--   published         — admin click Publish (set published_at)
--   rejected          — admin click Reject (kèm rejection_reason)
--   discarded         — admin "Trả lại pending" để regenerate
ALTER TABLE recipes
    ADD COLUMN IF NOT EXISTS status            VARCHAR(30) NOT NULL DEFAULT 'ready_for_review',
    ADD COLUMN IF NOT EXISTS rejection_reason  TEXT,
    -- Bản AI sinh raw (snapshot lúc upsert) — admin dùng để diff với bản đã edit.
    ADD COLUMN IF NOT EXISTS original_recipe   JSONB;

-- Migrate dishes có sẵn: nếu `published_at IS NOT NULL` thì set status='published'.
UPDATE recipes SET status = 'published' WHERE published_at IS NOT NULL AND status = 'ready_for_review';

CREATE INDEX IF NOT EXISTS idx_recipes_status ON recipes(status);

-- Đồng bộ nhãn `done` (legacy) → `published` (admin doc canonical).
UPDATE dishes SET status = 'published' WHERE status = 'done';
