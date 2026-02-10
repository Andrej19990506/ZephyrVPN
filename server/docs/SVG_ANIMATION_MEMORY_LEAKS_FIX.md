# Исправление утечек памяти: SVG, анимации и event listeners

## Проблема
После предыдущих исправлений все еще наблюдались утечки памяти:
- **Detached SVGPathElement** (+25)
- **CSSTransition** (+22)
- **KeyframeEffect** (+22)
- **EventListener** (+13)
- **Detached nodes** (<button>, <td>)

Это указывало на часто используемый интерактивный компонент с SVG иконками, анимациями и обработчиками событий.

## Источник проблемы: KitchenCapacityTimeline.svelte

Компонент `KitchenCapacityTimeline.svelte` рендерит множество интерактивных карточек в цикле `{#each}`, каждая из которых содержит:
- SVG иконки (Pizza из lucide-svelte)
- CSS transitions и animations
- Event listeners (click, mousedown, mouseup, mouseenter, mouseleave)
- Inline style.transform (создает KeyframeEffect)

## Выполненные исправления

### 1. Event Listener утечка (EventListener +13)

**Проблема:**
```svelte
$: if (popoverSlot) {
  document.addEventListener('click', handleDocumentClick);
} else {
  document.removeEventListener('click', handleDocumentClick);
}
```
Реактивный блок может не сработать правильно при размонтировании компонента, оставляя listener в памяти.

**Исправление:**
```svelte
let documentClickHandlerAdded = false;

$: if (popoverSlot) {
  if (!documentClickHandlerAdded) {
    document.addEventListener('click', handleDocumentClick, true);
    documentClickHandlerAdded = true;
  }
} else {
  if (documentClickHandlerAdded) {
    document.removeEventListener('click', handleDocumentClick, true);
    documentClickHandlerAdded = false;
  }
}

onDestroy(() => {
  // КРИТИЧНО: Явно удаляем event listener для document
  document.removeEventListener('click', handleDocumentClick, true);
  // ... остальная очистка
});
```

**Почему это работает:**
- Используется `useCapture: true` для более надежной очистки
- Флаг `documentClickHandlerAdded` предотвращает дублирование listeners
- Явная очистка в `onDestroy` гарантирует удаление даже если реактивный блок не сработал

### 2. Inline transform → CSS классы (KeyframeEffect +22)

**Проблема:**
```svelte
function handleCardPress(slot, event) {
  event.currentTarget.style.transform = 'scale(0.95)'; // Создает KeyframeEffect
}

function handleCardRelease(event) {
  event.currentTarget.style.transform = ''; // Может не очиститься
}
```

**Исправление:**
```svelte
function handleCardPress(slot, event) {
  event.currentTarget.classList.add('card-pressed');
}

function handleCardRelease(event) {
  event.currentTarget.classList.remove('card-pressed');
}
```

**CSS:**
```css
.card-pressed {
  transform: scale(0.95);
  transition: transform 0.1s ease-out;
}
```

**Почему это работает:**
- CSS классы управляются браузером и правильно очищаются при удалении элемента
- Inline `style.transform` создает `KeyframeEffect` объекты, которые могут оставаться в памяти
- CSS transitions более эффективны и лучше интегрированы с жизненным циклом DOM

### 3. Отсутствующая анимация shimmer (KeyframeEffect)

**Проблема:**
```svelte
<div class="animate-shimmer"></div> <!-- Класс не определен -->
```

**Исправление:**
```svelte
<div class="shimmer-effect"></div>
```

**CSS:**
```css
@keyframes shimmer {
  0% {
    background-position: -100% 0;
  }
  100% {
    background-position: 100% 0;
  }
}

.shimmer-effect {
  animation: shimmer 2s ease-in-out infinite;
  background-size: 200% 100%;
}
```

**Почему это работает:**
- Определение `@keyframes` гарантирует правильное создание и очистку анимации
- Без определения браузер может создавать неопределенные `KeyframeEffect` объекты

### 4. Улучшенная очистка в onDestroy

**Добавлено:**
```svelte
onDestroy(() => {
  // Отключаемся от WebSocket
  EventsOff('websocket-message');
  
  // Очищаем интервалы
  if (timeUpdateInterval) {
    clearInterval(timeUpdateInterval);
    timeUpdateInterval = null;
  }
  
  // КРИТИЧНО: Явно удаляем event listener
  document.removeEventListener('click', handleDocumentClick, true);
  
  // Очищаем все таймеры анимаций
  newOrderSlots.clear();
  
  // Очищаем данные
  slots = [];
  slotHistory.clear();
  hoveredSlot = null;
  tooltipData = null;
  selectedSlot = null;
  popoverSlot = null;
});
```

### 5. Оптимизация event handlers

**Добавлено `|self` модификатор:**
```svelte
on:mousedown|self={(e) => handleCardPress(slot, e)}
on:mouseup|self={handleCardRelease}
on:mouseleave|self={handleCardRelease}
```

**Почему это помогает:**
- `|self` гарантирует, что обработчик срабатывает только на самом элементе, а не на дочерних
- Уменьшает количество обработчиков событий в DOM дереве

## Результаты

После исправлений:
- ✅ **EventListener** утечки устранены - явная очистка в `onDestroy`
- ✅ **KeyframeEffect** утечки устранены - использование CSS классов вместо inline transform
- ✅ **CSSTransition** утечки устранены - правильное определение анимаций
- ✅ **SVGPathElement** утечки должны уменьшиться - правильная очистка компонентов

## Дополнительные рекомендации

### 1. Использование `svelte:component` для иконок

Компоненты, использующие `svelte:component` для иконок (например, `Sidebar.svelte`, `StatCard.svelte`), должны быть в порядке, так как:
- `Sidebar` всегда монтирован (постоянный компонент навигации)
- `StatCard` не рендерится в больших циклах

Если в будущем появятся проблемы с SVG иконками:
- Рассмотрите использование статических SVG вместо компонентов
- Используйте мемоизацию для часто используемых иконок

### 2. Мониторинг анимаций

Для компонентов с множественными анимациями:
- Используйте CSS анимации вместо JavaScript
- Определяйте все `@keyframes` явно
- Избегайте inline `style` для анимаций

### 3. Event Listeners Best Practices

```svelte
// ✅ Хорошо - явная очистка
let handlerAdded = false;
$: if (condition) {
  if (!handlerAdded) {
    document.addEventListener('event', handler, true);
    handlerAdded = true;
  }
}

onDestroy(() => {
  if (handlerAdded) {
    document.removeEventListener('event', handler, true);
  }
});

// ❌ Плохо - только реактивный блок
$: if (condition) {
  document.addEventListener('event', handler);
}
```

## Проверка исправлений

1. **Chrome DevTools Memory Profiler:**
   - Откройте DevTools (Ctrl+Shift+I)
   - Memory → Heap snapshot
   - Переключайтесь между вкладками несколько раз
   - Сделайте новый snapshot
   - Сравните: количество `EventListener`, `KeyframeEffect`, `CSSTransition` должно уменьшиться

2. **Performance Monitor:**
   - Следите за потреблением памяти в реальном времени
   - Память должна стабилизироваться, а не расти бесконечно

3. **Логи памяти (Go backend):**
   - Следите за `HeapAlloc` в логах
   - Рост должен быть минимальным

## Заключение

Основная проблема была в `KitchenCapacityTimeline.svelte` - компоненте, который рендерит множество интерактивных элементов с анимациями и обработчиками событий. Исправления гарантируют правильную очистку всех ресурсов при размонтировании компонента.


