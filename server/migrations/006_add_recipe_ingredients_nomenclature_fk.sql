-- Migration: Add Foreign Key Constraint for recipe_ingredients.nomenclature_id
-- Date: 2025-02-05
-- Description: 
--   Adds a FOREIGN KEY constraint to ensure referential integrity between
--   recipe_ingredients and nomenclature_items.
--   ON DELETE RESTRICT prevents deletion of nomenclature items that are referenced in recipes.
--
-- Business Rule: 
--   Every RecipeIngredient.NomenclatureID must point to an existing, active NomenclatureItem.

-- Step 1: Clean up orphaned ingredients before adding constraint
-- This ensures the migration won't fail if invalid references already exist
DO $$
DECLARE
    orphaned_count INTEGER;
BEGIN
    -- Find and count orphaned ingredients (pointing to non-existent or deleted items)
    SELECT COUNT(*) INTO orphaned_count
    FROM recipe_ingredients ri
    LEFT JOIN nomenclature_items n ON ri.nomenclature_id = n.id
    WHERE ri.nomenclature_id IS NOT NULL
      AND (n.id IS NULL 
           OR n.deleted_at IS NOT NULL 
           OR n.is_active = false);
    
    IF orphaned_count > 0 THEN
        RAISE NOTICE 'Found % orphaned recipe ingredients. Cleaning up...', orphaned_count;
        
        -- Option 1: Set nomenclature_id to NULL (soft cleanup - preserves recipe structure)
        -- This allows recipes to exist but ingredients will be marked as invalid
        UPDATE recipe_ingredients ri
        SET nomenclature_id = NULL
        FROM nomenclature_items n
        WHERE ri.nomenclature_id = n.id
          AND (n.deleted_at IS NOT NULL OR n.is_active = false);
        
        -- Option 2: Delete ingredients pointing to non-existent items (hard cleanup)
        -- Uncomment if you prefer to remove invalid ingredients completely
        -- DELETE FROM recipe_ingredients
        -- WHERE nomenclature_id IS NOT NULL
        --   AND nomenclature_id NOT IN (SELECT id FROM nomenclature_items);
        
        RAISE NOTICE 'Cleaned up orphaned ingredients. Set nomenclature_id to NULL for inactive/deleted items.';
    END IF;
END $$;

-- Step 2: Add Foreign Key Constraint
-- ON DELETE RESTRICT prevents deletion of nomenclature items that are used in recipes
DO $$ 
BEGIN
    -- Check if constraint already exists
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints 
        WHERE constraint_name = 'fk_recipe_ingredients_nomenclature'
        AND table_name = 'recipe_ingredients'
    ) THEN
        ALTER TABLE recipe_ingredients 
        ADD CONSTRAINT fk_recipe_ingredients_nomenclature 
        FOREIGN KEY (nomenclature_id) 
        REFERENCES nomenclature_items(id) 
        ON DELETE RESTRICT;
        
        RAISE NOTICE 'Foreign key constraint fk_recipe_ingredients_nomenclature added successfully';
    ELSE
        RAISE NOTICE 'Foreign key constraint fk_recipe_ingredients_nomenclature already exists';
    END IF;
EXCEPTION
    WHEN duplicate_object THEN
        RAISE NOTICE 'Foreign key constraint already exists (duplicate_object)';
    WHEN OTHERS THEN
        RAISE EXCEPTION 'Error adding foreign key constraint: %', SQLERRM;
END $$;

-- Step 3: Add comment to document the constraint
COMMENT ON CONSTRAINT fk_recipe_ingredients_nomenclature ON recipe_ingredients IS 
'Ensures referential integrity: recipe_ingredients.nomenclature_id must reference an existing nomenclature_items.id. ON DELETE RESTRICT prevents deletion of nomenclature items that are used in recipes.';

-- Step 4: Create index for better performance (if not exists)
CREATE INDEX IF NOT EXISTS idx_recipe_ingredients_nomenclature_id_fk 
ON recipe_ingredients (nomenclature_id)
WHERE nomenclature_id IS NOT NULL;

-- Verification: Log statistics
DO $$
DECLARE
    total_ingredients INTEGER;
    ingredients_with_nomenclature INTEGER;
    ingredients_with_sub_recipe INTEGER;
BEGIN
    SELECT COUNT(*) INTO total_ingredients FROM recipe_ingredients;
    SELECT COUNT(*) INTO ingredients_with_nomenclature 
    FROM recipe_ingredients WHERE nomenclature_id IS NOT NULL;
    SELECT COUNT(*) INTO ingredients_with_sub_recipe 
    FROM recipe_ingredients WHERE ingredient_recipe_id IS NOT NULL;
    
    RAISE NOTICE 'Migration completed successfully.';
    RAISE NOTICE 'Total recipe_ingredients: %', total_ingredients;
    RAISE NOTICE 'Ingredients with nomenclature_id: %', ingredients_with_nomenclature;
    RAISE NOTICE 'Ingredients with ingredient_recipe_id: %', ingredients_with_sub_recipe;
END $$;









