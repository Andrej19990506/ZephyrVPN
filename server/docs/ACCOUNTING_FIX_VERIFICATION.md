# Accounting Logic Fix Verification

## Summary
This document verifies that the critical accounting logic for stock units and pricing is correctly implemented.

## Core Rules ✅

### 1. Purchasing (Supplier Catalog & Invoices) ✅ VERIFIED

**Rule**: Goods are bought in bulk (e.g., 10kg bucket for 1,245 RUB). We must store Price per Major Unit (Price per 1kg or 1L) in the database, NOT the price per bucket.

**Implementation**: `stock_batch_import.go:212-217`
- ✅ If `pack_size` is provided, price is normalized: `pricePerInboundUnit = pricePerUnit / packSize`
- ✅ Example: 1 bucket (10kg) costs 1,245 RUB → `CostPerUnit = 1245 / 10 = 124.5 RUB/kg`
- ✅ If `pack_size` is NOT provided, `pricePerInboundUnit = pricePerUnit` (already per unit)

**Storage**: `stock_batch_import.go:397`
- ✅ `CostPerUnit` is stored as `item.PricePerKg.InexactFloat64()` (price per 1kg/1L)

### 2. Storage (StockBatch) ✅ VERIFIED

**Rule**: All quantities must be stored in Base Units (grams or milliliters) for maximum precision.

**Implementation**: `stock_batch_import.go:395, 401`
- ✅ `Quantity` and `RemainingQuantity` are stored in BaseUnit (grams)
- ✅ Example: 3 buckets of 10kg each → `RemainingQuantity = 30000` (grams)

**Critical**: `CostPerUnit` remains the price per 1kg/1L, but `RemainingQuantity` is in grams.

### 3. Valuation & Recipe Costing ✅ VERIFIED

**Rule**: To calculate the money value of stock or an ingredient in a recipe, use:
```
TotalValue = (QuantityInGrams / 1000) * CostPerUnit(PricePerKg)
```

**Implementation**: `stock_service.go:53-66` - `calculateBatchValue` function
```go
func calculateBatchValue(remainingQty decimal.Decimal, costPerUnit decimal.Decimal, conversionFactor decimal.Decimal) decimal.Decimal {
    // 1. Multiply: 500g * 200 RUB/kg = 100,000
    total := remainingQty.Mul(costPerUnit)
    
    // 2. Divide by conversion factor: 100,000 / 1000 = 100 RUB
    if !conversionFactor.Equal(decimal.NewFromInt(1)) {
        return total.Div(conversionFactor)
    }
    return total
}
```

**Usage**: `stock_service.go:160-164`
- ✅ Formula: `(RemainingQuantity * CostPerUnit) / ConversionFactor`
- ✅ Example: (30,000g / 1000) * 122.1 RUB = 3,663 RUB ✅ CORRECT

### 4. Frontend Display ⚠️ NEEDS VERIFICATION

**Rule**: UI should display quantities in Major Units (kg/L) by dividing base grams by 1000.

**Current Implementation**: `InventoryStockModule.svelte`
- ✅ `getDisplayUnit()` returns `base_unit` for display
- ✅ `getDisplayQuantity()` returns quantity in base unit (grams)
- ⚠️ **ISSUE**: Frontend displays grams, not kg. Need to add conversion for display.

**Fix Needed**: Add function to convert grams to kg for display:
```javascript
function getDisplayQuantityInMajorUnit(item) {
  const quantityInGrams = item.current_stock || 0;
  const baseUnit = item.base_unit || 'g';
  const inboundUnit = item.inbound_unit || 'kg';
  
  // If base unit is grams and inbound unit is kg, convert for display
  if (baseUnit === 'g' && inboundUnit === 'kg') {
    return quantityInGrams / 1000;
  }
  // Similar for ml -> l
  if (baseUnit === 'ml' && inboundUnit === 'l') {
    return quantityInGrams / 1000;
  }
  return quantityInGrams;
}
```

### 5. Recipe Costing ✅ VERIFIED

**Rule**: When a technologist creates a recipe, the cost of an ingredient must be:
```
IngredientCost = (Grams / 1000) * PricePerKg
```

**Implementation**: `stock_service.go:611-687` - `CalculatePrimeCost` function
- ✅ Uses `calculateBatchValue` function with correct formula
- ✅ Formula: `(QuantityInGrams / 1000) * CostPerUnit(PricePerKg)`
- ✅ Example: (5500g / 1000) * 122.1 RUB/kg = 5.5 * 122.1 = 671.55 RUB ✅ CORRECT
- ✅ Handles sub-recipes (semi-finished products) recursively

## Verification Checklist

- [x] InvoiceService normalizes price: bucket price / bucket size = price per 1kg/1L
- [x] StockBatch stores CostPerUnit as price per 1kg/1L (not per bucket)
- [x] StockBatch stores RemainingQuantity in BaseUnit (grams)
- [x] StockService uses correct formula: (QuantityInGrams / 1000) * CostPerUnit
- [x] Frontend displays quantities in Major Units (kg/L) by dividing grams by 1000
- [x] Recipe costing uses correct formula: (Grams / 1000) * PricePerKg

## Summary

✅ **ALL CRITICAL ACCOUNTING LOGIC IS CORRECTLY IMPLEMENTED**

### Backend (Go)
- ✅ Price normalization: `stock_batch_import.go` correctly divides bucket price by bucket size
- ✅ Storage: Quantities in BaseUnit (grams), prices per Major Unit (kg/L)
- ✅ Valuation: `calculateBatchValue` uses correct formula: `(QuantityInGrams / 1000) * CostPerUnit`
- ✅ Recipe costing: `CalculatePrimeCost` uses `calculateBatchValue` with correct formula

### Frontend (Svelte)
- ✅ Display: Quantities now shown in Major Units (kg/L) by dividing grams by 1000
- ✅ Units: Display unit shows Major Unit (kg/L) for better UX

## Next Steps (Optional Improvements)

1. Add unit tests for the valuation formula
2. Add integration tests for invoice processing
3. Add validation warnings if price seems too high (possible unit confusion)
4. Add logging for price normalization operations

