# Technical Audit: Recipe Management & Production Logic (Tech Cards)

**Date:** 2025-01-27  
**Status:** Partial Implementation

---

## Executive Summary

The Recipe Management system has **basic database schema** and **sale depletion logic** implemented, but is **missing critical features** for production-ready use:
- ❌ Unit conversion in production
- ❌ Prime cost calculation
- ❌ Nested recipes support
- ❌ UI for recipe management
- ❌ Production commit function

---

## 1. Data Schema ✅ **DONE**

### Database Tables

| Table Name | Status | Description |
|------------|--------|-------------|
| `recipes` | ✅ **EXISTS** | Main recipe/tech card table |
| `recipe_ingredients` | ✅ **EXISTS** | Bill of Materials (BOM) linking recipes to ingredients |

### Schema Details

**`recipes` table:**
- `id` (UUID, PK)
- `name` (VARCHAR(255))
- `description` (TEXT)
- `menu_item_id` (UUID, FK) - Link to menu item
- `portion_size` (DECIMAL(10,2)) - Default: 1
- `unit` (VARCHAR(20)) - Default: 'pcs'
- `is_active` (BOOLEAN)
- `created_at`, `updated_at`, `deleted_at`

**`recipe_ingredients` table:**
- `id` (UUID, PK)
- `recipe_id` (UUID, FK → `recipes.id`)
- `nomenclature_id` (UUID, FK → `nomenclature_items.id`)
- `quantity` (DECIMAL(10,4)) - Quantity per 1 portion
- `unit` (VARCHAR(20)) - Unit of measurement
- `is_optional` (BOOLEAN) - Optional ingredient flag
- `created_at`, `updated_at`

### Model Files
- **Location:** `internal/models/stock.go`
- **Models:** `Recipe`, `RecipeIngredient`
- **Migration:** ✅ AutoMigrate in `internal/models/menu_db.go`

---

## 2. Linking Finished Goods to Ingredients ✅ **DONE**

**Status:** ✅ **IMPLEMENTED**

- Recipes can link to multiple ingredients via `recipe_ingredients` table
- Each ingredient references `nomenclature_items` via `nomenclature_id`
- Relationship: `Recipe` → `[]RecipeIngredient` → `NomenclatureItem`

**Code Reference:**
```go
// internal/models/stock.go:54-68
type Recipe struct {
    // ...
    Ingredients []RecipeIngredient `json:"ingredients" gorm:"foreignKey:RecipeID"`
}
```

---

## 3. Nested Recipes (Semi-finished Products) ❌ **NOT IMPLEMENTED**

**Status:** ❌ **MISSING**

**Issue:** The current schema does NOT support nested recipes.

**Missing Features:**
- No `parent_recipe_id` field in `Recipe` model
- Cannot link a recipe to another recipe as an ingredient
- Example use case: "Pizza Margherita" → uses "Pizza Dough" (semi-finished) → which has its own recipe

**Required Changes:**
1. Add `parent_recipe_id` (UUID, nullable) to `Recipe` model
2. Add recursive relationship: `Recipe` → `[]Recipe` (child recipes)
3. Update `ProcessSaleDepletion` to handle recursive ingredient resolution

---

## 4. Unit Conversion in Production ❌ **NOT IMPLEMENTED**

**Status:** ❌ **CRITICAL BUG**

**Current Implementation:**
```go
// internal/services/stock_service.go:273-338
func (s *StockService) ProcessSaleDepletion(...) {
    for _, ingredient := range recipe.Ingredients {
        requiredQuantity := ingredient.Quantity * quantity
        // ❌ NO UNIT CONVERSION HERE!
        // Directly uses ingredient.Unit without checking batch.Unit
    }
}
```

**Problem:**
- Recipe ingredient has `unit` (e.g., "g" for 150g cheese)
- Stock batch has `unit` (e.g., "kg" for cheese stored in kg)
- **No conversion logic** between these units
- Will cause incorrect deductions (e.g., deducting 150kg instead of 0.15kg)

**Required Fix:**
1. Load `NomenclatureItem` to get `BaseUnit`, `InboundUnit`, `ConversionFactor`
2. Convert `ingredient.Quantity` from `ingredient.Unit` to `batch.Unit`
3. Use conversion logic similar to `stock_batch_import.go:ValidateInvoiceItem`

**Example Fix:**
```go
// Convert ingredient quantity to batch unit
if ingredient.Unit != batch.Unit {
    conversionFactor := getConversionFactor(ingredient.Unit, batch.Unit, ingredient.Nomenclature)
    requiredQuantity = requiredQuantity * conversionFactor
}
```

---

## 5. Production Logic ⚠️ **PARTIALLY IMPLEMENTED**

### 5.1 Commit Production Function ❌ **NOT IMPLEMENTED**

**Status:** ❌ **MISSING**

**Current State:**
- No dedicated `CommitProduction()` function
- No endpoint for manual production commits
- No logic to add finished products to stock after production

**Missing Features:**
1. Function to create finished product batch in `stock_batches`
2. Deduct ingredients using recipe
3. Create `StockMovement` records for both ingredients (negative) and finished product (positive)
4. Link to production order/document

**Required Implementation:**
```go
func (s *StockService) CommitProduction(
    recipeID string,
    quantity float64,
    branchID string,
    performedBy string,
    productionOrderID string,
) error {
    // 1. Deduct ingredients (reuse ProcessSaleDepletion logic)
    // 2. Create finished product batch
    // 3. Create movements
    // 4. Return error if insufficient ingredients
}
```

### 5.2 Auto-Deduction on Sales ✅ **IMPLEMENTED**

**Status:** ✅ **DONE**

**Implementation:**
- Function: `ProcessSaleDepletion()` in `stock_service.go:273`
- Endpoint: `POST /api/v1/inventory/stock/process-sale`
- Controller: `StockController.ProcessSaleDepletion()`

**Features:**
- ✅ FIFO deduction by expiry date
- ✅ Creates `StockMovement` records
- ✅ Updates `StockBatch.RemainingQuantity`
- ✅ Logs warnings for insufficient stock
- ❌ **BUT:** Missing unit conversion (see Section 4)

**Code Reference:**
- `internal/services/stock_service.go:273-338`
- `internal/api/stock_controller.go:87-120`

---

## 6. Cost Calculation ❌ **NOT IMPLEMENTED**

**Status:** ❌ **MISSING**

**Missing Function:**
- No `CalculatePrimeCost()` or `GetRecipeCost()` function
- No calculation of finished product cost based on ingredient prices

**Required Implementation:**
```go
func (s *StockService) CalculatePrimeCost(recipeID string) (float64, error) {
    // 1. Load recipe with ingredients
    // 2. For each ingredient:
    //    - Get current price from NomenclatureItem.LastPrice
    //    - Convert quantity to price unit
    //    - Calculate: ingredient_cost = quantity * price
    // 3. Sum all ingredient costs
    // 4. Return total prime cost per portion
}
```

**Use Cases:**
- Display cost in recipe UI
- Calculate profit margin
- Price optimization
- Cost tracking over time

---

## 7. UI/Frontend ❌ **NOT IMPLEMENTED**

**Status:** ❌ **MISSING**

**Missing Components:**
- No `RecipeManagementModule.svelte` or `TechCardsModule.svelte`
- No UI for creating/editing recipes
- No UI for adding/removing ingredients
- No UI for setting quantities and units

**Required UI Features:**
1. **Recipe List View:**
   - Table of all recipes
   - Filter by active/inactive
   - Search by name
   - Link to menu items

2. **Recipe Editor:**
   - Form: Name, Description, Portion Size, Unit
   - Ingredient list with add/remove
   - Ingredient selector (dropdown from nomenclature)
   - Quantity and unit inputs
   - Optional ingredient checkbox
   - Prime cost display (calculated)

3. **Ingredient Management:**
   - Add ingredient button
   - Remove ingredient button
   - Reorder ingredients (drag & drop?)

**Frontend Location:**
- Should be in: `ERPNative/Back-Office-Desktop/backoffice-desktop/frontend/src/components/`
- Suggested name: `RecipeManagementModule.svelte` or `TechCardsModule.svelte`

---

## Summary Table

| Feature | Status | Priority | Notes |
|---------|--------|----------|-------|
| **Database Schema** | ✅ DONE | - | Tables exist, migrations work |
| **Link Finished Goods → Ingredients** | ✅ DONE | - | Via `recipe_ingredients` table |
| **Nested Recipes** | ❌ MISSING | HIGH | Required for semi-finished products |
| **Unit Conversion** | ❌ MISSING | **CRITICAL** | **BUG:** Will cause incorrect deductions |
| **Commit Production** | ❌ MISSING | HIGH | No function to add finished products |
| **Auto-Deduction on Sales** | ✅ DONE | - | Works but has unit conversion bug |
| **Prime Cost Calculation** | ❌ MISSING | MEDIUM | Needed for pricing decisions |
| **UI for Recipe Management** | ❌ MISSING | HIGH | No frontend interface |

---

## Recommended Implementation Order

1. **🔴 CRITICAL:** Fix unit conversion in `ProcessSaleDepletion()` (Section 4)
2. **🟡 HIGH:** Implement `CommitProduction()` function (Section 5.1)
3. **🟡 HIGH:** Add nested recipes support (Section 3)
4. **🟡 HIGH:** Create UI for recipe management (Section 7)
5. **🟢 MEDIUM:** Implement prime cost calculation (Section 6)

---

## Code References

### Backend Models
- `internal/models/stock.go:53-107` - Recipe and RecipeIngredient models

### Backend Services
- `internal/services/stock_service.go:273-338` - ProcessSaleDepletion (has unit conversion bug)

### Backend API
- `internal/api/stock_controller.go:87-120` - ProcessSaleDepletion endpoint
- `main.go:342` - Route registration

### Database Migrations
- `internal/models/menu_db.go:150-162` - AutoMigrate for recipes

---

## Additional Notes

### Legacy Pizza Recipes
- There is a separate `pizza_recipes` table (legacy)
- Uses JSON fields for ingredients (not normalized)
- Located in `internal/models/menu_db.go:11-21`
- **Recommendation:** Migrate to new `recipes` table structure

### Related Features
- `NomenclatureItem` has `ProductionUnit` field (for production recipes)
- `StockBatch` supports `source = 'production'` (for finished products)
- `StockMovement` supports `movement_type = 'production'`

---

**Report Generated:** 2025-01-27  
**Next Review:** After implementing critical fixes

