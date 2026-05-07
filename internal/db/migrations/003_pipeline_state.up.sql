-- Concurrency-control + observability cho crawler service.
-- Crawler không truy cập Postgres trực tiếp — mọi state đi qua API service.

-- 1) Distributed lock + tracking lỗi cho từng dish.
ALTER TABLE dishes
    ADD COLUMN IF NOT EXISTS locked_by          VARCHAR(100),
    ADD COLUMN IF NOT EXISTS locked_until       TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_error_code    VARCHAR(100),
    ADD COLUMN IF NOT EXISTS last_error_message TEXT,
    ADD COLUMN IF NOT EXISTS last_error_stage   INT,
    ADD COLUMN IF NOT EXISTS last_error_at      TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_dishes_locked_until ON dishes(locked_until)
    WHERE locked_until IS NOT NULL;

-- 2) Recipe schema bổ sung để khớp với ComposedRecipe của crawler.
ALTER TABLE recipes
    ADD COLUMN IF NOT EXISTS seo_keywords        TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS composer_model      VARCHAR(100),
    ADD COLUMN IF NOT EXISTS composer_tokens_in  INT,
    ADD COLUMN IF NOT EXISTS composer_tokens_out INT,
    ADD COLUMN IF NOT EXISTS composer_cost_usd   NUMERIC(10,6),
    ADD COLUMN IF NOT EXISTS compose_run_id      VARCHAR(100);

ALTER TABLE recipe_steps
    ADD COLUMN IF NOT EXISTS tip TEXT;

-- 3) Idempotency cho POST /api/internal/recipes — ngăn duplicate khi crawler retry.
-- Cùng 1 Idempotency-Key trong 24h sẽ trả lại response đã ghi.
CREATE TABLE IF NOT EXISTS idempotency_keys (
    key         VARCHAR(200) PRIMARY KEY,
    status_code INT NOT NULL,
    response    JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '24 hours')
);
CREATE INDEX IF NOT EXISTS idx_idempotency_expires ON idempotency_keys(expires_at);

-- 4) Pipeline event log — admin nhìn vào đây để debug khi job fail.
CREATE TABLE IF NOT EXISTS pipeline_events (
    id             BIGSERIAL PRIMARY KEY,
    kind           VARCHAR(50) NOT NULL,
    dish_id        BIGINT REFERENCES dishes(id) ON DELETE SET NULL,
    stage          INT,
    error_code     VARCHAR(100),
    error_message  TEXT,
    compose_run_id VARCHAR(100),
    worker_id      VARCHAR(100),
    details        JSONB,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_pipeline_events_dish ON pipeline_events(dish_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_pipeline_events_kind ON pipeline_events(kind, created_at DESC);
