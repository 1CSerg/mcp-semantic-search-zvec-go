# Проверка реализации замечаний ревью (todo.md)

Дата проверки: 2026-07-22.
Проверены коммиты `eba97bf`, `5ef387a`, `fa00a46` («Исправления по ревью»).
Рабочий каталог — `HEAD` (main, после `fa00a46`).

## Сводка

Из 19 замечаний ревью **18 реализованы полностью**, **1 реализовано частично**
(№7 — глобальный runtime shutdown daemon при позднем `release`). Ниже — детали по
частично реализованным пунктам и косметическим нюансам. Полностью закрытые
пункты перечислены в конце.

Дополнительно подтверждено локально: `golangci-lint run ./...` → **0 issues**
(замечание 11); `go vet` по пакетам без CGO → exit 0.

---

## Частично реализовано

### [~] 7. Закрывать daemon workspace после позднего `release` — частично

Файлы: `internal/daemon/registry.go:293-316,347-374,377-453`;
`internal/daemon/registry_test.go:534-597`.

**Что сделано:**
- При последнем `release()` в состоянии `closing` handle отсоединяется от
  `r.open` (`delete(r.open, workspaceID)`) и per-workspace ресурсы закрываются
  асинхронно через `go r.discardHandle(h)` (`registry.go:308-313`).
- `discardHandle` вызывает `h.cancel()`, `h.phase1.Close()` и
  `zvec.ReclaimCollectionLock(...)` (`registry.go:347-374`).
- Тест `TestRegistryCloseSkipsShutdownWithHeldBorrow` расширен: после позднего
  `release()` проверяется, что запись исчезает из `open` и `discards == 0`
  (`registry_test.go:587-596`).

**Что НЕ сделано (пробел):**
- Глобальный shutdown runtime — `chunk.CloseResources()` и
  `zvec.ShutdownRuntime()` — вызывается **только** в `Close` при
  `skipRuntime == false` (`registry.go:447-452`). Когда `Close` завершился с
  пропуском (держали borrow), а все поздние `release()` уходят через
  `discardHandle`, **эти два вызова никогда не выполняются**. Глобальный
  zvec-runtime и переиспользуемые ресурсы чанкера (tokenizer и т.п.) остаются
  жить до конца процесса.
- `discardHandle` (`registry.go:347-374`) не вызывает `chunk.CloseResources()` /
  `zvec.ShutdownRuntime()` — grep подтверждает, что эти символы встречаются
  только в `Close`.
- Повторный `Close` — no-op из-за `closing == true` (`registry.go:379-382`), так
  что отложенный shutdown не запустится и позже.
- Тест (`registry_test.go:534-597`) не проверяет факт закрытия глобального
  runtime — в нём нет ссылок на `ShutdownRuntime`/`CloseResources`.

**Рекомендация:** на последнем `release()` в состоянии `closing`, после
завершения всех `discardHandle` и опустошения `r.open`, запускать отложенный
глобальный shutdown (`chunk.CloseResources()` + `zvec.ShutdownRuntime()`) —
например, отдельной goroutine-drein, продолжающей cleanup после возврата
`Close`. Покрыть тестом, что runtime действительно закрывается после поздних
релизов.

---

## Косметические нюансы (не блокируют, стоит подчистить)

### 6. Повторяемый `Phase1.Shutdown` после timeout — реализовано, устаревший комментарий

Файл: `internal/service/phase1.go`.

Реализация корректна: вместо `sync.Once` используется state machine
`running(0) → closing(1) → closed(2)` (`phase1.go:48-49,681-747`); переход в
`closed` gated на `indexIdle && searchesIdle`, поэтому deadline не помечает
shutdown завершённым; embedder закрывается только при `indexIdle && searchesIdle`
(`phase1.go:735-739`). Тест `TestPhase1ShutdownRetryAfterIndexTimeout`
(`phase1_test.go:2141-2241`) покрывает retry-сценарий и однократное закрытие zvec.

**Нюанс:** устаревший комментарий в `waitSearches` (`phase1.go:788-790`) всё ещё
ссылается на несуществующий `shutdownOnce` («Shutdown runs under shutdownOnce…»).
Стоит обновить. Мелкий пробел в тестах: проверяется однократное закрытие zvec, но
не embedder (в тестах используется реальный HTTP-клиент embedder без счётчика
close).

### 8. Семантика `path_glob` — реализовано, мёртвый код

Файл: `internal/store/zvec/collection.go:351-403`.

Итеративное расширение topK до заполнения `limit` или исчерпания коллекции
реализовано (`searchWithGlobExpansion`); тест
`TestIntegrationPathGlobExpansion` (`store_integration_test.go:244-290`) — 120
близких документов вне glob + 1 дальний внутри, Assert ровно 1 hit.

**Нюанс:** мёртвый/no-op код в `collection.go:368-371` —
`if queryTopK < topK { queryTopK = topK }` недостижим (сразу выше
`queryTopK := topK`). Безвредно, но избыточно.

### 13. Строгое JSON-body и размер — реализовано, мелкие пробелы в тестах

Файл: `internal/transport/http/server.go:408-424`.

`http.MaxBytesReader` + второй `Decode` с обязательным `io.EOF` + `413` для
`*http.MaxBytesError` (`writeDecodeError`, `server.go:399-406`) — всё на месте.
Тест `TestHandlerJSONBodyStrict` (`server_test.go:766-804`) покрывает trailing
JSON, trailing garbage, body-too-large.

**Нюанс:** нет явного теста на trailing whitespace (по дизайну принимается, но не
ассертится); тест «body too large» шлёт `1<<20` байт, а не точный `limit+1`.

### 14. Согласование transport/embedding timeouts — реализовано одной из веток

Файлы: `internal/transport/http/server.go:108-121`; `internal/service/proxy.go`;
`internal/config/config.go:454-473`.

Выбрана ветка «единый end-to-end budget»: `config.EmbedHTTPBudget(profile)`
(retries + backoff + 30s tail) задаёт и `http.Server.WriteTimeout`, и таймаут
stdio-proxy клиента. Тест `TestEmbedHTTPBudget` (`config_test.go:554-577`).

**Нюанс:** альтернативная ветка («endpoint-level deadline») не реализована — нет
per-request `context.WithTimeout` для обработчика поиска. Бюджет корректно
размерен, но дедлайн всё ещё enforced через `http.Server.WriteTimeout`, поэтому
при disconnect клиента работа горутины может продолжаться до возврата сервиса.
Ревью допускало выбор одной из веток («либо … либо»), так что замечание
считается закрытым, но оговорку стоит держать в уме.

---

## Полностью реализовано (справочно)

| # | Пункт | Где |
|---|-------|-----|
| 1 | Коллизии `DocID` — единый генератор `docid.Make`, byte offsets в AST/line_window/prose/SDBL, `AssertUnique`, chunking `version: 3`, тесты same-line | `internal/indexer/chunk/docid/`; `batches.go:117`; `config.yaml` |
| 2 | COW-обновление файла — cleanup journal, commit manifest → journal → delete-stale; порядок задокументирован (per-file generation не делался) | `internal/store/manifest/cleanup.go`; `coordinator.go:594-628`; `docs/ARCHITECTURE.md` |
| 3 | Частичный успех native write — `PartialWriteOutcome`, `FlushWriteError`, `flushFailZvec`, тесты | `internal/store/zvec/write_result.go`; `coordinator.go:557-562` |
| 4 | Усиление desync — сравнение `uniqueIDs != chunks`, `!= docCount`, точное множество через `DocIDsPresent` | `coordinator.go:734-795` |
| 5 | Не уничтожать индекс в начале force reindex — staging collection/manifest, promote после успеха; `ResetIndexForIdentityChange` не вайпит active | `coordinator.go:283-374`; `internal/store/zvec/staging.go`, `reconcile.go:51-67` |
| 9 | Сохранение handle/ошибки native `Close`; `WipeCollection` не удаляет каталог до успеха close; тест `TestCloseErrorPreservesHandle` | `collection.go:64-77,308-323`; `collection_close_test.go:13-48` |
| 10 | Wrapper/DLL versioning — marker `.zvec-lib-version`, очистка lib-каталога при смене тега, проверка SHA256, versioned unit в install | `scripts/fetch/fetch-zvec-libs.ps1/.sh`; `scripts/install/install.ps1/.sh` |
| 11 | `golangci-lint` — helper переименован в `*_test.go`, списки копирования обновлены; `golangci-lint run ./...` → **0 issues** (проверено локально) | `scripts/fetch/patches/zvec-go-acp/path_windows_test_helper_test.go` |
| 12 | Запрет lossy ACP — `WC_NO_BEST_FIT_CHARS`, `lpUsedDefaultChar`, ошибка при замене; тест проверяет ошибку, а не Skip | `scripts/fetch/patches/zvec-go-acp/path_windows.go:57-91`, `path_windows_test.go:29-48` |
| 15 | Panic поиска → typed `ErrInternalSearch`, логирование panic, generic 500 без утечки текста; тесты | `phase1.go:151-157,860-871`; `server.go:448-472` |
| 16 | Утечки путей — структурный redactor (без regex), `file_watcher.last_error` и все публичные поля, тесты Windows/UNC/Unix/spaces/Cyrillic | `internal/redact/paths.go`; `server.go:290-348`; `watcher.go:337` |
| 17 | Bootstrap/`.env` — повторное разрешение `WORKSPACE_ROOT`/`INDEX_DIR`/`CONFIG_PATH` после загрузки `.env`; `ENV_PATH` читается последним | `internal/config/options.go:32-49,173-183`; `dotenv.go:56-97` |
| 18 | `path_allowlist` для `config_path` — общий allowlist для `index_dir` и `config_path`, daemon/per-project тесты | `internal/config/paths.go:121-139`; `internal/daemon/config.go:105-131` |
| 19 | Content hash — SHA-256 файла; `mtime + size` как быстрый фильтр перед hash; `FileEntry.ContentHash`; тест same-mtime/same-size | `coordinator.go:519-534`; `coordinator_helpers.go:15-26`; `manifest/store.go:137-145` |

---

## Итог

К ревью осталось одно содержательное замечание — **№7** (глобальный runtime
shutdown daemon при позднем `release`); остальное реализовано. Пункты №6, №8,
№13, №14 закрыты, но содержат мелкие косметические нюансы/пробелы в тестах —
перечислены выше для подчистки.
