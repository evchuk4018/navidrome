#!/usr/bin/env python3
"""Synchronize reviewed genre repairs into the Beets library database.

``repair_music_genres.py`` changes the audio file and keeps Navidrome's
database read-only. This companion step is deliberately separate because a
future ``beet write`` can otherwise restore the old YouTube category from its
own library row. It uses Beets' ``Library``/``Item`` model for both reading
and writing the multi-value ``genres`` field, so it never guesses a database
delimiter or touches any other Beets column.

The genre repair manifest records the exact Beets item path, ID, old list, and
new list before each row is changed. ``--rollback`` restores only those rows
after checking that their path and current genre list still match the applied
manifest.
"""

from __future__ import annotations

import argparse
import json
import os
import sqlite3
import sys
import tempfile
from collections import defaultdict
from pathlib import Path
from typing import Any

try:
    from repair_music_genres import (
        MANIFEST_VERSION,
        RepairError,
        atomic_write_json,
        classify_existing_genres,
        ensure_manifest,
        normalize_genre,
        utc_now,
    )
except ImportError:  # pragma: no cover - supports package-style test imports.
    from deploy.repair_music_genres import (
        MANIFEST_VERSION,
        RepairError,
        atomic_write_json,
        classify_existing_genres,
        ensure_manifest,
        normalize_genre,
        utc_now,
    )


def eprint(message: str) -> None:
    print(message, file=sys.stderr)


def open_beets_library(db_path: Path, config_path: Path | None, music_root: Path | None) -> Any:
    try:
        import beets
        from beets import library
    except ImportError as exc:
        raise RepairError("beets is required for Beets genre synchronization") from exc

    # Match the normal `beet -c <config>` bootstrap: load defaults first,
    # overlay the configured file, and let Library/Item own serialization.
    beets.config.read()
    if config_path is not None:
        beets.config.set_file(str(config_path))
    return library.Library(
        str(db_path),
        directory=str(music_root) if music_root is not None else None,
        set_music_dir=False,
    )


def item_path(library: Any, item: Any) -> Path:
    path = Path(item.filepath)
    if not path.is_absolute():
        path = Path(os.fsdecode(library.directory)) / path
    return path.resolve(strict=False)


def item_genres(item: Any) -> list[str]:
    raw = item.get("genres", [])
    if raw is None:
        return []
    if isinstance(raw, str):
        return [raw] if raw else []
    return [str(value).strip() for value in raw if str(value).strip()]


def same_genres(left: list[str], right: list[str]) -> bool:
    return [normalize_genre(value) for value in left] == [normalize_genre(value) for value in right]


def find_exact_item(library: Any, expected_path: Path) -> Any | None:
    matches = [item for item in library.items() if item_path(library, item) == expected_path.resolve(strict=False)]
    if len(matches) > 1:
        raise RepairError(f"multiple Beets items match exact path: {expected_path}")
    return matches[0] if matches else None


def build_sync_plan(manifest: dict[str, Any], library: Any, source_db: Path) -> dict[str, Any]:
    items_by_path: dict[Path, list[Any]] = defaultdict(list)
    for item in library.items():
        items_by_path[item_path(library, item)].append(item)
    rows: list[dict[str, Any]] = []
    counts: dict[str, int] = {}
    for entry in manifest["entries"]:
        if entry.get("status") != "candidate":
            continue
        path = Path(str(entry.get("path") or "")).resolve(strict=False)
        result: dict[str, Any] = {
            "media_file_id": entry.get("media_file_id"),
            "path": str(path),
            "replacement_genres": list(entry.get("replacement_genres") or []),
            "status": "pending",
            "reason": "",
        }
        matches = items_by_path.get(path, [])
        if len(matches) > 1:
            result.update(
                status="skip-duplicate-beets-path",
                reason="multiple Beets items have this exact path",
            )
        elif not matches:
            result.update(status="skip-no-exact-beets-item", reason="no Beets item has this exact path")
        else:
            item = matches[0]
            current = item_genres(item)
            result.update(
                beets_item_id=int(item.id),
                beets_genres=current,
                beets_path=str(item_path(library, item)),
            )
            if classify_existing_genres(current) == "musical-or-unknown":
                result.update(status="skip-existing-genre", reason="preserve Beets musical or mixed genres")
            else:
                result.update(status="candidate", reason="replace category-only/blank Beets genre row")
        rows.append(result)
        counts[result["status"]] = counts.get(result["status"], 0) + 1
    return {
        "status": "dry-run",
        "created_at": utc_now(),
        # Keep the live source path in the plan even though dry-run reads a
        # coherent temporary SQLite snapshot to avoid Beets migrations.
        "beets_db": str(source_db.resolve()),
        "summary": counts,
        "entries": rows,
    }


def snapshot_sqlite(source: Path, destination: Path) -> None:
    """Copy a coherent SQLite snapshot without mutating the source DB."""
    try:
        source_connection = sqlite3.connect(f"file:{source.resolve()}?mode=ro", uri=True)
        destination_connection = sqlite3.connect(destination)
        try:
            source_connection.backup(destination_connection)
        finally:
            destination_connection.close()
            source_connection.close()
    except sqlite3.Error as exc:
        raise RepairError(f"snapshot Beets database read-only {source}: {exc}") from exc


def close_library(library: Any) -> None:
    close = getattr(library, "_close", None)
    if callable(close):
        close()


def get_item_checked(library: Any, sync_entry: dict[str, Any]) -> Any:
    item_id = sync_entry.get("beets_item_id")
    if item_id is None:
        raise RepairError(f"sync entry has no Beets item ID: {sync_entry}")
    item = library.get_item(int(item_id))
    if item is None:
        raise RepairError(f"Beets item no longer exists: {item_id}")
    expected = Path(str(sync_entry["path"])).resolve(strict=False)
    actual = item_path(library, item)
    if actual != expected:
        raise RepairError(f"Beets item {item_id} moved: {actual} != {expected}")
    return item


def apply_sync(manifest_path: Path, beets_db: Path, config_path: Path | None, music_root: Path | None, yes: bool) -> None:
    if not yes:
        raise RepairError("Beets apply requires --yes after reviewing the sync plan")
    manifest = ensure_manifest(manifest_path)
    if manifest.get("mode") != "applied":
        raise RepairError("sync requires an applied audio-file genre manifest")
    sync = manifest.get("beets_sync")
    if not isinstance(sync, dict) or sync.get("status") not in ("dry-run", "in-progress", "failed"):
        raise RepairError("sync requires a Beets --dry-run plan in the manifest")
    if Path(str(sync.get("beets_db") or "")).resolve(strict=False) != beets_db.resolve():
        raise RepairError("the Beets database differs from the reviewed sync plan")
    library = open_beets_library(beets_db, config_path, music_root)
    sync["status"] = "in-progress"
    sync["started_at"] = sync.get("started_at") or utc_now()
    atomic_write_json(manifest_path, manifest)
    changed = 0
    try:
        for row in sync["entries"]:
            if row.get("status") != "candidate":
                continue
            item = get_item_checked(library, row)
            old = item_genres(item)
            expected_old = list(row.get("beets_genres") or [])
            new = list(row.get("replacement_genres") or [])
            # A process may have been interrupted after Beets committed the
            # row but before this manifest entry was marked applied. Treat an
            # already verified new value as completed; refuse every other
            # unexpected change.
            if same_genres(old, new) and row.get("old_genres") is not None:
                row["status"] = "applied"
                changed += 1
                sync["changed"] = changed
                atomic_write_json(manifest_path, manifest)
                continue
            if not same_genres(old, expected_old):
                raise RepairError(f"Beets genres changed before apply for item {item.id}")
            # The record is durable before the row is changed. A process
            # interruption leaves enough information for row-level rollback.
            row["old_genres"] = old
            row["new_genres"] = new
            row["changed_at"] = utc_now()
            atomic_write_json(manifest_path, manifest)
            item["genres"] = new
            item.store(fields=["genres"])
            fresh = library.get_item(int(item.id))
            if fresh is None or not same_genres(item_genres(fresh), new):
                raise RepairError(f"Beets genre write did not verify for item {item.id}")
            row["status"] = "applied"
            changed += 1
            sync["changed"] = changed
            atomic_write_json(manifest_path, manifest)
            eprint(f"updated Beets genre row {changed}: {row['path']}")
    except Exception as exc:
        sync["status"] = "failed"
        sync["error"] = str(exc)
        atomic_write_json(manifest_path, manifest)
        raise
    sync["status"] = "complete"
    sync["finished_at"] = utc_now()
    atomic_write_json(manifest_path, manifest)
    print(f"Updated {changed} Beets genre rows.")


def rollback_sync(manifest_path: Path, beets_db: Path, config_path: Path | None, music_root: Path | None, yes: bool, force: bool) -> None:
    if not yes:
        raise RepairError("Beets rollback requires --yes")
    manifest = ensure_manifest(manifest_path)
    sync = manifest.get("beets_sync")
    if manifest.get("mode") not in ("applied", "rolled-back") or not isinstance(sync, dict) or sync.get("status") != "complete":
        raise RepairError("rollback requires a completed Beets sync")
    if Path(str(sync.get("beets_db") or "")).resolve(strict=False) != beets_db.resolve():
        raise RepairError("the Beets database differs from the reviewed sync plan")
    library = open_beets_library(beets_db, config_path, music_root)
    restored = 0
    for row in sync["entries"]:
        if row.get("status") != "applied":
            continue
        item = get_item_checked(library, row)
        current = item_genres(item)
        new = list(row.get("new_genres") or row.get("replacement_genres") or [])
        old = list(row.get("old_genres") or row.get("beets_genres") or [])
        if not force and not same_genres(current, new):
            raise RepairError(f"refusing Beets rollback after row changed for item {item.id}")
        item["genres"] = old
        item.store(fields=["genres"])
        fresh = library.get_item(int(item.id))
        if fresh is None or not same_genres(item_genres(fresh), old):
            raise RepairError(f"Beets rollback did not verify for item {item.id}")
        row["status"] = "rolled-back"
        row["rolled_back_at"] = utc_now()
        restored += 1
        atomic_write_json(manifest_path, manifest)
    sync["status"] = "rolled-back"
    sync["rolled_back"] = restored
    atomic_write_json(manifest_path, manifest)
    print(f"Restored {restored} Beets genre rows.")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--dry-run", action="store_true")
    mode.add_argument("--apply", action="store_true")
    mode.add_argument("--rollback", action="store_true")
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--beets-db", type=Path, required=True)
    parser.add_argument("--beets-config", type=Path, default=None)
    parser.add_argument("--music-root", type=Path, default=None)
    parser.add_argument("--yes", action="store_true")
    parser.add_argument("--force", action="store_true")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        if args.dry_run:
            manifest = ensure_manifest(args.manifest)
            if manifest.get("mode") not in ("dry-run", "applied"):
                raise RepairError("genre manifest must be dry-run or applied")
            with tempfile.TemporaryDirectory(prefix="navidrome-beets-genre-plan-") as temporary:
                snapshot = Path(temporary) / "beets.db"
                snapshot_sqlite(args.beets_db, snapshot)
                library = open_beets_library(snapshot, args.beets_config, args.music_root)
                try:
                    manifest["beets_sync"] = build_sync_plan(manifest, library, args.beets_db)
                finally:
                    close_library(library)
            atomic_write_json(args.manifest, manifest)
            print(json.dumps(manifest["beets_sync"]["summary"], sort_keys=True))
            print(f"Beets sync plan: {args.manifest.resolve()}")
            return 0
        if args.apply:
            apply_sync(args.manifest, args.beets_db, args.beets_config, args.music_root, args.yes)
            return 0
        rollback_sync(args.manifest, args.beets_db, args.beets_config, args.music_root, args.yes, args.force)
        return 0
    except (RepairError, OSError, ValueError) as exc:
        eprint(f"error: {exc}")
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
