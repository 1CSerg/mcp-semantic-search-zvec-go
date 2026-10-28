# Обновление zvec-go до последней версии

**Статус:** выполнено и проверено (2026-10-19). Все пункты объёма работ закрыты; см. «Результаты проверки» и «Найденные проблемы» ниже.

**Цель:** завершить и проверить обновление `github.com/zvec-ai/zvec-go` до последнего опубликованного upstream-релиза `v0.7.0`, сохранив совместимость Go-обвязки, native-библиотек и сценариев сборки.

## Исходное состояние

Рабочее дерево уже содержит незакоммиченные изменения, включая частичное обновление pin с `v0.6.0` на `v0.7.0`. Эти изменения и прочие пользовательские правки нельзя перезаписывать или откатывать. Локальный `.deps/zvec-go` уже находится на `v0.7.0`, но содержит локальный ACP-патч и неотслеживаемые файлы, являющиеся частью текущего механизма патчинга.

На момент исследования GitHub API для `zvec-ai/zvec-go` указывает `v0.7.0` как latest release и latest tag. Upstream asset для Windows — `zvec-libs-windows-x64.zip`.

Источники:
- https://github.com/zvec-ai/zvec-go/releases/tag/v0.7.0
- https://api.github.com/repos/zvec-ai/zvec-go/releases/latest
- https://api.github.com/repos/zvec-ai/zvec-go/tags?per_page=10

## Ограничения

- Сохранить все существующие незакоммиченные изменения пользователя.
- Не выполнять несвязанный рефакторинг или обновление других зависимостей.
- Не создавать commit без отдельной просьбы пользователя.
- Не запускать дополнительный сервер на рабочем индексе и не сбрасывать пользовательский индекс ради проверки.
- Использовать существующий механизм загрузки native-библиотек и ACP-патчинга.

## Объём работ

### 1. Dependency pin и локальный исходник

- [x] Повторно проверить последнюю опубликованную upstream-версию перед реализацией.
- [x] Проверить согласованность `go.mod`: `require github.com/zvec-ai/zvec-go v0.7.0` и `replace => ./.deps/zvec-go`.
- [x] Проверить, что `.deps/zvec-go` соответствует tag `v0.7.0`, а ACP-патч применяется чисто и не теряется при повторном запуске fetch-скриптов.
- [x] Не менять API приложения без подтверждённого compile/test failure. Если v0.7.0 изменил используемый API, адаптировать только затронутые вызовы в `internal/store/zvec` и связанных build-tagged файлах.

### 2. Native runtime и fetch-скрипты

- [x] Проверить версии в `scripts/fetch/fetch-zvec-libs.ps1` и `scripts/fetch/fetch-zvec-libs.sh`: значение по умолчанию должно быть `v0.7.0`.
- [x] Проверить checksum native-библиотеки и соответствие `.zvec-lib-version` pin-у.
- [x] Проверить повторный запуск fetch: корректная версия и checksum не должны приводить к ненужному обновлению; несовпадение должно обнаруживаться существующим механизмом.
- [x] Не добавлять бинарные артефакты в git, если текущая политика репозитория их не отслеживает.

### 3. Документация и release metadata

- [x] Проверить упоминания версии в `CHANGELOG.md` и `docs/DEVELOPMENT.md`, включая раздел Versions и инструкции fetch/build; исправить только необходимые места.
- [x] Проверить `internal/version/version.go` и связанные release/build-конфигурации, включая `docker/Dockerfile`: `ZvecGoVersion` и pin должны отражать `v0.7.0`.
- [x] Проверить, что существующая логика миграции индекса считает смену версии поводом для reset/reindex.
- [x] Не включать несвязанные изменения из текущего рабочего дерева в объём обновления.

### 4. Совместимость и проверки

- [x] Запустить стандартные unit-тесты: `go test ./...` без zvec-тега.
- [x] Проверить build-tagged native-путь с `zvec,onnx,treesitter` через существующий Windows build/fetch workflow, описанный в `docs/DEVELOPMENT.md` и `scripts/dev/build-zvec-windows.ps1`.
- [x] Проверить, что production-бинарник собирается, выполняет `--version`, а native DLL доступна рядом с бинарником.
- [x] При наличии compile/API ошибок исследовать их по месту и добавить минимальные regression-тесты только для исправляемого поведения.
- [x] Запустить существующие тесты миграции: индекс с прежним `zvec_go_version` должен распознаваться как несовместимый и требовать предусмотренный reset/reindex. Использовать временные тестовые индексы.
- [x] Проверить итоговые git diff/status: отличать существовавшие пользовательские изменения от внесённых в рамках задачи.

## Критерии приёмки

- `go.mod`, локальный `.deps/zvec-go`, fetch-скрипты, native version marker и документация согласованы на `v0.7.0`.
- `go test ./...` проходит.
- Production build с тегами `zvec,onnx,treesitter` проходит на текущей Windows-среде либо причина блокировки зафиксирована с точной диагностикой; при блокировке обновление не объявляется полностью проверенным.
- Native library checksum проверяется существующим механизмом.
- Индексная миграция при смене zvec-go версии не сохраняет несовместимый индекс молча и предусматривает reset/reindex.
- Существующие пользовательские незакоммиченные изменения не потеряны.
- Commit не создан без отдельной просьбы пользователя.

## Результаты проверки

### Согласованность версии `v0.7.0`

| Место | Значение | Как проверено |
|-------|----------|---------------|
| GitHub `releases/latest` | `v0.7.0` (опубликован 2026-08-26) | `api.github.com/repos/zvec-ai/zvec-go/releases/latest` |
| `go.mod` | `require github.com/zvec-ai/zvec-go v0.7.0`, `replace => ./.deps/zvec-go` | чтение файла |
| `.deps/zvec-go` | `git describe --tags --exact-match` → `v0.7.0` | git |
| `internal/version/version.go` | `ZvecGoVersion = "v0.7.0"` | чтение + `TestZvecGoVersionMatchesGoMod` |
| `scripts/fetch/fetch-zvec-libs.ps1` | default `v0.7.0` | чтение |
| `scripts/fetch/fetch-zvec-libs.sh` | default `v0.7.0` | чтение |
| `docker/Dockerfile` | `ARG ZVEC_GO_TAG=v0.7.0` | чтение |
| `scripts/install/install.ps1` / `.sh` | fallback `v0.7.0` | чтение |
| `.deps/zvec-go/lib/windows_amd64/.zvec-lib-version` | `v0.7.0` | чтение |
| `CHANGELOG.md`, `docs/DEVELOPMENT.md`, `README.md` | `v0.7.0` | чтение |

Upstream diff `v0.6.0...v0.7.0` — один коммит `feat: bump zvec submodule to v0.7.0 and bind new C-API`; затронуты только аддитивные файлы (`iterator.go`, `purego_*.go`, `query.go`, `schema.go`, `types.go`) плюс submodule `zvec`. Используемый приложением API (`CreateAndOpen`, `Open`, `Upsert`, `Delete`, `Fetch`, `Search`, `GetStats`) не менялся — адаптация вызовов не потребовалась.

### ACP-патч

Свежий `git clone --depth 1 --branch v0.7.0` + `git apply --unidiff-zero collection.go.patch` → exit 0. Все 7 файлов патча (`cpath.go`, `path_unix.go`, `path_windows.go`, `path*_test.go`, `collection_cyrillic_integration_test.go`) и пропатченный `collection.go` побайтово совпадают с `.deps/zvec-go` (SHA-256 MATCH). Патч не теряется при повторном fetch.

### Native libs и checksum

- `zvec_c_api.dll` SHA-256 = `214d2dab…e180a1`, совпадает с `.zvec-lib-sha256` и с `bin/zvec_c_api.dll`.
- Повторный запуск `fetch-zvec-libs.ps1` на корректном индексе: `zvec libs already present`, DLL mtime не изменился (идемпотентно).
- Изолированный прогон в temp-корне: чистая установка → скачивание; повреждение DLL → `zvec native lib SHA256 mismatch; refreshing lib...` + перекачивание; подмена маркера на `v0.6.0` → `tag mismatch (marker=v0.6.0 want=v0.7.0); refreshing lib...`. Оба механизма обнаружения работают.
- Бинарные артефакты не добавлены в git (`bin/`, `*.dll` в `.gitignore`).

### Сборка и запуск

- `scripts/dev/build-zvec-windows.ps1 -Release` (теги `zvec,onnx,treesitter`, `-ldflags -s -w`) — успешно.
- `bin/mcp-semantic-search-zvec-go.exe --version` → `0.3.0`; `zvec_c_api.dll` и `onnxruntime.dll` лежат рядом с бинарником.
- `go test -tags "zvec,treesitter" ./internal/store/zvec/...` — ok.
- `go test -tags "integration,zvec" ./internal/store/zvec/...` — все проходят, кроме `TestIntegrationPathGlobExpansion` (см. ниже, дефект существовал до обновления).
- End-to-end на временном workspace и временном индексе: auto-index собрал 2 файла / 2 чанка, `zvec_open_ok=true`, `zvec_doc_count=2`, `/v1/search` вернул результаты с `path`, `score`, `chunk_strategy=ast`.
- `go test ./...` — проходит (см. примечание про линковщик ниже).

### Миграция индекса

- `NeedsZvecGoMigration` + `Phase1.runZvecGoMigrationIfNeeded`: при расхождении `version.ZvecGoVersion` с `index_meta.json.zvec_go_version` коллекция и `manifest.db` сбрасываются, при `AUTO_INDEX_ON_START=true` запускается force reindex, иначе выставляется `startupMsg`.
- Проверено на реальном бинарнике: подмена `zvec_go_version` на `v0.6.0` → лог `zvec-go version changed; resetting index from=v0.6.0 to=v0.7.0` → полная переиндексация → `index_zvec_go_version=v0.7.0`, 2 чанка, 2 документа.
- Юнит- и сервисные тесты миграции на временных индексах проходят: `TestNeedsZvecGoMigration*`, `TestResetIndexForZvecMigration*`, `TestPrepareStartupMigrates*`, `TestGetIndexStatusZvecGoVersions`.

### Внесённые изменения

Только исправление дефекта staged force reindex, который ломал сам путь миграции (см. ниже):

| Файл | Изменение |
|------|-----------|
| `internal/store/zvec/collection.go` | добавлен `OpenWritable()` (открытие на запись с созданием коллекции) |
| `internal/store/zvec/staging.go` | добавлены `writableOpener`, `OpenStaging`, `openStaging` |
| `internal/indexer/coordinator.go` | staging открывается через `zvec.OpenStaging`; promote закрывает staging и активную коллекцию до `rename` |
| `internal/store/zvec/staging_test.go` | 3 unit-теста на `openStaging` |
| `internal/store/zvec/store_integration_test.go` | `TestIntegrationOpenStagingOnCleanIndex` (build tag `integration,zvec`) |
| `CHANGELOG.md` | запись о фиксе |

## Найденные проблемы

### 1. Staged force reindex был сломан (исправлено)

**Симптом:** `POST /v1/reindex {"force":true}` на любом индексе (и чистом, и здоровом) завершался `indexing failed: zvec collection not found`, после чего force-путь падал ещё раз — `rename active collection: … Access is denied`.

**Причины (две):**

1. Staging-коллекция открывалась через `candidate.Open()`, а `CollectionStore.Open()` — read-only. На несуществующей staging-коллекции `openCollection(true)` возвращает `ErrCollectionMissing`, `openErr != nil`, и код уходил в ветку `else if !isZvecUnavailable(openErr) → return err`. То есть staged-сборка не могла начаться никогда.
2. После исправления (1) выяснилось, что `PromoteStagingCollection` делает `os.Rename` активной коллекции, пока `c.Zvec` держит открытые handles. На Windows это `Access is denied`.

**Почему это важно для задачи:** путь автомиграции при смене `zvec_go_version` вызывает `coordinator.Start(true)`, то есть именно force reindex. Без фикса критерий «миграция предусматривает reset/reindex» не выполнялся бы: индекс сбрасывался, а переиндексация падала.

**Проверка фикса:** force reindex на здоровом индексе → `state=idle`, `indexing complete`; миграция `v0.6.0 → v0.7.0` на реальном бинарнике → полная переиндексация до `index_zvec_go_version=v0.7.0`.

### 2. `path_glob` не фильтрует результаты (не исправлено, вне объёма)

`TestIntegrationPathGlobExpansion` падает (`hits=0 want 1`) и в рабочем дереве, и на чистом `HEAD` (worktree `9bc7c2a`, zvec-go `v0.6.0`), и с `v0.7.0`. Воспроизведено и через HTTP: `path_glob: "pkg/*.go"` → 0 результатов при непустом индексе. `matchPathGlob` покрыт unit-тестами и корректен, поэтому дефект локализован в `searchWithGlobExpansion` — в расписании `queryTopK`.

Причина подтверждена чтением кода и арифметикой: удвоение `next := queryTopK * 2` не ограничено сверху `docCount`, поэтому проход перескакивает полный объём и последняя итерация никогда не покрывает все документы. `docCount=121, topK=1` → 1, 2, 4, …, 64, затем `128 > 121` → выход с `hits=0` (совпадение имеет худший ранг и не попадает ни в один запрос); `docCount=2, topK=5` → условие `topK <= docCount` ложно с самого начала, цикл не выполняется ни разу.

Дефект существовал до обновления и к `v0.7.0` отношения не имеет — вынесен в отдельный план: [2026-10-19-post-upgrade-followups.md](2026-10-19-post-upgrade-followups.md).

### 3. Окружение: `go test` для `internal/embeddings/openai` и флаки-тесты (не связано с обновлением)

- На этой машине `go test ./...` без флагов падает на сборке `internal/embeddings/openai` с `open.test.exe: Access is denied` (линковщик не может перезаписать PIE-бинарник). Дефект среды: `go test -c -o <явный путь>` собирается и проходит, `go test -ldflags=-buildmode=exe` и `-linkmode=internal` тоже. Итоговый прогон: `go test -p 1 -ldflags="-buildmode=exe" ./...` — **все пакеты ok**.
- Часть тестов `internal/service` и `internal/indexer` флакует на `t.TempDir()` cleanup (`unlinkat … The directory is not empty`) — как в рабочем дереве, так и на чистом `HEAD` (например `TestCoordinatorIncrementalRemovesDeletedFile`, `TestCoordinatorReindexesWhenContentHashMissingOrChanged`, `TestPhase1ManifestStats`). Не связано с обновлением.
- В корне репозитория лежит устаревший `zvec_c_api.dll` (34 618 368 байт, строки версии `0.5.1`), не отслеживаемый git. Windows ищет DLL раньше в CWD, поэтому `go run -tags zvec ./cmd/seed-index` из корня падал с `0xc0000139` (точка входа не найдена). Файл **не удалён** (это пользовательский артефакт), но его стоит убрать: он затеняет корректную библиотеку из `.deps`. Проверки выполнялись при временно отложенном файле, после чего он возвращён на место.

### 4. `scripts/smoke/run-phase1.ps1` не достоверен (не исправлено, вне объёма)

После устранения проблемы 3 seed-шаг проходит (`seeded 100 chunks at .../zvec/ws_...`), но HTTP-поиск отвечает `index not found — run reindex or seed a collection for testing`, то есть smoke не проверяет то, что заявляет. Дополнительно скрипт кладёт индекс в `$env:TEMP\mcp-zvec-smoke-index`, то есть вне workspace, и получает `path containment violation field=INDEX_DIR`. Диагностика и исправление — в отдельном плане.

### Следующие шаги

Все четыре пункта выше вынесены в [2026-10-19-post-upgrade-followups.md](2026-10-19-post-upgrade-followups.md) с причинами, объёмом работ, тестами и критериями приёмки.

## Итог по критериям приёмки

| Критерий | Статус |
|----------|--------|
| Все pin/marker/доки согласованы на `v0.7.0` | ✅ |
| `go test ./...` проходит | ✅ (`-p 1 -ldflags="-buildmode=exe"`; причина отличия — дефект линковщика среды, см. п. 3) |
| Production build `zvec,onnx,treesitter` на Windows | ✅ собран, `--version` работает, DLL рядом |
| Native checksum проверяется существующим механизмом | ✅ (mismatch и tag-mismatch воспроизведены) |
| Миграция не оставляет несовместимый индекс молча, есть reset/reindex | ✅ (после фикса п. 1) |
| Пользовательские изменения не потеряны | ✅ (`git status` содержит те же файлы, что и до работы, плюс мои 6) |
| Commit не создан | ✅ (`HEAD` = `9bc7c2a`) |
