# Data Flow Audit Report: Nomenclature, Recipes, and Stock

## Проблема 1: Остатки показывают 0.00г вместо 500кг

### Анализ:
В `StockService.GetStockItems()` остатки суммируются правильно, но есть проблема с единицами измерения:
- `current_stock` сохраняется в `BaseUnit` (строка 164: `current_stock: currentStock`)
- `currentStock` = `batch.RemainingQuantity`, который уже в `BaseUnit`
- **ПРОБЛЕМА**: Если товар хранится в `kg` (500кг), а `BaseUnit` = `g`, то при отображении нужно конвертировать

### Решение:
1. Проверить, что `StockBatch.Quantity` и `RemainingQuantity` всегда в `BaseUnit` номенклатуры
2. При отображении на дашборде конвертировать из `BaseUnit` в удобную единицу (кг для больших значений)
3. Добавить логирование для отладки

## Проблема 2: Новая пицца не появляется в списке рецептов

### Анализ:
1. `GetRecipes()` возвращает все рецепты без фильтрации по `MenuItemID`
2. `UnifiedCreateMenuItem` создает Recipe с `MenuItemID = &nomenclatureData.ID`
3. **ПРОБЛЕМА**: После создания рецепт должен быть виден в списке, но возможно:
   - Кэш не обновляется
   - Фильтрация на фронтенде неправильная
   - `is_active` или `deleted_at` установлены неправильно

### Решение:
1. Проверить, что `CreateRecipe` устанавливает `is_active = true` по умолчанию
2. Убедиться, что `GetRecipes` не фильтрует по `MenuItemID` (это правильно)
3. Добавить логирование создания рецепта

## Проблема 3: Фильтрация полуфабрикатов

### Анализ:
В `SemiFinishedProductsView.svelte` фильтрация правильная:
```javascript
recipes = allRecipes.filter(r => r.is_semi_finished === true);
```

**ПРОБЛЕМА**: Возможно:
- Поле `is_semi_finished` не сохраняется правильно при создании
- В БД значение NULL вместо true/false
- Регистр поля: `is_semi_finished` vs `IsSemiFinished`

### Решение:
1. Проверить, что `UnifiedCreateMenuItem` правильно устанавливает `IsSemiFinished`
2. Убедиться, что GORM правильно маппит поле
3. Добавить валидацию на фронтенде

