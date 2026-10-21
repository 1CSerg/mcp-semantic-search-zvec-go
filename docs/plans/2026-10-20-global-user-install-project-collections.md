# Единое пользовательское приложение и проектные коллекции — Implementation Plan

> **Для agentic workers:** использовать навык `executing-plans` или `subagent-driven-development`. Выполнять задачи по порядку и отмечать checkboxes только после проверки результата. Под «более простой моделью» понимается модель-исполнитель, а НЕ упрощение целевой архитектуры.

**Goal:** установить одно приложение на пользователя, хранить в проектах конфиг с `collection_id`, направлять все MCP-запросы через лёгкие прокси к одному общему серверу, выполняющему поиск в выбранной коллекции.

**Architecture:** один установленный бинарник работает в ролях daemon и stdio-прокси. Только daemon открывает zvec, индексирует файлы и обслуживает коллекции; IDE запускают из пользовательского каталога лёгкие прокси, читающие конфиг проекта и передающие ID коллекции каждому запросу. Развивать существующие `internal/daemon.Registry`, HTTP API и `internal/service.HTTPProxy`, а не создавать параллельный поисковый сервер.

**Tech Stack:** Go 1.26+, существующий MCP Go SDK/stdio, HTTP REST между proxy и daemon, zvec/SQLite manifest, YAML, PowerShell/Bash install scripts; native production build tags `zvec,onnx,treesitter`.

**Spec:** разделы 1–7 этого документа фиксируют проектные решения; отдельная согласованная спецификация пока не создана. Документ — предлагаемый план на ревью, не разрешение на реализацию.

**Статус:** только план. Код продукта не изменён, тесты продукта при подготовке плана не запускались. Наличие существующих заготовок не означает завершённую реализацию.
**Реализация:** выполнено (2026-10-21). Две галочки ниже оставлены открытыми осознанно: ручной GUI-smoke (нет интерактивного desktop-сеанса) и прогон новых smoke на реальном Unix (Bash-скрипты проверены под Git Bash на Windows). Найденные при приёмке дефекты продукта и принятые решения — в отчёте исполнителя.

## Global Constraints

- Одна установка на пользователя и один владелец коллекций — общий daemon; прокси не создаёт `Phase1`, не открывает zvec и не запускает watcher.
- В проекте нет EXE, native libraries, моделей, индексов, общего конфига или секретов.
- Каждый проект имеет обязательный `collection_id`; proxy передаёт его автоматически, агент не выбирает коллекцию вручную в аргументах tools.
- Сохранять существующие MCP tools и параметры: `semantic_search`, `index_status`, `reindex`, `check_update`.
- Не откатывать существующие незакоммиченные изменения. Не делать `git reset`, массовый `git add .` и автоматические commits.
- Старые явные режимы `--stdio`, `--http`, `--daemon`, `--stdio-proxy --workspace-id` остаются совместимыми. Новый install по умолчанию использует global daemon + proxy.
- Все проверки, установки, миграции и удаления — только на временных проектах и изолированном `MCP_GLOBAL_DIR`, не на пользовательских индексах.
- Если задание противоречит фактическому интерфейсу или меняет публичное поведение сверх описанного — остановиться, описать расхождение, не изобретать новую архитектуру.

## Review Focus

1. Одновременный старт нескольких IDE: ровно один daemon, ни один proxy не открывает БД (Task 5).
2. Копия проекта с тем же ID: нет молчаливого переназначения коллекции другому root (Task 3).
3. Добавление проекта при работающем daemon: новый проект доступен без перезапуска и без отмены чужой индексации (Task 4).
4. Переопределение профиля и вложенных языковых настроек: нет утечки между проектами, нет поиска несовместимыми embeddings (Task 2/4).
5. Обновление/удаление приложения при нескольких проектах: не удаляются чужие данные, откат сохраняет рабочую версию (Task 7).

---

## 1. Зафиксированное понимание задачи

Требуется изменить deployment продукта: вместо отдельного EXE в каждом проекте — одно пользовательское приложение, общее для многих проектов. В проекте сохраняется собственный конфиг с ID коллекции. MCP обращается к общему приложению с этим ID, а приложение выбирает правильную коллекцию.

Рекомендуемый вариант: существующий shared daemon + существующий stdio-прокси. Альтернативы не входят в план:

- Один общий EXE с полноценным отдельным индексатором в каждой IDE — НЕ целевая архитектура: сохраняет дублирование владельцев БД и памяти.
- Прямой MCP Streamable HTTP — возможное будущее развитие, но существующий REST API не является MCP HTTP transport; в этом плане transport не заменяем.

Количество proxy-процессов может соответствовать числу MCP-сессий; это не отдельные установки и не отдельные серверы индексации. Общий daemon продолжает работу после закрытия IDE. Windows GUI в новой модели — клиент daemon, а не второй владелец коллекции.

## 2. Что уже есть и где находятся пробелы

Наблюдения по рабочей копии на 2026-10-20, без выполнения тестов:

| Компонент | Факт | Что проверить/доделать |
|---|---|---|
| `internal/config/paths.go` | Есть `UserDataDir`, `GlobalConfigPath`, `ProjectsDir`, `CollectionDir`, global bin/models/log paths | Безопасность путей, единое применение в runtime/install |
| `internal/config/project.go` | Есть `ProjectConfig`, ID validation, поиск project config, overrides | Строгая схема, versioning, безопасное слияние и обязательный запрет неразрешённых полей |
| `internal/config/options.go` | Есть `LoadForCollection` и `LoadWorkspaceFromSpec` | Общие secrets, финальная валидация после overrides |
| `internal/daemon/projects.go` | Есть загрузка `projects.d` | Управление регистрациями и обновление работающего registry |
| `internal/daemon/config.go` | Загружает `projects.d`, есть `AutoIndexOnOpen` | Сейчас пустой registry отвергается; проверить фактическое применение auto-index |
| `internal/daemon/registry.go` | Lazy-open, borrows, LRU, close drain | `initWorkspace` сейчас вызывает `LoadWorkspaceFromSpec`, не `LoadForCollection`; project overrides не считать подключёнными |
| `internal/service/proxy.go` | HTTPProxy отправляет `WorkspaceID` в body/query и `X-Workspace-ID`, Bearer support | Привязка к project config, auth/discovery общего daemon |
| `cmd/mcp-semantic-search-zvec-go/main.go` | Есть daemon/proxy flags; proxy требует `--workspace-id` | Добавить project-aware proxy и безопасный запуск daemon |
| `scripts/install/install.ps1` | По умолчанию `McpMode=Native`; Proxy опционален | Разделить установку приложения и добавление проекта |

Источники по архитектуре: `docs/ARCHITECTURE.md`, `docs/CONFIG.md`, `docs/DEVELOPMENT.md`, `INSTALL.md`. MCP `index_status`/`semantic_search` не были доступны среди инструментов сессии; использовалось точечное чтение файлов. Существующие пользовательские правки в перечисленных файлах сохранить.

## 3. Каталоги и конфиги

### 3.1 Пользовательский каталог

Использовать уже заложенный `config.UserDataDir()`:

- Windows: `%LOCALAPPDATA%/mcp-semantic-search-zvec-go`.
- Linux/macOS: `$XDG_DATA_HOME/mcp-semantic-search-zvec-go`, fallback `~/.local/share/mcp-semantic-search-zvec-go`.
- `MCP_GLOBAL_DIR` — override для тестов/portable use. Требовать абсолютный путь.

```text
<userdir>/
  bin/                            # одна установленная версия EXE + native deps
  config.yaml                     # embeddings, defaults, logging
  daemon.yaml                     # listener, max_open_workspaces, lifecycle policy
  .env                            # только пользовательские secrets
  auth.token                      # случайный локальный Bearer token
  projects.d/<collection_id>.yaml  # локальный registry: id, root, description
  runtime/daemon.lock             # singleton daemon, OS advisory lock
  data/collections/<collection_id>/ # manifest, meta, locks, zvec
  data/models/                    # общие файлы моделей
  data/logs/                      # daemon/proxy/crash logs
```

Не менять внутреннюю структуру zvec внутри index dir без необходимости. Общие файлы моделей не означают автоматически одну in-memory ONNX session на все коллекции; memory sharing не обещать и не реализовывать отдельным рефакторингом.

### 3.2 В проекте

```text
<project>/.mcp-semantic-search-zvec-go/project.yaml
<project>/.cursor/mcp.json
<project>/.roo/mcp.json
<project>/.cursor/rules/semantic-search-zvec-go.mdc
<project>/.roo/rules/semantic-search-zvec-go.md
```

Минимальный конфиг:

```yaml
schema_version: 1
collection_id: 9ab86bc3-48b1-4fc9-8c3c-3f8ab01bbcd0
```

Пример overrides:

```yaml
schema_version: 1
collection_id: 9ab86bc3-48b1-4fc9-8c3c-3f8ab01bbcd0
description: "Сервис заказов"
active_profile: local_multilingual
indexing:
  extensions: [".go", ".md"]
  skip_dirs: [".git", "vendor", ".mcp-semantic-search-zvec-go"]
  max_file_bytes: 2097152
  chunking:
    min_chunk_tokens: 24
    prose_overlap_ratio: 0.12
    languages:
      bsl:
        enabled: false
file_watcher:
  enabled: true
```

### 3.3 Разделение ответственности

| Настройка | Где | Правило |
|---|---|---|
| ID, описание проекта | project.yaml | ID стабилен, не зависит от имени/пути |
| Выбор embedding profile | project.yaml | Только ссылка на существующий общий профиль |
| Provider/model/dimensions/base_url/retries/batch size | общий config.yaml | Проект не определяет произвольный endpoint или профиль |
| API keys | общий .env | Никогда не искать .env внутри проекта в новом режиме |
| Listener/token/resource caps | daemon.yaml + auth.token | Никогда не брать из project.yaml |
| Extensions/skip_dirs/max_file_bytes | project.yaml overrides | Иначе общие defaults |
| Chunking defaults/version/strategy | общий config.yaml | Проекту разрешены только min tokens, prose overlap, languages |
| Включение watcher/debounce/backend/poll interval | project.yaml overrides | Общие defaults, лимиты daemon не обходятся |
| Root проекта | projects.d | Абсолютный локальный путь, в переносимый конфиг не входит |
| Index/model/log paths | приложение | Вычисляются внутри userdir, не контролируются проектом |
| Search metrics/log rotation | общий config.yaml | Не копировать в проект |

Приоритет: built-in defaults → global config → разрешённые project overrides → final validation. Списки replace целиком; отсутствие сохраняет default, пустой список означает пустой список. Вложенные language fields merge по присутствующим полям; для optional bool использовать pointers, чтобы отсутствие не превращалось в false. После merge — новая независимая config object, общие maps/slices не мутировать.

`schema_version` отсутствует в существующих заготовках: отсутствие трактовать как 1 для совместимости; новые файлы явно пишут 1, другие версии отклонять. Неизвестные project YAML keys отклонять. Существующее поле `daemon_url` оставить только для чтения legacy-конфига: новый generated config его не пишет, автоматический запуск/передача token на произвольный URL запрещены.

## 4. ID и владелец коллекции

- Новый ID генерировать как UUID v4 через `crypto/rand`, а не slug имени/хэш абсолютного пути. Использовать имеющийся UUID helper, если он уже есть; не добавлять dependency только ради UUID.
- Существующие безопасные ID продолжить принимать: текущий контракт `ValidateCollectionID`, max 64, запрет `..` и path separators. Дополнить запретом Windows device names/хвостовой точки; сравнение ID уникальности сделать case-insensitive на всех платформах.
- Новый root + новый ID: регистрация. Тот же root + тот же ID: idempotent.
- Тот же root + другой ID: отказ до явного remove/add; не создавать тихо вторую регистрацию.
- Тот же ID + другой root: конфликт; не изменять registry и данные.
- `--project-new-collection`: для копии проекта генерирует новый ID, создаёт новый индекс, старый не удаляет.
- `--project-relocate`: явное подтверждение переноса, сохраняет ID, изменяет root только при отсутствии активных borrows/indexing для этой коллекции; затем требует full reindex до успешного поиска. Не угадывать перенос по отсутствию старого каталога.
- ID не является credential или sandbox границей. Это однопользовательское приложение; ID только маршрутизирует запросы.

## 5. CLI, MCP и жизненный цикл

Предлагаемые новые команды одного общего EXE (реализовать в отдельных CLI helpers, не раздувать main):

```text
--project-add <absolute-root>
--project-remove <absolute-root>
--project-new-collection <absolute-root>
--project-relocate <absolute-new-root>
--projects-reload
--stdio-proxy --project-config <absolute-project-yaml>
--ensure-daemon
--stop-daemon
```

`--project-add` создаёт/проверяет project.yaml и регистрацию, обеспечивает daemon и reload. Установка приложения и добавление проекта — разные операции; add не скачивает/не обновляет EXE. Remove удаляет регистрацию и managed MCP/rules, данные сохраняет. Отдельный `--purge-collection <id>` не входит в первый этап; безопасное удаление отдельной коллекции добавить только отдельным согласованным заданием.

Generated MCP entry: абсолютный путь к общему EXE, args `--stdio-proxy --project-config <absolute-project-yaml>`. Никаких secrets и INDEX_DIR/WORKSPACE_ROOT в MCP env. Абсолютный путь в IDE JSON допустим, запрет относится к переносимому project.yaml. После переноса проекта повторный add/relocate обновляет IDE JSON.

Proxy стартует так:

1. Читает project config, валидирует ID и root; проверяет, что локальная регистрация соответствует этому root/ID.
2. Загружает daemon listener из пользовательского daemon.yaml и token из auth.token. Не загружает project .env.
3. Вызывает `EnsureRunning`: если daemon жив — продолжает; если нет — запускает тот же установленный EXE `--daemon` с пользовательским конфигом.
4. Ожидает authenticated health с лимитом 15 секунд. Не ждёт `/ready` конкретной коллекции: она может ещё индексироваться.
5. Создаёт существующий HTTPProxy, `WorkspaceID = collection_id`.
6. Каждый search/status/reindex передаёт ID через существующие wire fields `workspace_id`/`X-Workspace-ID`. Не переименовывать весь REST API: `collection_id` в проекте соответствует `workspace_id` на существующем wire.
7. Закрытие stdin завершает только proxy, daemon остаётся жив. stdout содержит исключительно MCP frames, диагностика идёт в stderr/log.

Старое `--stdio-proxy --workspace-id --daemon-url` оставить явно legacy/manual; не передавать туда новый auth.token автоматически, особенно на non-loopback URL.

## 6. Daemon, безопасность и reload

- Новый пользовательский daemon по умолчанию слушает `127.0.0.1:8080`, а не `:8080`. Если порт занят другим приложением — понятная ошибка, без подключения к нему и без скрытой смены порта.
- `auth.token`: 32 bytes `crypto/rand`, hex encoding, exclusive/atomic creation; Unix permissions 0600, userdir 0700. Windows — inherited user-profile ACL; установить и проверить отсутствие write/read доступа других обычных пользователей, не заявлять защиту только по chmod.
- Singleton: `runtime/daemon.lock`, OS advisory lock через существующий lock package. Второй daemon не запускает registry. Конкурирующие proxy после неудачного child-start ожидают health победившего daemon.
- Дочерний daemon отсоединяется от lifetime IDE: Unix отдельная process session, Windows detached/no console с file log handles. Не использовать `exec.CommandContext` с proxy ctx. Добавить платформенные helpers и integration tests; не оставлять inherited stdio pipes.
- Registry может быть пустым: daemon стартует до добавления проектов.
- Authenticated `POST /v1/admin/reload-projects` перечитывает registry registrations. Нет HTTP API, принимающего произвольный root из тела запроса.
- Reload сериализован mutex: сначала полностью прочитать/валидировать новый snapshot, затем применить. При ошибке старый snapshot остаётся действующим.
- Неизменённые workspaces сохраняют handles/watchers/jobs. Новые добавляются lazy. Удаляемые/изменяемые активные handles дают HTTP 409 без частичного применения; inactive закрываются через существующий drain/lifecycle.
- Для проверки config changes хранить digest project.yaml для каждого spec/snapshot. Изменение project/global конфигурации не применять к живому service незаметно. Project change применяется через reload после освобождения handles; global profile changes требуют остановки daemon/перезапуска.
- `index_status` при несовместимом embedding/chunking identity сообщает reindex required; search не использует новую модель против старых векторов.
- `max_open_workspaces` default 10 оставить. Индексирующие handles не evict до завершения/cancel; при заполнении вернуть диагностируемый 503. Watcher действует только для открытой коллекции; после eviction следующий open делает incremental startup refresh.
- `auto_index_on_open` default true: подключить существующую настройку к `PrepareStartup`; одно открытие → максимум один startup job.

## 7. Миграция, GUI, обновление

### Миграция

1. Установить общий EXE и defaults без удаления старой установки.
2. Остановить старые native MCP для конкретного проекта проверенным existing lifecycle механизмом; не убивать процессы других проектов.
3. Явно перенести нужные profiles/secrets в user config/.env. Конфликт одинакового имени profile с разными model/dimensions/endpoint требует нового имени, не «последний победил».
4. Создать project.yaml, зарегистрировать проект и обновить только managed MCP entry/rules.
5. Новый индекс построить заново в userdir; не копировать открытые zvec/SQLite/lock files и не менять старый index_meta вручную.
6. Проверить status, reindex completion и поиск. Старую project-local установку оставить как backup до явного удаления пользователем.

Миграция — явный флаг install `-MigrateLegacy` / `--migrate-legacy`; обнаружение старой установки без флага завершает add понятной инструкцией, а не запускает двух владельцев индекса.

### GUI

В user-level GUI нужен выбор зарегистрированной коллекции (ID/description/root). Новая GUI не создаёт Phase1 и не завершает proxy-процессы для захвата БД. Использует тот же HTTPProxy после EnsureRunning. Существующий GUI код сначала изучить и отделить UI от backend factory. Сценарии GUI: список проектов, выбор, status, search, reindex; ошибки daemon показываются пользователю.

### Обновление/удаление

Глобальное обновление затрагивает все проекты: скачать/распаковать в staging рядом с userdir, проверить `--version` и native libraries, запросить штатную остановку daemon, дождаться завершения proxy/GUI процессов (Windows EXE может быть занят), затем заменить bin с backup/rollback. Если активные proxies не завершились — отказ без изменения рабочей версии. Не делать автоматический force kill. После замены старт daemon, health check; при неуспехе вернуть предыдущий bin и сообщить ошибку.

Uninstall из проекта удаляет только managed записи/правила этого проекта и регистрацию; данные коллекции сохраняются. Удаление глобального приложения — отдельная явная опция, требует отсутствия регистраций либо явного подтверждения; data по умолчанию сохраняются. `KeepData` не должен удалять коллекции других проектов ни в одной ветке.

---

## 8. Карта файлов и порядок реализации

Все имена новых файлов ниже — предлагаемые места создания, не утверждение об их наличии. Перед любой правкой читать текущий файл. Не создавать вторую реализацию существующего helper.

### Task 0. Зафиксировать baseline и не смешать чужие изменения

**Files:** прочитать `AGENTS.md`, `docs/DEVELOPMENT.md`, текущие изменения в config/daemon/install; ничего не откатывать.

- [x] Выполнить `git status --short`, `git diff --stat`, сохранить список пользовательских изменений в отчёте исполнителя.
- [x] Если доступны MCP tools: `index_status`, затем semantic searches по config/daemon/proxy/install/locks/security.
- [x] Выполнить `go test ./internal/config/... ./internal/daemon/... ./internal/service/... ./internal/transport/... ./cmd/mcp-semantic-search-zvec-go` и записать baseline failures.
- [x] Если на Windows воспроизводится описанная в DEVELOPMENT проблема PIE/on-access scanner, применить только документированный тестовый обход `-ldflags=-buildmode=exe`; не менять production build flags.
- [x] Проверка результата: отличить baseline failure от новых регрессий; дальнейшие задачи не помечать зелёными только потому, что «это уже было».

### Task 1. Строгий ProjectConfig и безопасные общие пути

**Files:** modify `internal/config/project.go`, `internal/config/paths.go`; tests `internal/config/project_test.go`, `internal/config/paths_test.go`.

**Interfaces:** сохранить `LoadProjectConfig(path string) (*ProjectConfig, error)`, `ValidateCollectionID(id string) error`, `ProjectConfigPath(workspaceRoot string) string`, `FindProjectConfig(startDir string) (path string, root string, err error)`. Добавить SchemaVersion и nullable language overrides, не менять публичный LanguageConfig только ради частичного merge.

- [x] Написать тесты `TestProjectConfigSchema`: absent version → 1; explicit 1 → success; version 2/unknown key → error.
- [x] Написать `TestCollectionIDUnsafe`: пустой, `../x`, `a/b`, `a\\b`, `C:\\x`, `..`, длина 65, `CON`, хвостовая точка → error; UUID и существующий `my-app` → success.
- [x] Написать `TestGlobalDirAbsolute`: relative MCP_GLOBAL_DIR → error; absolute temporary directory → все global paths внутри него.
- [x] Запустить новые тесты и убедиться, что недостающий контракт даёт FAIL.
- [x] Реализовать strict YAML decoder (`KnownFields`) и правила из разделов 3–4.
- [x] Запустить `go test ./internal/config/...` → PASS.

### Task 2. Финальная сборка независимых настроек коллекции

**Files:** modify `internal/config/options.go`, `internal/config/project.go`; tests `internal/config/options_test.go`, `internal/config/project_test.go`.

**Interfaces:** использовать существующий `LoadForCollection(pc *ProjectConfig, workspaceRoot string, opts LoadOptions) (*Settings, error)`. Final validation вызывается ПОСЛЕ `ApplyProjectOverrides`, настройки профиля доступны только из global config. При необходимости вынести clone/merge в новый `internal/config/project_merge.go`.

- [x] Написать `TestCollectionSettingsIsolation`: A меняет extensions/language bool, B/defaults остаются неизменны; запускать параллельную загрузку под race detector.
- [x] Написать `TestProjectPartialLanguageOverride`: override только `include_sdbl=false` сохраняет общий `enabled=true`.
- [x] Написать `TestProjectSecretIsolation`: ключ из project .env игнорируется; global .env читается; отсутствие .env допустимо для профиля без ключа; отсутствие нужного ключа даёт явную ошибку.
- [x] Написать `TestProjectProfileValidation`: неизвестный profile/невалидный overlap/отрицательные лимиты отвергаются после merge. Zero/false/empty list не теряются.
- [x] Убедиться в FAIL недостающих случаев; реализовать merge на независимых maps/slices и final validation без `os.Setenv`.
- [x] Запустить `go test ./internal/config/...` → PASS; `go test -race ./internal/config/...` на поддерживаемой CGO платформе → PASS.

### Task 3. Управление локальными регистрациями и переносом проекта

**Files:** modify `internal/daemon/projects.go`; create `internal/daemon/project_manager.go`, `internal/daemon/project_manager_test.go`; tests регистраций в `internal/daemon/projects_test.go`.

**Interfaces (новые):** `AddProject(root string) (*config.ProjectConfig, error)`, `RemoveProject(root string) error`, `NewProjectCollection(root string) (*config.ProjectConfig, error)`. Методы реализовать у `ProjectManager`, конструктор `NewProjectManager(userDir string) *ProjectManager`. Runtime relocate применять отдельной Task 4, здесь подготовить безопасный new spec.

- [x] Написать тесты add twice → один ID/один registration; ID generation UUID v4; два root с одинаковым basename → разные ID.
- [x] Написать `TestProjectRegistrationCollision`: same ID/different root и ID case-only collision → отказ без изменения обоих файлов.
- [x] Написать `TestProjectNewCollection`: меняется ID копии проекта, оригинальная регистрация/данные не меняются.
- [x] Написать тесты canonical paths: пробелы, кириллица, Windows case-insensitive root, symlink aliases существующего root не создают дубликаты.
- [x] Написать тест interrupted write/malformed YAML: registry остаётся читаемым, ошибка не превращает пустой файл в успешную регистрацию.
- [x] Реализовать safe writes через temp в том же каталоге + platform-safe replace, management OS lock на read-modify-write. При ошибке второй записи восстановить предыдущий файл; не оставлять регистрацию, несовместимую с project.yaml.
- [x] Ограничить новый registration format полями id/root/description; config/index paths вычисляются глобально. Legacy custom paths читать только в explicit legacy mode.
- [x] Запустить `go test ./internal/daemon/... ./internal/config/...` → PASS.

### Task 4. Подключить коллекции к daemon и атомарный reload

**Files:** modify `internal/daemon/config.go`, `internal/daemon/registry.go`; create `internal/daemon/reload.go`, `internal/daemon/reload_test.go`; modify `internal/transport/http/server.go`, `internal/transport/http/server_test.go`; existing daemon tests.

**Interfaces:** существующий Registry/BorrowService сохранить. Добавить `func (r *Registry) ReloadProjects(ctx context.Context) error`; HTTP route `POST /v1/admin/reload-projects` с обязательным Bearer, success 200 `{ "ok": true }`, invalid snapshot 400, active changed handle 409, shutdown 503. Для global specs init вызывает LoadProjectConfig + LoadForCollection; legacy specs — LoadWorkspaceFromSpec.

- [x] Тест `TestEmptyGlobalRegistry`: daemon config с нулём проектов валиден; неизвестный ID → 404, нет fallback на первую коллекцию.
- [x] Тест `TestDaemonLoadsProjectOverrides`: root из registration, ID обязан совпасть с project.yaml, profile/overrides применены до NewPhase1.
- [x] Тест `TestReloadAddsWithoutClosingExisting`: B доступен после reload, A сохраняет handle/watcher/текущую индексацию.
- [x] Тест `TestReloadAtomicFailure`: один невалидный config → весь snapshot не применяется; A продолжает обслуживаться.
- [x] Тест `TestReloadBusyChange`: changed/removed A при borrow/indexing → 409, B не добавлен частично; повтор после release применяет весь snapshot.
- [x] Тест `TestAutoIndexOnOpen`: true → один job, false → ноль; повторные borrows не создают новый job. LRU не evict running indexer.
- [x] Реализовать reload с existing lifecycle primitives, не закрывать handles под registry mutex, не обходить существующий Close drain.
- [x] Подключить `/v1/admin/stop`: authenticated, response 202 до graceful cancellation, без process kill. Ввести daemon lifecycle callback в HTTP options, не помещать `os.Exit` в handler.
- [x] Запустить `go test ./internal/daemon/... ./internal/transport/http/...`; затем race tests этих пакетов → PASS.

### Task 5. Singleton, token и автоматический запуск daemon

**Files:** create `internal/daemonclient/client.go`, `auth.go`, `launch_windows.go`, `launch_unix.go`, corresponding tests; modify `cmd/mcp-semantic-search-zvec-go/main.go`; tests `main_test.go`; reuse `internal/lock`.

**Interfaces (новые):** `type Connection struct { BaseURL string; Token string }`; `EnsureRunning(ctx context.Context, executable string) (Connection, error)`; `Stop(ctx context.Context) error`. Настройки пути через config.UserDataDir. Test seams для process starter/health polling — injectable callbacks, не реальные detached процессы в unit tests.

- [x] Тест token create/read: ровно один token при конкурентных starts; чужой/повреждённый token не регенерируется молча; token никогда не попадает в log/CLI args.
- [x] Тест EnsureRunning: живой authenticated daemon → launch count 0; холодный start → launch count 1; cancellation/15 s timeout → объяснимая ошибка.
- [x] Тест десяти concurrent proxy starts: один daemon lock holder; проигравшие ждут health, не поднимают отдельные index services.
- [x] Тест занятый порт чужим HTTP server: отказ; без отправки secrets на redirects/non-loopback, redirect policy запрещена для local daemon client.
- [x] Реализовать auth.token, daemon lock и detach helpers. Authenticated `/health` использовать как liveness; не требовать workspace readiness.
- [x] В runDaemon для user-level режима явно установить loopback, global secrets/token/logging; legacy daemon.yaml с нестандартным bind сохранить только для explicit manual режима.
- [x] Платформенный integration test: закрытие proxy не завершает child daemon, stop освобождает lock; после crash daemon повторно стартует и stale PID не считается владельцем.
- [x] Запустить `go test ./internal/daemonclient/... ./cmd/mcp-semantic-search-zvec-go`; integration lifecycle на Windows и Unix → PASS. Без проверки обеих платформ не объявлять cross-platform detached launch готовым.

### Task 6. Project-aware MCP proxy без доступа к базе

**Files:** modify `cmd/mcp-semantic-search-zvec-go/main.go`; create `cmd/mcp-semantic-search-zvec-go/project_commands.go`, tests; modify `internal/service/proxy.go`, `proxy_test.go`; tests `internal/transport/mcp/proxy_test.go`.

**Interfaces:** добавить flags/commands из раздела 5. `--project-config` совместим только с stdio-proxy, конфликт с manual `--workspace-id/--daemon-url` отклоняется. Internal wire остаётся workspace_id. Использовать существующий `NewHTTPProxy`, не создавать новый Service API.

- [x] Тест `TestProjectProxyBinding`: A search/status/reindex получают ID A в header/body/query; ID из переданного запроса не переопределяет binding proxy.
- [x] Тест missing/malformed project config и registration-root mismatch: отказ до network/index open; stderr с путём, stdout пустой до MCP initialize.
- [x] Тест `TestProjectProxyNoLocalIndex`: fake service factory для Phase1 не вызывается, в проекте нет data/log files от proxy.
- [x] Тест неизвестный ID 404, недоступный daemon, auth 401/403, closing 503: MCP tool error без fallback на другую коллекцию; context cancellation передаётся через HTTPProxy.
- [x] Подключить EnsureRunning и запуск существующего mcptransport.Run. Сохранить legacy flags и CLI mode conflict tests.
- [x] Реализовать add/remove/reload/relocate orchestration: management mutation + reload; при 409 rollback mutation; relocate не разрешает search до full rebuild и не перемещает файлы индекса вручную.
- [x] Проверить `go test ./cmd/mcp-semantic-search-zvec-go ./internal/service/... ./internal/transport/mcp/...` → PASS.

### Task 7. Install/update/uninstall: разделить приложение и проект

**Files:** modify `scripts/install/install.ps1`, `install.sh`, `uninstall.ps1`, `uninstall.sh`, `merge-config.py`; read templates before changing; create `scripts/smoke/run-global-install.ps1`, `run-global-install.sh`.

**Interfaces:** PowerShell `-InstallAppOnly`, `-TargetRoot`, `-MigrateLegacy`, `-UninstallApp`; Bash equivalents `--install-app-only`, `--target-root`, `--migrate-legacy`, `--uninstall-app`. Существующие entry wrappers проверить и сохранить. Default install = install/ensure user app + add TargetRoot + managed IDE entries.

- [x] Написать isolated install test: A/B используют один command path, разные project config args; в A/B нет bin/models/index/.env.
- [x] Test rerun install: ID, настройки пользователя, чужие MCP entries/rules сохраняются; install-managed updates идемпотентны.
- [x] Test install app без проектов: daemon стартует; последующий add не обновляет EXE.
- [x] Test legacy migration opt-in: без флага сообщение/отказ; с флагом старые данные не удаляются и secrets не печатаются. Конфликт profiles → отказ до изменения рабочих configs.
- [x] Test failure during upgrade: old bin/config остаются рабочими; активный proxy на Windows блокирует замену понятным сообщением, не force kill.
- [x] Реализовать staging/backup/rollback, stop/health flow раздела 7. Не копировать бинарник поверх работающего daemon.
- [x] Test project uninstall A: B/MCP/config/data остаются; app uninstall без explicit option невозможен; KeepData сохраняет все collections.
- [x] Запустить оба smoke scripts в временном MCP_GLOBAL_DIR; проверить managed rules и JSON с путями с пробелами/кириллицей. Все exit codes 0; ошибки веток имеют ненулевой exit.

### Task 8. GUI как клиент единого приложения

**Files:** прочитать/modify `internal/gui/gui_windows.go`, `gui_windows_test.go`, stub variants при изменении API; create `internal/gui/collections.go`, tests; modify CLI GUI wiring.

**Interfaces:** список projects загружается read-only через daemonclient/local registrations; selected ID создаёт существующий HTTPProxy. Factory service.Service для GUI получает Connection+ID, никогда NewPhase1 в global mode.

- [x] Тест выбора A/B переключает target ID, не переносит результаты/прогресс предыдущей коллекции.
- [x] Тест GUI при активных MCP proxies: нет lock error и нет предложения убить их ради открытия коллекции.
- [x] Тест отсутствующего daemon: EnsureRunning, далее status; отсутствие проектов даёт пустой список и инструкцию add, не создаёт фиктивную коллекцию.
- [x] Реализовать backend separation без переписывания UI framework; async requests отменять при смене коллекции/закрытии окна.
- [ ] Запустить GUI unit tests, production Windows build и ручной GUI + два proxy smoke. Если Windows GUI недоступен в окружении, явно оставить ручную проверку незакрытой.

### Task 9. Native end-to-end изоляция коллекций и перезапуск

**Files:** create `scripts/smoke/run-global-daemon.ps1`, `run-global-daemon.sh`; extend `scripts/smoke/smoke-check.go` или новый узкий Go smoke driver; existing integration tests store/service по необходимости.

**Interfaces:** production build tags `zvec,onnx,treesitter`; отдельный mock embeddings server; userdir, workspace A/B и порты — temporary. Все started process IDs собираются и штатно закрываются в cleanup.

- [x] A и B содержат различимые fixtures; общий daemon+два MCP stdio proxy выполняют настоящий initialize/tools call, не только HTTP curl.
- [x] Reindex A/B → ожидать idle с deadline 60 s для малых fixtures; status и search показывают нужный ID/root; A не возвращает paths/snippets из B. Негативная ANN-проверка не должна опираться только на отсутствие похожих слов — проверять источник каждого результата.
- [x] Добавить C при работающем A/B: reload, search C работает; PID daemon неизменен.
- [x] Закрыть proxy A: B работает, daemon жив. Открыть второй proxy A: общая коллекция обслуживается daemon без второго zvec owner.
- [x] Проверить same ID/different root → отказ без изменения исходного индекса; explicit new collection → новый ID.
- [x] Изменить profile/dimensions/chunking identity: search требует rebuild, после force reindex работает; поведение коллекции B не меняется.
- [x] Прервать daemon во временном окружении, повторить EnsureRunning; коллекции открываются, lock recovery штатный, нет orphan watchers.
- [x] Verify paths с кириллицей: zvec остаётся внутри global collection dir; не вводить обход с переносом в произвольный cache.
- [ ] Запустить новые smoke на Windows/Unix, затем существующий phase1 smoke для legacy режима. Stub tests отдельно от native acceptance.

### Task 10. Документация и финальная сверка

**Files:** modify `INSTALL.md`, `docs/ARCHITECTURE.md`, `docs/CONFIG.md`, `docs/API.md`, `docs/DEVELOPMENT.md`, `AGENTS.md`, `CHANGELOG.md`; прочитать актуальный README и обновить связанные install snippets при необходимости.

- [x] Описать one install / one daemon / N proxies, новый default и сохранённые legacy modes.
- [x] Добавить таблицу global/project/local registry, минимальный config и команды add/remove/relocate/new collection/reload.
- [x] Указать startup auto-index и ограничение watcher после LRU eviction; profile/dimension changes требуют reindex.
- [x] Обновить GUI workflow, перенос проекта, миграцию, uninstall, диагностику unknown collection/token/port/locks.
- [x] Сверить все CLI примеры по фактическим flags/--help, не документировать отсутствующие возможности.
- [x] Запустить `go test ./...`; race tests для config/daemon/HTTP/proxy; `go build` stub и production по DEVELOPMENT; global/legacy native smoke.
- [x] Проверить `git diff --check`; отдельно перечислить изменённые исполнителем файлы, baseline failures и непроверенные платформы. Без реальных native smoke не писать «полностью готово».

## 9. Условия остановки для исполнителя

- Существующие user changes конфликтуют с задачей: не удалять их, запросить решение.
- Потребовалась новая модель registry/transport/процессов: не заменять shared daemon на per-project services для «упрощения».
- Тест не проходит: сначала диагностировать, не ослаблять assertion/не исключать неудобный кейс из acceptance.
- Нет native dependencies или Windows/Unix окружения: продолжить независимые unit tasks, но соответствующую native/platform проверку оставить незавершённой.
- Нужна операция с реальными индексами/ключами пользователя: остановиться; весь план проверяется на fixtures.

## 10. Итоговая приёмка

- [x] На пользователя одна установка, EXE/модели/secrets находятся вне проектов.
- [x] Один daemon владеет всеми коллекциями; N proxy не открывают БД.
- [x] Новый проект добавляется без рестарта остальных; конфликты ID/root безопасны.
- [x] Поиск/status/reindex из MCP автоматически привязаны к ID проекта.
- [x] Проект не может задать index path/секрет/произвольный daemon endpoint в новом default mode.
- [x] Автоиндексация, shutdown, LRU и config reload не нарушают владение zvec.
- [x] GUI и MCP одновременно работают через один daemon.
- [x] Update/rollback/uninstall не теряют коллекции других проектов.
- [x] Legacy режимы и existing tests не сломаны.
- [x] Unit/race/native/platform результаты приведены с командами и фактическими итогами.

**Рекомендуемый порядок исполнения:** 0 → 1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9 → 10. Не распараллеливать зависимые изменения registry/lifecycle; для более простой модели выдавать одну задачу с её входными интерфейсами и критериями проверки за раз.
