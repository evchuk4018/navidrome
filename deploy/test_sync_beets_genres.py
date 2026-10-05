#!/usr/bin/env python3
"""Isolated Beets API tests for ``sync_beets_genres.py``.

These tests use a temporary Beets database and a temporary music directory.
They exercise the path conversion used by Beets 2.x, the multi-value genre
field, and the manifest guards around apply and rollback without opening the
homelab database or touching a real music file.
"""

from __future__ import annotations

import hashlib
import importlib.util
import tempfile
import unittest
from pathlib import Path
from typing import Any


SCRIPT = Path(__file__).with_name("sync_beets_genres.py")
SPEC = importlib.util.spec_from_file_location("sync_beets_genres", SCRIPT)
assert SPEC and SPEC.loader
sync = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(sync)

try:
    import beets  # noqa: F401
except ImportError:  # pragma: no cover - exercised only in minimal environments.
    BEETS_AVAILABLE = False
else:
    BEETS_AVAILABLE = True


def close_library(library: Any) -> None:
    """Close a Beets library across Beets versions used by the repair host."""

    close = getattr(library, "_close", None)
    if close is not None:
        close()


def database_hash(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


@unittest.skipUnless(BEETS_AVAILABLE, "beets is required for the isolated sync fixture")
class BeetsSyncFixtureTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory(prefix="navidrome-beets-sync-test-")
        self.root = Path(self.temp.name) / "music"
        self.root.mkdir()
        self.db = Path(self.temp.name) / "beets.db"

        self.paths = {
            "one": self.root / "Probe Artist" / "One.mp3",
            "two": self.root / "Probe Artist" / "Two.mp3",
            "three": self.root / "Probe Artist" / "Three.mp3",
        }
        for path in self.paths.values():
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(path.name.encode("ascii"))

        from beets import library

        lib = library.Library(
            str(self.db), directory=str(self.root), set_music_dir=False
        )
        try:
            self.items: dict[str, int] = {}
            values = {
                "one": (["Music", "People & Blogs"], 2020),
                "two": (["Music", "jazz"], 2021),
                "three": ([], 2022),
            }
            for key, (genres, year) in values.items():
                item = library.Item(
                    title=f"Track {key.title()}",
                    artist="Probe Artist",
                    album="Probe Album",
                    path=str(self.paths[key].relative_to(self.root)),
                    genres=list(genres),
                    year=year,
                )
                lib.add(item)
                item.store()
                self.items[key] = int(item.id)
        finally:
            close_library(lib)

    def tearDown(self) -> None:
        self.temp.cleanup()

    def open_library(self) -> Any:
        return sync.open_beets_library(self.db, None, self.root)

    def manifest(self) -> dict[str, Any]:
        return {
            "version": sync.MANIFEST_VERSION,
            "mode": "applied",
            "entries": [
                {
                    "status": "candidate",
                    "media_file_id": "media-one",
                    "path": str(self.paths["one"].resolve()),
                    "replacement_genres": ["pop", "indie"],
                },
                {
                    "status": "candidate",
                    "media_file_id": "media-two",
                    "path": str(self.paths["two"].resolve()),
                    "replacement_genres": ["electronic"],
                },
                {
                    "status": "candidate",
                    "media_file_id": "media-three",
                    "path": str(self.paths["three"].resolve()),
                    "replacement_genres": ["ambient"],
                },
            ],
        }

    def test_dry_run_matches_exact_paths_without_mutating_database(self) -> None:
        before = database_hash(self.db)
        snapshot = Path(self.temp.name) / "dry-run-snapshot.db"
        sync.snapshot_sqlite(self.db, snapshot)
        lib = sync.open_beets_library(snapshot, None, self.root)
        try:
            one = lib.get_item(self.items["one"])
            self.assertIsNotNone(one)
            assert one is not None
            # Beets 2.x stores this as bytes; item_path must still produce the
            # same absolute path as the reviewed audio-file manifest.
            self.assertEqual(sync.item_path(lib, one), self.paths["one"].resolve())
            plan = sync.build_sync_plan(self.manifest(), lib, self.db)
        finally:
            close_library(lib)

        self.assertEqual(database_hash(self.db), before)
        self.assertEqual(plan["beets_db"], str(self.db.resolve()))
        self.assertEqual(plan["summary"]["candidate"], 2)
        self.assertEqual(plan["summary"]["skip-existing-genre"], 1)
        candidate = {
            row["media_file_id"]: row
            for row in plan["entries"]
            if row["status"] == "candidate"
        }
        self.assertEqual(candidate["media-one"]["beets_genres"], ["Music", "People & Blogs"])
        self.assertEqual(candidate["media-three"]["beets_genres"], [])

    def test_apply_and_rollback_preserve_multi_values_and_unrelated_fields(self) -> None:
        import json

        manifest_path = Path(self.temp.name) / "genre-manifest.json"
        manifest = self.manifest()
        lib = self.open_library()
        try:
            manifest["beets_sync"] = sync.build_sync_plan(manifest, lib, self.db)
        finally:
            close_library(lib)
        manifest_path.write_text(json.dumps(manifest), encoding="utf-8")

        # The fixture represents an already-applied audio-file manifest; this
        # operation only changes the Beets rows.
        sync.apply_sync(manifest_path, self.db, None, self.root, True)

        lib = self.open_library()
        try:
            one = lib.get_item(self.items["one"])
            two = lib.get_item(self.items["two"])
            three = lib.get_item(self.items["three"])
            assert one is not None and two is not None and three is not None
            self.assertEqual(one.get("genres"), ["pop", "indie"])
            self.assertEqual(three.get("genres"), ["ambient"])
            self.assertEqual(two.get("genres"), ["Music", "jazz"])
            for item, title, year in (
                (one, "Track One", 2020),
                (two, "Track Two", 2021),
                (three, "Track Three", 2022),
            ):
                self.assertEqual(item.get("title"), title)
                self.assertEqual(item.get("artist"), "Probe Artist")
                self.assertEqual(item.get("album"), "Probe Album")
                self.assertEqual(item.get("year"), year)
        finally:
            close_library(lib)

        applied = json.loads(manifest_path.read_text(encoding="utf-8"))
        self.assertEqual(applied["beets_sync"]["status"], "complete")
        self.assertEqual(
            {row["status"] for row in applied["beets_sync"]["entries"]},
            {"applied", "skip-existing-genre"},
        )

        sync.rollback_sync(manifest_path, self.db, None, self.root, True, False)

        lib = self.open_library()
        try:
            one = lib.get_item(self.items["one"])
            two = lib.get_item(self.items["two"])
            three = lib.get_item(self.items["three"])
            assert one is not None and two is not None and three is not None
            self.assertEqual(one.get("genres"), ["Music", "People & Blogs"])
            self.assertEqual(three.get("genres"), [])
            self.assertEqual(two.get("genres"), ["Music", "jazz"])
            for item, year in ((one, 2020), (two, 2021), (three, 2022)):
                self.assertEqual(item.get("artist"), "Probe Artist")
                self.assertEqual(item.get("album"), "Probe Album")
                self.assertEqual(item.get("year"), year)
        finally:
            close_library(lib)

        rolled_back = json.loads(manifest_path.read_text(encoding="utf-8"))
        self.assertEqual(rolled_back["beets_sync"]["status"], "rolled-back")
        self.assertEqual(
            {row["status"] for row in rolled_back["beets_sync"]["entries"]},
            {"rolled-back", "skip-existing-genre"},
        )


if __name__ == "__main__":
    unittest.main()
