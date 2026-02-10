# Stock Valuation Fix - Under-valuation Issue

## Problem
Stock values are showing 1,000 times lower than they should be.
- Example: 10kg of Mayo at 1,234 RUB/kg shows as **12.34 RUB** instead of **12,340.00 RUB**

## Root Cause Analysis

The formula should be:
```
TotalValue = (RemainingQuantityInGrams * CostPerKg) / 1000
```

For 10kg (10000g) at 1,234 RUB/kg:
- Expected: (10000 * 1234) / 1000 = 12,340,000 / 1000 = **12,340 RUB**
- Actual: **12.34 RUB** (1,000 times too low)

This suggests:
1. Either `CostPerUnit` is stored as price per gram (1.234) instead of price per kg (1234)
2. Or there's an extra division by 1000 happening somewhere

## Verification & Fixes Applied

### 1. Backend Formula ✅ VERIFIED CORRECT

**File**: `stock_service.go:53-66` - `calculateBatchValue` function
```go
func calculateBatchValue(remainingQty decimal.Decimal, costPerUnit decimal.Decimal, conversionFactor decimal.Decimal) decimal.Decimal {
    total := remainingQty.Mul(costPerUnit)  // Multiply first
    if !conversionFactor.Equal(decimal.NewFromInt(1)) {
        return total.Div(conversionFactor)  // Divide by 1000 for g->kg
    }
    return total
}
```

**Formula**: `(RemainingQuantity * CostPerUnit) / ConversionFactor`
- ✅ Correct for: (10000g * 1234₽/kg) / 1000 = 12,340₽

### 2. Storage Logic ✅ VERIFIED CORRECT

**File**: `stock_batch_import.go:397-418`
- ✅ `CostPerUnit` is saved as `item.PricePerKg` (price per 1kg/1L)
- ✅ `RemainingQuantity` is saved in BaseUnit (grams)
- ✅ Price normalization: if `pack_size` is provided, price is divided by pack_size

### 3. Added Comprehensive Logging ✅

**Added logging in**:
- `stock_batch_import.go:390-398` - When saving batches
- `stock_service.go:168-185` - When calculating cost_value

**Logging includes**:
- RemainingQuantity in BaseUnit
- CostPerUnit (should be price per 1kg/1L)
- ConversionFactor
- Expected cost calculation
- Warning if CostPerUnit < 10 (might be stored as price per gram)

### 4. Frontend Display ✅ VERIFIED CORRECT

**File**: `InventoryStockModule.svelte`
- ✅ Uses `item.cost_value` directly from backend (no division)
- ✅ `totalCostValue` is sum of all `item.cost_value` (no division)

## Diagnostic Steps

### Check Database Values

Run this SQL query to check if `cost_per_unit` is stored incorrectly:
```sql
SELECT 
    sb.id,
    n.name,
    sb.remaining_quantity,
    sb.cost_per_unit,
    n.base_unit,
    n.inbound_unit,
    (sb.remaining_quantity * sb.cost_per_unit / 1000) as calculated_value
FROM stock_batches sb
JOIN nomenclature_items n ON sb.nomenclature_id = n.id
WHERE n.base_unit = 'g' AND n.inbound_unit = 'kg'
ORDER BY calculated_value DESC
LIMIT 10;
```

**Expected**: `cost_per_unit` should be > 10 (e.g., 1234.00 for 1234 RUB/kg)
**If wrong**: `cost_per_unit` might be < 10 (e.g., 1.234 for price per gram)

### Check Logs

After processing an invoice, check logs for:
```
💾 Сохранение StockBatch для товара 'Майонез' (ID: ...)
   Quantity (BaseUnit): 10000.00 g
   CostPerUnit (InboundUnit): 1234.00₽/kg (цена за 1кг/1л, НЕ за грамм!)
   Ожидаемая стоимость при чтении: (10000.00 * 1234.00) / 1000 = 12340.00₽
```

And when reading stock:
```
🔍 GetStockItems: расчет стоимости для Майонез (ID: ...)
   RemainingQuantity: 10000.00 g
   CostPerUnit: 1234.00₽/kg (должна быть цена за 1кг/1л, НЕ за грамм!)
   ConversionFactor: 1000
   Формула: (10000.00 * 1234.00) / 1000
   Результат: 12340.00₽
```

## Potential Issues & Solutions

### Issue 1: CostPerUnit Stored as Price Per Gram

**Symptom**: `cost_per_unit` in database is < 10 for items that should be > 100

**Solution**: 
1. Check if old data has incorrect values
2. Create migration to fix existing data:
```sql
UPDATE stock_batches sb
JOIN nomenclature_items n ON sb.nomenclature_id = n.id
SET sb.cost_per_unit = sb.cost_per_unit * 1000
WHERE n.base_unit = 'g' AND n.inbound_unit = 'kg' AND sb.cost_per_unit < 10;
```

### Issue 2: Frontend Sending Wrong Price

**Check**: When user enters price, verify it's sent as price per kg, not per gram

**Solution**: Ensure frontend always sends price per Major Unit (kg/L)

### Issue 3: Double Division

**Check**: Verify no extra division by 1000 in:
- API response mapping
- Frontend calculation
- Database triggers

## Testing

### Test Case 1: Normal Invoice
- Input: 10kg Mayo at 1,234 RUB/kg
- Expected: cost_value = 12,340 RUB
- Check logs for correct values

### Test Case 2: With Pack Size
- Input: 1 bucket (10kg) at 12,340 RUB
- Expected: CostPerUnit = 1,234 RUB/kg, cost_value = 12,340 RUB
- Check logs for price normalization

### Test Case 3: Multiple Batches
- Input: 2 batches of 5kg each at 1,234 RUB/kg
- Expected: Total cost_value = 12,340 RUB (sum of both batches)
- Check that batches are summed correctly

## Next Steps

1. ✅ Added comprehensive logging
2. ⏳ Check actual database values using SQL query above
3. ⏳ Review logs after processing an invoice
4. ⏳ If CostPerUnit is stored incorrectly, create migration to fix
5. ⏳ Verify frontend sends correct price format

## Summary

The formula and logic appear correct. The issue is likely:
1. **Existing data** has `cost_per_unit` stored as price per gram (need migration)
2. **Frontend** is sending price in wrong format (need verification)
3. **Somewhere** there's an extra division by 1000 (need to find and remove)

The added logging will help identify which of these is the actual problem.

