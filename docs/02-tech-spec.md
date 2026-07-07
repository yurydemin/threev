# Техническое задание (ТЗ)
## Проект: «S3 Desktop Client»
## Версия: 1.0
## Дата: 2026-07-05

---

### 1. Цель и область применения

Разработка кросс-платформенного десктопного приложения для работы с S3-совместимыми объектными хранилищами. Приложение должно обеспечивать высокую скорость передачи данных, устойчивость к нестабильным сетевым соединениям и нативный пользовательский опыт при минимальном потреблении системных ресурсов.

**Целевые платформы:** Windows 10/11, macOS 12+, Linux (Ubuntu 22.04+, Fedora).

---

### 2. Технологический стек

| Компонент | Технология | Версия | Обоснование |
|-----------|-----------|--------|-------------|
| Backend runtime | Go | 1.22+ | Высокая производительность сетевых операций, эффективный параллелизм goroutines |
| Desktop framework | Wails v2 | 2.8+ | Нативный WebView, лёгкие бинарники, IPC между Go и JS |
| Frontend | React + TypeScript | 18+ | Типобезопасность, богатая экосистема UI-компонентов |
| State management | Zustand | 4+ | Лёгкое управление состоянием без бойлерплейта |
| UI Kit | Tailwind CSS + Headless UI | 3.4+ | Кастомизируемые accessible компоненты |
| Локальное хранилище | SQLite (через `modernc.org/sqlite` или `go-sqlite3`) | 3 | Профили, история, кэш метаданных |
| S3 SDK | `aws-sdk-go-v2` (service/s3) | 1.30+ | Официальный SDK, поддержка всех S3-фич |
| HTTP-клиент | `net/http` (стандартный) + `aws-sdk-go-v2` transport | — | Контроль над connection pool, timeouts |
| Сборка | Wails CLI | 2.8+ | Кросс-компиляция под все платформы |

---

### 3. Общая архитектура

Приложение следует паттерну **Cloisonné** (разделение UI и бизнес-логики):

```
┌─────────────────────────────────────────────┐
│  Frontend (React + TypeScript)              │
│  ├─ UI Components (Tailwind + Headless UI)   │
│  ├─ State Management (Zustand)               │
│  └─ Wails Runtime API (автогенерация)      │
├─────────────────────────────────────────────┤
│  Wails Bridge (IPC)                          │
│  ├─ Go → JS: Events (прогресс, ошибки)     │
│  └─ JS → Go: Bindings (вызов методов)       │
├─────────────────────────────────────────────┤
│  Go Backend                                  │
│  ├─ API Layer (Wails bind)                 │
│  ├─ S3 Engine                                │
│  │  ├─ Connection Manager                    │
│  │  ├─ Transfer Scheduler                    │
│  │  ├─ Retry & Circuit Breaker             │
│  │  └─ Multipart / Range Handler           │
│  ├─ Local Storage (SQLite)                   │
│  └─ File System Adapter                      │
└─────────────────────────────────────────────┘
```

**Принципы:**
- Backend (Go) является единственным источником истины для всех операций с сетью и ФС.
- Frontend не выполняет прямых HTTP-запросов к S3. Все вызовы идут через Wails Bindings.
- Длительные операции (передачи) используют Wails Events для пуш-уведомлений о прогрессе.
- Исключение: frontend может открывать **presigned URLs** нативно в WebView для предпросмотра объектов (изображения, PDF), поскольку presigned URL не содержит credentials и является самодостаточным.

---

### 4. Функциональные требования

#### 4.1. Модуль управления подключениями (Connections)

**FR-CONN-001:** Приложение должно поддерживать хранение произвольного количества профилей подключений к S3-совместимым хранилищам.

**FR-CONN-002:** Профиль подключения содержит:
- Название профиля (уникальное, задаваемое пользователем)
- Endpoint URL (HTTP/HTTPS)
- Регион (строка, по умолчанию `us-east-1`)
- Access Key ID
- Secret Access Key
- Session Token (опционально, для STS)
- Стиль URL: `Virtual Hosted` (default) или `Path Style`
- Проверка SSL-сертификата (bool, default: true)
- Кастомные заголовки (опционально, key-value)

**FR-CONN-003:** При сохранении профиля выполняется валидация:
- Проверка формата URL.
- Проверка доступности endpoint (HEAD-запрос к корню или `ListBuckets`, если permissions позволяют).
- Проверка корректности credentials (вызов `ListBuckets` или `GetCallerIdentity` если доступно).

**FR-CONN-004:** Credentials хранятся в SQLite с шифрованием на уровне приложения (AES-256-GCM, ключ derived из machine-specific seed + user passphrase, если задана).

**FR-CONN-005:** Поддержка IAM-ролей (Instance Metadata Service) для запуска внутри AWS. Опционально.

**FR-CONN-006:** Пользователь может дублировать, редактировать и удалять профили.

#### 4.2. Модуль файлового менеджера (File Manager)

**FR-FM-001:** Интерфейс отображает список бакетов для выбранного профиля.

**FR-FM-002:** Навигация по объектам внутри бакета с поддержкой префиксов (логических «папок»).

**FR-FM-003:** Отображение колонок: имя, размер (human-readable), тип MIME, дата последнего изменения, storage class.

**FR-FM-004:** Сортировка по любой колонке (клик по заголовку). Сортировка выполняется на стороне backend для текущей «страницы» видимых объектов.

**FR-FM-005:** Пагинация или виртуальный скролл для бакетов с > 1000 объектов. Backend использует `ListObjectsV2` с `MaxKeys=1000` и кэширует результат локально на время сессии.

**FR-FM-006:** Поиск по префиксу внутри бакета (фильтрация на стороне клиента по уже загруженным данным; если нет — запрос к S3 с `Prefix`).

**FR-FM-007:** Предпросмотр объектов:
- Изображения: отображение thumbnail (предварительная загрузка через `GetObject` с лимитом размера или presigned URL).
- Текстовые файлы: отображение первых 100 КБ.
- PDF: отображение через нативный WebView (загрузка presigned URL).

**FR-FM-008:** Drag-and-drop для загрузки файлов и папок из системного файлового менеджера в окно приложения.

**FR-FM-009:** Контекстное меню (ПКМ) для объектов: скачать, удалить, копировать URL, получить presigned URL, изменить метаданные.

**FR-FM-010:** Отображение текущего пути (breadcrumbs) с возможностью быстрого перехода.

#### 4.3. Модуль передачи данных (Transfer)

**FR-TR-001:** Загрузка (Upload):
- Поддержка одиночных файлов и папок (рекурсивная загрузка).
- Автоматическое определение MIME-типа по расширению.
- Автоматический multipart upload для файлов > 5 МБ.
- Параллельная загрузка частей (parts) через goroutines.
- Размер части: адаптивный (минимум 5 МБ, максимум 128 МБ, по умолчанию 16 МБ).
- Конкурентность: количество одновременных частей регулируется (default: 4, max: 32).

**FR-TR-002:** Скачивание (Download):
- Поддержка одиночных объектов и префиксов (рекурсивное скачивание как zip или в развёрнутом виде).
- Параллельное скачивание через HTTP Range requests.
- Размер Range: адаптивный (default: 16 МБ).
- Конкурентность: default 4, max 32.

**FR-TR-003:** Докачка (Resume):
- При прерывании upload: сохранение `UploadId` и списка загруженных частей. Возможность возобновления из очереди.
- При прерывании download: сохранение текущего смещения в файл. Возобновление через `Range: bytes=offset-`.

**FR-TR-004:** Integrity:
- Проверка ETag после загрузки/скачивания.
- Для multipart: проверка составного ETag (`md5(parts)`).

**FR-TR-005:** Просчёт прогресса:
- Для каждой операции: bytes transferred, total bytes, скорость (скользящее среднее за 5 сек), ETA.
- События публикуются через Wails Events каждые 500 мс или при изменении > 1%.

**FR-TR-006:** Ограничение скорости (Bandwidth limiter): пользователь может задать лимит upload/download в МБ/с. Реализация через token bucket.

#### 4.4. Модуль массовых операций (Bulk Operations)

**FR-BULK-001:** Множественный выбор объектов (чекбоксы, Ctrl+A, Shift+click).

**FR-BULK-002:** Массовое удаление с подтверждающим диалогом, отображающим количество объектов.

**FR-BULK-003:** Копирование/перемещение объектов внутри хранилища (CopyObject + DeleteObject). Поддержка копирования между бакетами одного профиля.

**FR-BULK-004:** Изменение метаданных: Content-Type, Cache-Control, кастомные user-metadata (x-amz-meta-*).

**FR-BULK-005:** Генерация presigned URL для выбранного объекта с настраиваемым временем жизни (1 мин – 7 дней).

#### 4.5. Модуль очереди передач (Transfer Queue)

**FR-QUEUE-001:** Все передачи помещаются в централизованную очередь.

**FR-QUEUE-002:** Состояния задачи: Pending, Running, Paused, Completed, Failed, Cancelled.

**FR-QUEUE-003:** Управление очередью:
- Пауза/возобновление отдельной задачи или всей очереди.
- Отмена задачи (с корректным освобождением ресурсов).
- Перемещение задачи в очереди (изменение приоритета).
- Повторный запуск failed-задач (retry).

**FR-QUEUE-004:** Параллельность: максимум N одновременных передач (настраивается, default 2).

**FR-QUEUE-005:** Сохранение очереди в SQLite при закрытии приложения. Возобновление pending-задач при перезапуске (опционально, настраивается).

**FR-QUEUE-006:** История передач: лог всех завершённых задач с результатом, датой, размером.

#### 4.6. Модуль настроек и безопасности (Settings)

**FR-SET-001:** Настройки приложения:
- Тема: System / Light / Dark.
- Язык интерфейса: русский, английский (i18n через react-i18next).
- Лимит одновременных передач.
- Размер части по умолчанию.
- Поведение при закрытии: свернуть в трей / выйти.
- Автоматическое возобновление передач при старте.

**FR-SET-002:** Безопасность:
- Опциональное шифрование хранилища credentials (мастер-пароль при первом запуске).
- Автоблокировка интерфейса при бездействии (опционально).
- Очистка истории.

---

### 5. Нефункциональные требования

**NFR-001:** Приложение должно запускаться на целевых платформах без установки дополнительных runtime (включая .NET, JRE, Python).

**NFR-002:** Размер установочного пакета не более 50 МБ (сжатый).

**NFR-003:** Потребление RAM в состоянии простоя не более 150 МБ.

**NFR-004:** Потребление RAM во время активной передачи: не более 300 МБ (с учётом буферов и goroutines).

**NFR-005:** Время запуска (от клика до отрисовки UI) не более 2 секунд на SSD, не более 5 секунд на HDD.

**NFR-006:** Приложение должно корректно работать при обрыве соединения и возобновлять передачу без потери данных.

**NFR-007:** Поддержка прокси: HTTP, HTTPS, SOCKS5 (системные настройки + кастомные).

**NFR-008:** Логирование: ротация логов, уровни (Error, Warn, Info, Debug). Логи хранятся локально, не отправляются наружу.

**NFR-009:** Обновление приложения: механизм проверки обновлений через GitHub Releases (опционально, авто или ручное).

---

### 6. Требования к производительности

**PERF-001:** Скорость загрузки (upload) больших файлов (> 1 ГБ) должна быть не ниже 80% от пропускной способности канала при стабильном соединении.

**PERF-002:** Скорость скачивания (download) больших файлов должна быть не ниже 80% от пропускной способности канала.

**PERF-003:** Листинг 1000 объектов в бакете не должен занимать более 3 секунд (при условии latency < 100 мс до хранилища).

**PERF-004:** UI остаётся отзывчивым (60 FPS) во время активных передач. Операции интерфейса (навигация, сортировка) не блокируются сетевыми операциями.

**PERF-005:** При передаче 10 000 мелких файлов (< 1 МБ) приложение должно использовать batching или высокую конкурентность для минимизации overhead.

---

### 7. Требования к UI/UX

**UX-001:** Интерфейс файлового менеджера следует паттернам Finder (macOS) и Explorer (Windows): двухпанельный режим (опционально) или однопанельный с боковой панелью профилей.

**UX-002:** Drag-and-drop: поддержка перетаскивания файлов из системного файлового менеджера в окно приложения (upload) и из приложения в системный менеджер (download, через временные файлы или URI scheme).

**UX-003:** Прогресс передач: нижняя панель или отдельное окно с отображением текущих задач, скорости, ETA, возможностью паузы/отмены.

**UX-004:** Горячие клавиши:
- Ctrl/Cmd + N: новое подключение
- Ctrl/Cmd + D: удалить выбранные объекты
- Ctrl/Cmd + R: обновить листинг
- Ctrl/Cmd + A: выбрать все
- Delete: удалить
- F2 или Enter: переименовать (если поддерживается хранилищем)

**UX-005:** Тёмная и светлая тема с автоматическим определением системной темы.

**UX-006:** Тултипы, подтверждающие диалоги для деструктивных операций, индикаторы загрузки (skeleton screens для листинга).

**UX-007:** Обработка ошибок: пользовательские сообщения вместо технических stack trace. Возможность скопировать technical details.

---

### 8. Модель данных (SQLite)

#### 8.1. Таблица `profiles`
```sql
id INTEGER PRIMARY KEY AUTOINCREMENT,
name TEXT NOT NULL UNIQUE,
endpoint_url TEXT NOT NULL,
region TEXT NOT NULL DEFAULT 'us-east-1',
access_key_id TEXT NOT NULL,
secret_access_key TEXT NOT NULL,
session_token TEXT,
path_style BOOLEAN DEFAULT 0,
verify_ssl BOOLEAN DEFAULT 1,
custom_headers TEXT, -- JSON
created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
```

#### 8.2. Таблица `transfer_queue`
```sql
id INTEGER PRIMARY KEY AUTOINCREMENT,
profile_id INTEGER NOT NULL,
type TEXT NOT NULL CHECK(type IN ('upload', 'download')),
source_path TEXT NOT NULL,
destination_path TEXT NOT NULL,
status TEXT NOT NULL CHECK(status IN ('pending', 'running', 'paused', 'completed', 'failed', 'cancelled')),
total_bytes INTEGER DEFAULT 0,
transferred_bytes INTEGER DEFAULT 0,
error_message TEXT,
multipart_upload_id TEXT, -- для resume upload
parts_completed TEXT, -- JSON array of completed part numbers
file_offset INTEGER DEFAULT 0, -- для resume download
priority INTEGER DEFAULT 0,
created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
FOREIGN KEY (profile_id) REFERENCES profiles(id)
```

#### 8.3. Таблица `transfer_history`
```sql
id INTEGER PRIMARY KEY AUTOINCREMENT,
queue_id INTEGER,
profile_id INTEGER NOT NULL,
type TEXT NOT NULL,
source_path TEXT NOT NULL,
destination_path TEXT NOT NULL,
total_bytes INTEGER,
status TEXT,
completed_at TIMESTAMP,
error_message TEXT
```

#### 8.4. Таблица `settings`
```sql
key TEXT PRIMARY KEY,
value TEXT
```

---

### 9. API Frontend-Backend (Wails Runtime)

Wails автоматически экспонирует методы Go-структур через JavaScript. Все методы — асинхронные (Promise).

#### 9.1. ConnectionService

```go
type ConnectionService struct {}

// Возвращает список профилей (без секретов)
func (c *ConnectionService) GetProfiles() ([]ProfileDTO, error)

// Возвращает полный профиль с расшифровкой (только для редактирования)
func (c *ConnectionService) GetProfile(id int64) (Profile, error)

// Сохраняет/обновляет профиль. Выполняет валидацию подключения.
func (c *ConnectionService) SaveProfile(p Profile) (Profile, error)

// Удаляет профиль
func (c *ConnectionService) DeleteProfile(id int64) error

// Тестирует подключение
func (c *ConnectionService) TestConnection(p Profile) (ConnectionTestResult, error)
```

#### 9.2. FileManagerService

```go
type FileManagerService struct {}

// Листинг бакетов
func (f *FileManagerService) ListBuckets(profileId int64) ([]Bucket, error)

// Листинг объектов с пагинацией
func (f *FileManagerService) ListObjects(req ListObjectsRequest) (ListObjectsResponse, error)

// Получение метаданных объекта
func (f *FileManagerService) HeadObject(profileId int64, bucket, key string) (ObjectMeta, error)

// Удаление объекта/ов
func (f *FileManagerService) DeleteObjects(profileId int64, bucket string, keys []string) error

// Копирование/перемещение
func (f *FileManagerService) CopyObject(req CopyObjectRequest) error
func (f *FileManagerService) MoveObject(req MoveObjectRequest) error

// Presigned URL
func (f *FileManagerService) GetPresignedURL(profileId int64, bucket, key string, expirySeconds int64) (string, error)

// Изменение метаданных
func (f *FileManagerService) UpdateMetadata(profileId int64, bucket, key string, metadata map[string]string) error
```

#### 9.3. TransferService

```go
type TransferService struct {}

// Добавляет задачу в очередь. Возвращает ID задачи.
func (t *TransferService) QueueUpload(req UploadRequest) (int64, error)
func (t *TransferService) QueueDownload(req DownloadRequest) (int64, error)

// Управление очередью
func (t *TransferService) PauseTask(id int64) error
func (t *TransferService) ResumeTask(id int64) error
func (t *TransferService) CancelTask(id int64) error
func (t *TransferService) RetryTask(id int64) (int64, error)
func (t *TransferService) ReorderTask(id int64, newPriority int) error

// Получение списка задач
func (t *TransferService) GetQueue() ([]TransferTask, error)
func (t *TransferService) GetHistory(limit int) ([]TransferHistory, error)

// Очистка истории
func (t *TransferService) ClearHistory() error
```

#### 9.4. SettingsService

```go
type SettingsService struct {}

func (s *SettingsService) GetSettings() (AppSettings, error)
func (s *SettingsService) SaveSettings(settings AppSettings) error
func (s *SettingsService) SetMasterPassword(password string) error
func (s *SettingsService) Unlock(password string) (bool, error)
```

#### 9.5. Events (Go → Frontend)

Wails Events для пуш-уведомлений:

```go
// Прогресс передачи
type TransferProgressEvent struct {
    TaskId int64
    TransferredBytes int64
    TotalBytes int64
    SpeedBytesPerSec float64
    ETASeconds int64
    Status string // running, completed, failed
    Error string // если failed
}

// Обновление листинга (при изменениях)
type ObjectChangeEvent struct {
    Bucket string
    Prefix string
    Type string // create, delete
}
```

---

### 10. Архитектура S3 Engine (Go)

#### 10.1. Connection Manager

- Создаёт `aws.Config` для каждого профиля с кастомным HTTP transport.
- **HTTP Transport настройки**:
  - `MaxIdleConns`: 100
  - `MaxIdleConnsPerHost`: 32
  - `IdleConnTimeout`: 90s
  - `TLSHandshakeTimeout`: 10s
  - `ExpectContinueTimeout`: 1s
  - `ResponseHeaderTimeout`: 30s (адаптивный, см. 10.3)
- **DNS-кэширование**: кастомный RoundTripper или использование `net.Resolver` с кэшем. Приоритет: кэшировать все IP из DNS-ответа и распределять соединения round-robin.
- **Path Style**: поддержка через `UsePathStyle` в `aws-sdk-go-v2` на основе поля профиля.

#### 10.2. Multipart Upload

- **Алгоритм**:
  1. Создать multipart upload (`CreateMultipartUpload`), получить `UploadId`.
  2. Разбить файл на части. Размер части: адаптивный, но не менее 5 МБ.
  3. Запустить worker pool (goroutines) для параллельной загрузки частей.
  4. Каждая часть: `UploadPart` с `PartNumber` и `Body` (io.Reader, не загружать весь файл в RAM).
  5. Сохранять `ETag` каждой части.
  6. По завершении: `CompleteMultipartUpload` с `Parts` (ETag + PartNumber).
  7. При ошибке: `AbortMultipartUpload` (если не планируется resume).

- **Resume**: если задача в статусе paused/failed, при resume использовать `ListParts` для получения уже загруженных частей, пропустить их.

#### 10.3. Parallel Download (Range GET)

- **Алгоритм**:
  1. HEAD-запрос для получения `Content-Length` и `ETag`.
  2. Разбить на Range-сегменты (default 16 МБ).
  3. Worker pool для параллельных `GetObject` с `Range: bytes=start-end`.
  4. Запись в файл через `io.WriterAt` (memory-mapped или pre-allocated file).
  5. Resume: проверить размер локального файла, запросить `Range: bytes=localSize-`.

#### 10.4. Retry & Timeout Strategy

- **Таймауты**:
  - Connection timeout: 10s.
  - Request timeout: адаптивный. Для запросов < 512 КБ: 10s. Для частей multipart: `max(30s, partSize / currentSpeed * 2)`.
  - Global operation timeout: нет (операция контролируется через Task cancellation).

- **Retry**:
  - Максимум retries: 5 для частей, 3 для metadata-операций.
  - Backoff: exponential с jitter (2s, 4s, 8s, 16s, 32s).
  - При retry: **новое TCP-соединение** (не из pool) + **свежий DNS lookup**.
  - Отслеживать latency всех запросов. Для самых медленных 5% запросов: принудительный retry с новым соединением.

- **Circuit Breaker**: если 5 последовательных ошибок на одном IP — исключить IP из pool на 60s.

#### 10.5. Transfer Scheduler

- Центральный `TransferManager` управляет worker pool.
- Ограничение concurrency: semaphore с N permits (из настроек).
- Приоритизация: задачи с `priority` ниже (численно меньше) выполняются раньше.
- Batching для мелких файлов: если файл < 1 МБ, группировать до 10 файлов в один «batch» для последовательной передачи с переиспользованием соединения.

#### 10.6. Bandwidth Limiter

- Token bucket алгоритм (`golang.org/x/time/rate`).
- Отдельные лимитеры для upload и download.
- Применяется на уровне `io.Reader` / `io.Writer` wrapper.

---

### 11. Требования к безопасности

**SEC-001:** Credentials никогда не хранятся в открытом виде. Шифрование AES-256-GCM с ключом, derived через PBKDF2 или Argon2 из machine ID + optional user password.

**SEC-002:** Session tokens и временные credentials не сохраняются в истории и логах.

**SEC-003:** Presigned URLs генерируются с минимальным набором прав (только `GetObject`).

**SEC-004:** Поддержка отключения SSL verification только явно и с предупреждением.

**SEC-005:** Логи не содержат секретных ключей, токенов, содержимого файлов.

**SEC-006:** При блокировке экрана (master password) все sensitive данные в памяти обнуляются (zeroing).

---

### 12. Этапы реализации

#### Этап 1: Foundation
- Настройка проекта Wails v2 + React + Go.
- Реализация SQLite-схемы и миграций.
- ConnectionService: CRUD профилей, шифрование, тест подключения.
- Базовый UI: список профилей, форма добавления/редактирования.

#### Этап 2: File Manager
- FileManagerService: ListBuckets, ListObjects, HeadObject.
- UI: файловый менеджер, навигация, breadcrumbs, поиск, предпросмотр.
- Контекстное меню, drag-and-drop (upload).

#### Этап 3: Transfer Engine
- S3 Engine: Connection Manager, Multipart Upload, Range Download.
- TransferService: Queue, Scheduler, Retry, Bandwidth limiter.
- UI: очередь передач, прогресс, пауза/отмена.
- Докачка (resume).

#### Этап 4: Bulk Operations & Polish
- Массовые операции: delete, copy, move, metadata.
- Presigned URLs.
- Settings: темы, язык, лимиты, мастер-пароль.
- Горячие клавиши, тултипы, обработка ошибок.

#### Этап 5: QA & Release
- Кросс-платформенная сборка (Windows, macOS, Linux).
- Интеграционные тесты с MinIO и AWS S3.
- Профилирование памяти и CPU.
- Создание инсталляторов и GitHub Release.

---

### 13. Критерии приёмки

**AC-001:** Приложение компилируется под Windows, macOS, Linux без модификации кода.

**AC-002:** Успешное прохождение тестов подключения к MinIO (локально) и AWS S3 (реальный аккаунт).

**AC-003:** Upload файла 5 ГБ завершается корректно, ETag совпадает, скорость не ниже 80% канала.

**AC-004:** Download файла 5 ГБ с прерыванием на 50% и возобновлением — файл целостен.

**AC-005:** Приложение потребляет < 150 МБ RAM в простое (проверка через Activity Monitor / Task Manager).

**AC-006:** UI остаётся отзывчивым при 10 параллельных передачах (навигация, смена темы).

**AC-007:** Все credentials хранятся в зашифрованном виде (проверка через hex-дамп SQLite).

---

### 14. Требования к документации

**DOC-001:** README с инструкцией по сборке (`wails build`, `wails dev`).

**DOC-002:** ARCHITECTURE.md с описанием модулей Go и схемой данных.

**DOC-003:** API.md со списком всех Wails bindings и Events.

**DOC-004:** BUILD.md с инструкциями по кросс-компиляции и созданию инсталляторов.

---

### 15. Навыки и роли агентов разработки

| Роль | Навыки | Зона ответственности | Субагент |
|------|--------|---------------------|------|
| **Go-разработчик (Backend)** | Go 1.21+, goroutines, channels, context, `aws-sdk-go-v2`, `net/http`, SQLite, криптография (AES-GCM), опыт с S3 API (multipart, presigned URLs, ETag) | Разделы 10 (S3 Engine), 9 (Go services), 8 (SQLite schema), 11 (SEC-001, SEC-006) | golang-pro |
| **Frontend-разработчик (Wails/React)** | React 18+, TypeScript, Wails v2 (runtime, bindings, events), Tailwind CSS, Headless UI, Zustand, drag-and-drop API, i18n | Разделы 7 (UX), 4.2 (File Manager UI), 4.5 (Queue UI), 4.6 (Settings UI), интеграция с Wails bindings | react-specialist |
| **S3/Сетевой специалист** | Глубокое понимание S3 REST API, HTTP/1.1 keep-alive, DNS round-robin, TCP tuning, multipart upload, Range requests, retry strategies, circuit breaker | Разделы 10.1–10.4 (Connection Manager, Retry, Scheduler), 6 (Performance), 13 (AC-003, AC-004) | network-engineer |
| **DevOps/Сборщик** | GitHub Actions, Wails cross-compilation, Inno Setup (Windows), DMG creation (macOS), AppImage/deb (Linux), codesigning | Раздел 12 (Этап 5), 14 (BUILD.md), AC-001 | devops-engineer, sre-engineer, deployment-engineer, build-engineer |
