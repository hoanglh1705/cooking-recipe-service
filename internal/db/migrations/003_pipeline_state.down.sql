DROP TABLE IF EXISTS pipeline_events;
DROP TABLE IF EXISTS idempotency_keys;

ALTER TABLE recipe_steps DROP COLUMN IF EXISTS tip;

ALTER TABLE recipes
    DROP COLUMN IF EXISTS seo_keywords,
    DROP COLUMN IF EXISTS composer_model,
    DROP COLUMN IF EXISTS composer_tokens_in,
    DROP COLUMN IF EXISTS composer_tokens_out,
    DROP COLUMN IF EXISTS composer_cost_usd,
    DROP COLUMN IF EXISTS compose_run_id;

DROP INDEX IF EXISTS idx_dishes_locked_until;

ALTER TABLE dishes
    DROP COLUMN IF EXISTS locked_by,
    DROP COLUMN IF EXISTS locked_until,
    DROP COLUMN IF EXISTS last_error_code,
    DROP COLUMN IF EXISTS last_error_message,
    DROP COLUMN IF EXISTS last_error_stage,
    DROP COLUMN IF EXISTS last_error_at;
