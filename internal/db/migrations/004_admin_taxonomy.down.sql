DROP INDEX IF EXISTS idx_recipes_status;

ALTER TABLE recipes
    DROP COLUMN IF EXISTS original_recipe,
    DROP COLUMN IF EXISTS rejection_reason,
    DROP COLUMN IF EXISTS status;

DROP TABLE IF EXISTS dish_tags;
DROP TABLE IF EXISTS tags;
