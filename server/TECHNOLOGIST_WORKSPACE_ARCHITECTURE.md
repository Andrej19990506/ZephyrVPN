# Technologist Workspace - Architecture Documentation

## Overview

Technologist Workspace - это специализированный модуль для главного технолога, предназначенный для управления рецептами, полуфабрикатами, обучением персонала и контроля качества производства.

## Backend Architecture

### Models (`internal/models/technologist.go`)

#### RecipeVersion
- **Назначение**: Отслеживание истории изменений рецептов
- **Поля**:
  - `ID` (UUID)
  - `RecipeID` (UUID, FK → recipes)
  - `Version` (int) - номер версии
  - `ChangedBy` (string) - кто внес изменения
  - `ChangeReason` (text) - причина изменения
  - `IngredientsJSON` (text) - JSON снимок ингредиентов
  - `CreatedAt` (timestamp)

#### TrainingMaterial
- **Назначение**: Обучающие материалы (видео, фото, документы)
- **Поля**:
  - `ID` (UUID)
  - `RecipeID` (UUID, FK → recipes)
  - `Type` (enum: "video", "photo", "document")
  - `Title` (string)
  - `Description` (text)
  - `S3URL` (text) - URL файла в S3
  - `ThumbnailURL` (text) - превью
  - `Order` (int) - порядок отображения
  - `IsActive` (bool)
  - `CreatedBy` (string)

#### RecipeExam
- **Назначение**: Экзамены сотрудников по рецептам
- **Поля**:
  - `ID` (UUID)
  - `RecipeID` (UUID, FK → recipes)
  - `StaffID` (UUID, FK → staff)
  - `Status` (enum: "pending", "passed", "failed")
  - `Score` (int, 0-100)
  - `PassedAt` (timestamp)
  - `ExaminedBy` (string)
  - `Notes` (text)

### Services (`internal/services/technologist_service.go`)

#### TechnologistService

**Методы:**

1. **GetProductionDashboard(branchID string)**
   - Возвращает данные для Production Dashboard
   - Показывает остатки сырья, планируемое производство, недостатки
   - Статистика по рецептам (активные, полуфабрикаты, готовые товары)

2. **CreateRecipeVersion(recipeID, changedBy, changeReason)**
   - Создает новую версию рецепта при изменении
   - Сохраняет JSON снимок ингредиентов

3. **GetRecipeVersions(recipeID string)**
   - Возвращает все версии рецепта

4. **GetRecipeUsageTree(recipeID string)**
   - Рекурсивно строит дерево использования рецепта
   - Показывает, какие рецепты используют этот полуфабрикат

5. **CreateTrainingMaterial(material *TrainingMaterial)**
   - Создает обучающий материал
   - Файлы должны быть загружены в S3 заранее

6. **GetTrainingMaterials(recipeID string)**
   - Возвращает обучающие материалы для рецепта

7. **CreateRecipeExam(exam *RecipeExam)**
   - Создает/обновляет экзамен по рецепту

8. **GetRecipeExams(recipeID string)**
   - Возвращает экзамены по рецепту

9. **GetStaffRecipeExams(staffID string)**
   - Возвращает экзамены сотрудника

### API Endpoints (`internal/api/technologist_controller.go`)

Все endpoints защищены middleware `RequireTechnologistRole()` (требует роль `Technologist` или `SuperAdmin`).

**Base Path**: `/api/v1/technologist`

| Method | Endpoint | Описание |
|--------|----------|----------|
| GET | `/dashboard?branch_id=xxx` | Production Dashboard |
| GET | `/recipes/:id/versions` | Версии рецепта |
| GET | `/recipes/:id/usage-tree` | Дерево использования рецепта |
| POST | `/training-materials` | Создать обучающий материал |
| GET | `/recipes/:id/training-materials` | Материалы для рецепта |
| POST | `/recipe-exams` | Создать/обновить экзамен |
| GET | `/recipes/:id/exams` | Экзамены по рецепту |
| GET | `/staff/:id/recipe-exams` | Экзамены сотрудника |
| POST | `/unified-create` | Unified create Menu Item (с версионированием) |

### RBAC (Role-Based Access Control)

**Middleware**: `RequireTechnologistRole()`
- Проверяет роль пользователя из сессии/токена
- Разрешает доступ только для ролей: `Technologist`, `SuperAdmin`
- Возвращает `403 Forbidden` для других ролей

**Роли в системе:**
- `Technologist` - главный технолог (полный доступ к модулю)
- `SuperAdmin` - супер-администратор (полный доступ ко всем модулям)

## Frontend Architecture

### Components Structure

```
TechnologistWorkspace.svelte (Main Container)
├── UnifiedCreateWizard.svelte (Step-by-step creation wizard)
├── ProductionDashboard.svelte (Production statistics and stock levels)
├── SemiFinishedProductsView.svelte (Semi-finished products management)
├── TrainingMaterialsView.svelte (Training materials viewer)
└── RecipeVersionHistory.svelte (Recipe version history)
```

### Component Details

#### TechnologistWorkspace.svelte
- **Назначение**: Главный контейнер модуля
- **Tabs**: Dashboard, Create, Semi-finished, Training, Versions
- **Features**:
  - Выбор филиала
  - Навигация между вкладками
  - Модальное окно для создания товара

#### UnifiedCreateWizard.svelte
- **Назначение**: Пошаговый мастер создания товара
- **Steps**:
  1. **Nomenclature**: Название, тип (finished/semi-finished), единица измерения, SKU, категория
  2. **Recipe/BOM**: Добавление ингредиентов из номенклатуры
  3. **Publishing**: Цена (для finished), описание, причина создания, активация
- **Features**:
  - Валидация на каждом шаге
  - Progress bar
  - Создание в единой транзакции через API

#### ProductionDashboard.svelte
- **Назначение**: Dashboard производства
- **Displays**:
  - Статистика (активные рецепты, полуфабрикаты, готовые товары)
  - Остатки сырья с индикаторами статуса (sufficient/low/critical)
  - Недостатки на складе с указанием затронутых рецептов

#### SemiFinishedProductsView.svelte
- **Назначение**: Управление полуфабрикатами
- **Features**:
  - Список всех полуфабрикатов (IsSemiFinished=true)
  - Дерево использования (какие рецепты используют этот полуфабрикат)
  - Поиск по названию

#### TrainingMaterialsView.svelte
- **Назначение**: Просмотр и управление обучающими материалами
- **Features**:
  - Отображение видео, фото, документов
  - Загрузка новых материалов (интеграция с S3)
  - Превью для видео/фото

#### RecipeVersionHistory.svelte
- **Назначение**: История изменений рецепта
- **Features**:
  - Список всех версий с датами и авторами
  - Просмотр ингредиентов каждой версии
  - Причины изменений

## Database Schema

### Tables

1. **recipe_versions**
   - `id` (UUID, PK)
   - `recipe_id` (UUID, FK → recipes)
   - `version` (INTEGER, UNIQUE(recipe_id, version))
   - `changed_by` (VARCHAR(255))
   - `change_reason` (TEXT)
   - `ingredients_json` (TEXT)
   - `created_at` (TIMESTAMP)

2. **training_materials**
   - `id` (UUID, PK)
   - `recipe_id` (UUID, FK → recipes)
   - `type` (VARCHAR(50), CHECK: video/photo/document)
   - `title` (VARCHAR(255))
   - `description` (TEXT)
   - `s3_url` (TEXT)
   - `thumbnail_url` (TEXT)
   - `order` (INTEGER)
   - `is_active` (BOOLEAN)
   - `created_by` (VARCHAR(255))
   - `created_at`, `updated_at` (TIMESTAMP)

3. **recipe_exams**
   - `id` (UUID, PK)
   - `recipe_id` (UUID, FK → recipes)
   - `staff_id` (UUID, FK → staff)
   - `status` (VARCHAR(50), CHECK: pending/passed/failed)
   - `score` (INTEGER, 0-100)
   - `passed_at` (TIMESTAMP)
   - `examined_by` (VARCHAR(255))
   - `notes` (TEXT)
   - `created_at`, `updated_at` (TIMESTAMP)
   - UNIQUE(recipe_id, staff_id)

## Integration Points

### S3 Storage
- Все медиа-файлы (видео, фото, документы) хранятся в S3
- `TrainingMaterial.S3URL` содержит полный URL файла
- Загрузка файлов должна быть реализована отдельно (не входит в текущий scope)

### Telegram Integration (Stub)
- При обновлении рецепта готовится уведомление для Telegram
- Реализация интеграции - будущая задача
- Структура уведомления:
  ```json
  {
    "recipe_name": "...",
    "changed_by": "...",
    "change_reason": "...",
    "telegram_groups": ["technologists", "kitchen"]
  }
  ```

### Staff Module Integration
- `RecipeExam` связан с `Staff` через `staff_id`
- Технолог может видеть, кто прошел экзамен по рецепту
- Интеграция с модулем управления персоналом для отображения статуса обучения

## Security & Access Control

### RBAC Implementation
1. **Middleware**: `RequireTechnologistRole()` проверяет роль перед доступом к endpoints
2. **Frontend**: Компонент `TechnologistWorkspace` должен проверять роль пользователя перед отображением
3. **Database**: Нет прямых ограничений на уровне БД (контроль через приложение)

### Recommended Frontend RBAC Check
```javascript
// В TechnologistWorkspace.svelte
import { getCurrentUser } from '../stores/auth.js';

$: {
  const user = getCurrentUser();
  if (user && (user.role === 'Technologist' || user.role === 'SuperAdmin')) {
    // Показать модуль
  } else {
    // Редирект или скрыть модуль
  }
}
```

## Future Enhancements

1. **S3 Upload Integration**: Реализовать загрузку файлов в S3 через presigned URLs
2. **Telegram Bot**: Интеграция с Telegram Bot API для уведомлений
3. **Recipe Comparison**: Сравнение версий рецептов side-by-side
4. **Bulk Operations**: Массовое создание/обновление рецептов
5. **Export/Import**: Экспорт рецептов в Excel/PDF, импорт из файлов
6. **Recipe Templates**: Шаблоны рецептов для быстрого создания
7. **Quality Control Checklists**: Чек-листы контроля качества для каждого рецепта

## API Usage Examples

### Create Menu Item (Unified)
```bash
POST /api/v1/technologist/unified-create
Content-Type: application/json
Authorization: Bearer <token>

{
  "name": "Пицца Маргарита",
  "description": "Классическая пицца",
  "price": 599,
  "is_semi_finished": false,
  "change_reason": "Новый товар в меню",
  "nomenclature_data": {
    "name": "Пицца Маргарита",
    "sku": "PIZZA_MARGARITA",
    "base_unit": "pcs",
    "is_saleable": true
  },
  "ingredients": [
    {
      "nomenclature_id": "...",
      "quantity": 150,
      "unit": "g"
    }
  ]
}
```

### Get Production Dashboard
```bash
GET /api/v1/technologist/dashboard?branch_id=xxx
Authorization: Bearer <token>
```

### Get Recipe Usage Tree
```bash
GET /api/v1/technologist/recipes/{recipe_id}/usage-tree
Authorization: Bearer <token>
```

## Migration

Запустить миграцию:
```sql
\i migrations/009_create_technologist_tables.sql
```

Миграция создаст:
- Таблицу `recipe_versions`
- Таблицу `training_materials`
- Таблицу `recipe_exams`
- Индексы для оптимизации запросов







