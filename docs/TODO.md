# Оставшиеся задачи (tech debt)

Зафиксировано по итогам ревью всего проекта (v0.1.6). Часть находок ревью уже
исправлена (см. историю изменений); ниже — то, что **сознательно отложено** или
не закрыто, с обоснованием и планом реализации.

Severity: **High** / **Medium** / **Low**. Ссылки даны на файлы (номера строк могут
смещаться).

---

## Сознательно отложено в текущей итерации

### 1. Проброс `context.Context` через сервисный слой — **Medium**

- **Что:** MCP- и HTTP-хендлеры не передают `ctx` в долгие операции
  (`SemanticSearch`, `Reindex`, индексация). Отмена клиента/таймаут не
  останавливают работу. См. `_ = ctx` в [`internal/transport/mcp/server.go`](internal/transport/mcp/server.go),
  и `r.Context()` не используется в [`internal/transport/http/server.go`](internal/transport/http/server.go).
- **Зачем:** корректная отмена при дисконнекте Cursor / истечении HTTP-таймаута;
  освобождение ресурсов; предотвращение «зависших» фоновых задач.
- **Как:**
  1. Добавить `ctx context.Context` первым аргументом в методы интерфейса
     `service.Service` (`SemanticSearch/GetIndexStatus/Reindex/CheckUpdate`) и во
     все реализации (`Phase1`, `Stub`, `HTTPProxy`).
  2. В `Phase1` пробросить `ctx` в `Embedder.Embed` (уже принимает `ctx`) и в
     zvec-запрос; в `HTTPProxy` — в `http.NewRequestWithContext`.
  3. В хендлерах передавать `r.Context()` (HTTP) и `ctx` из `CallToolRequest` (MCP).
  4. Фоновая индексация: заменить `context.Background()` в
     [`internal/indexer/coordinator.go`](internal/indexer/coordinator.go) `Start` на
     дочерний от долгоживущего ctx сервиса, чтобы reindex отменялся при shutdown.
- **Риск:** изменение публичного интерфейса + правка всех вызовов и тестов.

### 2. Дефолтный bind `127.0.0.1` вместо `:8080` — **High (security)**

- **Что:** `DefaultHTTPAddr = ":8080"` слушает все интерфейсы
  ([`internal/config/config.go`](internal/config/config.go)). Сейчас при пустом
  `API_TOKEN` логируется громкое предупреждение, но дефолт не изменён.
- **Зачем:** безопасный дефолт (loopback) против случайного выставления API в LAN.
- **Как (не ломая Docker):**
  - Не менять глобальный дефолт (в контейнере нужен `0.0.0.0`), а сделать его
    зависимым от режима: per-project `--http` → дефолт `127.0.0.1:8080`;
    `--daemon`/Docker → `0.0.0.0:8080` (или явный `HTTP_ADDR`).
  - Альтернатива: оставить `:8080`, но **отказывать в старте** (fail-fast) при
    пустом `API_TOKEN` и не-loopback bind, с флагом `--allow-insecure-http` для
    осознанного обхода.
  - Обновить [`docs/CONFIG.md`](docs/CONFIG.md), [`INSTALL.md`](INSTALL.md),
    `docker-compose*.yml`.

### 3. OS-level блокировка файлов вместо `O_EXCL`+heartbeat — **Medium**

- **Что:** в [`internal/lock/lock.go`](internal/lock/lock.go) reclaim — классический
  TOCTOU (`isStale()` → `Remove` → `O_CREATE|O_EXCL`).
- **Зачем:** исключить гонку двух reclaim'ов и редкие окна двойного захвата при
  расхождении heartbeat/mtime.
- **Как:** держать открытый fd на весь жизненный цикл процесса и брать
  `flock`/`LockFileEx` (через build-tagged unix/windows реализацию); stale-логику
  свести к одному механизму. Сохранить совместимость формата файла (PID+ts) для
  диагностики. Покрыть тестами кросс-процессного захвата.

---

## Открытые находки ревью (не закрыты)

### 4. Legacy-индекс: пустые поля идентичности после миграции — **Medium**

- **Что:** при наличии `manifest.db` без/с битым `index_meta.json` миграция пишет
  meta лишь с `ZvecGoVersion`; затем `EnsureIndexMeta` — no-op, т.к. meta «есть»
  ([`internal/store/zvec/meta.go`](internal/store/zvec/meta.go) `EnsureIndexMeta`,
  [`internal/store/zvec/migrate.go`](internal/store/zvec/migrate.go)). Поля
  `WorkspaceID/Root/CollectionName` могут остаться пустыми навсегда.
- **Зачем:** без идентичности не сработает защита от смешения индексов и детект
  переноса (см. п.8 ниже уже частично закрыт в reconcile).
- **Как:** при миграции заполнять идентичность из текущих `settings` (или
  переписывать meta целиком логикой уровня `EnsureIndexMeta`, а не точечным
  reset'ом). Тест: legacy `manifest.db` без meta → после старта meta заполнена.

### 5. `GET /v1/workspaces` раскрывает структуру ФС — **Medium**

- **Что:** возвращает `root`/`index_dir`/`config_path` всех workspace
  ([`internal/daemon/registry.go`](internal/daemon/registry.go) `ListWorkspaces`,
  хендлер в [`internal/transport/http/server.go`](internal/transport/http/server.go)).
  При слабой/отсутствующей auth — полное раскрытие путей.
- **Зачем:** минимизировать утечку информации о хосте.
- **Как:** возвращать только `id` (и опц. статус) по умолчанию; полные пути — за
  отдельным флагом/ролью; либо требовать обязательный `API_TOKEN` для этого
  маршрута в daemon-режиме.

### 6. Нет проверки containment для `INDEX_DIR`/`CONFIG_PATH`/daemon-путей — **Medium**

- **Что:** пути резолвятся в абсолютные без проверки нахождения под
  `workspace_root` ([`internal/config/options.go`](internal/config/options.go),
  [`internal/daemon/config.go`](internal/daemon/config.go)). Кривой/подложенный
  `daemon.yaml` может направить индекс/логи в произвольную доступную на запись папку.
- **Зачем:** защита от записи за пределами ожидаемого install-дерева.
- **Как:** опциональная валидация «путь под workspace root или в явном allowlist»;
  предупреждать/отклонять иначе. Сделать строгость настраиваемой (per-project vs daemon).

### 7. `last_crash.json`: recovery есть не во всех режимах — **частично, Low**

- **Что:** паник-recovery добавлен в per-project, daemon, stdio-proxy. Остаётся:
  стек в `last_crash.json` пишется полностью (пути) — для multi-user хоста можно
  редактировать. Файл теперь `0600`.
- **Как (опц.):** опция редактирования абсолютных путей в стеке до записи.

### 8. PID-reuse в детекте stale-процессов/локов — **Low**

- **Что:** `processAlive(pid)` / `OpenProcess` ([`internal/lock/lock.go`](internal/lock/lock.go),
  `internal/lock/process_*.go`) — при переиспользовании PID мёртвый держатель
  может выглядеть живым (или наоборот) в коротком окне.
- **Как:** добавить в payload лока время старта процесса (или иной не-PID
  идентификатор) и сверять при reclaim; объединить с проверкой cmdline.

### 9. Хрупкий детект `index_owner_mismatch` по подстроке — **Low**

- **Что:** [`internal/indexer/errors.go`](internal/indexer/errors.go) и reconcile
  определяют фатальность через `strings.Contains(msg, "index_owner_mismatch")`.
- **Зачем:** устойчивость к изменению текста сообщений.
- **Как:** ввести типизированную/sentinel-ошибку `zvec.ErrOwnerMismatch` и
  использовать `errors.Is`. Обновить тесты, которые сейчас матчат текст.

### 10. `WalkDir` молча пропускает нечитаемые каталоги — **Low**

- **Что:** callback возвращает `nil` на ошибках обхода
  ([`internal/indexer/scan/scan.go`](internal/indexer/scan/scan.go)).
- **Как:** считать/логировать пропущенные пути; при желании — отражать в
  `indexing.message`/диагностике. Отличать «git недоступен» от «пустой репозиторий».

### 11. Большие файлы целиком в память + `strings.Split` — **Low**

- **Что:** [`internal/indexer/chunk/chunk.go`](internal/indexer/chunk/chunk.go)
  читает весь файл и режет по строкам — высокий расход памяти на крупных файлах.
- **Как:** ограничить размер индексируемого файла (конфиг) или стримить чанкинг.

### 12. `countStats`/`ChunksIndexed` глотают ошибки и неточны — **Low**

- **Что:** [`internal/indexer/coordinator.go`](internal/indexer/coordinator.go)
  `countStats` при ошибке manifest возвращает `0,0` → `FinishIdle` может показать
  `files=0, chunks=0`; счётчик `ChunksIndexed` обновляется неравномерно во время run.
- **Как:** пробрасывать ошибку в обработчик завершения или сохранять последние
  известные значения; периодически брать счётчики из `manifest.Stats()`.

### 13. Readiness-ошибка раскрывает детали эмбеддингов — **Low**

- **Что:** `handleReady` ([`internal/transport/http/server.go`](internal/transport/http/server.go))
  возвращает `err.Error()` (может содержать endpoint эмбеддингов).
- **Как:** обобщить текст в ответе `/ready`, детали — в server-side лог (как уже
  сделано для `writeError`).

### 14. Доверие `workspace-root.txt` рядом с бинарником — **Low**

- **Что:** маркер используется для резолва workspace и области stale-kill
  ([`internal/config/install_root.go`](internal/config/install_root.go)).
- **Как:** трактовать маркер как hint только когда env не задан; валидировать, что
  путь существует и соответствует ожидаемой install-структуре.

### 15. WAL для SQLite-manifest — осознанно НЕ включён — **Low**

- **Что:** добавлен `busy_timeout=5000` и одно соединение
  ([`internal/store/manifest/store.go`](internal/store/manifest/store.go)), но WAL
  не включён.
- **Зачем не включён:** WAL создаёт `-wal`/`-shm` файлы, что проблемно на
  cloud-synced каталогах (Yandex.Disk/GDrive) — целевой сценарий проекта.
- **Как (если потребуется):** включать WAL только когда индекс не в synced-папке
  (переиспользовать детект из `internal/service/status_paths.go`
  `pathIsSyncedCloudDrive`).

---

## Как проверять перед коммитом

См. [`.cursor/rules/development.mdc`](.cursor/rules/development.mdc) и
[`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md):

```bash
gofmt -w .
go vet ./...
go test ./...
make test-cover-check   # ≥88% по ./internal/..., ≥50% на пакет
```

Изменения в [`internal/store/zvec/collection.go`](internal/store/zvec/collection.go)
(build tag `zvec`) проверяются только сборкой с нативными зависимостями:

```bash
make fetch-zvec-libs
go build -tags zvec ./internal/store/zvec/
```
