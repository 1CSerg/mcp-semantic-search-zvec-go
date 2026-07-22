# Ревью ошибок проекта — остаточные замечания

Дата проверки: 2026-07-22.
Проверен коммит `5ef387a` «Исправления по ревью: целостность индекса и runtime
hardening».

**Статус close-out (этот проход):** все пункты ниже закрыты кодом/тестами/доками.

## Сводка

| # | Пункт | Статус |
|---|-------|--------|
| 1 | Коллизии `DocID` | ✅ Закрыто |
| 2 | Copy-on-write обновление файла | ✅ Закрыто (компромисс задокументирован) |
| 3 | Частичный успех native write/delete | ✅ Закрыто |
| 5 | Не уничтожать индекс в начале force reindex | ✅ Закрыто |
| 8 | Семантика `path_glob` | ✅ Закрыто (комментарий о Go-side filter) |
| 9 | Не терять ошибку native `Close` | ✅ Закрыто |
| 11 | Блокирующая ошибка `golangci-lint` | ✅ Закрыто |
| 14 | Согласовать transport и embedding timeouts | ✅ Закрыто |

---

### [x] 11. golangci-lint

Удалена `sanitizeDaemonStatusText`; `var zvecTarget = c.Zvec`. `golangci-lint run ./...` — exit 0.

### [x] 1. DocID / byte offsets

AST / line_window / partial окна заполняют `StartByte`/`EndByte`. Default `chunking.version` → **3**. Тесты: `TestASTSameLineMultiSymbolDocIDsUnique`, `TestProseSameLineMultiPartDocIDsUnique`.

### [x] 2. COW compromise

`docs/ARCHITECTURE.md` — секция indexer durability. Порядок: upsert → manifest → journal append → delete-stale. Тесты `TestCoordinatorManifestUpsertFailureDoesNotJournalDeleteLiveDocs`, `TestCoordinatorJournalAppendFailureStillDeletesStale`. Per-file generation не делался.

### [x] 3. FlushWriteError

`flushFailZvec` + `TestCoordinatorFlushWriteErrorDoesNotCommitManifest`; `TestPartialWriteOutcome` покрывает `FlushWriteError`.

### [x] 5. Force + identity staging

`ResetIndexForIdentityChange` не вайпит same-name active / manifest; orphan cleanup только при смене имени коллекции. Staging promote закрывает manifest перед rename (Windows). Тесты staging + `TestCoordinatorForceReindexPreservesActiveOnEmbedFailure`.

### [x] 8. path_glob

Комментарий над `searchWithGlobExpansion`: native `SetFilter` не используется.

### [x] 9. Close error regression

`TestCloseErrorPreservesHandle` (`collection_close_test.go`, `zvec` tag).

### [x] 14. Proxy EmbedHTTPBudget

`NewHTTPProxyForProfile` / `newStdioProxyService`; `TestEmbedHTTPBudget`. Per-request svc deadline не добавлялся.
