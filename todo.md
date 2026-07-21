# TODO: Ревью кода mcp-semantic-search-zvec-go

Документ создан по итогам ревью Go-кода проекта (упор на ошибки в коде).
Дата ревью: 2026-07-21.

## Сводка проверок на момент ревью

| Проверка | Результат |
|----------|-----------|
| `go build ./...` | ✅ без ошибок |
| `go vet ./...` | ✅ без замечаний |
| `golangci-lint run` (errcheck, govet, ineffassign, staticcheck, unused) | ✅ 0 issues |
| `go test -race` на `service`, `daemon`, `indexer`, `watcher` и подпакетах | ✅ PASS |

Кодовая база (~129 Go-файлов) в целом очень качественная: аккуратная работа с
shutdown-последовательностями (WaitGroup contract, `searchMu` +
`searchesShuttingDown`), защитой от use-after-free на CGO-хэндлах (tree-sitter,
zvec, ONNX), rollback'ом при неудачном upsert, cross-process lock'ами с
проверкой живого PID по `StartTime`.

Легенда статусов:

- ✅ **Готово** — исправление уже применено в этом ревью (коммит не сделан).
- 🟡 **Запланировано** — дефект выявлен, требует действия.
- ⬜ **Проверено, не баг** — подозрение не подтвердилось (намеренная защита).

---

## ✅ 1. `internal/service/phase1.go:721-727` — недостижимая ветка в `Shutdown`

**Тип:** dead code / логический дефект.

**Описание.** В `Phase1.Shutdown` ветка `else if !closeZvec && searchesIdle &&
p.coordinator == nil && !p.isIndexingRunning()` **никогда не выполняется**.
Причина: переменная `indexIdle` инициализируется `true` (строка 685), а
мутируется в `false` **только** внутри блока `if p.coordinator != nil` (строки
686–712). Следовательно, при `coordinator == nil` всегда `indexIdle == true`, и
`closeZvec = indexIdle && searchesIdle = searchesIdle`. Условие ветки требует
одновременно `!closeZvec` (т.е. `!searchesIdle`) и `searchesIdle` — оно
противоречиво и недостижимо. Это не runtime-баг, но мёртвый код, вводящий в
заблуждение при чтении shutdown-логики.

**Действия по устранению (ВЫПОЛНЕНО):**

1. Открыть `internal/service/phase1.go`, метод `Shutdown`.
2. Удалить ветку `else if !closeZvec && searchesIdle && p.coordinator == nil && !p.isIndexingRunning()`.
3. Дополнить комментарий перед `closeZvec := ...` пояснением, почему при
   `coordinator == nil` отдельная ветка не нужна.
4. Прогнать `go test -race -count=1 ./internal/service/...` — должно быть PASS.

**Статус:** ✅ Правка применена. Тесты `internal/service` под race-детектором — PASS.

---

## ✅ 2. `internal/indexer/chunk/stream.go:33-82` — две недостижимые ветки в `streamChunkBatched`

**Тип:** dead code.

**Описание.** В цикле потокового чтения были две мёртвые ветки:

- В EOF-блоке при `len(buf) < window`: `else if end == lineCount { return nil }`
  стоял после `if ch != nil { ... }` (без `return`) и перед безусловным
  `return nil` — не давал никакого наблюдаемого эффекта.
- В ветке полного окна: `if err == io.EOF { if ch == nil && end == lineCount { return nil } return nil }`
  — обе внутренние ветки возвращали `nil`, условие было избыточным.

**Действия по устранению (ВЫПОЛНЕНО):**

1. В `streamChunkBatched` упростить EOF-блок `len(buf) < window`: вынести
   `return coll.add(ch)` для непустого `ch` и `return nil` иначе.
2. Удалить неиспользуемую переменную `end` в этой ветке.
3. Во второй EOF-ветке (после emit полного окна) оставить единый
   `return nil` с комментарием.
4. Прогнать тесты `TestStreamChunk*` — все кейсы (basic, BOM, CRLF,
   whitespace-only, exact-window, overlap-defaults, no-trailing-newline,
   classic-mac-line-ending, rejects-long-line, empty-file) должны быть PASS.

**Статус:** ✅ Правка применена. Streaming-тесты `internal/indexer/chunk` — PASS.

---

## ✅ 3. `internal/logging/rotate.go` — oversized-запись проходит лимит молча (и `slog.Warn` из `Write` запрещён)

**Тип:** наблюдаемость / потенциальная рекурсия.

**Описание.** В `rotatingWriter.Write` если одна запись больше `maxBytes`,
ротация бесполезна: новый файл пуст, а запись всё равно переполняет бюджет. Это
типично для огромного stacktrace в crash-логе. Старый код делал это молча.

**Важный подводный камень, обнаруженный при правке:** `rotatingWriter` является
sink'ом для `slog` (см. `logging.Setup` → `io.MultiWriter(os.Stderr, rw)` →
`slog.SetDefault`). Вызов `slog.Warn(...)` **изнутри** `Write` приводит к
рекурсии `slog.Warn → handler.Handle → rw.Write → slog.Warn → ...` и панике
`io.multi.go` (подтверждено регресс-тестом: `TestSetup` падал после первой
версии правки). Логировать из writer'а в его собственный sink **нельзя**.

**Действия по устранению (ВЫПОЛНЕНО):**

1. Не использовать `slog.*` внутри `rotatingWriter.Write`.
2. Добавить `var oversizedRecordOnce sync.Once` в пакет `logging`.
3. В `Write`, при `int64(len(p)) > int64(w.maxBytes)`, вызывать
   `oversizedRecordOnce.Do(func(){ fmt.Fprintf(os.Stderr, ...) })`. stderr
   безопасен (он второй sink в MultiWriter, и `fmt.Fprintf` не идёт через slog).
4. `sync.Once` гарантирует, что предупреждение сработает один раз за процесс,
   а не на каждой oversized-строке (которых может быть много).
5. Прогнать `go test ./internal/logging/...` — `TestSetup` и все
   `TestRotatingWriter*` должны быть PASS; в stderr должно появиться
   `log record larger than max_bytes (174 > 128); ...`.

**Статус:** ✅ Правка применена. Все тесты `internal/logging` — PASS, предупреждение видно.

---

## ✅ 4. `cmd/mcp-semantic-search-zvec-go/main.go:isLoopbackAddr` — ручной разбор `host:port` ломается на IPv6 и пустом host

**Тип:** корректность предупреждения о безопасности.

**Описание.** Старый код разбирал адрес через `strings.LastIndex(addr, ":")`.
Edge-кейсы:

- `"[::1]:8080"` — работало (после `Trim "[]"` → `"::1"`), но хрупко.
- `":8080"` (демон-дефолт `DefaultHTTPAddrDaemon`) → `LastIndex` = 0, `host = ""`
  → не loopback → срабатывает громкое предупреждение «binds to a non-loopback
  address; the API is open to the network». Это **технически верно** (`:8080`
  слушает все интерфейсы), но сообщение сбивает с толку, т.к. это
  задокументированный дефолт демона, а не ошибочная конфигурация.
- `"::"` (IPv6 wildcard) — не обрабатывалось явно.

**Действия по устранению (ВЫПОЛНЕНО):**

1. Заменить ручной `LastIndex` на стандартный `net.SplitHostPort(addr)`,
   который корректно разбирает и IPv4, и IPv6-литералы (`[::1]:8080`), и форму
   без host (`:8080`).
2. `case "127.0.0.1", "::1", "localhost"` → `true`; пустой host явно → `false`
   с комментарием, что `":8080"` слушает **все** интерфейсы, а не loopback.
3. Добавить импорт `"net"` в `main.go`.
4. Прогнать `TestIsLoopbackAddr` — кейсы `127.0.0.1:8080` (true),
   `[::1]:8080` (true), `localhost:8080` (true), `:8080` (false),
   `0.0.0.0:8080` (false) должны остаться зелёными.

**Статус:** ✅ Правка применена. `TestIsLoopbackAddr` — PASS.

---

## ⬜ 5. Проверено, НЕ является багом (намеренная защита)

Перечислено, чтобы избежать повторных «исправлений» при будущих ревью.

### 5.1 Двойной `searchWG.Add/Done` в `phase1.go`

`SemanticSearch` (140 Add / 143 Done) + `zvecSearchWithContext` (853 Add /
857 Done внутри горутины) дают **+2/−2 = 0** на каждый запрос. Две пары
отслеживают **разные lifetime'ы**: внешняя — весь запрос, внутренняя —
горутина `zvec.Search`, которая переживает ctx-cancellation вызывающего
(буферизованный канал на 852 предотвращает блокировку send). Ранние возвраты в
`SemanticSearch` (пустой запрос, ошибка embed) происходят **до** вызова
`zvecSearchWithContext`, поэтому внутренний Add не срабатывает — баланс
сохраняется. Если внутренний `beginSearch` вернёт `ErrShuttingDown`, Add не
происходит и горутина не стартует. **Не баг.**

### 5.2 Mutex-дисциплина в `daemon/registry.go:BorrowService`

Все ветки с `r.mu.Lock()` отпускают мьютекс перед `return`. Ветка `r.closing`
(167–185) либо отпускает мьютекс перед `beginDiscard` (178, чтобы избежать
self-deadlock на non-reentrant `sync.Mutex`), либо в `else` (182). При панике
`initWorkspace` recover устанавливает `err`, `h` остаётся `nil`, строка 187
`err == nil` ложна — nil не сохраняется, строка 192 возвращает ошибку.
`initWorkspace` не имеет пути `(nil, nil)`, поэтому строки 187/196 не могут
достичь nil-разыменования. **Не баг.**

### 5.3 Retry в `openai/client.go:embedBatch` при `statusCode == 0`

`statusCode == 0` бывает только для transport-ошибок до получения ответа
(ошибка `NewRequest`, сбой `httpClient.Do`) — они по природе транзиентны
(сеть/DNS/connection refused), ретрай валиден. `context.Canceled` ловится в
следующей итерации через `sleepContext` (которая немедленно возвращает
`ctx.Err()` через `<-ctx.Done()`). Ошибки парсинга JSON возвращают `statusCode`
2xx (т.к. проверка `>= 400` на 240 прошла) → попадают в
`!isRetryableHTTPStatus(200)` на 208 и **не** ретраятся. **Не баг.**

### 5.4 `recover.go:RecoverStalledProgress` — пометка `Running=true` как errored при отсутствии lock-файла

Это и есть **цель** функции: восстановление после краха предыдущего процесса,
который оставил `progress.json` с `Running=true`, но lock-файла нет,
`LiveHolder()` false, live-peer'а нет, `IsLocked()` false. Без этой пометки
состояние `Running=true` зависло бы навсегда и блокировало бы новые запуски.
**Не баг.**

### 5.5 `search_rerank.go` — сортировка по возрастанию `score` и boost как `Score - boost`

zvec с `MetricTypeCosine` (см. `schema.go:45`) возвращает distance (меньше =
ближе). `sort.SliceStable(... score[i] < score[j])` ставит лучшие (меньший
distance) первыми. `adj := h.Score - pathMatchBoost(...)` **уменьшает**
distance для path-matching hits, поднимая их в ранжировании — корректно.
Подтверждено тестом `TestRerankSearchHitsPrefersPathMatch`: middleware.go с
`Score=0.66` становится первым. **Не баг.**

### 5.6 `bearerAuthorized` (http/server.go:87–97) — edge-case'ы

`len(auth) < 7` отсекает `"Bearer"` (6 символов) и `"Bearer "` после
`TrimSpace`. `EqualFold(auth[:7], "Bearer ")` гарантирует, что первые 7
символов — ровно `Bearer ` с пробелом, поэтому `auth[7:]` безопасно и корректно
извлекает токен. Tab-разделитель (`Bearer\t…`) проваливает `EqualFold` → false.
**Не баг.**

### 5.7 `coordinator.go:Start` — stale `curProgress` после неудачного `Save`

При ошибке `c.progress.Save(p)` (180) `c.running` сбрасывается в false (182),
но `c.curProgress` остаётся `StartRunning(force)` (Running=true в памяти).
Это безопасно: `IsRunning()` читает `c.running` → false; `CurrentProgress()`
возвращает `c.curProgress` **только** при `c.running == true` (строка 129),
иначе читает с диска. Индексирующая горутина стартует только после успешного
`Save`, поэтому stale-значение никто не читает. Следующий успешный `Start`
перезаписывает его. **Не баг.**

### 5.8 `filepath.Base`/`filepath.Ext` в `search_rerank.go:70` для slash-путей

Пути в индексе хранятся slash-нормализованными (`chunk.go:167`:
`filepath.ToSlash(rel)`). На Windows `filepath.Base` stdlib корректно
обрабатывает **оба** разделителя (подтверждено тестом: для
`"internal/service/server.go"` возвращает `"server.go"`). **Не баг.**

---

## 🟡 6. Кандидаты на улучшение (не сделаны — обратитесь при желании)

Ниже — наблюдения, не влияющие на корректность. Включены для полноты; правок
по умолчанию не делают, чтобы не менять поведение без согласования.

### 6.1 `lock/process_unix.go:ProcessAlive` — зомби-процессы

`syscall.Kill(pid, 0)` возвращает success и для живых, и для зомби
(не-reaped детей). Теоретически lock может считаться «живым» для зомби, который
уже не работает. На практике MCP-процессы долгоживущие и не порождают зомби
для своего lock-holder'а. Возможное улучшение: дополнительно сравнивать
`StartTime` (как уже делается в `process_matches_lock.go`).

### 6.2 `update/github.go:Checker.Check` — нет singleflight

При конкурентных вызовах (HTTP-демон) каждый поток проверяет кэш под mutex, но
между проверкой и обновлением кэша окно есть — несколько горутин могут
параллельно дёрнуть GitHub API. Не критично (GitHub простит, TTL 1 ч), но можно
добавить `golang.org/x/sync/singleflight` для дедупликации.

### 6.3 `versionGreater`/`splitSemver` — edge-кейсы

Сравнение версий разной длины (`"1.2"` vs `"1.2.0"`) работает (добивается
нулём), но формально по semver `1.2 == 1.2.0`, а код даёт equal — корректно.
Нечисловые core-сегменты (`"1.2.x"`) добиваются нулём — приемлемо для release-тегов.

### 6.4 Дополнительные дед-коды (мелкие)

При беглом просмотре других пакетов могут встречаться аналогичные
`stream.go`/`phase1.go` избыточные `else`-ветки после `return`. Глобальной
проблемы нет (`staticcheck` чист), но при желании можно прогнать
`unused`/`deadcode`-анализаторы по индивидуальным пакетам.

---

## Финальная проверка после правок

| Проверка | Команда | Результат |
|----------|---------|-----------|
| Сборка | `go build ./...` | ✅ |
| Vet | `go vet ./...` | ✅ |
| Lint | `golangci-lint run --timeout 5m ./...` | ✅ 0 issues |
| Race, service | `go test -race ./internal/service/...` | ✅ PASS |
| Race, daemon | `go test -race ./internal/daemon/...` | ✅ PASS |
| Race, indexer | `go test -race ./internal/indexer/...` | ✅ PASS |
| Race, watcher | `go test -race ./internal/watcher/...` | ✅ PASS |
| Logging | `go test ./internal/logging/...` | ✅ PASS |
| isLoopbackAddr | `go test -run TestIsLoopbackAddr ./cmd/...` | ✅ PASS |
| Stream chunk | `go test -run Stream ./internal/indexer/chunk/...` | ✅ PASS |

> Примечание к окружению: на Windows `go test` напрямую падает с
> `fork/exec ...: Access is denied` — это блокировка Defender свежесобранных
> бинарей в `%TEMP%`, не проблема кода. Тесты прогонялись через
> `go test -c -o .testbin/<name>.exe` + копирование + ручной запуск.

## Изменённые файлы (4)

```
 cmd/mcp-semantic-search-zvec-go/main.go   | 16 +++++++++++++---
 internal/indexer/chunk/stream.go          | 15 +++++----------
 internal/logging/rotate.go                | 17 +++++++++++++++++
 internal/service/phase1.go                |  9 +++++++--
 4 files changed, 42 insertions(+), 15 deletions(-)
```

Изменения **не закоммичены** — пользователь контролирует коммит.
