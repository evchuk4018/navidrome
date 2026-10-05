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
from unittest import mock


SCRIPT = Path(__file__).with_name("repair_music_genres.py")
SPEC = importlib.util.spec_from_file_location("repair_music_genres", SCRIPT)
assert SPEC and SPEC.loader
repair = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(repair)


def _id3v2_end(data: bytes) -> int:
    if len(data) < 10 or data[:3] != b"ID3":
        return 0
    size = 0
    for byte in data[6:10]:
        size = (size << 7) | (byte & 0x7F)
    return 10 + size + (10 if data[5] & 0x10 else 0)


def _id3v1_footer(data: bytes) -> bytes:
    if len(data) >= 128 and data[-128:-125] == b"TAG":
        return data[-128:]
    return b""


def _mpeg_payload(data: bytes) -> bytes:
    footer = _id3v1_footer(data)
    end = _id3v2_end(data)
    return data[end:-128] if footer else data[end:]


def _id3v1_field(value: str) -> bytes:
    return value.encode("latin-1", "replace")[:30].ljust(30, b"\x00")


def _append_id3v1_footer(path: Path) -> None:
    footer = b"".join(
        (
            b"TAG",
            _id3v1_field("Historical V1 Title"),
            _id3v1_field("Historical V1 Artist"),
            _id3v1_field("Historical V1 Album"),
            b"1999",
            _id3v1_field("Historical V1 Comment"),
            bytes((255,)),
        )
    )
    with path.open("ab") as handle:
        handle.write(footer)


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

    def test_replace_preserves_id3v1_history_with_long_unicode_v2_tags(self) -> None:
        from mutagen.id3 import COMM, ID3, TALB, TIT2, TPE1

        tags = ID3(self.path)
        tags.delall("TIT2")
        tags.delall("TPE1")
        tags.delall("TALB")
        tags.delall("COMM")
        tags.add(
            TIT2(
                encoding=3,
                text="A deliberately long Unicode v2 title — これは新しいタイトルです",
            )
        )
        tags.add(
            TPE1(
                encoding=3,
                text="A deliberately long Unicode v2 artist — артист исполнителя",
            )
        )
        tags.add(TALB(encoding=3, text="A long v2 album — アルバム"))
        tags.add(COMM(encoding=3, lang="eng", desc="", text="A new v2 comment — комментарий"))
        tags.save(self.path)
        _append_id3v1_footer(self.path)

        before_bytes = self.path.read_bytes()
        before_footer = _id3v1_footer(before_bytes)
        self.assertTrue(before_footer)
        repair.replace_genres(self.path, ["Rock"])
        after_bytes = self.path.read_bytes()
        after_footer = _id3v1_footer(after_bytes)
        self.assertTrue(after_footer)
        self.assertEqual(_mpeg_payload(before_bytes), _mpeg_payload(after_bytes))
        self.assertEqual(before_footer[:127], after_footer[:127])
        self.assertEqual([str(frame) for frame in ID3(self.path).getall("TCON")], ["Rock"])
        self.assertEqual(after_footer[127], 17)

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

    def test_interrupted_apply_resumes_and_rolls_back(self) -> None:
        second = self.root / "Probe Artist" / "resume.mp3"
        shutil.copy2(self.path, second)
        with sqlite3.connect(self.db) as connection:
            connection.execute(
                "INSERT INTO media_file VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
                (
                    "media-resume",
                    "library-1",
                    "Probe Artist/resume.mp3",
                    "Probe Song",
                    "Probe Artist",
                    "recording-resume",
                    0,
                    json.dumps({"genre": [{"id": "genre-music", "value": "Music"}]}),
                ),
            )
            connection.commit()

        before_first = repair.file_state(self.path)
        before_second = repair.file_state(second)

        def candidate(
            media_id: str, path: Path, before: dict[str, object], recording_id: str
        ) -> dict[str, object]:
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

        manifest_path = Path(self.temp.name) / "resume-manifest.json"
        manifest_path.write_text(
            json.dumps(
                {
                    "version": repair.MANIFEST_VERSION,
                    "mode": "dry-run",
                    "db_path": str(self.db.resolve()),
                    "music_root": "",
                    "entries": [
                        candidate("media-1", self.path, before_first, "recording-1"),
                        candidate("media-resume", second, before_second, "recording-resume"),
                    ],
                }
            ),
            encoding="utf-8",
        )

        backup_dir = Path(self.temp.name) / "resume-backup"
        real_replace = repair.replace_genres
        calls = 0

        def interrupt_on_second(path: Path, replacement: list[str]) -> dict[str, object]:
            nonlocal calls
            calls += 1
            if calls == 2:
                raise RuntimeError("simulated interruption")
            return real_replace(path, replacement)

        with mock.patch.object(repair, "replace_genres", side_effect=interrupt_on_second):
            with self.assertRaises(RuntimeError):
                repair.apply_manifest(manifest_path, self.db, backup_dir, True)

        failed = json.loads(manifest_path.read_text(encoding="utf-8"))
        self.assertEqual(failed["apply"]["status"], "failed")
        self.assertTrue(failed["entries"][0].get("post_file"))
        self.assertFalse(failed["entries"][1].get("post_file"))

        repair.apply_manifest(manifest_path, self.db, backup_dir, True)
        applied = json.loads(manifest_path.read_text(encoding="utf-8"))
        self.assertEqual(applied["mode"], "applied")
        self.assertEqual(applied["apply"]["status"], "complete")
        self.assertTrue(all(entry.get("post_file") for entry in applied["entries"]))

        repair.rollback_manifest(manifest_path, self.db, True, False)
        rolled_back = json.loads(manifest_path.read_text(encoding="utf-8"))
        self.assertEqual(rolled_back["mode"], "rolled-back")
        self.assertEqual(repair.file_state(self.path)["sha256"], before_first["sha256"])
        self.assertEqual(repair.file_state(second)["sha256"], before_second["sha256"])


class GenreGroupingTest(unittest.TestCase):
    def _review_fixture(
        self,
        temporary: str,
        *,
        media_id: str = "media-1",
        genres: list[str] | None = None,
        mbid: str = "",
    ) -> tuple[Path, list[dict[str, object]], dict[str, object]]:
        root = Path(temporary)
        path = root / "probe.mp3"
        path.write_bytes(b"probe")
        file_genres = list(genres or ["Music"])
        state: dict[str, object] = {
            "sha256": "a" * 64,
            "size": 5,
            "mtime_ns": 1,
            "mode": 0o644,
            "genres": file_genres,
            "native_tags_sha256": "unchanged",
        }
        tracks: list[dict[str, object]] = [
            {
                "id": media_id,
                "library_id": "library-1",
                "library_path": str(root),
                "path": "probe.mp3",
                "absolute_path": str(path.resolve()),
                "title": "Probe Song",
                "artist": "Probe Artist",
                "mbz_recording_id": mbid,
                "missing": 0,
                "path_error": "",
                "tags": json.dumps({"genre": [{"value": value} for value in file_genres]}),
                "db_genres": file_genres,
            }
        ]
        return root, tracks, state

    def _review_input(self, tracks: list[dict[str, object]], genres: list[str] | None = None) -> dict[str, object]:
        track = tracks[0]
        return {
            "version": 1,
            "entries": [
                {
                    "media_file_id": track["id"],
                    "genres": list(genres or ["pop"]),
                    "sources": ["https://example.test/review/probe"],
                    "evidence_note": "Exact track evidence from the reviewed source.",
                    "expected": {
                        "path": track["path"],
                        "title": track["title"],
                        "artist": track["artist"],
                        "file_sha256": "a" * 64,
                    },
                }
            ],
        }

    def test_reviewed_no_mbid_candidate_skips_automatic_artist_lookup(self) -> None:
        with tempfile.TemporaryDirectory(prefix="navidrome-reviewed-no-mbid-") as temporary:
            root, tracks, state = self._review_fixture(temporary, mbid="")
            review_path = root / "reviewed.json"
            review_path.write_text(json.dumps(self._review_input(tracks)), encoding="utf-8")

            class NoLookupClient:
                base_url = "test"
                user_agent = "test"
                cache_dir = root
                request_count = 0

                def fetch(self, *args: object, **kwargs: object) -> object:
                    raise AssertionError("reviewed no-MBID candidate must not trigger MusicBrainz")

            with mock.patch.object(repair, "file_state", return_value=state):
                reviewed = repair.load_reviewed_genres(review_path, tracks, root, 2)
                manifest = repair.make_manifest(
                    root / "navidrome.db",
                    tracks,
                    NoLookupClient(),
                    root / "manifest.json",
                    root,
                    1,
                    2,
                    False,
                    reviewed,
                    review_path,
                )
            entry = manifest["entries"][0]
            self.assertEqual(entry["status"], "candidate")
            self.assertEqual(entry["replacement_genres"], ["pop"])
            self.assertEqual(entry["source"]["provenance"], "reviewed per-file proposal")
            self.assertEqual(entry["source"]["sources"], ["https://example.test/review/probe"])
            self.assertEqual(
                entry["source"]["evidence_note"],
                "Exact track evidence from the reviewed source.",
            )
            self.assertEqual(manifest["reviewed_genres"]["entry_count"], 1)
            self.assertEqual(manifest["groups"], {})
            self.assertEqual(manifest["audit"]["reviewed_candidates"], 1)

    def test_reviewed_duplicate_and_unknown_ids_are_rejected(self) -> None:
        with tempfile.TemporaryDirectory(prefix="navidrome-reviewed-ids-") as temporary:
            root, tracks, state = self._review_fixture(temporary)
            with mock.patch.object(repair, "file_state", return_value=state):
                duplicate = self._review_input(tracks)
                duplicate["entries"].append(dict(duplicate["entries"][0]))
                duplicate_path = root / "duplicate.json"
                duplicate_path.write_text(json.dumps(duplicate), encoding="utf-8")
                with self.assertRaisesRegex(repair.RepairError, "duplicate reviewed genre"):
                    repair.load_reviewed_genres(duplicate_path, tracks, root, 2)

                unknown = self._review_input(tracks)
                unknown["entries"][0]["media_file_id"] = "does-not-exist"
                unknown_path = root / "unknown.json"
                unknown_path.write_text(json.dumps(unknown), encoding="utf-8")
                with self.assertRaisesRegex(repair.RepairError, "unknown reviewed genre"):
                    repair.load_reviewed_genres(unknown_path, tracks, root, 2)

    def test_reviewed_identity_and_hash_mismatch_are_rejected(self) -> None:
        with tempfile.TemporaryDirectory(prefix="navidrome-reviewed-fingerprint-") as temporary:
            root, tracks, state = self._review_fixture(temporary)
            for field, value, message in (
                ("path", "other.mp3", "expected path"),
                ("title", "Changed title", "expected title"),
                ("artist", "Changed artist", "expected artist"),
                ("file_sha256", "b" * 64, "expected file hash"),
            ):
                proposal = self._review_input(tracks)
                proposal["entries"][0]["expected"][field] = value
                review_path = root / f"{field}.json"
                review_path.write_text(json.dumps(proposal), encoding="utf-8")
                with mock.patch.object(repair, "file_state", return_value=state):
                    with self.assertRaisesRegex(repair.RepairError, message):
                        repair.load_reviewed_genres(review_path, tracks, root, 2)

    def test_reviewed_genre_validation_rejects_categories_and_format_labels(self) -> None:
        with tempfile.TemporaryDirectory(prefix="navidrome-reviewed-genres-") as temporary:
            root, tracks, state = self._review_fixture(temporary)
            for value in ("Music", "People & Blogs", "slowed", "guitar remix", "edit audio", "https://genre.test"):
                proposal = self._review_input(tracks, [value])
                review_path = root / f"{repair.normalize_genre(value).replace(' ', '-')}.json"
                review_path.write_text(json.dumps(proposal), encoding="utf-8")
                with mock.patch.object(repair, "file_state", return_value=state):
                    with self.assertRaisesRegex(repair.RepairError, "non-musical genre"):
                        repair.load_reviewed_genres(review_path, tracks, root, 2)

    def test_reviewed_proposal_preserves_existing_musical_genres(self) -> None:
        with tempfile.TemporaryDirectory(prefix="navidrome-reviewed-preserve-") as temporary:
            root, tracks, state = self._review_fixture(temporary, genres=["jazz"])
            review_path = root / "reviewed.json"
            review_path.write_text(json.dumps(self._review_input(tracks, ["pop"])), encoding="utf-8")

            class NoLookupClient:
                base_url = "test"
                user_agent = "test"
                cache_dir = root
                request_count = 0

            with mock.patch.object(repair, "file_state", return_value=state):
                reviewed = repair.load_reviewed_genres(review_path, tracks, root, 2)
                manifest = repair.make_manifest(
                    root / "navidrome.db",
                    tracks,
                    NoLookupClient(),
                    root / "manifest.json",
                    root,
                    1,
                    2,
                    False,
                    reviewed,
                    review_path,
                )
            entry = manifest["entries"][0]
            self.assertEqual(entry["status"], "skip-existing-genre")
            self.assertNotIn("replacement_genres", entry)
            self.assertFalse(entry["reviewed"]["applied"])
            self.assertEqual(entry["file_genres"], ["jazz"])

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
