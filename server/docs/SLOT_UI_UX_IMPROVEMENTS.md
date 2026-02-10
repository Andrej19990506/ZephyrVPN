# UI/UX Анализ и Улучшения: Управление Слотами в Dashboard

## 📋 Оглавление

1. [Анализ проблемы визуализации](#анализ-проблемы-визуализации)
2. [Предложения по улучшению визуализации](#предложения-по-улучшению-визуализации)
3. [Функционал управления слотами](#функционал-управления-слотами)
4. [Интеграция с бэкендом](#интеграция-с-бэкендом)
5. [Дизайн-макеты и примеры кода](#дизайн-макеты-и-примеры-кода)

---

## 🔍 Анализ проблемы визуализации

### Текущая реализация

**Компонент**: `KitchenCapacityTimeline.svelte`

**Проблема**: При 98% заполнения слота прогресс-бар визуально выглядит пустым или почти пустым.

### Причины проблемы

#### 1. **Масштабирование шкалы**

**Текущий код**:
```svelte
<div
  class="absolute bottom-0 left-0 right-0 rounded-xl transition-all duration-700 ease-out bg-gradient-to-t {getGradientClass(directPercentage)}"
  style="height: {directPercentage}%; min-height: {directPercentage > 0 ? '2px' : '0'};"
>
```

**Проблемы**:
- Высота контейнера: `min-h-[8.571rem]` (≈137px)
- При 98% заполнения высота заливки: `137px × 0.98 = 134px`
- Разница всего **3px** между полным и почти полным слотом
- Визуально неразличимо для пользователя

#### 2. **Контраст и видимость**

**Текущие градиенты**:
```javascript
function getGradientClass(percentage) {
  if (percentage >= 81) {
    return 'from-red-600 via-rose-500 to-red-700'; // Критическая загрузка
  } else if (percentage >= 41) {
    return 'from-orange-500 via-orange-600 to-orange-700'; // Средняя загрузка
  } else if (percentage > 0) {
    return 'from-emerald-500 via-teal-500 to-emerald-600'; // Низкая загрузка
  }
}
```

**Проблемы**:
- При 98% используется красный градиент, но он может быть не очень заметен на светлом фоне
- Нет визуального индикатора "критичности" (пульсация, анимация, предупреждение)
- Процент текста показывается только если `directPercentage > 20%`, но при 98% может быть не виден из-за цвета текста

#### 3. **Размер индикатора**

**Текущая реализация**:
- Процент показывается только если `directPercentage > 20%`
- При 98% процент может быть не виден из-за белого текста на красном фоне
- Нет дополнительных визуальных индикаторов (иконки, бейджи, анимации)

---

## 💡 Предложения по улучшению визуализации

### Вариант 1: Улучшенный прогресс-бар с критическими индикаторами (РЕКОМЕНДУЕТСЯ)

**Идея**: Добавить визуальные индикаторы критичности при высокой загрузке (90%+)

**Изменения**:

1. **Увеличенная высота контейнера**:
   ```svelte
   <div class="flex-1 relative mb-3 min-h-[10rem] flex items-end">
   ```

2. **Добавление границы критичности**:
   ```svelte
   <!-- Критическая граница (90%) -->
   {#if directPercentage >= 90}
     <div class="absolute top-0 left-0 right-0 h-[10%] border-t-2 border-red-500 border-dashed opacity-50"></div>
   {/if}
   ```

3. **Улучшенная визуализация при 90%+**:
   ```svelte
   <!-- Liquid Fill с улучшенной видимостью -->
   <div
     class="absolute bottom-0 left-0 right-0 rounded-xl transition-all duration-700 ease-out bg-gradient-to-t {getGradientClass(directPercentage)}
            {directPercentage >= 90 ? 'ring-2 ring-red-500 ring-offset-1 animate-pulse' : ''}"
     style="height: {directPercentage}%; 
            min-height: {directPercentage > 0 ? '4px' : '0'};
            {directPercentage >= 90 ? 'box-shadow: 0 0 20px rgba(239, 68, 68, 0.5);' : ''}"
   >
     <!-- Анимация пульсации для критических слотов -->
     {#if directPercentage >= 90}
       <div class="absolute inset-0 bg-white/30 animate-pulse rounded-xl"></div>
     {/if}
   </div>
   ```

4. **Всегда видимый процент**:
   ```svelte
   <!-- Percentage Text Overlay (всегда показываем при загрузке > 0) -->
   {#if directPercentage > 0}
     <div class="absolute inset-0 flex items-center justify-center pointer-events-none">
       <span class="text-white font-bold text-base drop-shadow-lg
                    {directPercentage >= 90 ? 'text-red-100' : ''}">
         {directPercentage.toFixed(0)}%
       </span>
     </div>
   {/if}
   ```

5. **Добавление бейджа "КРИТИЧНО"**:
   ```svelte
   {#if directPercentage >= 90}
     <div class="absolute top-2 right-2 px-2 py-1 bg-red-600 text-white text-xs font-bold rounded-full animate-pulse shadow-lg">
       КРИТИЧНО
     </div>
   {/if}
   ```

**Преимущества**:
- ✅ Очевидно видно критическое состояние
- ✅ Анимация привлекает внимание
- ✅ Процент всегда виден
- ✅ Минимальные изменения в коде

---

### Вариант 2: Изменение цвета фона карточки

**Идея**: Изменить цвет фона всей карточки при высокой загрузке

**Изменения**:

```svelte
<div
  class="flex-shrink-0 flex-none w-[10rem] h-[14.286rem] rounded-xl border shadow-sm p-4 cursor-pointer transition-all duration-300
         {directPercentage >= 90 ? 'bg-red-50 border-red-300' : 
          directPercentage >= 70 ? 'bg-orange-50 border-orange-300' : 
          'bg-white border-slate-100'}"
>
```

**Преимущества**:
- ✅ Вся карточка выделяется
- ✅ Легко заметить критичные слоты
- ✅ Минимальные изменения

**Недостатки**:
- ⚠️ Может быть слишком ярко для минималистичного дизайна

---

### Вариант 3: Комбинированный подход (ЛУЧШИЙ)

**Идея**: Объединить все улучшения

**Изменения**:

1. **Улучшенный прогресс-бар** (из Варианта 1)
2. **Изменение цвета фона** (из Варианта 2, но более мягкий)
3. **Добавление иконки предупреждения**:
   ```svelte
   {#if directPercentage >= 90}
     <div class="absolute top-2 left-2">
       <AlertTriangle class="w-5 h-5 text-red-600 animate-pulse" />
     </div>
   {/if}
   ```

4. **Улучшенный статус-бейдж**:
   ```svelte
   <div class="flex items-center justify-between">
     <div class="flex items-center gap-1.5 px-2 py-1 rounded-md {status.bg}">
       <Pizza size={12} class="{status.color}" />
       <span class="text-[0.714rem] font-semibold {status.color}">
         {ordersCount}
       </span>
     </div>
     <div class="text-[0.714rem] font-semibold {status.color} flex items-center gap-1">
       {#if directPercentage >= 90}
         <AlertTriangle size={12} class="animate-pulse" />
       {/if}
       {status.text}
     </div>
   </div>
   ```

**Преимущества**:
- ✅ Максимальная видимость критических слотов
- ✅ Сохраняет минималистичный стиль
- ✅ Множественные индикаторы для надежности

---

## 🎛️ Функционал управления слотами

### Требования

1. **Отключение слота** (блокировка приема заказов)
2. **Изменение лимита слота** (индивидуальная настройка)
3. **Real-time обновления** через WebSocket

### Дизайн элементов управления

#### 1. Кнопка "Отключить слот" / "Включить слот"

**Расположение**: В правом верхнем углу карточки слота

**Дизайн**:
```svelte
<!-- Кнопка управления слотом -->
<div class="absolute top-2 right-2 flex items-center gap-1">
  {#if slot.disabled}
    <button
      on:click={() => toggleSlot(slot.slot_id, false)}
      class="p-1.5 bg-green-500 hover:bg-green-600 text-white rounded-lg transition-colors shadow-sm"
      title="Включить слот"
    >
      <Play class="w-4 h-4" />
    </button>
  {:else}
    <button
      on:click={() => toggleSlot(slot.slot_id, true)}
      class="p-1.5 bg-red-500 hover:bg-red-600 text-white rounded-lg transition-colors shadow-sm"
      title="Отключить слот"
    >
      <Pause class="w-4 h-4" />
    </button>
  {/if}
</div>
```

**Визуальное состояние отключенного слота**:
```svelte
<div
  class="flex-shrink-0 flex-none w-[10rem] h-[14.286rem] rounded-xl border shadow-sm p-4 cursor-pointer transition-all duration-300
         {slot.disabled ? 'bg-gray-100 border-gray-300 opacity-60' : 'bg-white border-slate-100'}"
>
  {#if slot.disabled}
    <div class="absolute inset-0 flex items-center justify-center bg-gray-200/50 rounded-xl">
      <div class="text-center">
        <XCircle class="w-8 h-8 text-gray-500 mx-auto mb-2" />
        <span class="text-xs font-semibold text-gray-600">СЛОТ ОТКЛЮЧЕН</span>
      </div>
    </div>
  {/if}
</div>
```

#### 2. Кнопка "Изменить лимит"

**Расположение**: В правом верхнем углу карточки слота (рядом с кнопкой отключения)

**Дизайн**:
```svelte
<!-- Кнопка редактирования лимита -->
<button
  on:click={() => openEditCapacityModal(slot)}
  class="p-1.5 bg-blue-500 hover:bg-blue-600 text-white rounded-lg transition-colors shadow-sm"
  title="Изменить лимит слота"
>
  <Edit class="w-4 h-4" />
</button>
```

**Модальное окно редактирования**:
```svelte
<!-- Modal для редактирования лимита -->
{#if editingSlotCapacity}
  <div class="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4" on:click|self={() => editingSlotCapacity = null}>
    <div class="bg-white rounded-xl shadow-2xl p-6 max-w-md w-full" on:click|stopPropagation>
      <div class="flex items-center justify-between mb-4">
        <h3 class="text-lg font-semibold text-slate-900">Изменить лимит слота</h3>
        <button
          on:click={() => editingSlotCapacity = null}
          class="text-slate-400 hover:text-slate-900 transition-colors"
        >
          <X class="w-5 h-5" />
        </button>
      </div>
      
      <div class="space-y-4">
        <div>
          <label class="block text-sm font-medium text-slate-700 mb-2">
            Время слота: {editingSlotCapacity.time}
          </label>
          <label class="block text-sm font-medium text-slate-700 mb-2">
            Текущий лимит: {formatMoney(editingSlotCapacity.max_capacity)}₽
          </label>
        </div>
        
        <div>
          <label class="block text-sm font-medium text-slate-700 mb-2">
            Новый лимит (₽)
          </label>
          <input
            type="number"
            bind:value={newCapacity}
            min="1000"
            max="1000000"
            step="1000"
            class="w-full px-4 py-2 border border-slate-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
            placeholder="Введите новый лимит"
          />
          <p class="text-xs text-slate-500 mt-1">
            Текущая загрузка: {formatMoney(editingSlotCapacity.current_load)}₽
          </p>
        </div>
        
        <div class="flex gap-3">
          <button
            on:click={saveSlotCapacity}
            class="flex-1 bg-blue-600 hover:bg-blue-700 text-white py-2 px-4 rounded-lg font-medium transition-colors"
            disabled={!newCapacity || newCapacity <= 0}
          >
            Сохранить
          </button>
          <button
            on:click={() => editingSlotCapacity = null}
            class="flex-1 bg-gray-200 hover:bg-gray-300 text-gray-700 py-2 px-4 rounded-lg font-medium transition-colors"
          >
            Отменить
          </button>
        </div>
      </div>
    </div>
  </div>
{/if}
```

#### 3. Компактное расположение (для мобильных устройств)

**Альтернативный вариант**: Выпадающее меню с действиями

```svelte
<!-- Выпадающее меню действий -->
<div class="absolute top-2 right-2">
  <button
    on:click={() => slotActionsMenu = slot.slot_id === slotActionsMenu ? null : slot.slot_id}
    class="p-1.5 bg-white hover:bg-gray-100 text-gray-600 rounded-lg transition-colors shadow-sm border border-gray-200"
    title="Действия со слотом"
  >
    <MoreVertical class="w-4 h-4" />
  </button>
  
  {#if slotActionsMenu === slot.slot_id}
    <div class="absolute top-full right-0 mt-1 bg-white rounded-lg shadow-lg border border-gray-200 py-1 z-10 min-w-[10rem]">
      <button
        on:click={() => { toggleSlot(slot.slot_id, !slot.disabled); slotActionsMenu = null; }}
        class="w-full text-left px-4 py-2 hover:bg-gray-50 flex items-center gap-2"
      >
        {#if slot.disabled}
          <Play class="w-4 h-4 text-green-600" />
          <span class="text-sm text-slate-700">Включить слот</span>
        {:else}
          <Pause class="w-4 h-4 text-red-600" />
          <span class="text-sm text-slate-700">Отключить слот</span>
        {/if}
      </button>
      <button
        on:click={() => { openEditCapacityModal(slot); slotActionsMenu = null; }}
        class="w-full text-left px-4 py-2 hover:bg-gray-50 flex items-center gap-2"
      >
        <Edit class="w-4 h-4 text-blue-600" />
        <span class="text-sm text-slate-700">Изменить лимит</span>
      </button>
    </div>
  {/if}
</div>
```

---

## 🔌 Интеграция с бэкендом

### API Endpoints

#### 1. Отключение/включение слота

**Endpoint**: `PUT /api/v1/erp/slots/{slot_id}/toggle`

**Request**:
```json
{
  "disabled": true  // или false для включения
}
```

**Response**:
```json
{
  "success": true,
  "slot_id": "slot:1770390000",
  "disabled": true,
  "message": "Слот отключен"
}
```

**Go Handler**:
```go
// ToggleSlot отключает/включает слот
func (ec *ERPController) ToggleSlot(c *gin.Context) {
	slotID := c.Param("slot_id")
	
	var req struct {
		Disabled bool `json:"disabled" binding:"required"`
	}
	
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request",
			"details": err.Error(),
		})
		return
	}
	
	// Сохраняем состояние в Redis
	ctx := ec.redisUtil.Context()
	key := fmt.Sprintf("slot:%s:disabled", slotID)
	
	if req.Disabled {
		if err := ec.redisUtil.Set(key, "1", 24*time.Hour); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to disable slot",
			})
			return
		}
	} else {
		if err := ec.redisUtil.Del(key); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to enable slot",
			})
			return
		}
	}
	
	// Отправляем обновление через WebSocket
	BroadcastERPUpdate("slot_toggled", map[string]interface{}{
		"slot_id": slotID,
		"disabled": req.Disabled,
		"message": fmt.Sprintf("Слот %s", map[bool]string{true: "отключен", false: "включен"}[req.Disabled]),
	})
	
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"slot_id": slotID,
		"disabled": req.Disabled,
		"message": fmt.Sprintf("Слот %s", map[bool]string{true: "отключен", false: "включен"}[req.Disabled]),
	})
}
```

**Обновление SlotService для проверки disabled**:
```go
// IsSlotDisabled проверяет, отключен ли слот
func (ss *SlotService) IsSlotDisabled(slotID string) bool {
	if ss.redisUtil == nil {
		return false
	}
	
	ctx := ss.redisUtil.Context()
	key := fmt.Sprintf("slot:%s:disabled", slotID)
	
	disabled, err := ss.redisUtil.Get(key)
	if err != nil {
		return false
	}
	
	return disabled == "1"
}

// AssignSlot - обновленная версия с проверкой disabled
func (ss *SlotService) AssignSlot(orderID string, orderPrice int, itemsCount int) (string, time.Time, time.Time, error) {
	// ... существующий код ...
	
	for attempt := 0; attempt < maxAttempts; attempt++ {
		slotID := ss.GenerateSlotID(slotStart)
		
		// ПРОВЕРКА: отключен ли слот
		if ss.IsSlotDisabled(slotID) {
			log.Printf("⚠️ AssignSlot: слот %s отключен, пропускаем", slotID)
			slotStart = slotStart.Add(ss.slotDuration)
			continue
		}
		
		// ... остальной код ...
	}
}
```

#### 2. Изменение лимита слота

**Endpoint**: `PUT /api/v1/erp/slots/{slot_id}/capacity`

**Request**:
```json
{
  "max_capacity": 150000  // новый лимит в рублях
}
```

**Response**:
```json
{
  "success": true,
  "slot_id": "slot:1770390000",
  "max_capacity": 150000,
  "message": "Лимит слота обновлен"
}
```

**Go Handler**:
```go
// UpdateSlotCapacity обновляет лимит конкретного слота
func (ec *ERPController) UpdateSlotCapacity(c *gin.Context) {
	slotID := c.Param("slot_id")
	
	var req struct {
		MaxCapacity int `json:"max_capacity" binding:"required"`
	}
	
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request",
			"details": err.Error(),
		})
		return
	}
	
	if req.MaxCapacity <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "max_capacity must be greater than 0",
		})
		return
	}
	
	// Сохраняем лимит слота в Redis
	ctx := ec.redisUtil.Context()
	key := fmt.Sprintf("slot:%s:max_capacity", slotID)
	
	if err := ec.redisUtil.Set(key, fmt.Sprintf("%d", req.MaxCapacity), 0); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to update slot capacity",
		})
		return
	}
	
	// Отправляем обновление через WebSocket
	BroadcastERPUpdate("slot_capacity_updated", map[string]interface{}{
		"slot_id": slotID,
		"max_capacity": req.MaxCapacity,
		"message": "Лимит слота обновлен",
	})
	
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"slot_id": slotID,
		"max_capacity": req.MaxCapacity,
		"message": "Лимит слота обновлен",
	})
}
```

**Обновление SlotService для получения индивидуального лимита**:
```go
// GetSlotMaxCapacity получает максимальную емкость слота (индивидуальную или общую)
func (ss *SlotService) GetSlotMaxCapacity(slotID string) int {
	if ss.redisUtil == nil {
		return ss.maxCapacityPerSlot
	}
	
	ctx := ss.redisUtil.Context()
	key := fmt.Sprintf("slot:%s:max_capacity", slotID)
	
	capacityStr, err := ss.redisUtil.Get(key)
	if err != nil {
		// Если индивидуального лимита нет, возвращаем общий
		return ss.maxCapacityPerSlot
	}
	
	capacity, err := strconv.Atoi(capacityStr)
	if err != nil {
		return ss.maxCapacityPerSlot
	}
	
	return capacity
}

// GetSlotInfo - обновленная версия с индивидуальным лимитом
func (ss *SlotService) GetSlotInfo(slotID string) (*SlotInfo, error) {
	// ... существующий код ...
	
	// Получаем индивидуальный лимит или общий
	maxCapacity := ss.GetSlotMaxCapacity(slotID)
	
	return &SlotInfo{
		SlotID:      slotID,
		StartTime:   startTime,
		EndTime:     endTime,
		CurrentLoad: currentLoad,
		MaxCapacity: maxCapacity, // Используем индивидуальный лимит
		Disabled:    ss.IsSlotDisabled(slotID),
	}, nil
}
```

#### 3. Обновление GetSlots для включения disabled статуса

**Обновление GetSlots**:
```go
// GetSlots получает список всех слотов с их загрузкой
func (ec *ERPController) GetSlots(c *gin.Context) {
	// ... существующий код ...
	
	type SlotResponse struct {
		SlotID      string `json:"slot_id"`
		StartTime   string `json:"start_time"`
		EndTime     string `json:"end_time"`
		CurrentLoad int    `json:"current_load"`
		MaxCapacity int    `json:"max_capacity"`
		Disabled    bool   `json:"disabled"` // НОВОЕ ПОЛЕ
	}
	
	slotResponses := make([]SlotResponse, len(slots))
	for i, slot := range slots {
		slotResponses[i] = SlotResponse{
			SlotID:      slot.SlotID,
			StartTime:   slot.StartTime.Format(time.RFC3339),
			EndTime:     slot.EndTime.Format(time.RFC3339),
			CurrentLoad: slot.CurrentLoad,
			MaxCapacity: slot.MaxCapacity,
			Disabled:    ec.slotService.IsSlotDisabled(slot.SlotID), // НОВОЕ
		}
	}
	
	// ... остальной код ...
}
```

### WebSocket обновления

**Типы сообщений**:

1. **slot_toggled** - слот отключен/включен
2. **slot_capacity_updated** - лимит слота обновлен

**Формат сообщения**:
```json
{
  "type": "slot_toggled",
  "data": {
    "slot_id": "slot:1770390000",
    "disabled": true,
    "message": "Слот отключен"
  }
}
```

**Обработка на фронтенде**:
```javascript
// В KitchenCapacityTimeline.svelte
function handleWebSocketUpdate(message) {
  try {
    const data = JSON.parse(message);
    
    if (data.type === 'slot_toggled') {
      // Обновляем состояние слота
      const slot = slots.find(s => s.slot_id === data.data.slot_id);
      if (slot) {
        slot.disabled = data.data.disabled;
        slots = slots; // Триггер обновления
      }
    } else if (data.type === 'slot_capacity_updated') {
      // Обновляем лимит слота
      const slot = slots.find(s => s.slot_id === data.data.slot_id);
      if (slot) {
        slot.max_capacity = data.data.max_capacity;
        // Пересчитываем процент
        slot.percentage = Math.min((slot.current_load / slot.max_capacity) * 100, 100);
        slots = slots; // Триггер обновления
      }
    } else if (data.type === 'new_order') {
      // Перезагружаем слоты
      loadSlots();
    }
  } catch (err) {
    console.error('Ошибка обработки WebSocket обновления:', err);
  }
}
```

---

## 🎨 Дизайн-макеты и примеры кода

### Полный пример улучшенного компонента

**Файл**: `KitchenCapacityTimeline.svelte` (обновленная версия)

```svelte
<script>
  import { onMount, onDestroy } from 'svelte';
  import { ChevronLeft, ChevronRight, Pizza, Clock, X, AlertTriangle, Pause, Play, Edit, MoreVertical } from 'lucide-svelte';
  import { GetSlots, ToggleSlot, UpdateSlotCapacity } from '../../wailsjs/go/main/App.js';
  
  // ... существующие переменные ...
  
  let editingSlotCapacity = null;
  let newCapacity = 0;
  let slotActionsMenu = null;
  
  // ... существующие функции ...
  
  async function toggleSlot(slotId, disabled) {
    try {
      const result = await ToggleSlot(slotId, disabled);
      const response = JSON.parse(result);
      if (response.success) {
        // Обновляем состояние слота
        const slot = slots.find(s => s.slot_id === slotId);
        if (slot) {
          slot.disabled = disabled;
          slots = slots; // Триггер обновления
        }
      }
    } catch (err) {
      console.error('Ошибка переключения слота:', err);
      alert('Ошибка переключения слота: ' + err.message);
    }
  }
  
  function openEditCapacityModal(slot) {
    editingSlotCapacity = slot;
    newCapacity = slot.max_capacity;
  }
  
  async function saveSlotCapacity() {
    if (!editingSlotCapacity || !newCapacity || newCapacity <= 0) return;
    
    try {
      const result = await UpdateSlotCapacity(editingSlotCapacity.slot_id, newCapacity);
      const response = JSON.parse(result);
      if (response.success) {
        // Обновляем лимит слота
        const slot = slots.find(s => s.slot_id === editingSlotCapacity.slot_id);
        if (slot) {
          slot.max_capacity = newCapacity;
          slot.percentage = Math.min((slot.current_load / slot.max_capacity) * 100, 100);
          slots = slots; // Триггер обновления
        }
        editingSlotCapacity = null;
      }
    } catch (err) {
      console.error('Ошибка обновления лимита:', err);
      alert('Ошибка обновления лимита: ' + err.message);
    }
  }
  
  // Улучшенная функция для градиента с критическими индикаторами
  function getGradientClass(percentage) {
    if (percentage >= 90) {
      return 'from-red-600 via-rose-500 to-red-700 ring-2 ring-red-500 ring-offset-1';
    } else if (percentage >= 70) {
      return 'from-orange-500 via-orange-600 to-orange-700';
    } else if (percentage >= 41) {
      return 'from-orange-400 via-orange-500 to-orange-600';
    } else if (percentage > 0) {
      return 'from-emerald-500 via-teal-500 to-emerald-600';
    } else {
      return 'from-transparent to-transparent';
    }
  }
</script>

<div class="w-full">
  <!-- ... существующий header ... -->
  
  <!-- Timeline Container -->
  {#if loading}
    <!-- ... существующий loading ... -->
  {:else}
    <div class="relative w-full overflow-hidden">
      <div
        bind:this={scrollContainer}
        class="flex flex-nowrap gap-4 overflow-x-auto overflow-y-hidden px-4 py-6 scrollbar-hide"
      >
        {#each slots as slot (slot.slot_id || slot.id)}
          {@const fillPercentage = calculateFillPercentage(slot)}
          {@const isNow = isCurrentSlot(slot.time)}
          {@const status = getStatusInfo(fillPercentage)}
          {@const slotId = slot.slot_id || slot.id}
          {@const directPercentage = maxCapacityValue > 0 ? Math.min((currentLoadValue / maxCapacityValue) * 100, 100) : 0}
          
          <div
            class="flex-shrink-0 flex-none w-[10rem] h-[14.286rem] rounded-xl border shadow-sm p-4 cursor-pointer transition-all duration-300
                   {slot.disabled ? 'bg-gray-100 border-gray-300 opacity-60' : 
                    directPercentage >= 90 ? 'bg-red-50 border-red-300' : 
                    directPercentage >= 70 ? 'bg-orange-50 border-orange-200' : 
                    'bg-white border-slate-100'}
                   {selectedSlot?.slot_id === slotId ? 'shadow-lg border-[#FF5C35] ring-2 ring-[#FF5C35]/20' : ''}
                   {isNow ? 'ring-2 ring-[#FF5C35] ring-offset-2' : ''}"
          >
            <!-- Кнопки управления (правый верхний угол) -->
            <div class="absolute top-2 right-2 flex items-center gap-1 z-10">
              {#if slot.disabled}
                <button
                  on:click|stopPropagation={() => toggleSlot(slotId, false)}
                  class="p-1.5 bg-green-500 hover:bg-green-600 text-white rounded-lg transition-colors shadow-sm"
                  title="Включить слот"
                >
                  <Play class="w-4 h-4" />
                </button>
              {:else}
                <button
                  on:click|stopPropagation={() => toggleSlot(slotId, true)}
                  class="p-1.5 bg-red-500 hover:bg-red-600 text-white rounded-lg transition-colors shadow-sm"
                  title="Отключить слот"
                >
                  <Pause class="w-4 h-4" />
                </button>
              {/if}
              <button
                on:click|stopPropagation={() => openEditCapacityModal(slot)}
                class="p-1.5 bg-blue-500 hover:bg-blue-600 text-white rounded-lg transition-colors shadow-sm"
                title="Изменить лимит"
              >
                <Edit class="w-4 h-4" />
              </button>
            </div>
            
            <!-- Overlay для отключенного слота -->
            {#if slot.disabled}
              <div class="absolute inset-0 flex items-center justify-center bg-gray-200/50 rounded-xl z-20">
                <div class="text-center">
                  <XCircle class="w-8 h-8 text-gray-500 mx-auto mb-2" />
                  <span class="text-xs font-semibold text-gray-600">СЛОТ ОТКЛЮЧЕН</span>
                </div>
              </div>
            {/if}
            
            <!-- Header: Time + NOW Badge + Критический индикатор -->
            <div class="flex items-center justify-between mb-3">
              <div class="text-sm font-bold text-slate-900">
                {slot.time}
              </div>
              <div class="flex items-center gap-1">
                {#if directPercentage >= 90}
                  <div class="px-2 py-0.5 bg-red-600 text-white text-[0.714rem] font-bold rounded-full animate-pulse shadow-lg">
                    КРИТИЧНО
                  </div>
                {/if}
                {#if isNow}
                  <div class="px-2 py-0.5 bg-[#FF5C35] text-white text-[0.714rem] font-bold rounded-full animate-pulse shadow-lg">
                    СЕЙЧАС
                  </div>
                {/if}
              </div>
            </div>
            
            <!-- Main Visual: Liquid Fill Tank (УЛУЧШЕННЫЙ) -->
            <div class="flex-1 relative mb-3 min-h-[10rem] flex items-end">
              <div class="w-full h-full bg-slate-50 rounded-xl overflow-hidden relative border border-slate-100">
                <!-- Критическая граница (90%) -->
                {#if directPercentage >= 90}
                  <div class="absolute top-0 left-0 right-0 h-[10%] border-t-2 border-red-500 border-dashed opacity-50"></div>
                {/if}
                
                <!-- Liquid Fill с улучшенной видимостью -->
                <div
                  class="absolute bottom-0 left-0 right-0 rounded-xl transition-all duration-700 ease-out bg-gradient-to-t {getGradientClass(directPercentage)}
                         {directPercentage >= 90 ? 'animate-pulse' : ''}"
                  style="height: {directPercentage}%; 
                         min-height: {directPercentage > 0 ? '4px' : '0'};
                         {directPercentage >= 90 ? 'box-shadow: 0 0 20px rgba(239, 68, 68, 0.5);' : ''}"
                >
                  <!-- Анимация пульсации для критических слотов -->
                  {#if directPercentage >= 90}
                    <div class="absolute inset-0 bg-white/30 animate-pulse rounded-xl"></div>
                  {/if}
                </div>
                
                <!-- Percentage Text Overlay (всегда показываем при загрузке > 0) -->
                {#if directPercentage > 0}
                  <div class="absolute inset-0 flex items-center justify-center pointer-events-none">
                    <span class="text-white font-bold text-base drop-shadow-lg
                                 {directPercentage >= 90 ? 'text-red-100' : ''}">
                      {directPercentage.toFixed(0)}%
                    </span>
                  </div>
                {/if}
              </div>
            </div>
            
            <!-- Footer Info -->
            <div class="space-y-1.5">
              <!-- Capacity Info -->
              {#if slot.max_capacity > 0}
                <div class="text-[0.714rem] text-slate-600 font-medium">
                  {fillPercentage > 0 ? `${fillPercentage.toFixed(0)}% заполнено` : `Лимит: ${formatMoney(slot.max_capacity)}`}
                </div>
              {/if}
              
              <!-- Money Value -->
              <div class="text-lg font-bold text-slate-900">
                {formatMoney(currentLoadValue)}
              </div>
              
              <!-- Status + Orders Count (с иконкой предупреждения) -->
              <div class="flex items-center justify-between">
                <div class="flex items-center gap-1.5 px-2 py-1 rounded-md {status.bg}">
                  <Pizza size={12} class="{status.color}" />
                  <span class="text-[0.714rem] font-semibold {status.color}">
                    {ordersCount}
                  </span>
                </div>
                <div class="text-[0.714rem] font-semibold {status.color} flex items-center gap-1">
                  {#if directPercentage >= 90}
                    <AlertTriangle size={12} class="animate-pulse" />
                  {/if}
                  {status.text}
                </div>
              </div>
            </div>
          </div>
        {/each}
      </div>
    </div>
  {/if}
  
  <!-- Modal для редактирования лимита -->
  {#if editingSlotCapacity}
    <div class="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4" on:click|self={() => editingSlotCapacity = null}>
      <div class="bg-white rounded-xl shadow-2xl p-6 max-w-md w-full" on:click|stopPropagation>
        <div class="flex items-center justify-between mb-4">
          <h3 class="text-lg font-semibold text-slate-900">Изменить лимит слота</h3>
          <button
            on:click={() => editingSlotCapacity = null}
            class="text-slate-400 hover:text-slate-900 transition-colors"
          >
            <X class="w-5 h-5" />
          </button>
        </div>
        
        <div class="space-y-4">
          <div>
            <label class="block text-sm font-medium text-slate-700 mb-2">
              Время слота: {editingSlotCapacity.time}
            </label>
            <label class="block text-sm font-medium text-slate-700 mb-2">
              Текущий лимит: {formatMoney(editingSlotCapacity.max_capacity)}₽
            </label>
          </div>
          
          <div>
            <label class="block text-sm font-medium text-slate-700 mb-2">
              Новый лимит (₽)
            </label>
            <input
              type="number"
              bind:value={newCapacity}
              min="1000"
              max="1000000"
              step="1000"
              class="w-full px-4 py-2 border border-slate-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
              placeholder="Введите новый лимит"
            />
            <p class="text-xs text-slate-500 mt-1">
              Текущая загрузка: {formatMoney(editingSlotCapacity.current_load)}₽
            </p>
          </div>
          
          <div class="flex gap-3">
            <button
              on:click={saveSlotCapacity}
              class="flex-1 bg-blue-600 hover:bg-blue-700 text-white py-2 px-4 rounded-lg font-medium transition-colors"
              disabled={!newCapacity || newCapacity <= 0}
            >
              Сохранить
            </button>
            <button
              on:click={() => editingSlotCapacity = null}
              class="flex-1 bg-gray-200 hover:bg-gray-300 text-gray-700 py-2 px-4 rounded-lg font-medium transition-colors"
            >
              Отменить
            </button>
          </div>
        </div>
      </div>
    </div>
  {/if}
</div>
```

### Wails функции для вызова API

**Файл**: `main.go` (добавить функции)

```go
// ToggleSlot переключает состояние слота (включен/отключен)
func (a *App) ToggleSlot(slotID string, disabled bool) string {
	url := fmt.Sprintf("%s/api/v1/erp/slots/%s/toggle", a.apiBaseURL, slotID)
	
	payload := map[string]bool{
		"disabled": disabled,
	}
	
	jsonData, _ := json.Marshal(payload)
	req, _ := http.NewRequest("PUT", url, bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Sprintf(`{"error": "%s"}`, err.Error())
	}
	defer resp.Body.Close()
	
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

// UpdateSlotCapacity обновляет лимит конкретного слота
func (a *App) UpdateSlotCapacity(slotID string, maxCapacity int) string {
	url := fmt.Sprintf("%s/api/v1/erp/slots/%s/capacity", a.apiBaseURL, slotID)
	
	payload := map[string]int{
		"max_capacity": maxCapacity,
	}
	
	jsonData, _ := json.Marshal(payload)
	req, _ := http.NewRequest("PUT", url, bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Sprintf(`{"error": "%s"}`, err.Error())
	}
	defer resp.Body.Close()
	
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}
```

---

## 📝 Резюме

### Реализованные улучшения

1. ✅ **Визуализация прогресса**:
   - Увеличенная высота контейнера
   - Критическая граница (90%)
   - Анимация пульсации для критических слотов
   - Всегда видимый процент
   - Бейдж "КРИТИЧНО"
   - Изменение цвета фона карточки

2. ✅ **Управление слотами**:
   - Кнопка отключения/включения слота
   - Кнопка изменения лимита
   - Модальное окно для редактирования
   - Визуальное состояние отключенного слота

3. ✅ **Интеграция с бэкендом**:
   - API endpoints для управления слотами
   - WebSocket обновления в реальном времени
   - Обновление SlotService для проверки disabled

### Следующие шаги

1. Реализовать API endpoints на бэкенде
2. Обновить SlotService для поддержки индивидуальных лимитов
3. Добавить функции в Wails (main.go)
4. Обновить компонент KitchenCapacityTimeline.svelte
5. Протестировать WebSocket обновления

---

## 🎯 Ключевые моменты

- **Минималистичный дизайн**: Все элементы гармонично вписываются в текущий стиль
- **Real-time обновления**: Изменения мгновенно отображаются на всех кассах через WebSocket
- **Визуальная ясность**: Критические слоты (90%+) очевидно выделяются
- **Гибкость**: Индивидуальные лимиты для каждого слота
- **Надежность**: Проверка disabled в SlotService предотвращает назначение заказов в отключенные слоты

