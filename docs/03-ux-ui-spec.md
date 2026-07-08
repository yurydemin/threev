# UI/UX Design Specification
## Проект: «S3 Desktop Client»
## Версия: 1.0
## Дата: 2026-07-09

---

## 1. Философия дизайна

**Принцип:** «Нативный файловый менеджер, но для облака». Пользователь не должен переучиваться — интерфейс следует паттернам macOS Finder и Windows Explorer, адаптированным для S3-специфики.

**Ключевые акценты:**
- **Плотность информации** — максимум данных на экране без визуального шума.
- **Мгновенная обратная связь** — hover, active, focus состояния без задержки.
- **Контекстность** — всё, что нужно для текущей задачи, под рукой. Лишнее скрыто.
- **Профессиональная строгость** — никаких ярких акцентов, градиентов, теней. Только функциональная эстетика.

---

## 2. Цветовая система

### 2.1. Тёмная тема (default, системная)

| Токен | Значение | Применение |
|-------|----------|------------|
| `--bg-primary` | `#0f172a` | Фон всего приложения, боковые панели |
| `--bg-secondary` | `#1e293b` | Карточки, панели, модальные окна, хедеры таблиц |
| `--bg-tertiary` | `#334155` | Hover-состояния строк, активные элементы |
| `--bg-elevated` | `#1e293b` + `border: 1px solid #334155` | Модальные окна, dropdowns, popovers |
| `--fg-primary` | `#f8fafc` | Основной текст, заголовки, иконки |
| `--fg-secondary` | `#94a3b8` | Вторичный текст, подписи, метаданные, disabled |
| `--fg-muted` | `#64748b` | Третичный текст, разделители, плейсхолдеры |
| `--accent` | `#3b82f6` | Primary actions, выделение, прогресс, активные табы |
| `--accent-hover` | `#2563eb` | Hover на accent-элементах |
| `--accent-subtle` | `rgba(59, 130, 246, 0.15)` | Фон выделенных строк, подсветка активных элементов |
| `--success` | `#22c55e` | Успешные операции, online-статус |
| `--warning` | `#f59e0b` | Предупреждения, retry, ожидание |
| `--danger` | `#ef4444` | Ошибки, удаление, отмена |
| `--danger-hover` | `#dc2626` | Hover на destructive actions |
| `--border` | `#334155` | Разделители, границы панелей, таблицы |
| `--border-subtle` | `#1e293b` | Тонкие разделители внутри списков |

### 2.2. Светлая тема

| Токен | Значение | Применение |
|-------|----------|------------|
| `--bg-primary` | `#f8fafc` | Фон приложения |
| `--bg-secondary` | `#ffffff` | Карточки, панели |
| `--bg-tertiary` | `#f1f5f9` | Hover, активные элементы |
| `--bg-elevated` | `#ffffff` + `border: 1px solid #e2e8f0` | Модальные окна |
| `--fg-primary` | `#0f172a` | Основной текст |
| `--fg-secondary` | `#475569` | Вторичный текст |
| `--fg-muted` | `#94a3b8` | Третичный текст |
| `--accent` | `#2563eb` | Primary actions |
| `--accent-hover` | `#1d4ed8` | Hover accent |
| `--accent-subtle` | `rgba(37, 99, 235, 0.10)` | Фон выделенных строк |
| `--success` | `#16a34a` | Успех |
| `--warning` | `#d97706` | Предупреждение |
| `--danger` | `#dc2626` | Ошибки, удаление |
| `--danger-hover` | `#b91c1c` | Hover destructive |
| `--border` | `#e2e8f0` | Разделители |
| `--border-subtle` | `#f1f5f9` | Тонкие разделители |

### 2.3. Типографика

| Элемент | Шрифт | Размер | Вес | Line-height | Letter-spacing |
|---------|-------|--------|-----|-------------|----------------|
| Заголовок окна | System UI | 14px | 600 | 1.2 | -0.01em |
| Заголовок секции | System UI | 13px | 600 | 1.3 | 0 |
| Тело списка | System UI | 13px | 400 | 1.4 | 0 |
| Метаданные | System UI | 12px | 400 | 1.3 | 0 |
| Моноширинный | JetBrains Mono / SF Mono | 12px | 400 | 1.4 | 0 |
| Кнопка | System UI | 13px | 500 | 1 | 0 |
| Маленький тег | System UI | 11px | 500 | 1 | 0.01em |

**System UI stack:** `-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif`

---

## 3. Глобальные Layout-константы

| Константа | Значение | Описание |
|-----------|----------|----------|
| `SIDEBAR_WIDTH` | `240px` | Ширина левой боковой панели (фиксированная) |
| `HEADER_HEIGHT` | `48px` | Высота верхнего хедера |
| `BOTTOM_PANEL_HEIGHT` | `180px` | Высота нижней панели передач (resizable, min 120px, max 400px) |
| `ROW_HEIGHT` | `36px` | Высота строки в таблице/списке |
| `ICON_SIZE` | `16px` | Размер иконок в списках |
| `ICON_SIZE_LARGE` | `20px` | Размер иконок в тулбаре |
| `PADDING_BASE` | `16px` | Базовый внутренний отступ |
| `PADDING_TIGHT` | `8px` | Компактный отступ |
| `BORDER_RADIUS` | `6px` | Скругление кнопок, инпутов, карточек |
| `BORDER_RADIUS_SMALL` | `4px` | Скругление тегов, маленьких элементов |
| `TRANSITION_FAST` | `150ms ease-out` | Быстрые переходы (hover, focus) |
| `TRANSITION_NORMAL` | `250ms ease-out` | Обычные переходы (открытие панелей, модалок) |

---

## 4. Глобальные компоненты

### 4.1. Title Bar (системная, Wails)
- Высота: `32px` (macOS) / `36px` (Windows/Linux)
- Цвет: `--bg-primary` с `border-bottom: 1px solid --border`
- Содержимое: название приложения «S3 Client» + имя текущего профиля/бакета (если подключено)
- Кнопки окна (minimize, maximize, close) — нативные, Wails отрисовывает сам

### 4.2. Button

**Primary:**
- Фон: `--accent`, текст: `#ffffff`
- Hover: `--accent-hover`
- Active: `scale(0.97)` + `--accent-hover`
- Padding: `6px 12px`
- Border-radius: `BORDER_RADIUS`
- Font: 13px, weight 500
- Disabled: opacity 0.5, cursor not-allowed

**Secondary:**
- Фон: `--bg-secondary`, текст: `--fg-primary`, border: `1px solid --border`
- Hover: `--bg-tertiary`
- Active: `scale(0.97)`

**Danger:**
- Фон: `--danger`, текст: `#ffffff`
- Hover: `--danger-hover`

**Ghost (иконка + текст):**
- Фон: transparent, текст: `--fg-secondary`
- Hover: `--bg-tertiary`
- Active: `--bg-tertiary` + `scale(0.97)`

**Icon-only:**
- Размер: `32px × 32px`
- Border-radius: `BORDER_RADIUS_SMALL`
- Иконка: `ICON_SIZE_LARGE`
- Hover: `--bg-tertiary`

### 4.3. Input / Text Field
- Высота: `32px`
- Фон: `--bg-secondary`
- Border: `1px solid --border`, border-radius: `BORDER_RADIUS`
- Padding: `0 10px`
- Font: 13px
- Focus: `border-color: --accent`, `box-shadow: 0 0 0 2px --accent-subtle`
- Placeholder: `--fg-muted`
- Disabled: opacity 0.5
- Error: `border-color: --danger`, `box-shadow: 0 0 0 2px rgba(239, 68, 68, 0.15)`

### 4.4. Select / Dropdown
- Аналогично Input, но с right-aligned chevron-иконкой (`16px`)
- Dropdown list: `--bg-elevated`, border-radius: `BORDER_RADIUS`, shadow: `0 4px 12px rgba(0,0,0,0.15)`
- Item hover: `--bg-tertiary`
- Item selected: `--accent-subtle` + текст `--accent`
- Max-height: `240px` с overflow-y auto

### 4.5. Checkbox
- Размер: `16px × 16px`
- Border: `1px solid --border`, border-radius: `4px`
- Checked: фон `--accent`, иконка галочки белая (`12px`)
- Indeterminate: фон `--accent`, горизонтальная черта белая (`10px`)
- Hover: `border-color: --accent`

### 4.6. Table / List Row
- Высота: `ROW_HEIGHT`
- Padding: `0 PADDING_TIGHT 0 PADDING_BASE`
- Hover: фон `--bg-tertiary`
- Selected: фон `--accent-subtle`, левая граница `2px solid --accent`
- Focused (keyboard): `outline: 2px solid --accent`, outline-offset: `-2px`
- Анимация hover: `background-color TRANSITION_FAST`

### 4.7. Modal / Dialog
- Overlay: `rgba(0, 0, 0, 0.50)` с `backdrop-filter: blur(2px)`
- Контейнер: `--bg-elevated`, border-radius: `BORDER_RADIUS`
- Max-width: зависит от контента (default `480px`, large `640px`)
- Padding: `PADDING_BASE`
- Header: title (13px, weight 600) + close button (icon-only, top-right)
- Body: padding-top `PADDING_TIGHT`
- Footer: flex row, justify-end, gap `8px`, padding-top `PADDING_BASE`
- Анимация: fade-in overlay `150ms`, slide-up контент `200ms` (transform: translateY(8px) → 0)

### 4.8. Toast / Notification
- Позиция: bottom-right, `16px` от краёв
- Размер: max-width `360px`
- Фон: `--bg-elevated`
- Border-left: `3px solid` (success: `--success`, error: `--danger`, warning: `--warning`, info: `--accent`)
- Padding: `10px 14px`
- Auto-dismiss: `5s` для success/info, `10s` для error (с кнопкой закрытия)
- Анимация: slide-in from right `250ms`, fade-out `200ms`

### 4.9. Tooltip
- Фон: `#0f172a` (тёмный, независимо от темы), текст `#f8fafc`
- Padding: `4px 8px`
- Border-radius: `BORDER_RADIUS_SMALL`
- Font: 12px
- Arrow: 6px, centered bottom
- Задержка появления: `400ms`

### 4.10. Progress Bar
- Высота: `4px` (thin) или `8px` (standard)
- Фон трека: `--bg-tertiary`
- Заполнение: `--accent`
- Анимация: width transition `300ms linear`
- Для многофазных: сегментированный, каждый сегмент — отдельный цвет (completed: `--accent`, pending: `--bg-tertiary`, error: `--danger`)

### 4.11. Context Menu
- Фон: `--bg-elevated`
- Border: `1px solid --border`
- Border-radius: `BORDER_RADIUS`
- Shadow: `0 4px 16px rgba(0,0,0,0.20)`
- Item: height `32px`, padding `0 12px`, font 13px
- Item hover: `--bg-tertiary`
- Item disabled: `--fg-muted`, opacity 0.5
- Separator: `1px solid --border`, margin `4px 0`
- Accelerator (hotkey): right-aligned, `--fg-muted`, font 12px
- Анимация: scale(0.95) → scale(1) + opacity `150ms`

---

## 5. Экраны и Layout

### 5.1. Экран «Приветствие / Нет подключений»

**Появление:** при первом запуске или когда нет сохранённых профилей.

**Layout:**
```
┌─────────────────────────────────────────────┐
│ Title Bar                                   │
├─────────────────────────────────────────────┤
│                                             │
│          [Логотип: облако + стрелка]        │
│                                             │
│          «S3 Desktop Client»                │
│          Управляйте облачными               │
│          хранилищами как локальными         │
│                                             │
│          [+ Добавить подключение]           │
│                                             │
│          Или перетащите файл .env           │
│          с credentials сюда                 │
│                                             │
├─────────────────────────────────────────────┤
│ Footer: версия + ссылка на GitHub          │
└─────────────────────────────────────────────┘
```

**Детали:**
- Центрированный контент, вертикально и горизонтально.
- Логотип: иконка `CloudArrowUp` (`48px`, `--accent`).
- Заголовок: 20px, weight 600, `--fg-primary`.
- Подзаголовок: 14px, `--fg-secondary`.
- Кнопка: Primary, размер large (padding `10px 20px`, font 14px).
- Drag-and-drop зона: пунктирная рамка `2px dashed --border`, border-radius `BORDER_RADIUS`, padding `40px`. При drag-over: `border-color: --accent`, фон `--accent-subtle`.
- Footer: 12px, `--fg-muted`, прижат к низу.

---

### 5.2. Экран «Список подключений» (Sidebar view)

**Layout:**
```
┌─────────────────────────────────────────────────────────────────────────────┐
│ Title Bar: S3 Client                                                        │
├──────────┬──────────────────────────────────────────────────────────────────┤
│          │  Хедер: «Подключения»                        [+ Новое] [Import] │
│  Sidebar │  ─────────────────────────────────────────────────────────────  │
│  (240px) │  ┌──────────────────────────────────────────────────────────┐  │
│          │  │ [🔵] AWS Production                                        │  │
│  [Лого]  │  │    s3.amazonaws.com  •  us-east-1                          │  │
│          │  │    [Подключиться]  [⋯]                                     │  │
│  ──────  │  └──────────────────────────────────────────────────────────┘  │
│  Подкл.  │  ┌──────────────────────────────────────────────────────────┐  │
│  Передачи│  │ [🟡] Yandex Cloud                                          │  │
│  История │  │    storage.yandexcloud.net  •  ru-central1                 │  │
│  Настройки│  │    [Подключиться]  [⋯]                                     │  │
│          │  └──────────────────────────────────────────────────────────┘  │
│          │  ┌──────────────────────────────────────────────────────────┐  │
│          │  │ [🟢] MinIO Local                                           │  │
│          │  │    localhost:9000  •  us-east-1                            │  │
│          │  │    [Подключиться]  [⋯]                                     │  │
│          │  └──────────────────────────────────────────────────────────┘  │
│          │                                                                   │
│          │  [+ Добавить подключение]  (Secondary, centered)                │
│          │                                                                   │
├──────────┴──────────────────────────────────────────────────────────────────┤
│ Status Bar: готов                                                           │
└─────────────────────────────────────────────────────────────────────────────┘
```

**Sidebar:**
- Ширина: `SIDEBAR_WIDTH` (240px), фон `--bg-primary`.
- Верх: логотип приложения (24px) + название «S3 Client» (13px, weight 600), padding `PADDING_BASE`.
- Разделитель: `1px solid --border`.
- Навигация: вертикальный список пунктов.
  - Каждый пункт: height `36px`, padding `0 PADDING_BASE`, font 13px, `--fg-secondary`.
  - Иконка слева (`ICON_SIZE`), текст справа, gap `10px`.
  - Active: фон `--accent-subtle`, текст `--accent`, иконка `--accent`.
  - Hover: фон `--bg-tertiary`.
  - Пункты: «Подключения», «Передачи», «История», «Настройки».
- Низ sidebar: информация о версии (12px, `--fg-muted`), padding `PADDING_BASE`.

**Карточка подключения (Connection Card):**
- Фон: `--bg-secondary`, border: `1px solid --border`, border-radius: `BORDER_RADIUS`.
- Padding: `PADDING_BASE`.
- Статус-индикатор: круг `8px`, цвет по статусу (последний тест: зелёный `--success`, ошибка: `--danger`, не проверялось: `--fg-muted`).
- Название: 14px, weight 600, `--fg-primary`.
- Endpoint + регион: 12px, `--fg-secondary`, моноширинный шрифт для URL.
- Кнопки: «Подключиться» (Primary, small) + «⋯» (Icon-only, secondary) с dropdown: Редактировать, Дублировать, Удалить, Тестировать.
- Hover карточки: `border-color: --accent`, transition `TRANSITION_FAST`.
- Gap между карточками: `12px`.
- Максимум 3 карточки в ряд (grid: `repeat(auto-fill, minmax(300px, 1fr))`).

**Пустое состояние:**
- Если нет профилей: центрированный текст «Нет сохранённых подключений» + кнопка «Добавить».

---

### 5.3. Модальное окно «Новое подключение / Редактирование»

**Layout:**
```
┌─────────────────────────────────────────────┐
│  Новое подключение                    [×]   │
├─────────────────────────────────────────────┤
│  Название:                                  │
│  [________________________________]         │
│                                             │
│  Endpoint URL:                              │
│  [https://________________________]         │
│                                             │
│  Регион:          [us-east-1    ▼]          │
│                                             │
│  Access Key ID:                             │
│  [________________________________]         │
│                                             │
│  Secret Access Key:                         │
│  [________________________________]         │
│  [ ] Показать пароль                        │
│                                             │
│  Session Token (опционально):               │
│  [________________________________]         │
│                                             │
│  ┌─ Advanced ───────────────────────────┐   │
│  │  [ ] Path-style URL                  │   │
│  │  [ ] Проверять SSL-сертификат        │   │
│  │  Custom headers: [+ Добавить]        │   │
│  └──────────────────────────────────────┘   │
│                                             │
│  [Тестировать]           [Отмена] [Сохранить]│
└─────────────────────────────────────────────┘
```

**Детали:**
- Размер: `520px` max-width.
- Поля расположены вертикально, label над input (label: 12px, weight 500, `--fg-secondary`, margin-bottom `4px`).
- Поле «Secret Access Key»: type password, с кнопкой-глазом справа внутри input для переключения видимости.
- Advanced: collapsible секция, по умолчанию свёрнута. Chevron-иконка справа от заголовка. Анимация: height transition `TRANSITION_NORMAL`.
- Custom headers: динамический список пар key-value с кнопками удаления.
- Кнопка «Тестировать»: Secondary, слева в футере. При клике: spinner + текст «Проверка…». Результат: зелёная галочка «Подключение успешно» или красный текст ошибки под кнопкой.
- Кнопка «Сохранить»: Primary. Disabled до тех пор, пока не заполнены обязательные поля (name, endpoint, key, secret).
- Валидация inline: красный border + текст ошибки под полем (12px, `--danger`).

---

### 5.4. Экран «Файловый менеджер» (основной)

**Layout:**
```
┌─────────────────────────────────────────────────────────────────────────────┐
│ Title Bar: S3 Client  —  AWS Production  /  my-bucket                     │
├──────────┬──────────────────────────────────────────────────────────────────┤
│          │  Toolbar (48px)                                                  │
│  Sidebar │  ┌──────────────────────────────────────────────────────────┐    │
│  (240px) │  │ [←] [→] [↻]  |  bucket-name  >  folder1  >  folder2   │    │
│          │  │                              [🔍 Поиск...]  [≡ Вид]     │    │
│  [Лого]  │  └──────────────────────────────────────────────────────────┘    │
│  ──────  │  ───────────────────────────────────────────────────────────────   │
│  Профили │  Object List (таблица/сетка)                                     │
│  ──────  │  ┌──────────────────────────────────────────────────────────┐    │
│  Бакеты  │  │ [☐] Имя          │ Размер │ Тип      │ Изменён        │    │
│  (tree)  │  ├──────────────────────────────────────────────────────────┤    │
│          │  │ [☐] folder1/     │  —     │ Папка    │ 2026-07-01     │    │
│          │  │ [☐] image.png    │ 2.4 MB │ image/png│ 2026-07-05     │    │
│  ──────  │  │ [☐] data.csv     │ 156 KB │ text/csv │ 2026-07-04     │    │
│  Передачи│  │ [☐] backup.zip   │ 1.2 GB │ app/zip  │ 2026-07-02     │    │
│  История │  │ [☐] notes.txt    │ 4 KB   │ text/pl… │ 2026-07-05     │    │
│  Настройки│  └──────────────────────────────────────────────────────────┘    │
│          │                                                                   │
│          │  Status Bar: 5 объектов  •  1.2 GB total  •  [⚡ 2 передачи]     │
├──────────┴──────────────────────────────────────────────────────────────────┤
│  Transfer Panel (resizable, 180px)                                         │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │ Upload: image.png  ████████░░  45%  12 MB/s  ETA 2m  [⏸] [✕]       │  │
│  │ Download: backup…  ████░░░░░  28%  8 MB/s   ETA 5m  [⏸] [✕]       │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### 5.4.1. Toolbar

- Высота: `HEADER_HEIGHT` (48px).
- Фон: `--bg-secondary`, border-bottom: `1px solid --border`.
- Padding: `0 PADDING_BASE`.
- **Left group:**
  - Навигация: кнопки «Назад», «Вперёд» (Icon-only, disabled если нет истории), «Обновить» (Icon-only).
  - Разделитель: `1px solid --border`, height `24px`, vertical.
  - Breadcrumbs: кликабельные сегменты (bucket-name > folder1 > folder2). Каждый сегмент: 13px, `--fg-primary`, hover: `--accent`. Разделитель: `/` (12px, `--fg-muted`).
- **Right group:**
  - Поиск: Input с иконкой лупы слева, placeholder «Поиск в текущей папке…», width `200px` (focus → `280px`, transition `TRANSITION_FAST`).
  - Переключатель вида: Icon-only кнопка (список/сетка/дерево). Dropdown при клике.
  - Дополнительно: кнопка «Загрузить» (Primary, small) + «Создать папку» (Secondary, small) — если permissions позволяют.

#### 5.4.2. Sidebar (левая панель внутри файлового менеджера)

- Ширина: `SIDEBAR_WIDTH` (240px).
- **Секция «Профили»:**
  - Заголовок: «Профили» (11px, weight 600, uppercase, `--fg-muted`, letter-spacing `0.05em`).
  - Список: каждый профиль — строка `ROW_HEIGHT`, padding `0 PADDING_TIGHT 0 PADDING_BASE`.
  - Иконка статуса (`8px` круг) + название профиля (13px, truncated).
  - Active профиль: фон `--accent-subtle`, левый border `2px solid --accent`.
  - Hover: `--bg-tertiary`.
- **Секция «Бакеты»:**
  - Заголовок: «Бакеты» (аналогично «Профили»).
  - Список бакетов выбранного профиля.
  - Active бакет: `--accent-subtle`, текст `--accent`.
  - Двойной клик или Enter — открыть бакет.
- **Секция «Быстрый доступ»:**
  - Закреплённые пути (префиксы), добавленные пользователем через ПКМ.

#### 5.4.3. Object List (табличный вид)

- **Header:**
  - Высота: `36px`, фон `--bg-secondary`, border-bottom: `1px solid --border`.
  - Колонки: Checkbox (ширина `36px`), Имя (flex: 3), Размер (flex: 1, right-aligned), Тип (flex: 1), Изменён (flex: 1), Действия (flex: 0, width `40px`).
  - Заголовки колонок: 12px, weight 600, `--fg-muted`, uppercase, letter-spacing `0.03em`.
  - Сортировка: клик по заголовку → сортировка по этой колонке (asc → desc → none). Индикатор: chevron up/down рядом с текстом.
- **Row:**
  - Высота: `ROW_HEIGHT`, border-bottom: `1px solid --border-subtle`.
  - Checkbox: выравнивание по центру ячейки.
  - Имя: иконка типа (`16px`, цвет по типу: папка `--accent`, изображение `--warning`, документ `--fg-secondary`) + текст (13px, `--fg-primary`). Папки: жирный текст, суффикс `/`.
  - Размер: 12px, `--fg-secondary`, моноширинный, human-readable (2.4 MB, 156 KB).
  - Тип: 12px, `--fg-muted`, truncated до 20 символов.
  - Изменён: 12px, `--fg-secondary`, формат «5 июл 2026, 14:32» или относительный («2 часа назад») в tooltip.
  - Действия: кнопка «⋯» (Icon-only), появляется только на hover строки (или всегда на touch). Dropdown: Скачать, Копировать URL, Получить presigned URL, Свойства, Удалить.
- **Hover:** фон `--bg-tertiary`.
- **Selected:** фон `--accent-subtle`, левый border `2px solid --accent`.
- **Drag-and-drop:**
  - При drag файлов извне: вся область списка получает overlay с рамкой `2px dashed --accent` и текстом «Отпустите файлы для загрузки».
  - При drag внутри: визуальный индикатор drop-target (папка подсвечивается).
- **Empty state:**
  - Центрированная иконка папки (`48px`, `--fg-muted`) + текст «Эта папка пуста» + кнопка «Загрузить файлы».
- **Loading state:**
  - Skeleton rows: 5 строк, каждая — серые пульсирующие прямоугольники (animation: pulse `1.5s infinite`).

#### 5.4.4. Object List (сеточный вид)

- Grid: `repeat(auto-fill, minmax(120px, 1fr))`, gap `12px`.
- Каждый элемент: вертикальный layout.
  - Превью/иконка: `64px × 64px`, центрирована. Изображения — thumbnail, файлы — иконка по типу.
  - Имя: 12px, weight 500, `--fg-primary`, max 2 lines, ellipsis.
  - Размер: 11px, `--fg-muted`.
- Hover: `--bg-tertiary`, border-radius `BORDER_RADIUS`.
- Selected: `--accent-subtle`, border `1px solid --accent`.

#### 5.4.5. Context Menu (ПКМ на объекте)

```
┌──────────────────────────────┐
│  Открыть / Предпросмотр      │
│  Скачать...                  │
│  Копировать URL              │
│  Получить presigned URL...   │
│  ─────────────────────────── │
│  Копировать                  │
│  Переместить...              │
│  Переименовать      (F2)     │
│  ─────────────────────────── │
│  Изменить метаданные...      │
│  ─────────────────────────── │
│  Удалить            (Delete)  │
└──────────────────────────────┘
```

- Для папок: «Открыть» вместо «Предпросмотр», «Скачать как ZIP».
- Для множественного выбора: «Скачать выбранные», «Удалить N объектов».

#### 5.4.6. Status Bar

- Высота: `28px`, фон `--bg-primary`, border-top: `1px solid --border`.
- Padding: `0 PADDING_BASE`.
- Left: количество объектов, общий размер, выбранные объекты (если есть).
- Right: индикаторы передач (количество активных), кнопка-переключатель нижней панели передач (chevron up/down).

---

### 5.5. Экран «Передачи» (Transfer Queue)

**Layout:**
```
┌─────────────────────────────────────────────────────────────────────────────┐
│ Title Bar: S3 Client  —  Передачи                                           │
├──────────┬──────────────────────────────────────────────────────────────────┤
│          │  Хедер: «Передачи»                                               │
│  Sidebar │  Tabs: [Активные (2)]  [Завершённые]  [Все]                     │
│          │  ─────────────────────────────────────────────────────────────   │
│          │  ┌──────────────────────────────────────────────────────────┐    │
│          │  │ [⏸] [✕]  Upload: image.png                               │    │
│          │  │          s3://bucket/images/  →  45%  12 MB/s  ETA 2m    │    │
│          │  │          ████████░░░░░░░░░  450 MB / 1 GB                │    │
│          │  └──────────────────────────────────────────────────────────┘    │
│          │  ┌──────────────────────────────────────────────────────────┐    │
│          │  │ [⏸] [✕]  Download: backup.zip                            │    │
│          │  │          s3://bucket/backups/  →  local/Downloads/       │    │
│          │  │          ████░░░░░░░░░░░░░  28%  8 MB/s  ETA 5m           │    │
│          │  └──────────────────────────────────────────────────────────┘    │
│          │                                                                   │
│          │  [⏸ Пауза все]  [▶ Возобновить все]  [✕ Отменить все]        │
│          │                                                                   │
├──────────┴──────────────────────────────────────────────────────────────────┤
│ Status Bar: 2 активных передачи                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

**Детали:**
- Tabs: горизонтальный список, active tab — border-bottom `2px solid --accent`, текст `--accent`. Hover: `--bg-tertiary`.
- **Карточка передачи:**
  - Фон: `--bg-secondary`, border: `1px solid --border`, border-radius: `BORDER_RADIUS`.
  - Padding: `PADDING_BASE`.
  - Top row: кнопки управления (Pause/Resume `20px`, Cancel `20px`) + тип операции (Upload/Download, тег цветом: upload `--accent`, download `--success`) + имя файла (14px, weight 500, truncated).
  - Middle row: путь источника → путь назначения (12px, `--fg-secondary`, моноширинный, truncated).
  - Bottom row: процент (13px, weight 600), скорость (12px, `--fg-secondary`), ETA (12px, `--fg-secondary`).
  - Progress bar: `8px` height, `--accent` для upload, `--success` для download. Анимация width `300ms`.
  - Hover: `border-color: --accent`.
- **Групповые действия:** под списком, Secondary кнопки. Disabled если нет подходящих задач.
- **Завершённые:** аналогичные карточки, но без progress bar. Статус: галочка зелёная (success), крестик красный (failed), текст причины ошибки (12px, `--danger`).
- **Empty state:** иконка «Всё чисто» + текст «Нет активных передач».

---

### 5.6. Экран «История»

- Аналогичен «Завершённые» в передачах, но таблица вместо карточек.
- Колонки: Дата/время, Тип, Имя файла, Размер, Статус, Профиль, Бакет.
- Сортировка по дате (desc по умолчанию).
- Фильтры: по типу (Upload/Download), по статусу, по профилю, по дате (range picker).
- Пагинация: 50 записей на страницу.
- Кнопка «Очистить историю» (Danger, small) в хедере.

---

### 5.7. Экран «Настройки»

**Layout:**
```
┌─────────────────────────────────────────────────────────────────────────────┐
│ Title Bar: S3 Client  —  Настройки                                          │
├──────────┬──────────────────────────────────────────────────────────────────┤
│          │  Sidebar навигация по настройкам (200px)                         │
│  Sidebar │  ┌────────────────────────┐  ┌────────────────────────────────┐  │
│  (240px) │  │ Общие                  │  │  Тема оформления               │  │
│          │  │ Внешний вид            │  │  [○ Системная] [○ Светлая]    │  │
│  [Лого]  │  │ Передачи               │  │  [● Тёмная]                    │  │
│  ──────  │  │ Безопасность           │  │                                 │  │
│  Профили │  │ Сетевые                │  │  Язык интерфейса               │  │
│  Передачи│  │ О приложении           │  │  [Русский ▼]                   │  │
│  История │  └────────────────────────┘  │                                 │  │
│  Настройки│                             │  Поведение                     │  │
│          │                             │  [✓] Свернуть в трей при закрытии│  │
│          │                             │  [✓] Автовозобновление передач   │  │
│          │                             │  [ ] Автоблокировка при простое  │  │
│          │                             │                                 │  │
│          │                             │  [Сохранить изменения]          │  │
│          │                             └────────────────────────────────┘  │
├──────────┴──────────────────────────────────────────────────────────────────┤
│ Status Bar: готов                                                           │
└─────────────────────────────────────────────────────────────────────────────┘
```

**Sidebar настроек:**
- Ширина: `200px`, фон `--bg-secondary`, border-right: `1px solid --border`.
- Пункты: «Общие», «Внешний вид», «Передачи», «Безопасность», «Сетевые», «О приложении».
- Active: фон `--accent-subtle`, текст `--accent`, левый border `2px solid --accent`.
- Hover: `--bg-tertiary`.

**Контентная область:**
- Padding: `PADDING_BASE`.
- Заголовок секции: 18px, weight 600, `--fg-primary`, margin-bottom `PADDING_BASE`.
- Группы настроек: разделены `1px solid --border-subtle`, padding `PADDING_BASE 0`.
- Label: 13px, weight 500, `--fg-secondary`, margin-bottom `6px`.
- Описание под полем: 12px, `--fg-muted`, margin-top `4px`.
- Radio buttons: горизонтальная группа, custom styled (круг `16px`, selected: inner dot `8px` `--accent`).
- Checkboxes: стандартные (см. 4.5).
- Sliders: для числовых значений (лимит скорости, размер чанка). Трек: `4px`, `--bg-tertiary`; заполнение: `--accent`; thumb: `16px` круг, `--accent`, shadow.

**Секции:**
- **Общие:** язык, поведение при закрытии, автовозобновление, автоблокировка.
- **Внешний вид:** тема, масштаб интерфейса (90%, 100%, 110%, 125%), шрифт (system, monospace).
- **Передачи:** лимит одновременных передач (slider 1–10), размер чанка (radio: 5MB, 16MB, 64MB, 128MB, adaptive), лимит скорости (input MB/s или unlimited), retry attempts (1–10), таймаут соединения (input секунд).
- **Безопасность:** мастер-пароль (установить/сменить/удалить), шифрование credentials (toggle), очистка кэша, очистка истории.
- **Сетевые:** HTTP-прокси (none / system / custom: host, port, user, pass), SOCKS5, таймауты.
- **О приложении:** версия, лицензия, ссылки на GitHub, проверка обновлений (кнопка + статус).

---

### 5.8. Модальные окна (дополнительные)

#### 5.8.1. «Удаление объектов»
- Размер: `400px`.
- Иконка: предупреждение (`--danger`, `32px`).
- Текст: «Вы уверены, что хотите удалить N объектов?»
- Список: scrollable, max-height `200px`, имена файлов (13px).
- Кнопки: «Отмена» (Secondary) + «Удалить» (Danger). Focus на «Отмена» по умолчанию.
- Checkbox: «[ ] Не спрашивать снова для этой сессии» (если реализуется).

#### 5.8.2. «Presigned URL»
- Размер: `480px`.
- Поле: read-only input с сгенерированным URL (моноширинный, 12px).
- Кнопка «Копировать» (Primary) рядом с полем.
- Slider: время жизни (1 мин – 7 дней, с подписями).
- Кнопка «Сгенерировать заново» (Secondary) — если менялся срок.

#### 5.8.3. «Свойства объекта»
- Размер: `520px`.
- Таблица key-value: Имя, Размер, Тип, ETag, Последнее изменение, Storage Class, Владелец.
- Секция «Метаданные»: editable key-value (Content-Type, Cache-Control, x-amz-meta-*).
- Кнопки: «Сохранить» (Primary) + «Отмена» (Secondary).

#### 5.8.4. «Прогресс массовой операции»
- Не модальное, а inline overlay поверх списка.
- Top bar: «Удаление 150 объектов…» + progress bar + [Отмена].
- При завершении: auto-hide через 3s или переход в toast.

---

## 6. Анимации и переходы

| Элемент | Тип | Параметры |
|---------|-----|-----------|
| Hover строки | Background | `background-color TRANSITION_FAST` |
| Hover кнопки | Background + Scale | `background TRANSITION_FAST`, `transform: scale(0.97)` on active |
| Modal overlay | Fade | `opacity 0 → 1, 150ms ease-out` |
| Modal content | Slide + Fade | `opacity + translateY(8px → 0), 200ms ease-out` |
| Dropdown | Scale + Fade | `opacity + scale(0.95 → 1), 150ms ease-out` |
| Context menu | Scale + Fade | `opacity + scale(0.95 → 1), 150ms ease-out` |
| Toast | Slide | `translateX(100% → 0), 250ms ease-out` |
| Tab switch | Fade content | `opacity 150ms` |
| Sidebar resize | Width | `transition: width 200ms ease-out` (если collapsible) |
| Progress bar | Width | `width 300ms linear` |
| Skeleton | Pulse | `opacity 0.4 → 0.8, 1.5s infinite ease-in-out` |
| Page transition | Fade | `opacity 100ms` (мгновенно, desktop-приложение) |
| Drag overlay | Border + Background | `border-color + background-color, 200ms` |
| Search expand | Width | `width 200px → 280px, TRANSITION_FAST` |
| Collapsible section | Height | `max-height 0 → auto, TRANSITION_NORMAL` |

---

## 7. Responsive и адаптивность (Desktop-specific)

- **Минимальное разрешение:** `1024 × 768`. При меньшем: горизонтальный скролл или compact mode (sidebar collapsible, убрать некоторые колонки).
- **Sidebar:** можно скрыть кнопкой (hamburger) или сочетанием `Ctrl/Cmd + B`. При скрытии: overlay при наведении или полное скрытие с кнопкой вызова.
- **Bottom panel:** resizable через drag handle (3px line, `--border`, cursor `ns-resize`). Double-click — toggle collapse/expand.
- **Table columns:** пользователь может менять ширину (drag на границе заголовка). Настройки ширин сохраняются.
- **Масштаб интерфейса:** 90%, 100%, 110%, 125%. Применяется через CSS `zoom` или rem-переменные.

---

## 8. Иконографика

**Библиотека:** `lucide-react` (единый стиль, тонкие линии, 1.5px stroke).

**Ключевые иконки:**

| Иконка | Назначение | Размер |
|--------|-----------|--------|
| `Cloud` | Логотип, профили | 24px |
| `CloudArrowUp` | Upload, drag-and-drop | 16–48px |
| `CloudArrowDown` | Download | 16px |
| `Folder` | Папки в списке | 16px |
| `File` | Общий файл | 16px |
| `FileImage`, `FileText`, `FileArchive`, `FileCode` | Типы файлов | 16px |
| `HardDrive` | Локальные пути | 16px |
| `Server` | Endpoint | 16px |
| `Globe` | Регион | 16px |
| `Key` | Credentials | 16px |
| `Lock` | Безопасность | 16px |
| `Settings` | Настройки | 16px |
| `History` | История | 16px |
| `Play`, `Pause`, `Square` | Управление передачами | 16–20px |
| `RotateCcw` | Обновить, retry | 16px |
| `Trash2` | Удалить | 16px |
| `Copy` | Копировать | 16px |
| `ExternalLink` | Открыть URL | 16px |
| `Search` | Поиск | 16px |
| `ChevronRight`, `ChevronDown` | Раскрытие, breadcrumbs | 12–16px |
| `MoreHorizontal` | Дополнительные действия | 16px |
| `Check` | Успех, checkbox | 12–16px |
| `X` | Закрыть, отмена, ошибка | 14–16px |
| `AlertTriangle` | Предупреждение | 16–32px |
| `Info` | Информация | 16px |
| `Eye`, `EyeOff` | Показать/скрыть пароль | 16px |
| `Link` | Presigned URL | 16px |
| `Gauge` | Скорость, лимиты | 16px |
| `Shield` | Безопасность | 16px |
| `Plug` | Подключение | 16px |
| `Unplug` | Отключение | 16px |

---

## 9. Состояния и обратная связь

### 9.1. Загрузка (Loading)
- **Список объектов:** skeleton rows (5 штук), пульсирующие `--bg-tertiary`.
- **Превью:** placeholder с иконкой типа + spinner (`16px`, `--accent`, вращение).
- **Кнопка:** текст заменяется на spinner + label (например, «Сохранение…»).
- **Глобальная:** overlay с полупрозрачным фоном + центрированный spinner (только для блокирующих операций > 500ms).

### 9.2. Пустое состояние (Empty)
- Центрированный layout: иконка (`48px`, `--fg-muted`) + заголовок (16px, `--fg-primary`) + описание (13px, `--fg-secondary`) + action-кнопка (если применимо).
- Примеры: «Нет подключений», «Эта папка пуста», «Нет результатов поиска», «Нет завершённых передач».

### 9.3. Ошибка (Error)
- **Inline:** красный текст под полем/элементом (12px, `--danger`).
- **Toast:** для фоновых ошибок (невалидный credentials, таймаут скачивания).
- **Modal:** для блокирующих ошибок (не удалось подключиться, конфликт).
- **Retry:** для recoverable ошибок — кнопка «Повторить» рядом с сообщением.
- **Empty с ошибкой:** иконка `AlertTriangle` (`--danger`) + текст ошибки + кнопка retry.

### 9.4. Успех (Success)
- **Toast:** зелёный бордюр, краткое сообщение («Загрузка завершена», «Подключение сохранено»).
- **Inline:** зелёная галочка рядом с полем (например, после теста подключения).
- **Progress:** полная заливка progress bar + статус «Завершено».

---

## 10. Accessibility (a11y)

- **Контраст:** все текстовые пары проходят WCAG AA (4.5:1 для normal, 3:1 для large).
- **Focus:** видимый outline `2px solid --accent` на всех интерактивных элементах. Outline-offset `2px`.
- **Keyboard navigation:**
  - `Tab` / `Shift+Tab` — перемещение между элементами.
  - `Enter` / `Space` — активация.
  - `Arrow keys` — навигация в списках, таблицах, dropdowns.
  - `Esc` — закрытие модалок, dropdowns, context menus.
  - `Ctrl/Cmd + A` — выделить все в списке.
- **Screen reader:** все иконки имеют `aria-label`, все кнопки — `aria-label` или visible text, таблицы — семантические `<table>` с `<th scope="col">`.
- **Reduced motion:** при `prefers-reduced-motion: reduce` — все анимации отключаются (transition: none).

---

## 11. Tailwind CSS конфигурация

```javascript
// tailwind.config.ts
export default {
  darkMode: 'class', // переключение через class на html
  theme: {
    extend: {
      colors: {
        bg: {
          primary: 'var(--bg-primary)',
          secondary: 'var(--bg-secondary)',
          tertiary: 'var(--bg-tertiary)',
          elevated: 'var(--bg-elevated)',
        },
        fg: {
          primary: 'var(--fg-primary)',
          secondary: 'var(--fg-secondary)',
          muted: 'var(--fg-muted)',
        },
        accent: {
          DEFAULT: 'var(--accent)',
          hover: 'var(--accent-hover)',
          subtle: 'var(--accent-subtle)',
        },
        success: 'var(--success)',
        warning: 'var(--warning)',
        danger: {
          DEFAULT: 'var(--danger)',
          hover: 'var(--danger-hover)',
        },
        border: {
          DEFAULT: 'var(--border)',
          subtle: 'var(--border-subtle)',
        },
      },
      fontFamily: {
        sans: ['-apple-system', 'BlinkMacSystemFont', 'Segoe UI', 'Roboto', 'Helvetica Neue', 'Arial', 'sans-serif'],
        mono: ['JetBrains Mono', 'SF Mono', 'Menlo', 'Monaco', 'monospace'],
      },
      fontSize: {
        '2xs': '11px',
      },
      spacing: {
        'sidebar': '240px',
        'header': '48px',
        'bottom-panel': '180px',
        'row': '36px',
      },
      borderRadius: {
        DEFAULT: '6px',
        sm: '4px',
      },
      transitionDuration: {
        fast: '150ms',
        normal: '250ms',
      },
      animation: {
        'pulse-slow': 'pulse 1.5s ease-in-out infinite',
      },
    },
  },
  plugins: [],
}
```

**CSS Variables (globals.css):**
```css
@layer base {
  :root {
    --bg-primary: #0f172a;
    --bg-secondary: #1e293b;
    --bg-tertiary: #334155;
    --bg-elevated: #1e293b;
    --fg-primary: #f8fafc;
    --fg-secondary: #94a3b8;
    --fg-muted: #64748b;
    --accent: #3b82f6;
    --accent-hover: #2563eb;
    --accent-subtle: rgba(59, 130, 246, 0.15);
    --success: #22c55e;
    --warning: #f59e0b;
    --danger: #ef4444;
    --danger-hover: #dc2626;
    --border: #334155;
    --border-subtle: #1e293b;
  }

  .light {
    --bg-primary: #f8fafc;
    --bg-secondary: #ffffff;
    --bg-tertiary: #f1f5f9;
    --bg-elevated: #ffffff;
    --fg-primary: #0f172a;
    --fg-secondary: #475569;
    --fg-muted: #94a3b8;
    --accent: #2563eb;
    --accent-hover: #1d4ed8;
    --accent-subtle: rgba(37, 99, 235, 0.10);
    --success: #16a34a;
    --warning: #d97706;
    --danger: #dc2626;
    --danger-hover: #b91c1c;
    --border: #e2e8f0;
    --border-subtle: #f1f5f9;
  }
}
```

---

## 12. Файловая структура компонентов (рекомендация)

```
src/
├── components/
│   ├── ui/                    # Базовые UI-компоненты
│   │   ├── Button.tsx
│   │   ├── Input.tsx
│   │   ├── Select.tsx
│   │   ├── Checkbox.tsx
│   │   ├── Modal.tsx
│   │   ├── Toast.tsx
│   │   ├── Tooltip.tsx
│   │   ├── ProgressBar.tsx
│   │   ├── ContextMenu.tsx
│   │   ├── Skeleton.tsx
│   │   └── Icon.tsx           # Обертка над lucide-react
│   ├── layout/
│   │   ├── TitleBar.tsx       # Wails title bar (если кастомный)
│   │   ├── Sidebar.tsx
│   │   ├── Toolbar.tsx
│   │   ├── StatusBar.tsx
│   │   └── BottomPanel.tsx    # Resizable transfer panel
│   ├── connection/
│   │   ├── ConnectionCard.tsx
│   │   ├── ConnectionForm.tsx
│   │   └── ConnectionList.tsx
│   ├── file-manager/
│   │   ├── FileList.tsx         # Таблица / сетка
│   │   ├── FileRow.tsx
│   │   ├── FileGridItem.tsx
│   │   ├── FileIcon.tsx         # По MIME-типу
│   │   ├── Breadcrumbs.tsx
│   │   ├── ObjectPreview.tsx
│   │   └── EmptyState.tsx
│   ├── transfer/
│   │   ├── TransferCard.tsx
│   │   ├── TransferQueue.tsx
│   │   ├── TransferHistory.tsx
│   │   └── TransferProgress.tsx
│   └── settings/
│       ├── SettingsSidebar.tsx
│       ├── SettingsSection.tsx
│       └── SettingsForm.tsx
├── screens/
│   ├── WelcomeScreen.tsx
│   ├── ConnectionsScreen.tsx
│   ├── FileManagerScreen.tsx
│   ├── TransfersScreen.tsx
│   ├── HistoryScreen.tsx
│   └── SettingsScreen.tsx
├── hooks/
│   ├── useWailsBindings.ts
│   ├── useWailsEvents.ts
│   ├── useTheme.ts
│   ├── useKeyboardShortcuts.ts
│   └── useDragAndDrop.ts
├── stores/
│   ├── useAppStore.ts         # Zustand: theme, settings, sidebar state
│   ├── useConnectionStore.ts
│   ├── useFileManagerStore.ts
│   └── useTransferStore.ts
├── lib/
│   ├── wails.ts               # Runtime API wrapper
│   ├── utils.ts               # cn(), formatters
│   └── constants.ts           # Layout constants, colors
├── types/
│   └── index.ts               # TypeScript interfaces (DTO из Go)
├── App.tsx
└── main.tsx
```
