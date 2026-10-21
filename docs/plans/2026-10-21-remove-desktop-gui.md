# Удаление desktop GUI — план реализации

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` or `executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** полностью удалить нативный GUI MCP Semantic-search-zvec-go, сохранив сервер и инструменты управления; будущий веб-интерфейс реализуется отдельно.

**Architecture:** приложение остаётся headless Go-сервисом с явными режимами CLI, MCP stdio, HTTP, daemon и stdio-proxy. Удаляются Fyne UI и его клиентские адаптеры; существующие daemon, daemonclient, service и HTTPProxy сохраняются. Запуск без аргументов на любой ОС показывает справку и завершается, не инициализируя сервисы.

**Tech Stack:** Go 1.26.3 (текущий go.mod), существующие MCP SDK, HTTP, zvec, ONNX и tree-sitter. Fyne и исключительно GUI-транзитивные зависимости удаляются.

**Spec:** требования и границы зафиксированы в разделе 1 этого документа по запросу пользователя и уточнениям в диалоге. Это план, не разрешение на реализацию. Отдельная спецификация веб-интерфейса не входит в задачу.

## 1. Требования и глобальные ограничения

- Удаляется существующий Windows desktop GUI, а не только его автоматический запуск.
- Новый веб-интерфейс, HTML/JS assets, открытие браузера, новый web-сервер, CORS и новые API в эту работу не входят.
- Без аргументов на Windows/Linux/macOS: общая справка в stdout, exit code 0; без чтения конфигурации, создания каталогов, открытия коллекций и запуска daemon.
- `--help` и `-h`: справка, exit code 0, без побочных эффектов.
- `--gui` удаляется из регистрации флагов; обычный неизвестный флаг: диагностика и справка в stderr, exit code 2. Не делать алиас на HTTP, stdio или daemon.
- Сохранить `--version` / `-version`, явные режимы `--stdio`, `--http`, `--stdio --http`, `--daemon`, `--stdio-proxy` и команды управления проектами.
- Флаги конфигурации без режима, например `--config <path>`, по-прежнему дают ошибку выбора режима, exit code 2. Не превращать их в неявный stdio.
- Сохранить протокол MCP: служебная диагностика не попадает в stdout работающего stdio/proxy.
- Не менять HTTP routes, auth, redaction, выбор collection_id, схемы конфигурации, индекса или данных.
- Не удалять CGO из production: zvec, ONNX и tree-sitter по-прежнему его требуют. Убирается только GUI/OpenGL-зависимость.
- Не переносить GUI Session/Collections в новый абстрактный frontend/backend пакет ради гипотетического web-клиента.
- Не удалять пользовательские данные, auth.token, projects.d, project.yaml, настройки и индексы.
- Рабочее дерево содержит значительные сторонние незакоммиченные изменения, включая CLI, GUI, go.mod и документацию. Не откатывать их и не включать автоматически в коммиты этой задачи. Перед коммитами согласовать выделение изменений или подготовить чистую исходную точку.

## 2. Review Focus

1. Запуск без аргументов внутри незарегистрированного/невалидного проекта или при повреждённой глобальной конфигурации всё равно выводит справку и ничего не создаёт — Task 1.
2. Устаревший `--gui` не запускает сервер; комбинация с management-флагом не изменяет регистрацию — Task 1.
3. Активные MCP proxy/daemon не завершаются и не теряют коллекции при вызове справки или ошибочного GUI-флага — Tasks 1, 4.
4. После удаления GUI остаются штатный shutdown, восстановление прерванной индексации и защита от двойного владельца коллекции — Tasks 2, 4.
5. Windows stub действительно собирается без desktop CGO/OpenGL, но production продолжает работать с native-компонентами — Tasks 3, 4.

## 3. Наблюдения по текущему коду

- `cmd/mcp-semantic-search-zvec-go/main.go`: регистрация `--gui`, поле guiFlag, выбор GUI по умолчанию на Windows, runGUI и guiUsesGlobalCollections.
- `main_test.go`: тесты закрепляют прежние OS-specific defaults и конфликты GUI/серверных режимов.
- `internal/gui/gui_windows.go`: Fyne UI, polling статуса, поиск, reindex, legacy lock actions; build constraint windows && cgo.
- `internal/gui/collections.go`: клиентский слой выбора коллекций, Session, отмена запросов и подключение к daemon; не серверный источник данных.
- `internal/gui/gui_stub.go` и `gui_windows_nocgo.go`: платформенные заглушки GUI.
- `go.mod`: прямой fyne.io/fyne/v2 и множество UI-зависимостей.
- GUI описан в README, INSTALL, API, DEVELOPMENT, ARCHITECTURE, AGENTS и realworld checklist.
- В проверенных .github workflows и Makefile текстовых упоминаний GUI/Fyne не найдено. Менять их без установленной необходимости не нужно.
- MCP semantic-search tools не предоставлены текущей сессии; исследование выполнено точечным чтением и поиском. Перед реализацией повторить index_status и тематический semantic_search, если инструменты появятся.

## 4. Карта файлов

**Изменить:**
- `cmd/mcp-semantic-search-zvec-go/main.go`: headless entrypoint, справка, режимы запуска.
- `cmd/mcp-semantic-search-zvec-go/main_test.go`: новый контракт выбора режима.
- `go.mod`, `go.sum`: минимальный dependency graph без Fyne.
- `README.md`, `INSTALL.md`, `docs/API.md`, `docs/DEVELOPMENT.md`, `docs/ARCHITECTURE.md`, `AGENTS.md`: текущие пользовательские контракты.
- `tests/realworld/README.md`, `scripts/README.md`: убрать неактуальный GUI smoke, автоматизировать no-args контракт.
- `CHANGELOG.md`: breaking change запуска без аргументов и удаление --gui.

**Создать:**
- `cmd/mcp-semantic-search-zvec-go/cli_contract_test.go`: subprocess-контракты реального бинарника без GUI/сервисных побочных эффектов.

**Удалить:**
- `internal/gui/gui_windows.go`
- `internal/gui/gui_windows_test.go`
- `internal/gui/gui_windows_nocgo.go`
- `internal/gui/gui_stub.go`
- `internal/gui/gui_stub_test.go`
- `internal/gui/collections.go`
- `internal/gui/collections_test.go`

**Сохранить:** internal/daemon, internal/daemonclient, internal/service, internal/transport/http, internal/transport/mcp, lifecycle, lock, indexer и store. Тесты этих компонентов не удалять вместе с GUI-тестами.

## Task 1: закрепить headless CLI-контракт и убрать GUI из запуска

**Files:** main.go, main_test.go; create cli_contract_test.go.

**Interfaces:**
- Заменить `resolveRunMode(in runMode, goos string, nflag int) (runMode, error)` на `resolveRunMode(in runMode) (runMode, error)`.
- runMode содержит только stdio, stdioProxy, httpFlag, daemonFlag.
- `printUsage(w io.Writer, program string)` выводит единое описание режимов, management-команд и флагов; использовать для no-args и flag.Usage.
- `errNoMode` сохраняется; GUI-specific errModeConflict удаляется. Не менять существующий приоритет proxy/daemon в этой задаче.

- [ ] Добавить table tests: пустой runMode → errNoMode; stdio/http/stdio+http сохраняются; daemon включает http и выключает stdio; proxy сохраняет нынешнюю нормализацию.
- [ ] Добавить subprocess tests `TestCLIHeadlessContract` с общей stub-сборкой бинарника и отдельным temp-каталогом на кейс: no args, -h, --help → 0; --gui, --gui --project-add <temp-root>, --config <missing-path> → 2; --version/-version → 0. Для no-args/help stdout содержит поддерживаемые режимы, stderr пуст; для ошибок stderr непуст. В справке нет --gui.
- [ ] Во всех этих кейсах задать изолированные абсолютные MCP_GLOBAL_DIR и WORKSPACE_ROOT, CONFIG_PATH/ENV_PATH на отсутствующие пути; проверить, что до/после не появились global dir, project metadata и каталоги индекса. Повторить no-args/help с невалидным YAML в temp config: справка не зависит от валидности конфигурации.
- [ ] Subprocess должен завершаться за 10 секунд; по timeout завершить дочерний процесс и провалить тест. Не проверять отсутствие зависания открытым stdin без ограничения времени.
- [ ] Запустить новые тесты на старом коде и зафиксировать ожидаемый FAIL по no-args/--gui, а не падение из-за build environment. Для Windows stub использовать CGO_ENABLED=0.
- [ ] Реализовать ранний no-args/help путь до loadSettings, setupPerProject, signal/parent watchers и management dispatch. Назначить flag.Usage с выводом в FlagSet.Output; no-args выводит тот же текст явно в stdout.
- [ ] Удалить import gui, регистрацию и переменную guiFlag, GUI dispatch, runGUI, guiUsesGlobalCollections, defaultNoArgsModes и GUI-specific usage/error branches. Удалять runtime import только если больше не используется.
- [ ] Обновить вызов resolveRunMode и комментарии. Не рефакторить весь парсер CLI и не менять существующее раннее поведение --version с дополнительными аргументами.
- [ ] Запустить `go test ./cmd/mcp-semantic-search-zvec-go -count=1` с CGO_ENABLED=0; ожидается PASS. Просмотреть diff на сохранение management/project-proxy wiring.
- [ ] После согласованного выделения изменений создать отдельный коммит `refactor(cli): remove desktop GUI entrypoint`.

## Task 2: удалить GUI-пакет без удаления серверной логики

**Files:** семь файлов internal/gui из карты; main.go только при наличии осиротевших ссылок.

**Interfaces:** публичные в рамках репозитория server/service/daemonclient контракты не меняются; GUI Run, Session, Collections и их helpers исчезают.

- [ ] Перед удалением найти ссылки на internal/gui, GUI Run/Session/Collections, guiUsesGlobalCollections и setupPerProject во всём отслеживаемом исходном коде. Не включать .deps, build output и index data.
- [ ] Проверить использование lifecycle.PrepareWorkspaceLocks, FindStdioForWorkspace, RecoverInterruptedProgress, Phase1.Shutdown и HTTPProxy вне GUI. Сохранить общие реализации и тесты; не заменять их GUI-кодом и не удалять по названию.
- [ ] Если важный инвариант встречается только в удаляемом GUI-тесте, сначала перенести серверную проверку в пакет владельца: отсутствие второго владельца zvec — daemon/lifecycle; drain и отмена — service. Не переносить проверки виджетов или переключения UI-сессии.
- [ ] Проверить абсолютные пути семи удаляемых файлов и удалить только их. Учитывать, что collections.go и collections_test.go сейчас untracked: это тоже пользовательские изменения, не восстанавливать их из HEAD вместо удаления по согласованному плану.
- [ ] Оставить perProjectOptions и startup pipeline без дополнительного упрощения: удаление GUI не требует рефакторинга lifecycle. В частности, не менять порядок release locks/close service/runtime.
- [ ] Запустить `go test ./cmd/mcp-semantic-search-zvec-go ./internal/lifecycle ./internal/daemon ./internal/daemonclient ./internal/service ./internal/transport/http ./internal/transport/mcp -count=1` со stub-конфигурацией. Ожидается PASS; оставшиеся ссылки на GUI отсутствуют.
- [ ] После проверки diff создать коммит `refactor: remove native desktop GUI package`.

## Task 3: очистить зависимости и подтвердить сборку

**Files:** go.mod, go.sum; scripts/dev/build-zvec-windows.ps1 и workflows только при доказанной GUI-зависимости.

**Interfaces:** production tags остаются `zvec,onnx,treesitter`; replacement zvec-go остаётся `./.deps/zvec-go`.

- [ ] Снять до/после список модулей; удалить прямое требование fyne.io/fyne/v2 и выполнить `go mod tidy`.
- [ ] Если tidy не проходит из-за отсутствующего локального replacement, выполнить документированный fetch zvec-libs, затем повторить. Не менять replacement/версию библиотеки ради обхода ошибки.
- [ ] Просмотреть dependency diff: Fyne/systray/fyne-io/go-gl должны исчезнуть, если больше никем не нужны. Не удалять вручную x/sys, x/image или другие разделяемые модули только потому, что их использовал GUI.
- [ ] Повторить `go mod tidy`; go.mod/go.sum не должны измениться повторно.
- [ ] Запустить с CGO_ENABLED=0: `go test ./... -count=1`, `go vet ./...`, `go build -o <temp-output> ./cmd/mcp-semantic-search-zvec-go`. На Windows проверить dependency list через `go list -deps ./cmd/mcp-semantic-search-zvec-go`: Fyne/OpenGL/GLFW отсутствуют.
- [ ] Проверить stub на Windows и Linux (CI/реальный runner), выполнить cross-build stub для darwin без заявления, что cross-build заменяет runtime smoke.
- [ ] Выполнить production Windows build штатным scripts/dev/build-zvec-windows.ps1 и production Linux build через make build-zvec с подготовленными native deps. Не отключать CGO и не менять shipped tags.
- [ ] Зафиксировать фактические результаты, а недоступные платформы/native deps перечислить как непроверенные. Коммит `build: drop Fyne desktop dependencies`.

## Task 4: регрессия сервера, установки и существующих коллекций

**Files:** cli_contract_test.go; существующие project_commands_test.go, project_proxy_test.go, internal/transport/http/admin_test.go при необходимости дополнительных assertions. Использовать существующие smoke scripts.

**Interfaces:** HTTP/MCP/management контракты неизменны; новый контракт только у запуска без аргументов и удалённого --gui.

- [ ] На изолированной temp-установке запустить штатный daemon, зарегистрировать два проекта и подключить proxy согласно существующему global smoke. Не использовать рабочий пользовательский MCP_GLOBAL_DIR.
- [ ] При активном daemon и proxy выполнить no args, --help и --gui. Проверить exit codes 0/0/2, неизменность PID daemon и registrations, успешность повторного index_status через оба proxy. Убедиться, что тест не запускает второй daemon и не завершает чужие процессы.
- [ ] Проверить существующие HTTP health/status/search/reindex, auth и redaction тестами `go test ./internal/transport/http ./internal/transport/mcp ./internal/daemonclient -count=1`.
- [ ] Выполнить `scripts/smoke/run-global-daemon.ps1` и `scripts/smoke/run-global-install.ps1`; Unix counterparts запускать на реальном Unix runner. Сначала прочитать параметры скриптов и обеспечить отдельные каталоги, чтобы не затронуть активную установку.
- [ ] Production smoke: stdio handshake/list tools, proxy к зарегистрированной collection, HTTP status, incremental reindex/search, штатная остановка с незавершённым запросом и повторное открытие коллекции без orphan locks. Использовать существующие lifecycle/service/native tests, а не новый UI harness.
- [ ] Проверить, что install по-прежнему создаёт явные --stdio / --stdio-proxy launchers; пользовательские данные и collection_id сохраняются при обновлении. Если GUI-only shortcut не обнаружен, не добавлять миграционное удаление неизвестных ярлыков.
- [ ] Запустить доступные tagged native tests по docs/DEVELOPMENT.md. Race tests — на поддерживаемом runner с CGO/toolchain; если окружение не поддерживает их, отметить отдельно от обычных unit tests.
- [ ] Тестовые дополнения выделить коммитом `test: cover headless CLI and daemon regressions`.

## Task 5: документация и выпуск

**Files:** README.md, INSTALL.md, docs/API.md, docs/DEVELOPMENT.md, docs/ARCHITECTURE.md, AGENTS.md, tests/realworld/README.md, scripts/README.md, CHANGELOG.md.

- [ ] README: убрать GUI из позиционирования и примеров. Показать no-args/help и явные серверные режимы.
- [ ] API: удалить --gui из таблицы и конфликтов management-команд; описать no-args/help → 0, неизвестный --gui → 2, config-only → ошибка. Явно назвать изменение Linux/macOS default, а не только Windows.
- [ ] INSTALL: заменить GUI-инструкции CLI/MCP/HTTP-эквивалентами; force rebuild выполняется через reindex force:true, проекты регистрируются --project-add. Удалить инструкции нажатия GUI-кнопок. Сохранить общие lock/recovery рекомендации с проверенными headless способами.
- [ ] DEVELOPMENT: убрать Fyne toolchain и Windows default GUI; оставить native prerequisites zvec/ONNX/tree-sitter и режимы явного запуска.
- [ ] ARCHITECTURE: удалить desktop GUI node/edges и GUI-specific lifecycle текст; не добавлять ещё не реализованный Web UI. Сохранить daemon/client/HTTPProxy связи.
- [ ] AGENTS: заменить GUI troubleshooting на headless диагностику, сохранить семантический поиск и команды регистрации проектов.
- [ ] Realworld README: удалить T6 GUI checklist, CLI5 заменить ссылкой на автоматический TestCLIHeadlessContract. Обновить упоминания manual GUI в scripts README и DEVELOPMENT.
- [ ] CHANGELOG: добавить запись в текущую unreleased секцию, не стирая историю; перечислить удаление --gui, Fyne и breaking no-args default. Старые release notes и исторические планы не переписывать.
- [ ] Поиск по актуальному коду и документации: нет обещаний GUI/default stdio без аргументов. Упоминания GUI допустимы в changelog, этом плане, исторических планах и отрицательных regression tests.
- [ ] Проверить ссылки Markdown и примеры команд; коммит `docs: describe headless-only application`.

## 5. Порядок, риски и критерии приёмки

**Порядок:** Tasks 1 → 2 → 3 → 4 → 5. Документацию можно готовить параллельно после фиксации CLI-контракта; изменения общего main.go и go.mod не распараллеливать.

**Главные риски:**
- Скрытое использование запуска без аргументов интеграциями Linux/macOS: предупредить в changelog, в примерах всегда указывать режим.
- Незакоммиченная global-install работа: повторно проверить исходную точку перед правками, не делать git reset/clean, blanket git add или checkout пользовательских файлов.
- Потеря общих shutdown/lock механизмов при удалении GUI: удалять consumer, а не backend; подтвердить regression tests и native smoke.
- Чрезмерная очистка зависимостей: ориентироваться на go mod tidy и dependency graph, не на названия модулей.
- Ложная уверенность по Windows stub: отдельно проверить production с CGO и native libs.

**Готово, когда:**
1. Desktop GUI package, --gui wiring и Fyne dependencies отсутствуют.
2. No-args/help завершаются со справкой и exit 0 на поддерживаемых ОС без side effects; --gui даёт exit 2.
3. Явные серверные режимы, управление проектами и MCP stdout-контракт сохранены.
4. Данные, auth, schemas и collection ownership не изменены; daemon/proxy smoke проходит.
5. Stub и production проверки имеют записанные результаты, непроверенные пункты явно обозначены.
6. Актуальная документация описывает headless-only продукт; будущий web-интерфейс не реализован в этом изменении.

## 6. Оценка и откат

Ориентир: 1–2 рабочих дня на реализацию, очистку документации и stub-проверки; до ещё 1 дня на native/cross-platform smoke в зависимости от доступности окружений. Это оценка, не обязательство по срокам.

Откат: отменить только выделенные коммиты этой задачи либо вернуть предыдущий release-бинарник штатным механизмом обновления. Миграции данных отсутствуют, переиндексация из-за удаления GUI не требуется. Не восстанавливать весь working tree из HEAD поверх сторонних изменений.

## 7. Статус подготовки

План составлен по текущему рабочему дереву. Изменения продукта, удаление файлов, сборки и тесты в рамках подготовки плана не выполнялись. Перед исполнением требуется ревью плана пользователем и выбор способа реализации.
