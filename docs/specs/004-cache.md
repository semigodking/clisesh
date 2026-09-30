# Spec 004 — Description Cache

Status: Implemented (Task 4)

## Purpose

Persist generated session descriptions so they are not regenerated on every
run. Caches are **per provider** and **versioned** so a format change cleanly
invalidates stale data.

## Location

`<cache_dir>/<provider>-descriptions.json`, where `cache_dir` comes from
configuration (default `$XDG_CACHE_HOME/clisesh` or `~/.cache/clisesh`).
Example: `~/.cache/clisesh/kiro-descriptions.json`.

## File format

```json
{
  "__version__": 1,
  "<session-id>": "description text",
  "<session-id>": "another description"
}
```

- `__version__` records the cache schema version. On load, a mismatch
  discards all entries (returns an empty cache).
- All other keys are session IDs mapped to their cached description.

## Semantics

- `Open(cacheDir, provider)` loads the cache (empty if missing or version
  mismatch); never errors on a missing file.
- `Get(id) (string, bool)` / `Set(id, desc)` / `Delete(id)` operate in memory.
- `Save()` writes atomically: write to a temp file in the same directory,
  then `rename` over the target so readers never see a partial file. Creates
  the cache directory if needed.
- `Len()` returns the number of cached descriptions (excluding metadata).
- Corrupt JSON is treated as an empty cache (self-healing), not a fatal error.

## Deletion cleanup

When sessions are deleted, the app calls `Delete(id)` for each removed id
(including cascaded children) then `Save()`, keeping the cache in sync.

## Tests

- Round-trip: Set → Save → Open → Get returns the value.
- Version invalidation: a file with a different `__version__` loads empty.
- Atomic save: no partial file; temp file removed after rename.
- Corrupt file: loads as empty cache without error.
- Delete removes an entry and Len decrements.

## Demo

`clisesh cache stats` prints, per provider file found in the cache dir, the
number of cached descriptions.
