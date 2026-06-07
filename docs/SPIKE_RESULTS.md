# ZVEC spike — результаты прогона

## Сводка (2026-06-08 — миграция на zvec-ai/zvec-go v0.3.1)

| Способ | Результат | Примечание |
|--------|-----------|------------|
| Docker (`run-spike-docker-inner.sh`) | **PASS** | `fetch-zvec-libs` + integration tests ~16 с |
| CI job `zvec-integration` | **PASS** (локально) | Эквивалент Docker spike; push на GitHub — после merge |
| Windows native (MinGW CGO) | **PASS** | `scripts/build-zvec-windows.ps1`; WinLibs gcc + vendor DLL |
| Legacy `make deps` (danieleugenewilliams) | Deprecated | Заменён vendor mode |

**Вывод:** spike checklist (integration `-tags integration,zvec`) **пройден** в Docker/Linux и на Windows native.

---

## Windows native (2026-06-08)

### Команда

```powershell
.\scripts\build-zvec-windows.ps1
$env:Path = "$env:ZVEC_LIB_DIR;D:\tools\winlibs\mingw64\bin;" + $env:Path
$env:CGO_ENABLED = "1"; $env:CC = "gcc"
go test -tags "integration,zvec" -count=1 ./internal/store/zvec/...
```

### Результат

- `go build -tags zvec` → `bin/mcp-semantic-search-zvec-go.exe` + `zvec_c_api.dll`
- `TestIntegrationSpikeChecklist` — **PASS** (после `Close()` коллекции перед cleanup TempDir)
- CGO: **WinLibs MinGW gcc** (VS Build Tools `cl` без `clang-cl` не подходит для cgo `-Werror`)

---

## Docker spike (2026-06-08)

### Команда

```powershell
docker run --rm -v "${PWD}:/src" -w /src golang:1.26.3-bookworm bash /src/scripts/run-spike-docker-inner.sh
```

### Результат

```
=== RUN   TestIntegrationSpikeChecklist
--- PASS: TestIntegrationSpikeChecklist (0.13s)
PASS
ok  	github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec	0.251s
```

Checklist [ZVEC_SPIKE.md](ZVEC_SPIKE.md): пункты 1–7 покрыты `TestIntegrationSpikeChecklist`.

---

## SDK

| | |
|---|---|
| Модуль | `github.com/zvec-ai/zvec-go v0.3.1` |
| Replace | `./.deps/zvec-go` |
| Native libs | `scripts/fetch-zvec-libs.sh` → GitHub Release artifacts |
| API wrapper | `internal/store/zvec/` |

---

## Legacy (до 2026-06-08)

Предыдущие прогоны с `danieleugenewilliams/zvec-go` + `make deps` — см. git history этого файла. Блокеры Arrow/CMake задокументированы в [instr/ZVEC_BUILD.md](instr/ZVEC_BUILD.md) appendix и upstream issues [#1](https://github.com/danieleugenewilliams/zvec-go/issues/1), [alibaba/zvec#468](https://github.com/alibaba/zvec/issues/468).
