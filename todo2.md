# Ревью ошибок проекта — остаточные замечания

Дата проверки: 2026-07-22.
Проверен коммит `5ef387a` «Исправления по ревью: целостность индекса и runtime
hardening».

Этот файл содержит **только не реализованные или частично реализованные**
замечания из `todo.md`. Полностью закрытые пункты (4, 6, 7, 10, 12, 13, 15, 16,
17, 18, 19) сюда не включены.

## Сводка

| # | Пункт | Статус |
|---|-------|--------|
| 1 | Коллизии `DocID` | ⚠️ Частично |
| 2 | Copy-on-write обновление файла | ⚠️ Частично |
| 3 | Частичный успех native write/delete | ⚠️ Частично |
| 5 | Не уничтожать индекс в начале force reindex | ⚠️ Частично |
| 8 | Семантика `path_glob` | ⚠️ Частично |
| 9 | Не терять ошибку native `Close` | ⚠️ Частично (нет регр.-теста) |
| 11 | Блокирующая ошибка `golangci-lint` | ❌ Регресс/блокер |
| 14 | Согласовать transport и embedding timeouts | ⚠️ Частично |

---

## ❌ Блокер

### [ ] 11. Исправить новые ошибки `golangci-lint`

Исходная ошибка `func systemACPToUTF16 is unused` **устранена**: файл
`scripts/fetch/patches/zvec-go-acp/path_windows_test_helper.go` переименован в
`path_windows_test_helper_test.go`, списки копирования обновлены в обоих
fetch-скриптах (`fetch-zvec-libs.ps1`, `fetch-zvec-libs.sh`).

**НО** `golangci-lint run ./...` всё ещё завершается с **exit 1** — появились
**2 новые ошибки**:

```
internal\indexer\coordinator.go:295:17: ST1023: should omit type zvec.Store
    from declaration; it will be inferred from the right-hand side (staticcheck)
	var zvecTarget zvec.Store = c.Zvec

internal\transport\http/server.go:350:6: func sanitizeDaemonStatusText
    is unused (unused)
	func sanitizeDaemonStatusText(s string) string {
```

- `sanitizeDaemonStatusText` (`server.go:350`) — мёртвый код, оставшийся после
  рефакторинга санитизации путей в рамках п.16: функцию заменила
  централизованная `redactDaemonStatusPaths`, вызовов не осталось.
- `var zvecTarget zvec.Store = c.Zvec` (`coordinator.go:295`) — стиль staticcheck
  `ST1023`.

**Правки:**

1. Удалить функцию `sanitizeDaemonStatusText` целиком в
   `internal/transport/http/server.go:350`.
2. В `internal/indexer/coordinator.go:295` убрать явный тип:
   `var zvecTarget = c.Zvec`.
3. Проверить `golangci-lint run ./...` — должен быть зелёным (exit 0).

---

## ⚠️ Частично реализованные (P0)

### [ ] 1. Устранить коллизии `DocID` (доработать)

**Что сделано:**

- Единый генератор `internal/indexer/chunk/docid/docid.go` (`Make`, `AssertUnique`)
  вместо трёх копий; старая логика удалена из `ast/config.go`, `prose/config.go`,
  `prose/markdown.go`.
- В ID включены `ChunkIndex` (порядковый номер), `ChunkStrategy`, `ChunkType`,
  `SymbolName` и `contentFingerprint(Snippet)` (sha256).
- `AssertUniqueDocIDs` вызывается в `ProcessBatches` (`chunk/batches.go:117-119`)
  с ошибкой `"%s: %w"` — рекомендация «проверка уникальности перед записью»
  выполнена.
- Chunking identity v2: `IndexIdentity.ChunkingVersion` +
  `ValidateIndexMeta` (`meta.go:85-86`) + `ReconcileIndex` + обязательный
  `force=true`.

**Что не доделано:**

1. **Несоответствие CHANGELOG.** `CHANGELOG.md:5` заявляет «DocIDs now include
   **byte offsets**», но `StartByte`/`EndByte` проставляются **только для prose**
   (`prose/markdown.go:473-474`). Для AST-чанков они остаются `0`:

   - `internal/indexer/chunk/ast/config.go:121-132` — чанк `code` (line_window)
     строится без `StartByte`/`EndByte`.
   - `internal/indexer/chunk/ast/engine.go:339-350` (а также `:406-412`,
     `:553`) — AST/partial/tail чанки тоже без byte offsets.

   Подтверждено grep: `StartByte`/`EndByte` в AST-движке используются только для
   чтения через `node.StartByte()`/`node.EndByte()` у tree-sitter, но в поля
   `zvec.Chunk` не копируются. Уникальность для Go/Python/JS держится на
   `ChunkIndex + fingerprint`, не на offsets.

   **Решение (одно из двух):**
   - либо проставить `StartByte`/`EndByte` в AST-чанках из tree-sitter
     `node.StartByte()`/`node.EndByte()` (привести код в соответствие с
     CHANGELOG);
   - либо убрать упоминание «byte offsets» из `CHANGELOG.md:5`.

2. **Отсутствуют два рекомендованных теста:**

   - «несколько функций/выражений на одной строке» через реальный AST-движок
     (есть только `docid_test.go::TestMakeDistinctForSameLineChunks` на уровне
     `docid.Make`, без прокрутки через `ast.engine`);
   - «длинный prose-текст, разбитый на несколько частей одной строки».
   - Проверка инварианта `chunkCount == len(unique DocIDs) == zvec DocCount` в
     явном виде отсутствует (ближайшее — round-trip в
     `TestCoordinatorPreservesIndexOnSecondBatchEmbedFailure`, не инвариант
     коллизий).

### [ ] 2. Copy-on-write обновление файла (доработать)

**Что сделано:**

- Safe rollback: `rollbackNewDocIDs`/`staleDocIDs` (`coordinator.go:541-545,
  569-573`) удаляет **только** новые ID, отсутствующие в закоммиченном
  `old.DocIDs` — старые векторы не затрагиваются.
- `deleteStaleVectorsFrom` идёт только **после** успешного `manStore.Upsert`.
- Durable `CleanupJournal` (`manifest/cleanup.go`, файл `INDEX_DIR/cleanup.jsonl`,
  `Append`/`Pending`/`Clear`), записывается до удаления stale
  (`coordinator.go:580-582`).
- Replay на старте: `reconcileCleanupJournal` (`coordinator_helpers.go:28-41`)
  дочитывает pending ID и удаляет их из zvec.
- Требуемый тест `TestCoordinatorPreservesIndexOnSecondBatchEmbedFailure`
  (`coordinator_test.go:1346-1434`) есть и проходит.

**Что не доделано:**

Рекомендованная архитектура «новая generation → атомарная публикация → удаление
старой generation» **не реализована**. Текущее решение — safe rollback + journal.
Окно для orphan-векторов: между записью новых ID в zvec и `Append` в journal /
`Upsert` манифеста при crash новые orphan-векторы остаются в zvec (не влияют на
поиск по содержимому, но полной reconciliation нет).

**Решение:** реализовать generation-based staging для обновления одного файла
(как уже сделано для force reindex в п.5) либо задокументировать принятый
компромисс и добавить тест на crash между zvec-write и journal-append.

### [ ] 3. Обрабатывать частичный успех native write/delete (доработать)

**Что сделано:**

- `internal/store/zvec/write_result.go`: типы `PartialWriteError`,
  `FlushWriteError`, функция `PartialWriteOutcome(err)` (распознаёт оба через
  `errors.As`).
- `UpsertChunks` (`collection.go:131-186`): при `wr.ErrorCount > 0` возвращает
  `PartialWriteError` с `Succeeded`; при ошибке `Flush` — `FlushWriteError`.
  `DeleteByIDs` (`collection.go:189-225`) — симметрично.
- Coordinator (`coordinator.go:529-534`) вызывает `PartialWriteOutcome` и
  добавляет в `newDocIDs` только `succeeded`, затем возвращает ошибку (не
  продолжает incremental flow).
- `deleteStaleVectorsFrom` (`coordinator_helpers.go:51-56`) при partial
  переmитает оставшиеся `ids = staleDocIDs(ids, succeeded)`.
- Fault-injection для partial upsert есть: `partialUpsertZvec`
  (`coordinator_test.go:1436-1453`) + тест
  `TestCoordinatorPartialUpsertDoesNotCommitManifest`.

**Что не доделано:**

Нет fault-injection теста для ошибки **`Flush`**. Тип `FlushWriteError` и путь
кода для него реализованы, но не покрыты тестом (требовалось в рекомендации
«добавить fault-injection adapter для partial success **и ошибки Flush**»).

**Решение:** добавить тест, инжектирующий `FlushWriteError` (ошибка после
успешных upsert-ов, на финальном Flush), и проверить, что coordinator не
коммитит манифест и корректно откатывает.

### [ ] 5. Не уничтожать рабочий индекс в начале force reindex (доработать)

**Что сделано (для обычного `force=true`):**

- `internal/store/zvec/staging.go`: `StagingCollectionSuffix = "_staging"`,
  `StagingManifestPath` (`manifest.staging.db`), `PromoteStagingCollection`
  (atomic rename: active→`.old`, staging→active, с rollback при ошибке),
  `PromoteStagingManifest`, `DiscardStaging*`.
- `coordinator.go:295-331`: при `force=true` открывается отдельный staging store,
  `WipeCollection` вызывается на **staging** store, рабочая коллекция и
  `manifest.db` не трогаются.
- `promoteStaging` (`coordinator.go:335-349`) вызывается **только в конце**
  `run()` после успешного завершения индексации.

**Что не доделано (критическое расхождение с п.1 рекомендация 4):**

Перед блоком staging в `run()` стоит `ReconcileIndex` (`coordinator.go:267-281`).
При **identity mismatch** (owner mismatch, move `workspace_root` или **смена
`chunking_version`** — а это именно сценарий обязательного `reindex(force=true)`
из п.1) `ReconcileIndex → ResetIndexForIdentityChange` (`reconcile.go:73-96`,
вайпает коллекцию + манифест **до** построения, **без staging**. Старый рабочий
индекс уничтожается в начале.

Подтверждено тестом `TestCoordinatorForceReindexOwnerMismatch`
(`coordinator_test.go:717-775`): он намеренно ожидает, что старая коллекция
удалена (`old collection still present` — fatal, если осталась). То есть п.1
рекомендация 4 и п.5 в одной точке противоречат друг другу: обязательный force
reindex при смене chunking version идёт **не** через staging.

Дополнительно: если staging store не открылся из-за `isZvecUnavailable`
(`coordinator.go:689-691`), срабатывает fallback на разрушительный путь
`c.Zvec.WipeCollection()` + `manStore.Clear()` (`coordinator.go:323-330`). В
production это неактуально, но путь существует.

**Решение:**

1. Пропустить identity-mismatch через тот же staging path, что и обычный
   `force=true`, чтобы `ResetIndexForIdentityChange` не вайпал активный индекс
   до успешной пересборки.
2. Перевести `TestCoordinatorForceReindexOwnerMismatch` на ожидание сохранения
   старого индекса (либо добавить параллельный тест на безопасное поведение).
3. Добавить unit-тесты на `PromoteStagingCollection`/`PromoteStagingManifest`/
   `DiscardStaging*` (сейчас grep по тестам пуст) и fault-тест «force reindex
   упал на embedding/чтении → старый индекс сохранён» (прямо требовался п.5).

---

## ⚠️ Частично реализованные (P1)

### [ ] 8. Исправить семантику `path_glob` (опциональная доработка)

**Что сделано:**

- `internal/store/zvec/collection.go:327-375` — `searchWithGlobExpansion`:
  итеративно расширяет `queryTopK` (`next := queryTopK * 2`, строка 368),
  повторно запрашивает и фильтрует, пока `len(hits) < topK` и
  `queryTopK <= docCount`. Закрывает основную проблему (раньше фильтрация шла по
  фиксированной выборке `topK*4`).
- Вызов делегируется из `collection.go:240-242` при непустом glob.
- Тест `TestIntegrationPathGlobExpansion` (`store_integration_test.go:244-290`):
  120 документов вне glob + один внутри, проверяет `hits[0].Path ==
  "target/match.go"`. Соответствует описанному в todo сценарию.

**Что не доделано:**

Первая рекомендация («фильтр на уровне zvec query» / native glob) **не
реализована**. В zvec-go есть нативный `SearchQuery.SetFilter`
(`.deps/zvec-go/query.go:275-281`, вызывает `zvec_vector_query_set_filter`), но
он не используется ни в `queryLocked`, ни в `searchWithGlobExpansion` — фильтрация
идёт в Go через `matchPathGlob`.

Если native filter не годится для glob-семантики (`**/*.go`), текущее решение
приемлемо, но в коде нет комментария или проверки доступности native glob.

**Решение:** либо задокументировать выбор Go-side фильтра комментарием
(почему не native `SetFilter`), либо при наличии подходящего native API
перенести фильтр в zvec для производительности на больших коллекциях.

### [ ] 9. Не терять ошибку native `Close` (добавить регр.-тест)

**Что сделано (реализация верная):**

- `Close` (`collection.go:62-75`): при ошибке `s.col.Close()` возвращает ошибку
  раньше (строки 69-71), **не обнуляя** `s.col` и `s.open` — handle сохранён для
  повторного close.
- `WipeCollection` (`collection.go:292-307`): при ошибке `s.col.Close()`
  возвращает `fmt.Errorf("close before wipe: %w", err)` **до** `os.RemoveAll`
  (строки 296-298), не удаляя каталог.

Обе рекомендации пункта выполнены на уровне реализации.

**Что не доделано:**

Нет прямого fault-injection unit-теста, который подаёт ошибку из native
`col.Close()` и проверяет `s.col != nil` / `s.open == true` после ошибки.
`TestResetIndexForZvecMigrationWipeError` (`migrate_test.go:266-276`) тестирует
только ошибку `Wipe` на уровне `ResetIndexForZvecMigration` через
`wipeTrackingStore`, а не поведение `CollectionStore.WipeCollection`/`Close` при
ошибке native close.

**Решение:** добавить regression-тест с mock-коллекцией, чей `Close` возвращает
ошибку, и проверить сохранение handle/state и отказ от `os.RemoveAll` в
`WipeCollection`.

---

## ⚠️ Частично реализованные (P2)

### [ ] 14. Согласовать transport и embedding timeouts (доработать)

**Что сделано:**

- HTTP `WriteTimeout` теперь вычисляется динамически:
  `internal/transport/http/server.go:108-119` —
  `writeTimeout = config.EmbedHTTPBudget(profile)` вместо жёстких 120 с.
  `ReadHeaderTimeout` 10 с, `ReadTimeout` 30 с.
- `EmbedHTTPBudget` (`internal/config/config.go:454-473`):
  `TimeoutSeconds * MaxRetries + retry_base_ms*(retries-1) + 30s` — учитывает
  retries/backoff, как рекомендовано.
- Для `lmstudio_qwen` (`config.yaml:64`, `timeout_seconds: 200`,
  `embed_budget_ratio: 0.50`) это даёт бюджет ~`200*3 + 500ms*2 + 30s ≈ 631 с`.

**Что не доделано:**

1. **Прокси stdio→HTTP (MCP к daemon) остался на жёстком таймауте.**
   `internal/service/proxy.go:30` и `:147` —
   `&http.Client{Timeout: 660 * time.Second}`. Вместо применения общего
   `EmbedHTTPBudget` выбрана константа «с запасом». Единого end-to-end budget
   нет: сервер считает бюджет из профиля, клиент зафиксирован на 660 с.
2. **Нет unit-теста на `EmbedHTTPBudget`** (grep по `EmbedHTTPBudget` в
   `internal/config/*_test.go` пуст). Проверяется только косвенно через поле
   `EmbedBudgetRatio` (`config_test.go:445,488`) — это другой параметр (расход
   токенов, не таймаут).
3. Не реализован «endpoint-level deadline» на уровне одного запроса к `svc`
   (context с deadline = budget), ограничение осталось на уровне серверного
   `WriteTimeout`.

**Решение:**

1. Применить `EmbedHTTPBudget(profile)` в `proxy.go` вместо жёстких 660 c (с
   fallback на 660 c, если профиль недоступен).
2. Добавить прямой unit-тест на `EmbedHTTPBudget` (проверка формулы для разных
   `timeout_seconds`/`max_retries`/`retry_base_ms`).

---

## Метод проверки

- Прочитан `todo.md` целиком; для каждого пункта проверены указанные файлы и
  строки через Read/grep (строки в большинстве пунктов сместились — код
  переписан).
- Лично подтверждены критические расхождения:
  - `golangci-lint run ./...` — exit 1, 2 новые ошибки (п.11);
  - `StartByte`/`EndByte` для AST-чанков = 0, для prose — заполнены (п.1);
  - `ReconcileIndex → ResetIndexForIdentityChange` вайпает индекс до построения
    при identity mismatch, в обход staging (п.5);
  - `proxy.go:30,147` — жёсткий `660 * time.Second` (п.14).
