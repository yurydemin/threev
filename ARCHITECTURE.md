# Архитектура threev

Кроссплатформенный десктопный S3-клиент: Go-бэкенд + React/TypeScript-фронтенд в одном процессе через [Wails v2](https://wails.io/) (WebView встроен в нативное окно — WKWebView на macOS, WebView2 на Windows, WebKitGTK на Linux).

## Стек

- **Backend**: Go 1.25, `aws-sdk-go-v2` (S3), `modernc.org/sqlite` (чистый Go, без CGO), `golang.org/x/crypto` (Argon2id), `github.com/denisbrodbeck/machineid`.
- **Frontend**: React 19 + TypeScript, Zustand (стейт), Tailwind CSS 3 + Headless UI, react-i18next.
- **Мост**: Wails v2.13 — Go-методы биндятся напрямую во фронтенд (`frontend/wailsjs/go/**`, автогенерируется при `wails build`/`wails dev`), события идут через `runtime.EventsEmit`/`EventsOn`.

## Backend: модули (`internal/`)

| Пакет | Назначение |
|---|---|
| `domain` | Общие доменные типы и ошибки (`Profile`, `ObjectEntry`, `TransferTask`, `AppSettings`, события, `ErrLocked` и т.д.) — без логики, только структуры. |
| `config` | Кроссплатформенные пути (`AppDataDir`, `DBPath`) через `os.UserConfigDir()`. |
| `storage` | Открытие SQLite, embed-миграции, репозитории (`ProfileRepository`, `TransferQueueRepository`, `TransferHistoryRepository`), generic k/v `settings`-таблица (`GetSetting`/`SetSetting`). |
| `crypto` | Argon2id-деривация ключа, AES-256-GCM Encrypt/Decrypt, `KeyBox` — потокобезопасный контейнер над ключом шифрования (пуст, пока приложение заблокировано мастер-паролем). |
| `connection` | `ConnectionService` — CRUD профилей подключения, валидация, `TestConnection` (реальный `ListBuckets` с таймаутом). |
| `s3client` | Фабрика `*s3.Client` из профиля, `ConnectionManager` (кэш клиентов на профиль, pooled/fresh HTTP-транспорт), `CircuitBreaker` (на уровне хоста), `WithRetry` (backoff + jitter, адаптивный timeout). SDK-ретрай отключён (`aws.NopRetryer{}`) — единственный источник повторов свой. |
| `filemanager` | `FileManagerService` — листинг бакетов/объектов (с сортировкой/кэшем страницы), `HeadObject`, presigned URL, текстовое превью, массовые delete/copy/move/rename/metadata/create-folder с прогрессом и отменой. |
| `transfer` | `TransferService` — очередь загрузок/скачиваний: multipart upload и range download воркер-пулами, adaptive part size, resume через `ListParts`/размер локального файла, bandwidth limiter, scheduler с семафором на параллелизм. |
| `appsettings` | `SettingsService` — чтение/запись `AppSettings` (Общие/Внешний вид/Передачи), master-password (`Unlock`/`SetMasterPassword`/`RemoveMasterPassword`), синхронно применяет настройки к `TransferService`. |
| `mimetype` | Статическая карта расширение→MIME (детерминированная на всех ОС, в отличие от `mime.TypeByExtension`). |
| `profiling` | Опциональный HTTP pprof-сервер, включается только через `THREEV_PPROF_ADDR` (dev-only, никогда не активен в обычной сборке). |
| `integration` | Чёрно-ящичные тесты (`//go:build integration`) против реального MinIO — см. «Тестирование» ниже. |

Точка входа — `main.go` (создаёт `App`, конфигурирует `options.App`, вызывает `wails.Run`) и `app.go` (конструирует и связывает все сервисы, реализует `App.startup`/`App.shutdown`/`App.beforeClose`, хранит и восстанавливает геометрию окна).

## Frontend (`frontend/src/`)

- `screens/` — экраны верхнего уровня: `WelcomeScreen`, `ConnectionsScreen`, `FileManagerScreen`, `TransferScreen`, `SettingsScreen`, `UnlockScreen`.
- `stores/` — состояние на Zustand, по одному стору на предметную область (`useConnectionStore`, `useFileManagerStore`, `useTransferStore`, `useSettingsStore`, `useSecurityStore`, `useBulkOperationStore`, `useToastStore`, `useConfirmStore`, `useAppStore` — тема/масштаб/язык).
- `lib/wails/` — типизированные обёртки над автогенерированными Wails-биндингами (`app.ts`, `connection.ts`, `fileManager.ts`, `transfer.ts`, `appsettings.ts`, `errors.ts` — маппинг Go-ошибок в понятный фронтенду формат).
- `components/` — по предметным областям (`connection/`, `file-manager/`, `transfer/`, `settings/`, `layout/`, `ui/` — базовые примитивы).
- `i18n/` — `react-i18next`, `locales/{ru,en}.json`, суффиксы `_one`/`_few`/`_many` для русской плюрализации.

## База данных

SQLite-файл `threev.db` в каталоге приложения (см. «Расположение данных» ниже), схема — `internal/storage/migrations/0001_init.sql`, применяется embed-раннером при каждом запуске.

- **`profiles`** — сохранённые подключения. `secret_access_key`/`session_token` хранятся зашифрованными (AES-256-GCM), в остальном как введены (`endpoint_url`, `region`, `path_style`, `verify_ssl`, `custom_headers` как JSON).
- **`transfer_queue`** — активные/приостановленные задачи передачи. `multipart_upload_id` используется для resume; `parts_completed`/`file_offset` в схеме присутствуют, но не являются источником истины (см. `docs/backlog.md`).
- **`transfer_history`** — завершённые/проваленные/отменённые задачи, переносятся из `transfer_queue` одной транзакцией при достижении терминального статуса.
- **`settings`** — общая k/v-таблица: Argon2id-соль, поля `AppSettings`, master-password verifier, геометрия окна (`window_width/height/x/y/maximized`).

## Безопасность credentials

- Ключ шифрования — Argon2id из machine-specific seed (`machineid`, с fallback на файл-сид) либо, если установлен мастер-пароль, из пароля пользователя — та же соль в обоих случаях.
- `KeyBox` — единственный держатель актуального ключа в памяти, общий для всех сервисов; пока пуст (мастер-пароль не введён после запуска) — все методы, которым нужны credentials, возвращают `domain.ErrLocked`.
- Смена мастер-пароля — одна SQL-транзакция, расшифровывающая все профили старым ключом и зашифровывающая новым; ключ в `KeyBox` заменяется только после успешного commit.
- Zeroing ключа в памяти при `KeyBox.Clear()` — best-effort (Go GC не даёт криптографических гарантий).

## Передачи: multipart upload / range download

- Adaptive part/chunk size по итоговому размеру файла (от 5 МБ до 128 МБ), с клэмпом под лимит S3 в 10000 частей.
- Resume upload — сверка через `ListParts` (не через `parts_completed` в БД). Resume download — сверка размера локального файла.
- Верификация целостности: composite ETag (`hex(md5(concat(part_md5)))-N`) для multipart, обычный MD5 для single-part; при нестандартном формате ETag (SSE-KMS и т.п.) верификация пропускается.
- Circuit breaker на уровне хоста (не IP): 5 подряд `network`/`timeout`-ошибок → open на 60с → half-open → closed/open. `auth`/4xx-ошибки breaker не открывают.
- Pause = отмена `context.Context` конкретной задачи (не graceful drain) — недокачанная часть перекачивается заново при Resume.
- Прогресс — atomic-счётчик байт, throttled события в UI (`transfer:progress`, ~500 мс) и в БД (~3 с) отдельно.

## Wails-события

| Событие | Источник | Назначение |
|---|---|---|
| `transfer:progress` | `TransferService` | live-прогресс задачи передачи (throttled ~500мс) |
| `object:change` | `TransferService`, `FileManagerService` | инвалидация текущего листинга во фронтенде при изменении содержимого префикса |
| `bulk:progress` | `FileManagerService` | live-прогресс массовой операции (delete/copy/move), throttled ~250мс, считает объекты, не байты |

## Тестирование

- Юнит-тесты — `httptest`-моки, во всех пакетах кроме `integration`, без внешних зависимостей.
- `internal/integration` (`go test -tags=integration ./internal/integration/...`) — чёрный ящик против реального MinIO (`docker run` локально, либо `bitnamilegacy/minio` service container в CI): `ConnectionService.TestConnection`, `ListBuckets`/`ListObjects`, upload/download round-trip с проверкой ETag, `DeleteObjects`/`CopyObject`/`MoveObject`. Пропускается целиком (`t.Skip`), если эндпоинт недоступен на старте пакета.
- Параметры интеграционных тестов полностью заданы через переменные окружения (`THREEV_INTEGRATION_S3_ENDPOINT`/`_ACCESS_KEY`/`_SECRET_KEY`) — тот же набор тестов пройдёт и против реального AWS S3 при смене этих переменных, без изменения кода.
- `go test ./... -race` — обязателен для `transfer`/`filemanager`/`crypto` (конкурентный доступ к scheduler/`KeyBox`/circuit breaker).

## CI/CD

- `.github/workflows/ci.yml` — на каждый push/PR: `lint` (golangci-lint, один раз на ubuntu), `frontend` (`npm ci && npm run build`, публикует `frontend/dist` артефактом), `unit-tests` (матрица ubuntu/macos/windows, `go test ./... -race`), `integration-tests` (ubuntu + MinIO service container).
- `.github/workflows/release.yml` — по тегу `v*.*.*`: `check-version` (сверяет тег с `wails.json`'s `productVersion`) → `package` (матрица 3 ОС, `scripts/package-{dmg,windows-nsis,appimage}`) → `publish` (`gh release create` с тремя инсталляторами).
- Ни один инсталлятор не подписан сертификатом разработчика (macOS — ad-hoc self-signed, Windows/Linux — без подписи вовсе); пользователь увидит предупреждение Gatekeeper/SmartScreen при первом запуске.

## Расположение данных

`config.AppDataDir()` = `os.UserConfigDir() + "/threev"`:

| ОС | Путь |
|---|---|
| Windows | `%AppData%\threev` |
| macOS | `~/Library/Application Support/threev` |
| Linux | `~/.config/threev` |

Файл базы данных — `threev.db` внутри этого каталога.

## Производительность (измерено на macOS arm64, релизная сборка, 2026-07-13)

**RAM в простое (AC-005)**: `threev` (Go-хост) ≈172 МБ + `com.apple.WebKit.WebContent` ≈59 МБ ≈ **231 МБ суммарно**, при целевом пороге тех-спека в 150 МБ. Live heap-профиль (`go tool pprof`) показал фактическую Go-кучу приложения ~4 МБ — превышение порога не является утечкой, а структурными накладными расходами WKWebView-архитектуры (cgo-мост к Cocoa/WebKit, статически слинкованные библиотеки). Для сравнения — заметно ниже типичного простоя Electron-приложений (300–500+ МБ за счёт бандленного Chromium).

**Отзывчивость под нагрузкой (AC-006)**: 10 параллельных передач против реального S3-совместимого сервера — UI оставался отзывчивым (навигация, смена темы, скроллинг без подвисаний). После завершения — Go-куча ~8.7 МБ, 14 активных горутин (тот же порядок, что и на простое) — признаков утечки нет.

Baseline измерен только на macOS arm64 — для Windows/Linux аналогичных цифр нет (см. `docs/backlog.md`).

## Известные ограничения

Полный и актуальный список — `docs/backlog.md` (функциональность, технический долг, инфраструктура/релиз, локализация).
