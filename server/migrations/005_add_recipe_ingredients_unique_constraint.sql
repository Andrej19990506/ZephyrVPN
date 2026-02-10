-- Migration: Add unique constraint to prevent duplicate ingredients in recipes
-- Date: 2025-02-05
-- Description: 
--   Prevents the same nomenclature_id or ingredient_recipe_id from being added twice to the same recipe.
--   This acts as a database-level fail-safe to prevent duplicate ingredients even if application logic fails.
--
-- Business Rule: 
--   A Recipe cannot have the same nomenclature_id twice (for raw materials).
--   A Recipe cannot have the same ingredient_recipe_id twice (for semi-finished products).

-- Step 1: Remove any existing duplicate ingredients before adding constraint
-- This ensures the migration won't fail if duplicates already exist
DO $$
DECLARE
    duplicate_count INTEGER;
BEGIN
    -- Find and log duplicates by recipe_id + nomenclature_id
    SELECT COUNT(*) INTO duplicate_count
    FROM (
        SELECT recipe_id, nomenclature_id, COUNT(*) as cnt
        FROM recipe_ingredients
        WHERE nomenclature_id IS NOT NULL
        GROUP BY recipe_id, nomenclature_id
        HAVING COUNT(*) > 1
    ) duplicates;
    
    IF duplicate_count > 0 THEN
        RAISE NOTICE 'Found % duplicate groups by recipe_id + nomenclature_id. Removing duplicates...', duplicate_count;
        
        -- Keep only the first occurrence (lowest ID) of each duplicate group
        DELETE FROM recipe_ingredients
        WHERE id IN (
            SELECT id
            FROM (
                SELECT id,
                       ROW_NUMBER() OVER (
                           PARTITION BY recipe_id, nomenclature_id 
                           ORDER BY created_at ASC, id ASC
                       ) as rn
                FROM recipe_ingredients
                WHERE nomenclature_id IS NOT NULL
            ) ranked
            WHERE rn > 1
        );
    END IF;
    
    -- Find and log duplicates by recipe_id + ingredient_recipe_id
    SELECT COUNT(*) INTO duplicate_count
    FROM (
        SELECT recipe_id, ingredient_recipe_id, COUNT(*) as cnt
        FROM recipe_ingredients
        WHERE ingredient_recipe_id IS NOT NULL
        GROUP BY recipe_id, ingredient_recipe_id
        HAVING COUNT(*) > 1
    ) duplicates;
    
    IF duplicate_count > 0 THEN
        RAISE NOTICE 'Found % duplicate groups by recipe_id + ingredient_recipe_id. Removing duplicates...', duplicate_count;
        
        -- Keep only the first occurrence (lowest ID) of each duplicate group
        DELETE FROM recipe_ingredients
        WHERE id IN (
            SELECT id
            FROM (
                SELECT id,
                       ROW_NUMBER() OVER (
                           PARTITION BY recipe_id, ingredient_recipe_id 
                           ORDER BY created_at ASC, id ASC
                       ) as rn
                FROM recipe_ingredients
                WHERE ingredient_recipe_id IS NOT NULL
            ) ranked
            WHERE rn > 1
        );
    END IF;
END $$;

-- Step 2: Create unique partial index for nomenclature_id (raw materials)
-- This ensures that within a single recipe, each nomenclature_id can only appear once
CREATE UNIQUE INDEX IF NOT EXISTS idx_recipe_ingredients_unique_nomenclature 
ON recipe_ingredients (recipe_id, nomenclature_id)
WHERE nomenclature_id IS NOT NULL;

-- Step 3: Create unique partial index for ingredient_recipe_id (semi-finished products)
-- This ensures that within a single recipe, each ingredient_recipe_id can only appear once
CREATE UNIQUE INDEX IF NOT EXISTS idx_recipe_ingredients_unique_ingredient_recipe 
ON recipe_ingredients (recipe_id, ingredient_recipe_id)
WHERE ingredient_recipe_id IS NOT NULL;

-- Step 4: Add comment to document the constraint
COMMENT ON INDEX idx_recipe_ingredients_unique_nomenclature IS 
'Prevents duplicate raw materials (nomenclature_id) within the same recipe';

COMMENT ON INDEX idx_recipe_ingredients_unique_ingredient_recipe IS 
'Prevents duplicate semi-finished products (ingredient_recipe_id) within the same recipe';

-- Verification: Log the number of ingredients after cleanup
DO $$
DECLARE
    total_ingredients INTEGER;
BEGIN
    SELECT COUNT(*) INTO total_ingredients FROM recipe_ingredients;
    RAISE NOTICE 'Migration completed. Total recipe_ingredients after cleanup: %', total_ingredients;
END $$;









