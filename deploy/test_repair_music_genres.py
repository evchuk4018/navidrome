#!/usr/bin/env python3
"""Temporary-fixture tests for deploy/repair_music_genres.py.

These tests intentionally use a real silent MP3 when ffmpeg and Mutagen are
available. They are useful before an operator runs --apply against the live
library, but are not part of Navidrome's Go test suite.
"""

from __future__ import annotations

import importlib.util
import json
import shutil
import sqlite3
import subprocess
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("repair_music_genres.py")
SPEC = importlib.util.spec_from_file_location("repair_music_genres", SCRIPT)
assert SPEC and SPEC.loader
repair = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(repair)


@unittest.skipUnless(shutil.which("ffmpeg"), "ffmpeg is required for the audio fixture")
class GenreRepairFixtureTest(unittest.TestCase):
    def setUp(self) -> None:
        try:
            import mutagen  # noqa: F401
        except ImportError:
            self.skipTest("mutagen is required for the audio fixture")
        self.temp = tempfile.TemporaryDirectory(prefix="navidrome-genre-repair-test-")
        self.root = Path(self.temp.name) / "music"
        self.root.mkdir()
        self.path = self.root / "Probe Artist" / "probe.mp3"
        self.path.parent.mkdir()
        subprocess.run(
            [
                "ffmpeg",
                "-hide_banner",
                "-loglevel",
                "error",
                "-f",
                "lavfi",
                "-i",
                "anullsrc=r=8000:cl=mono",
                "-t",
                "0.1",
                "-q:a",
                "9",
                str(self.path),
            ],
            check=True,
        )
        from mutagen.id3 import COMM, ID3, TALB, TIT2, TPE1, TCON, TXXX

        tags = ID3(self.path)
        tags.add(TIT2(encoding=3, text="Probe Song"))
        tags.add(TPE1(encoding=3, text="Probe Artist"))
        tags.add(TALB(encoding=3, text="Probe Album"))
        tags.add(TCON(encoding=3, text="Music"))
        tags.add(TXXX(encoding=3, desc="custom", text="must-survive"))
        tags.add(COMM(encoding=3, lang="eng", desc="", text="source-comment"))
        tags.save(self.path)

        self.db = Path(self.temp.name) / "navidrome.db"
        with sqlite3.connect(self.db) as connection:
            connection.executescript(
                """
                CREATE TABLE library (id TEXT PRIMARY KEY, path TEXT NOT NULL);
                CREATE TABLE media_file (
                    id TEXT PRIMARY KEY,
                    library_id TEXT NOT NULL,
                    path TEXT NOT NULL,
                    title TEXT,
                    artist TEXT,
                    mbz_recording_id TEXT,
                    missing INTEGER NOT NULL DEFAULT 0,
                    tags TEXT
                );
                """
            )
            connection.execute("INSERT INTO library VALUES (?, ?)", ("library-1", str(self.root)))
            connection.execute(
                "INSERT INTO media_file VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
                (
                    "media-1",
                    "library-1",
                    "Probe Artist/probe.mp3",
                    "Probe Song",
                    "Probe Artist",
                    "recording-1",
                    0,
                    json.dumps({"genre": [{"id": "genre-music", "value": "Music"}]}),
                ),
            )
            connection.commit()

    def tearDown(self) -> None:
        self.temp.cleanup()

    def test_apply_and_rollback_preserve_non_genre_frames(self) -> None:
        before = repair.file_state(self.path)
        manifest_path = Path(self.temp.name) / "manifest.json"
        backup_dir = Path(self.temp.name) / "backup"
        manifest = {
            "version": repair.MANIFEST_VERSION,
            "mode": "dry-run",
            "db_path": str(self.db.resolve()),
            "music_root": "",
            "entries": [
                {
                    "media_file_id": "media-1",
                    "library_id": "library-1",
                    "library_path": str(self.root),
                    "relative_path": "Probe Artist/probe.mp3",
                    "path": str(self.path.resolve()),
                    "title": "Probe Song",
                    "artist": "Probe Artist",
                    "mbz_recording_id": "recording-1",
                    "db_genres": ["Music"],
                    "file_genres": ["Music"],
                    "genre_class": "video-category-only",
                    "status": "candidate",
                    "replacement_genres": ["pop"],
                    "pre_file": before,
                }
            ],
        }
        manifest_path.write_text(json.dumps(manifest), encoding="utf-8")

        repair.apply_manifest(manifest_path, self.db, backup_dir, True)
        applied = json.loads(manifest_path.read_text(encoding="utf-8"))
        entry = applied["entries"][0]
        self.assertEqual(applied["mode"], "applied")
        self.assertEqual(entry["pre_file"]["native_tags_sha256"], entry["post_file"]["native_tags_sha256"])
        self.assertTrue(Path(entry["backup_path"]).is_file())
        self.assertFalse(Path(entry["backup_path"]).resolve().is_relative_to(self.root.resolve()))
        self.assertNotEqual(before["mtime_ns"], entry["post_file"]["mtime_ns"])

        from mutagen.id3 import COMM, ID3

        tags = ID3(self.path)
        self.assertEqual(str(tags.get("TIT2")), "Probe Song")
        self.assertEqual(str(tags.get("TPE1")), "Probe Artist")
        self.assertEqual(str(tags.get("TXXX:custom")), "must-survive")
        self.assertIn("source-comment", " ".join(text for frame in tags.getall("COMM") for text in frame.text))
        self.assertEqual([str(frame) for frame in tags.getall("TCON")], ["pop"])

        repair.rollback_manifest(manifest_path, self.db, True, False)
        rolled_back = json.loads(manifest_path.read_text(encoding="utf-8"))
        self.assertEqual(rolled_back["mode"], "rolled-back")
        self.assertEqual(repair.file_state(self.path)["sha256"], before["sha256"])
        self.assertEqual(repair.file_state(self.path)["native_tags_sha256"], before["native_tags_sha256"])
        self.assertEqual(str(ID3(self.path).get("TCON")), "Music")

    def test_failed_apply_rollback_preserves_later_user_edit(self) -> None:
        from mutagen.id3 import ID3, TCON

        second = self.root / "Probe Artist" / "second.mp3"
        shutil.copy2(self.path, second)
        with sqlite3.connect(self.db) as connection:
            connection.execute(
                "INSERT INTO media_file VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
                (
                    "media-2",
                    "library-1",
                    "Probe Artist/second.mp3",
                    "Probe Song",
                    "Probe Artist",
                    "recording-2",
                    0,
                    json.dumps({"genre": [{"id": "genre-music", "value": "Music"}]}),
                ),
            )
            connection.commit()

        before_first = repair.file_state(self.path)
        before_second = repair.file_state(second)

        def candidate(media_id: str, path: Path, before: dict[str, object], recording_id: str) -> dict[str, object]:
            return {
                "media_file_id": media_id,
                "library_id": "library-1",
                "library_path": str(self.root),
                "relative_path": str(path.relative_to(self.root)),
                "path": str(path.resolve()),
                "title": "Probe Song",
                "artist": "Probe Artist",
                "mbz_recording_id": recording_id,
                "db_genres": ["Music"],
                "file_genres": ["Music"],
                "genre_class": "video-category-only",
                "status": "candidate",
                "replacement_genres": ["pop"],
                "pre_file": before,
            }

        manifest_path = Path(self.temp.name) / "partial-manifest.json"
        manifest_path.write_text(
            json.dumps(
                {
                    "version": repair.MANIFEST_VERSION,
                    "mode": "dry-run",
                    "db_path": str(self.db.resolve()),
                    "music_root": "",
                    "entries": [
                        candidate("media-1", self.path, before_first, "recording-1"),
                        candidate("media-2", second, before_second, "recording-2"),
                    ],
                }
            ),
            encoding="utf-8",
        )

        # Block the second entry after the manifest was created. The first
        # entry will be applied, leaving a durable partial-failure manifest.
        tags = ID3(second)
        tags.delall("TCON")
        tags.add(TCON(encoding=3, text=["changed-by-user"]))
        tags.save(second)
        backup_dir = Path(self.temp.name) / "partial-backup"
        with self.assertRaises(repair.RepairError):
            repair.apply_manifest(manifest_path, self.db, backup_dir, True)
        failed = json.loads(manifest_path.read_text(encoding="utf-8"))
        self.assertEqual(failed["mode"], "dry-run")
        self.assertEqual(failed["apply"]["status"], "failed")
        self.assertTrue(failed["entries"][0].get("post_file"))

        # A later user edit to the already-applied first file must block a
        # non-forced rollback instead of overwriting that edit.
        first_tags = ID3(self.path)
        first_tags.delall("TCON")
        first_tags.add(TCON(encoding=3, text=["later-user-edit"]))
        first_tags.save(self.path)
        edited_hash = repair.sha256_file(self.path)
        with self.assertRaises(repair.RepairError):
            repair.rollback_manifest(manifest_path, self.db, True, False)
        self.assertEqual(repair.sha256_file(self.path), edited_hash)


class GenreGroupingTest(unittest.TestCase):
    def test_blank_recording_id_joins_verified_artist_group(self) -> None:
        with tempfile.TemporaryDirectory(prefix="navidrome-genre-group-test-") as temporary:
            root = Path(temporary)
            first = root / "first.mp3"
            second = root / "second.mp3"
            first.write_bytes(b"first")
            second.write_bytes(b"second")
            original_file_state = repair.file_state
            repair.file_state = lambda path: {
                "sha256": path.name,
                "size": 1,
                "mtime_ns": 1,
                "mode": 0o644,
                "genres": ["Music"],
                "native_tags_sha256": "unchanged",
            }

            class FakeClient:
                base_url = "test"
                user_agent = "test"
                cache_dir = root
                request_count = 0

                def fetch(self, kind: str, identifier: str, params: dict[str, str]):
                    if kind == "recording":
                        return (
                            {
                                "artist-credit": [
                                    {"artist": {"id": "artist-1", "name": "Probe Artist"}, "name": "Probe Artist"}
                                ]
                            },
                            True,
                        )
                    return ({"genres": [{"name": "pop", "count": 5}]}, True)

            common = {
                "library_id": "library-1",
                "library_path": str(root),
                "title": "Probe",
                "artist": "Probe Artist",
                "missing": 0,
                "tags": json.dumps({"genre": [{"id": "genre-music", "value": "Music"}]}),
                "path_error": "",
                "db_genres": ["Music"],
            }
            tracks = [
                {**common, "id": "1", "path": first.name, "absolute_path": str(first), "mbz_recording_id": "rec-1"},
                {**common, "id": "2", "path": second.name, "absolute_path": str(second), "mbz_recording_id": ""},
            ]
            try:
                manifest = repair.make_manifest(
                    root / "navidrome.db",
                    tracks,
                    FakeClient(),
                    root / "manifest.json",
                    None,
                    1,
                    2,
                    False,
                )
            finally:
                repair.file_state = original_file_state
            self.assertEqual([entry["status"] for entry in manifest["entries"]], ["candidate", "candidate"])
            self.assertEqual(manifest["audit"]["missing_recording_id"], 1)


if __name__ == "__main__":
    unittest.main()
