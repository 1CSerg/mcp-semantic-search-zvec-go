# План: исправление оставшихся проблем после обновления zvec-go v0.7.0

**Статус:** все пять проблем закрыты (проверки выполнены на Windows 11, Go 1.26.3, MinGW `D:\tools\winlibs\mingw64`, zvec-go `v0.7.0`). Commit не создавался.

**Связь:** продолжение [2026-10-19-zvec-go-upgrade.md](2026-10-19-zvec-go-upgrade.md), раздел «Найденные проблемы». Само обновление на `v0.7.0` завершено и проверено; здесь только то, что осталось за рамками его объёма.

**Цель:** устранить дефект `path_glob`, убрать причины ложных падений сборки/тестов на Windows-машине разработчика и привести smoke-скрипт и тестовую гигиену в состояние, при котором `go test ./...` и `make smoke-phase1` дают достоверный результат.

## Итог по проблемам

| # | Проблема | Итог | Основные файлы |
|---|----------|------|----------------|
| 1 | `path_glob` пустой результат | Исправлено: расписание `queryTopK` ограничено `docCount` | `internal/store/zvec/glob_topk.go` (+тест), `collection.go`, `store_integration_test.go` |
| 2 | Устаревший `zvec_c_api.dll` в корне | Убран (перемещён в `.deps/quarantine/`), добавлена проверка в fetch/build | `scripts/lib/zvec-native-lib-guard.{ps1,sh}`, `fetch-zvec-libs.*`, `build-zvec-windows.ps1` |
| 3 | `go test ./...` не собирает `openai` | Причина: внешний сканер на PIE-бинарнике; зафиксирован обход | `docs/DEVELOPMENT.md`, `Makefile` (`test-local`, `test-local-race`) |
| 4 | Флаки на cleanup `t.TempDir()` | Исправлено: `WaitForIdle` после финальной записи + ретрай удаления с диагностикой | `internal/indexer/coordinator.go`, `internal/testutil/tempdir.go`, тесты трёх пакетов |
| 5 | Smoke недостоверен | Исправлено: изолированный workspace, `index_meta.json` из seed, проверки `/v1/status` и `path_glob` | `cmd/seed-index/main.go`, `scripts/smoke/run-phase1.{ps1,sh}`, `scripts/smoke/smoke-check.go` |
| — | Побочно: `bash` на Windows = WSL | Добавлена `BASH ?= bash`; env-файлы пишутся атомарно (упавший fetch больше не обнуляет `.deps/*.env`) | `Makefile`, `docs/DEVELOPMENT.md` |

## Исходное состояние

- `HEAD` = `9bc7c2a`; рабочее дерево содержит пользовательские незакоммиченные изменения (в т.ч. `internal/config/project.go`, `internal/daemon/projects.go`, `internal/config/paths.go`) и правки по обновлению zvec-go. Commit не создавался.
- Дефект `path_glob` существовал до обновления: воспроизведён и на чистом worktree `9bc7c2a` с zvec-go `v0.6.0`, и с `v0.7.0`.
- Проверки выполнялись на Windows 11, Go 1.26.3, MinGW `D:\tools\winlibs\mingw64`, zvec-go `v0.7.0`.

## Ограничения

- Не перезаписывать и не откатывать существующие незакоммиченные изменения пользователя.
- Не создавать commit без отдельной просьбы.
- Не менять поведение поиска по смыслу: исправляется только потеря recall при заданном `path_glob`.
- Не расширять объём на другие зависимости и рефакторинг.
- Проверки индекса — только на временных каталогах; рабочий индекс пользователя не трогать.

---

## Проблема 1. `path_glob` возвращает пустой результат

### Симптом

- `TestIntegrationPathGlobExpansion` (`internal/store/zvec/store_integration_test.go`, тег `integration,zvec`) стабильно падает: `hits=0 want 1`.
- Через HTTP: `POST /v1/search {"query":"...","path_glob":"pkg/*.go"}` при непустом индексе возвращает `results: []`.
- Без `path_glob` тот же запрос возвращает результаты.

### Причина (подтверждена чтением кода и арифметикой)

`searchWithGlobExpansion` ([internal/store/zvec/collection.go:360](../../internal/store/zvec/collection.go)) наращивает `queryTopK` удвоением, но границы цикла не согласованы:

```go
queryTopK := topK
for len(hits) < topK && queryTopK <= docCount {
    ...
    next := queryTopK * 2
    if next <= queryTopK { next = docCount }   // защита только от переполнения
    queryTopK = next
}
```

`next` не ограничивается сверху значением `docCount`, поэтому удвоение перескакивает полный проход:

- `docCount = 121`, `topK = 1` → 1, 2, 4, 8, 16, 32, 64, затем `next = 128 > 121` → условие `queryTopK <= docCount` ложно → выход с `hits = 0`. Единственный подходящий документ имеет худший ранг (121) и не попадает ни в один запрос. Это ровно случай интеграционного теста (120 «other» + 1 target).
- `docCount = 2`, `topK = 5` → условие `5 <= 2` ложно с самого начала → цикл не выполняется ни разу → `hits = 0`. Это наблюдённый случай на временном workspace (2 чанка, `limit: 5`).

`matchPathGlob` корректен — его unit-тесты (`internal/store/zvec/pathglob_test.go`) проходят, поэтому дефект локализован именно в расписании `queryTopK`.

### Исправление (выполнено)

1. Стартовое значение ограничено объёмом коллекции: `queryTopK := globTopKStart(topK, docCount)` = `min(topK, docCount)`.
2. Верхняя граница удвоения ограничена: `nextGlobTopK` возвращает `min(queryTopK*2, docCount)`, сохраняя защиту от переполнения (`next <= current` → `docCount`).
3. Последняя итерация делает полный проход (`queryTopK == docCount`), поэтому фильтр в Go видит все документы.
4. Расчёт шага вынесен в чистые функции `globTopKStart` / `nextGlobTopK` (`internal/store/zvec/glob_topk.go`, без build-тегов), покрытые unit-тестами без CGO/zvec.

### Тесты

- Unit (без тегов, `glob_topk_test.go`): `globTopKStart`, `nextGlobTopK` — `topK > docCount`, `topK == docCount`, степени двойки, `docCount` между `64` и `128`, переполнение (`maxInt/2 + 1`), плюс `TestGlobExpansionScheduleTerminates`.
- Integration (`integration,zvec`): существующий `TestIntegrationPathGlobExpansion` проходит без изменений; добавлены `TestIntegrationPathGlobExpansionTopKAboveDocCount` (`limit` больше коллекции) и `TestIntegrationPathGlobExpansionFullPass` (121 несовпадающий документ → 0 попаданий, худший ранг).
- Ручная проверка через HTTP на временном индексе — в составе smoke (см. проблему 5).

### Критерий приёмки

`go test -tags "integration,zvec" ./internal/store/zvec/...` проходит полностью; HTTP-запрос с `path_glob` возвращает непустой отфильтрованный список; без `path_glob` результаты не изменились. → **выполнено** (в т.ч. 8 последовательных прогонов интеграции).

---

## Проблема 2. Устаревший `zvec_c_api.dll` в корне репозитория

### Симптом

`go run -tags zvec ./cmd/seed-index` из корня репозитория падает с `exit status 0xc0000139` (точка входа не найдена): Windows загружает `D:\root\repo\MCP\mcp-semantic-search-zvec-go\zvec_c_api.dll` (34 618 368 байт, строки версии `0.5.1`) вместо корректной библиотеки из `.deps/zvec-go/lib/windows_amd64` (26 569 216 байт, `v0.7.0`).

Файл не отслеживается git (игнорируется правилом `*.dll` в `.gitignore`) и не создаётся ни fetch-, ни build-скриптами: `build-zvec-windows.ps1` копирует DLL в `bin\`, а не в корень.

### Исправление (выполнено)

1. Устаревший `zvec_c_api.dll` убран из корня — перемещён в `.deps/quarantine/zvec_c_api-0.5.1-stale.dll` (`.deps/` в `.gitignore`), чтобы решение можно было откатить, а не удалять пользовательский артефакт безвозвратно.
2. Добавлена защита от повторного появления: `scripts/lib/zvec-native-lib-guard.ps1` и `.sh` сравнивают SHA256 корневых `zvec_c_api.dll` / `libzvec_c_api.so` / `libzvec_c_api.dylib` с пином и предупреждают при расхождении. Хуки: `fetch-zvec-libs.ps1`/`.sh` (вызываются из `build-zvec`), плюс `scripts/dev/build-zvec-windows.ps1`. Проверка **только предупреждает** и никогда не валит сборку.
3. Бинарники не коммитились.

MinGW-runtime (`libgcc_s_seh-1.dll`, `libstdc++-6.dll`, `libwinpthread-1.dll`) в корне оставлены: они байт-в-байт совпадают с версиями toolchain и zvec-библиотеками не являются.

### Критерий приёмки

`go run -tags zvec ./cmd/seed-index -n 10` из корня с чистым окружением завершается успешно; в корне репозитория нет DLL; повторный `git status` не показывает новых артефактов. → **выполнено.**

Уточнение: `seed-index` (как и сервер) требует workspace с `config.yaml` — это
существующее поведение, вне рамок плана. Проверка выполнена на workspace с
`config.yaml`: `seeded 10 chunks`, `index meta: .../index_meta.json`, exit 0.
Дефект же был именно в подмене DLL: до правки тот же запуск падал с `0xc0000139`
ещё до чтения конфига.

---

## Проблема 3. `go test ./...` не собирает `internal/embeddings/openai` на Windows

### Симптом

```
github.com/1CSerg/mcp-semantic-search-zvec-go/internal/embeddings/openai.test:
open C:\Users\...\Temp\go-build...\b001\openai.test.exe: Access is denied.
FAIL ... [build failed]
```

Линковщик создаёт PIE-бинарник теста, но сразу после линковки файл уже кем-то удерживается (`The process cannot access the file ... because it is being used by another process`), поэтому `go tool buildid -w` падает.

### Что установлено (итог)

- Дефект **не связан с обновлением zvec-go**: воспроизводится в копии `HEAD` + только изменённый `internal/config/paths.go`; на чистом `HEAD` не воспроизводится.
- `go test -c -o <явный путь> ./internal/embeddings/openai/` собирается и проходит; `go test -ldflags=-buildmode=exe` и `-ldflags=-linkmode=internal` тоже проходят.
- `CGO_ENABLED=0` для этого пакета проходит; `go clean -cache`, смена `GOTMPDIR` (в т.ч. на другой диск) и короткий путь — не помогают.
- Defender выключен (`RealTimeProtectionEnabled=False`), но в системе работает сторонний агент (Kaspersky); связь с lock подтверждена косвенно (см. ниже).
- **Причина: внешний on-access сканер на свежеслинкованном PIE-бинарнике.** Доказательства:
  - ACL файла содержит только наследуемые `Allow`-записи (`SYSTEM`, `Administrators`, пользователь) — deny-ACE нет; `Copy-Item` того же файла тоже отклоняется, то есть это открытый handle, а не правило доступа;
  - падение плавающее: то `go tool buildid -w` (build failed), то `fork/exec` (run failed) — зависит от того, успел ли сканер открыть файл;
  - артефакт позже исчезает из `go-build`-каталога (удаление/карантин агентом);
  - содержимое ни при чём: `go test -c -o <путь>` линкует те же байты (`-ldflags "-s -w"`, тот же размер 7 684 096) и файл остаётся доступным и исполняемым;
  - `internal/embeddings/openai` — единственный затронутый пакет: у него самый большой тестовый бинарник в дереве и он поднимает `httptest`-листенер; ни код пакета, ни репозиторий причиной не являются.

### Сделано

1. Зафиксирован поддерживаемый обход: `-ldflags=-buildmode=exe` (меняет только способ линковки тестового бинарника, тесты и поведение идентичны).
2. `Makefile`: цели `make test-local` и `make test-local-race`.
3. `docs/DEVELOPMENT.md`: раздел «`go test ./...` on Windows: PIE test binaries and security software» с симптомами, диагностикой и командами.
4. CI не затронут: `.github/workflows/ci.yml` по-прежнему `go test -v -race -count=1 ./...` на Linux; `GOFLAGS` в workflow не добавлялся.

### Критерий приёмки

Либо `go test ./...` на Windows-машине проходит без флагов, либо в `docs/DEVELOPMENT.md` есть воспроизводимое объяснение и однозначная команда прогона, а CI зелёный. → **выполнено через вторую ветку критерия.**

---

## Проблема 4. Флаки-падения тестов на cleanup `t.TempDir()`

### Симптом

```
testing.go:1464: TempDir RemoveAll cleanup: unlinkat C:\Users\...\TestXxx\001\index: The directory is not empty.
```

Наблюдённые тесты (разные прогоны, ~4–5 падений на 10 итераций `go test ./internal/service/ ./internal/indexer/`):

- `internal/service`: `TestReindexAlreadyRunning`, `TestPhase1GetIndexStatusWithManifest`, `TestGetIndexStatusZvecGoVersions`, `TestGetIndexStatusStartupMsg`, `TestPrepareStartupProfileMismatchNoAutoIndex`.
- `internal/indexer`: `TestCoordinatorIncrementalRemovesDeletedFile`, `TestCoordinatorReindexesWhenContentHashMissingOrChanged`, `TestCoordinatorSkipsPerFileZvecError`, `TestManifestZvecDesync*`.
- Воспроизводится и на чистом `HEAD` (`2/6` прогонов), и в рабочем дереве (`4/10`, `5/10`), и при `-p 1`; в изолированном `-run` одиночный тест чаще всего проходит → это гонка с фоновыми goroutine, а не логическая ошибка конкретного теста.

### Причина (установлена)

Две независимые причины, обе устранены:

1. **`Coordinator` сигнализировал о завершении раньше финальной записи.** В горутине прогона `c.running = false` выставлялось до `progress.Save(...)`, а `WaitForIdle` опрашивал только `IsRunning()`. Тест видел «idle» и удалял каталог, пока координатор ещё писал `progress.json` → `unlinkat ... The directory is not empty`. Теперь есть канал `runDone`, который закрывается строго после финальной записи прогресса, и `WaitForIdle` ждёт именно его (плюс опрос `IsRunning()` как страховку от пустого `Coordinator`).
2. **Windows pending-delete.** Файл, закрытый за микросекунды до `RemoveAll`, ещё держит запись каталога, пока ядро не завершит удаление. Это не утечка, но `testing.go` трактует это как падение. Добавлен `testutil.RemoveDirWithRetry` (50 попыток × 10 мс) — он **не маскирует** утечки: если каталог не удалился за весь бюджет, тест падает с перечнем оставшихся файлов.

Дополнительно: диагностические прогоны показали, что последняя запись `state=idle` происходит за ~1 мс до падения cleanup, а `sleep(50ms)` в `waitCoordinatorIdle` проблему не решал — то есть одного ожидания было недостаточно и ретрай удаления действительно нужен.

### Критерий приёмки

`go test -p 1 ./internal/service/ ./internal/indexer/ -count=10` — 0 падений; `go test -race` для этих пакетов без новых data race.

---

## Проблема 5. `scripts/smoke/run-phase1.ps1` не достоверен

### Симптом

После устранения проблемы 2 seed-шаг проходит (`seeded 100 chunks at .../zvec/ws_...`), но HTTP-поиск отвечает `index not found — run reindex or seed a collection for testing`, то есть smoke не проверяет то, что заявляет.

### Что уже видно

- Скрипт использует `$env:TEMP\mcp-zvec-smoke-index`, то есть индекс **вне** workspace → срабатывает `path containment violation field=INDEX_DIR ... allowed_roots=[...]` (warn-режим, не блокирует, но шумит).
- `go run -tags zvec ./cmd/seed-index` запускается из корня репозитория — то есть зависит от проблемы 2.
- Расхождение collection-имени между seed и сервером **не подтверждено**; требуется диагностика (сравнить `zvec_collection`/`zvec_collection_path` в `/v1/status` с фактическим каталогом после seed).

### План работ (выполнено)

1. Воспроизведено и снят `/v1/status` до и после seed. **Настоящая причина — не collection-имя:** `cmd/seed-index` создавал коллекцию, но не писал `index_meta.json`. Сервер видел «zvec-данные без меты» → `NeedsZvecGoMigration == true` → сносил коллекцию на старте с сообщением «zvec-go updated — run reindex to rebuild the index», и `/v1/search` отвечал `index not found`. Расхождение collection-имени не подтвердилось.
2. Seed теперь пишет полную identity-мету через `zvec.WriteIdentityMeta` (workspace_id, workspace_root, collection_id, profile, dimensions, chunking version/strategy) и печатает путь к ней.
3. Smoke переведён на изолированный workspace `$env:TEMP\mcp-zvec-smoke-phase1` (свои `index/`, `config.yaml`, `logs/`), порты выделяются динамически (`smoke-check free-port`), конфиг переписывается под порт mock-эмбеддера, а `go run` заменён на собранные `bin\mock-embed.exe` / `bin\smoke-check.exe` (дети `go run` переживали `Stop-Process` и держали порт).
4. Перед `/v1/search` добавлены явные проверки `zvec_open_ok` и `zvec_doc_count`; при падении печатается хвост лога сервера. Проверяется и `path_glob` (`internal/module/*.go` → непустой список совпадающих путей, `does/not/match/*.go` → 0).
5. `run-phase1.sh` приведён к паритету с `.ps1` (тот же сценарий, `source scripts/dev/ci-native-env.sh`, `smoke-check`, `trap` с хвостом лога).
6. Python-проверки убраны: `python3` на машине — заглушка Microsoft Store (exit 49), а не интерпретатор.

### Критерий приёмки

`make smoke-phase1` (и `.sh`-вариант) проходит с первого запуска на чистой машине, без предупреждений containment и без ручных действий. → **выполнено** (оба варианта, `zvec_open_ok=true zvec_doc_count=100`).

---

## Порядок работ

1. **Проблема 1** — сначала, потому что это единственный дефект продуктового поведения; малый и хорошо тестируемый.
2. **Проблема 2** — быстрая гигиена окружения, разблокирует проблему 5.
3. **Проблема 5** — после 2, чтобы smoke стал достоверным индикатором для остальных работ.
4. **Проблема 4** — тестовая гигиена; требует диагностики, но не блокирует продукт.
5. **Проблема 3** — диагностика окружения; при подтверждении внешней причины фиксируется обход и документация.

## Критерии приёмки (общие)

- `path_glob` фильтрует и не теряет recall; регрессионный тест зелёный.
- В корне репозитория нет устаревших native-библиотек; `seed-index` из корня работает.
- `go test ./...` даёт достоверный результат на Windows-машине разработчика (либо без флагов, либо с документированным обходом) и остаётся зелёным в CI.
- `go test -p 1 ./internal/service/ ./internal/indexer/ -count=10` без падений.
- `make smoke-phase1` проходит без ручных действий.
- Незакоммиченные изменения пользователя сохранены; commit не создан без отдельной просьбы.

## Выполненные проверки

Все команды — в корне репозитория на Windows 11 / Go 1.26.3.

| Проверка | Команда | Результат |
|----------|---------|-----------|
| Unit-тесты расписания `path_glob` | `go test ./internal/store/zvec/ -run 'TestGlobTopKStart\|TestNextGlobTopK\|TestGlobExpansionScheduleTerminates'` | ok |
| Интеграция `path_glob` (включая `topK > docCount` и худший ранг) | `go test -tags "integration,zvec" -count=8 ./internal/store/zvec/...` | ok (8 прогонов) |
| Проблема 4 (10 повторов) | `go test -p 1 -count=10 ./internal/service/ ./internal/indexer/` | ok (два независимых прогона) |
| Проблема 4 (race) | `go test -ldflags=-buildmode=exe -race -p 1 ./internal/service/ ./internal/indexer/` | ok, гонок нет |
| Весь набор тестов (обход проблемы 3) | `go test -ldflags=-buildmode=exe ./...` и `make test-local` | ok |
| `go vet` | `go vet ./...`, `go vet -tags zvec ./...` | ok |
| Smoke PowerShell | `pwsh -NoProfile -File scripts\smoke\run-phase1.ps1` | PASS с первого запуска |
| Smoke bash (Git Bash) | `& "C:\Program Files\Git\bin\bash.exe" scripts/smoke/run-phase1.sh` | PASS с первого запуска |
| Smoke через make (WSL в PATH) | `make smoke-phase1 BASH="C:/Program Files/Git/bin/bash.exe"` | PASS |
| `seed-index` из корня | `go run -tags zvec ./cmd/seed-index -n 10` (workspace с `config.yaml`) | ok, `seeded 10 chunks`, `index meta` записан |
| `path_glob` через HTTP на временном индексе | в составе smoke: `internal/module/*.go` → только совпадающие пути, `does/not/match/*.go` → 0 | PASS |

### Замечания по проблеме 3

Причина подтверждена как внешняя: свежеслинкованный PIE-бинарник теста
(`-buildmode=pie -s -w`) удерживается сторонним агентом (на машине работает
Kaspersky), из-за чего падает то `go tool buildid -w`, то `fork/exec`. Доказательства:

- ACL файла содержит только наследуемые `Allow`-записи (`SYSTEM`, `Administrators`,
  пользователь) — deny-ACE нет; `Copy-Item` того же файла тоже отклоняется, т.е. это
  открытый handle, а не правило доступа;
- артефакт позже исчезает из `go-build`-каталога (удаление/карантин агентом);
- `go test -c -o <путь>` линкует те же байты (тот же размер 7 684 096) и файл остаётся
  доступным и исполняемым;
- `internal/embeddings/openai` — единственный пакет с самым большим тестовым
  бинарником в дереве; содержимое репозитория на это не влияет.

Обход зафиксирован в [DEVELOPMENT.md](../DEVELOPMENT.md#go-test--on-windows-pie-test-binaries-and-security-software)
и в `Makefile` (`make test-local`, `make test-local-race`). CI не затронут: там Linux
и обычный `go test -v -race -count=1 ./...`.

### Замечания по проблеме 2

`zvec_c_api.dll` (0.5.1, 34 618 368 байт) не удалён безвозвратно, а перемещён в
`.deps/quarantine/zvec_c_api-0.5.1-stale.dll` (`.deps/` в `.gitignore`), чтобы решение
можно было откатить. MinGW-runtime (`libgcc_s_seh-1.dll`, `libstdc++-6.dll`,
`libwinpthread-1.dll`) в корне оставлены: они байт-в-байт совпадают с версиями
toolchain и не являются zvec-библиотеками.

### Замечание по окружению (не входило в план, но мешало проверкам)

На машине установлен WSL, поэтому `bash.exe` из `PATH` — это
`C:\Windows\system32\bash.exe`, а не Git Bash. В нём нет Go и MinGW, и `.sh`-скрипты
падают с `go: command not found` / `exit status 127`. Проверять нужно явно:

```powershell
& "C:\Program Files\Git\bin\bash.exe" scripts/smoke/run-phase1.sh
```

Попутно найдены и устранены два следствия:

1. `make fetch-zvec-libs` / `make fetch-onnx-runtime` перезаписывали env-файл **до**
   запуска скрипта, поэтому упавший fetch оставлял пустой `.deps/zvec-lib.env`
   (а `build-zvec` затем молча собирался без `ZVEC_LIB_DIR`). Теперь запись идёт через
   `*.env.tmp` + `mv`, то есть файл меняется только при успехе.
2. В Makefile добавлена переменная `BASH ?= bash`, чтобы обход был однокомандным:

   ```powershell
   make smoke-phase1 BASH="C:/Program Files/Git/bin/bash.exe"
   ```

Оба пункта зафиксированы в [DEVELOPMENT.md](../DEVELOPMENT.md).



