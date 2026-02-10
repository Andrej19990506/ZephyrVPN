# Исправления утечек памяти в Wails Svelte приложении

## Проблема
Приложение потребляло оперативную память, которая постоянно росла (с 600 МБ до 12 ГБ), даже в режиме простоя. Memory profiling выявил множество Detached DOM Nodes (отсоединенных DOM элементов) и большое количество строк в памяти, что указывало на то, что компоненты создаются, но не размонтируются правильно при переключении маршрутов.

## Выполненные исправления

### 1. DashboardNew.svelte (Главный роутер)
**Проблема:** Компоненты монтировались/размонтировались через `{#if}`, но данные не очищались при переключении вкладок.

**Исправления:**
- ✅ Добавлен `onDestroy` хук для очистки всех данных при размонтировании
- ✅ Добавлен `{#key activeTab}` блок для принудительного размонтирования компонентов при смене вкладки
- ✅ Очистка массивов: `orders = []`, `popularItems = []`
- ✅ Сброс объектов: `stats`, `kitchenStatus`

**Код:**
```svelte
onDestroy(() => {
  orders = [];
  stats = { /* сброс */ };
  popularItems = [];
  kitchenStatus = { /* сброс */ };
});

{#key activeTab}
  {#if activeTab === 'orders'}
    <OrdersManagement />
  {/if}
{/key}
```

### 2. ProcurementCatalogSetup.svelte
**Проблема:** Большие массивы данных (`categories`, `flatItems`, `uomRules`) не очищались при размонтировании. Таймеры для toast-уведомлений не очищались.

**Исправления:**
- ✅ Добавлен `onDestroy` хук
- ✅ Очистка всех массивов: `categories = []`, `flatItems = []`, `uomRules = []`
- ✅ Очистка таймера toast: `clearTimeout(toastTimer)`
- ✅ Сброс всех форм и флагов

**Код:**
```svelte
let toastTimer = null;

onDestroy(() => {
  if (toastTimer) {
    clearTimeout(toastTimer);
    toastTimer = null;
  }
  categories = [];
  flatItems = [];
  uomRules = [];
  // ... остальная очистка
});

function showToastMessage(message, type = 'success') {
  if (toastTimer) {
    clearTimeout(toastTimer);
  }
  // ... показ toast
  toastTimer = setTimeout(() => {
    showToast = false;
    toastTimer = null;
  }, 3000);
}
```

### 3. InventoryInboundModule.svelte
**Проблема:** Таймер `searchDebounceTimer` не очищался, большие массивы данных не освобождались.

**Исправления:**
- ✅ Добавлен `onDestroy` хук
- ✅ Очистка таймера: `clearTimeout(searchDebounceTimer)`
- ✅ Очистка всех массивов: `invoices = []`, `availableProducts = []`, `categories = []`
- ✅ Сброс форм и модальных окон

**Код:**
```svelte
onDestroy(() => {
  if (searchDebounceTimer) {
    clearTimeout(searchDebounceTimer);
    searchDebounceTimer = null;
  }
  invoices = [];
  availableProducts = [];
  categories = [];
  // ... остальная очистка
});
```

### 4. InventoryStockModule.svelte
**Проблема:** `setInterval` не сохранялся в переменную и не очищался при размонтировании.

**Исправления:**
- ✅ Добавлен `onDestroy` хук
- ✅ Сохранение интервала в переменную: `refreshInterval = setInterval(...)`
- ✅ Очистка интервала: `clearInterval(refreshInterval)`

**Код:**
```svelte
let refreshInterval = null;

onMount(async () => {
  // ...
  refreshInterval = setInterval(async () => {
    await loadStockItems();
    // ...
  }, 5 * 60 * 1000);
});

onDestroy(() => {
  if (refreshInterval) {
    clearInterval(refreshInterval);
    refreshInterval = null;
  }
});
```

## Принципы исправления утечек памяти

### 1. Всегда используйте `onDestroy` для очистки
```svelte
import { onMount, onDestroy } from 'svelte';

onDestroy(() => {
  // Очистка таймеров
  if (timer) clearTimeout(timer);
  if (interval) clearInterval(interval);
  
  // Очистка массивов
  dataArray = [];
  
  // Отписка от событий
  EventsOff('event-name');
  
  // Очистка ссылок
  elementRef = null;
});
```

### 2. Сохраняйте таймеры в переменные
```svelte
// ❌ Плохо - нет возможности очистить
setTimeout(() => { /* ... */ }, 1000);

// ✅ Хорошо - можно очистить
let timer = setTimeout(() => { /* ... */ }, 1000);
onDestroy(() => {
  if (timer) clearTimeout(timer);
});
```

### 3. Используйте `{#key}` для принудительного размонтирования
```svelte
// ✅ Принудительно размонтирует компонент при изменении key
{#key activeTab}
  {#if activeTab === 'orders'}
    <OrdersManagement />
  {/if}
{/key}
```

### 4. Очищайте большие массивы данных
```svelte
onDestroy(() => {
  // ✅ Очистка массивов освобождает память
  largeArray = [];
  // Не просто присваивание null, а пустой массив для реактивности Svelte
});
```

### 5. Отписывайтесь от событий
```svelte
onMount(() => {
  EventsOn('websocket-message', handleMessage);
});

onDestroy(() => {
  EventsOff('websocket-message'); // ✅ Обязательно отписаться
});
```

## Компоненты, которые уже правильно используют onDestroy

- ✅ `KitchenCapacityTimeline.svelte` - очищает EventsOff и clearInterval
- ✅ `OrdersManagement.svelte` - очищает clearInterval
- ✅ `RecipeBook.svelte` - очищает EventsOff
- ✅ `RecipeManagementModule.svelte` - очищает EventsOff и clearTimeout
- ✅ `TechnologistWorkspace.svelte` - очищает EventsOff
- ✅ `BrowserWindowsBar.svelte` - очищает clearInterval
- ✅ `Dashboard.svelte` - очищает EventsOff и clearInterval
- ✅ `SlotManagement.svelte` - очищает EventsOff

## Ожидаемый результат

После этих исправлений:
- ✅ Компоненты правильно размонтируются при переключении вкладок
- ✅ Таймеры и интервалы очищаются, не остаются в памяти
- ✅ Большие массивы данных освобождаются
- ✅ Detached DOM Nodes больше не накапливаются
- ✅ Потребление памяти стабилизируется и не растет бесконечно

## Мониторинг

Для проверки эффективности исправлений используйте:
1. **Chrome DevTools Memory Profiler:**
   - Откройте DevTools (Ctrl+Shift+I)
   - Вкладка "Memory" → "Heap snapshot"
   - Сравните снимки до и после переключения вкладок

2. **Go pprof (для бэкенда):**
   ```bash
   go tool pprof http://localhost:6060/debug/pprof/heap
   ```

3. **Логи памяти (уже добавлены в main.go):**
   - Каждые 30 секунд выводятся статистики памяти
   - Следите за `HeapAlloc` и `NumGoroutine`

## Дополнительные рекомендации

1. **Используйте виртуализацию для больших списков** (если компонент отображает >100 элементов)
2. **Ленивая загрузка данных** - загружайте только то, что видно на экране
3. **Мемоизация вычислений** - используйте `$:` реактивные блоки с кэшированием
4. **Избегайте циклических ссылок** - не храните ссылки на родительские компоненты


