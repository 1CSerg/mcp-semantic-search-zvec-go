---
title: Changelog
version_schema: semver
---

# Changelog

Release history for the integration minirepo.

## Version table

| Version | Date | Notes |
|---------|------|-------|
| 1.2.0 | 2026-06-01 | Hybrid chunking E2E fixtures |
| 1.1.0 | 2026-05-15 | Added BSL embedded SDBL samples |
| 1.0.0 | 2026-05-01 | Initial minirepo |

## 1.2.0

- Added markdown front matter and version table boundaries for prose chunking tests.
- Ensures table rows are not split across prose chunks.

```yaml
feature: prose_overlap
enabled: true
```
