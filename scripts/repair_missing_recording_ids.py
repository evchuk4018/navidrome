#!/usr/bin/env python3
"""Build or apply a conservative repair manifest for missing recording MBIDs.

The default mode is read-only: it reads the Navidrome database, the optional
beets database, and any completed download/radio records that point directly at
a media-file row, then writes reviewable evidence.  File and Beets mutations
are available only through the explicit ``--apply --yes`` or
``--canonicalize-applied --yes`` modes.  Those modes require a reviewed
manifest, recheck identity and hashes, write full backups before each change,
and leave Navidrome's database to its normal scanner.

The only automatic candidates are backed by an unambiguous MusicBrainz
recording ID already present in a local source of truth:

* an exact relative-path match in beets with ``mb_trackid``;
* a succeeded song download job whose ``media_file_id`` points at the row;
* a personal-radio or discovery row whose ``media_file_id`` points at the row.

If multiple sources disagree, the row is placed in ``conflicts`` and is not a
candidate.  The optional MusicBrainz search mode is suggestion-only and marks
its results ``review_required``; it is never used for the local candidate set.

This follows repair.md: keep the original files, retain before/after evidence,
and review file-tag changes before any Navidrome rescan.  The optional SQL
output is only a proposal for a later, coordinated operation; it is not an
application mechanism and does not replace writing the file tags.
"""

from __future__ import annotations

import argparse
import contextlib
import enum
import hashlib
import json
import os
import re
import shutil
import sqlite3
import sys
import tempfile
import time
import unicodedata
import urllib.parse
import urllib.request
from collections import Counter, defaultdict
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Iterable


MBID_RE = re.compile(
    r"^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-"
    r"[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$"
)
DEFAULT_USER_AGENT = (
    "NavidromeMissingRecordingIDRepair/1.0 "
    "(https://github.com/evchuk4018/navidrome)"
)


def text(value: Any) -> str:
    """Convert SQLite TEXT/BLOB values to stable UTF-8 strings."""

    if value is None:
        return ""
    if isinstance(value, bytes):
        return value.decode("utf-8", errors="replace")
    return str(value)


def clean(value: Any) -> str:
    return text(value).strip()


def valid_mbid(value: Any) -> bool:
    return bool(MBID_RE.fullmatch(clean(value)))


def normalized_key(value: Any) -> str:
    """Normalize names for external suggestions, never for local joins."""

    value = unicodedata.normalize("NFKC", clean(value)).casefold()
    return " ".join(re.findall(r"[^\W_]+", value, flags=re.UNICODE))


def duration_evidence(media_seconds: Any, reference_milliseconds: Any) -> dict[str, Any]:
    """Compare a media duration with a catalog duration without guessing."""

    result: dict[str, Any] = {
        "media_duration_seconds": media_seconds,
        "reference_length_ms": reference_milliseconds,
        "duration_delta_seconds": None,
        "duration_match": False,
    }
    try:
        media = float(media_seconds or 0)
        reference_ms = float(reference_milliseconds or 0)
        if media <= 0 or reference_ms <= 0:
            return result
        delta = abs(media - reference_ms / 1000.0)
        result["duration_delta_seconds"] = round(delta, 3)
        result["duration_match"] = delta <= max(2.0, media * 0.02)
    except (TypeError, ValueError):
        return result
    return result


def beets_length_milliseconds(value: Any) -> float | None:
    """Convert Beets' ``items.length`` seconds to the evidence unit (ms)."""

    try:
        seconds = float(value)
    except (TypeError, ValueError):
        return None
    if seconds <= 0:
        return None
    return round(seconds * 1000.0, 3)


def marker_tokens(value: Any) -> set[str]:
    """Extract version markers that should agree during review."""

    normalized = normalized_key(value)
    markers = {
        "acoustic",
        "alternate",
        "edit",
        "extended",
        "instrumental",
        "live",
        "mix",
        "nightcore",
        "remaster",
        "remix",
        "reverb",
        "slowed",
        "sped",
        "version",
    }
    return {token for token in normalized.split() if token in markers}


def path_key(value: Any) -> str:
    """Return the exact relative path representation used for local joins."""

    return text(value)


def atomic_write_json(path: Path, value: Any) -> None:
    """Write a manifest atomically so a failed apply leaves valid JSON."""

    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary_name = tempfile.mkstemp(
        prefix=f".{path.name}.", suffix=".tmp", dir=path.parent
    )
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
            json.dump(value, handle, ensure_ascii=False, indent=2)
            handle.write("\n")
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary_name, path)
    finally:
        with contextlib.suppress(FileNotFoundError):
            os.unlink(temporary_name)


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def file_inside(root: Path, path: Path) -> bool:
    try:
        path.relative_to(root)
        return True
    except ValueError:
        return False


def resolve_media_path(music_root: Path, relative_path: str) -> Path:
    if not relative_path:
        raise RuntimeError("media path is empty")
    root = music_root.expanduser().resolve(strict=False)
    candidate = (root / Path(relative_path)).resolve(strict=False)
    if not file_inside(root, candidate):
        raise RuntimeError(f"media path escapes music root: {relative_path}")
    if candidate.is_symlink() or not candidate.is_file():
        raise RuntimeError(f"media path is not a regular file: {candidate}")
    return candidate


def import_mutagen_file() -> Any:
    try:
        from mutagen import File as mutagen_file
    except ImportError as exc:
        raise RuntimeError("mutagen is required for audio-tag capture/apply") from exc
    return mutagen_file


def open_audio(path: Path) -> Any:
    mutagen_file = import_mutagen_file()
    try:
        audio = mutagen_file(path, easy=False)
    except Exception as exc:  # mutagen exposes format-specific errors
        raise RuntimeError(f"read audio tags {path}: {exc}") from exc
    if audio is None:
        raise RuntimeError(f"unsupported audio format or missing tags: {path}")
    return audio


def tag_value_for_snapshot(value: Any) -> Any:
    """Serialize native Mutagen values without losing frame attributes."""

    if value is None or isinstance(value, (str, int, float, bool)):
        return str(value)
    if isinstance(value, enum.Enum):
        return {"enum": type(value).__name__, "value": tag_value_for_snapshot(value.value)}
    if isinstance(value, bytes):
        return {"bytes_sha256": hashlib.sha256(value).hexdigest(), "length": len(value)}
    if isinstance(value, (list, tuple)):
        return [tag_value_for_snapshot(item) for item in value]
    if isinstance(value, dict):
        return {
            str(key): tag_value_for_snapshot(item)
            for key, item in sorted(value.items(), key=lambda pair: str(pair[0]))
        }
    try:
        attributes = getattr(value, "__dict__", None)
    except (AttributeError, TypeError):
        attributes = None
    if isinstance(attributes, dict):
        return {
            "type": type(value).__name__,
            "attributes": tag_value_for_snapshot(attributes),
        }
    return str(value)


def legacy_tag_value_for_snapshot(value: Any) -> Any:
    """Serialize values as the first-pass manifest did, for compatibility."""

    if isinstance(value, bytes):
        return {"bytes_sha256": hashlib.sha256(value).hexdigest(), "length": len(value)}
    if isinstance(value, (list, tuple)):
        return [tag_value_for_snapshot(item) for item in value]
    if hasattr(value, "text"):
        return legacy_tag_value_for_snapshot(getattr(value, "text"))
    return str(value)


def is_recording_id_key(key: Any) -> bool:
    """Return whether a native tag key carries a recording MBID.

    Navidrome's mapping calls the recording ID ``musicbrainz_recordingid``
    but also accepts the older MusicBrainz/Picard representations.  In
    particular, ID3 files use ``UFID:http://musicbrainz.org`` and some files
    use ``TXXX:MusicBrainz Track Id``.  Keep ``release track id`` out of this
    predicate because that is a different MusicBrainz entity.
    """

    key_text = str(key).casefold()
    normalized = re.sub(r"[^a-z0-9]", "", key_text)
    if "musicbrainzrecordingid" in normalized:
        return True
    if key_text.startswith("ufid:http://musicbrainz.org"):
        return True
    return "musicbrainztrackid" in normalized and "releasetrackid" not in normalized


def recording_tag_value(value: Any) -> str:
    """Read a recording ID from mutagen's text and UFID frame objects."""

    # Native ID3 frames expose ``text`` (TXXX) or ``data`` (UFID), while
    # MP4/Vorbis mappings may already be plain strings, bytes, or one-element
    # lists.  Start with the value itself so those direct representations are
    # not converted to an empty string by a missing attribute lookup.
    raw = value
    if hasattr(value, "text"):
        raw = getattr(value, "text")
    elif hasattr(value, "data"):
        raw = getattr(value, "data")
    if isinstance(raw, bytes):
        return clean(raw.decode("utf-8", errors="replace"))
    if isinstance(raw, (list, tuple)):
        return clean(raw[0]) if raw else ""
    return clean(raw)


def recording_tag_values(tags: Any) -> list[str]:
    if tags is None:
        return []
    values: list[str] = []
    for key, value in tags.items():
        if not is_recording_id_key(key):
            continue
        parsed = recording_tag_value(value)
        if parsed:
            values.append(parsed)
    return [value for value in values if value]


def non_recording_tag_snapshot(path: Path) -> str:
    audio = open_audio(path)
    tags = audio.tags
    values: dict[str, Any] = {}
    if tags is not None:
        for key in sorted(tags.keys(), key=str):
            if is_recording_id_key(key):
                continue
            key_text = str(key)
            if hasattr(tags, "getall"):
                values[key_text] = [
                    tag_value_for_snapshot(item) for item in tags.getall(key_text)
                ]
            else:
                values[key_text] = tag_value_for_snapshot(tags[key])
    encoded = json.dumps(
        values, sort_keys=True, separators=(",", ":"), ensure_ascii=False
    ).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def legacy_non_recording_tag_snapshot(path: Path) -> str:
    """Recreate the first-pass snapshot for guards on an already-applied manifest."""

    def legacy_recording_key(key: Any) -> bool:
        normalized = re.sub(r"[^a-z0-9]", "", str(key).casefold())
        return "musicbrainzrecordingid" in normalized

    audio = open_audio(path)
    tags = audio.tags
    values: dict[str, Any] = {}
    if tags is not None:
        for key, value in tags.items():
            if legacy_recording_key(key):
                continue
            values[str(key)] = legacy_tag_value_for_snapshot(value)
    encoded = json.dumps(
        values, sort_keys=True, separators=(",", ":"), ensure_ascii=False
    ).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def audio_file_state(path: Path) -> dict[str, Any]:
    stat = path.stat()
    return {
        "sha256": sha256_file(path),
        "size": stat.st_size,
        "mtime_ns": stat.st_mtime_ns,
        "other_tags_sha256": non_recording_tag_snapshot(path),
        "legacy_other_tags_sha256": legacy_non_recording_tag_snapshot(path),
        "recording_id_tags": recording_tag_values(open_audio(path).tags),
    }


def write_recording_id_tag(
    path: Path, recording_id: str, *, allow_existing: bool = False
) -> None:
    """Atomically add one scanner-recognized MusicBrainz recording ID.

    For ID3, Navidrome's canonical mapping is the UFID owner
    ``http://musicbrainz.org``.  The earlier repair pass wrote a descriptive
    TXXX frame that mutagen could read but Navidrome did not map, so remove
    that repair-only frame when present and populate the existing UFID frame.
    """

    temporary_name: str | None = None
    temporary_path: Path | None = None
    original_stat = path.stat()
    audio = open_audio(path)
    tags = audio.tags
    try:
        existing_recording_ids = recording_tag_values(audio.tags)
        if existing_recording_ids and (
            not allow_existing or existing_recording_ids != [recording_id]
        ):
            raise RuntimeError(f"recording ID tag is no longer blank: {path}")

        # Work on a same-directory copy and replace the original only after a
        # successful save and readback.  This prevents a partial rewrite from
        # leaving a truncated audio file if the process is interrupted.
        descriptor, temporary_name = tempfile.mkstemp(
            prefix=f".{path.name}.", suffix=".tmp", dir=path.parent
        )
        os.close(descriptor)
        temporary_path = Path(temporary_name)
        shutil.copy2(path, temporary_path)
        audio = open_audio(temporary_path)
        tags = audio.tags
        if tags is None:
            try:
                audio.add_tags()
                tags = audio.tags
            except Exception as exc:
                raise RuntimeError(f"audio has no writable tag container: {path}: {exc}") from exc
        if tags is None:
            raise RuntimeError(f"audio has no writable tag container: {path}")

        from mutagen.id3 import ID3, UFID

        if isinstance(tags, ID3):
            # The first pass added this non-canonical frame.  It is safe to
            # remove because pre_file recorded no recording ID before that
            # pass; retain every unrelated frame unchanged.
            for key in list(tags.keys()):
                normalized = re.sub(r"[^a-z0-9]", "", str(key).casefold())
                if normalized == "txxxmusicbrainzrecordingid":
                    del tags[key]
            tags["UFID:http://musicbrainz.org"] = UFID(
                owner="http://musicbrainz.org", data=recording_id.encode("ascii")
            )
        elif audio.__class__.__name__.casefold() == "mp4":
            tags["----:com.apple.iTunes:MusicBrainz Track Id"] = [
                recording_id.encode("utf-8")
            ]
        else:
            tags["musicbrainz_recordingid"] = [recording_id]
        audio.save()

        temporary_state = audio_file_state(temporary_path)
        if temporary_state["recording_id_tags"] != [recording_id]:
            raise RuntimeError(
                f"recording ID verification failed for temporary file {path}: "
                f"{temporary_state['recording_id_tags']!r}"
            )
        if temporary_state["other_tags_sha256"] != non_recording_tag_snapshot(path):
            raise RuntimeError(f"non-recording tags changed while writing {path}")
        os.chmod(temporary_path, original_stat.st_mode)
        os.replace(temporary_path, path)
        temporary_path = None
    except Exception as exc:
        raise RuntimeError(f"write MusicBrainz recording ID tag {path}: {exc}") from exc

    finally:
        if temporary_path is not None:
            with contextlib.suppress(FileNotFoundError):
                temporary_path.unlink()

    state = audio_file_state(path)
    if state["recording_id_tags"] != [recording_id]:
        raise RuntimeError(
            f"recording ID verification failed for {path}: {state['recording_id_tags']!r}"
        )


def copy_backup(source: Path, target: Path) -> None:
    target.parent.mkdir(parents=True, exist_ok=True)
    temporary = target.with_name(f".{target.name}.tmp")
    shutil.copy2(source, temporary)
    os.replace(temporary, target)


def sqlite_backup(source: Path, target: Path) -> None:
    if target.exists():
        raise RuntimeError(f"backup already exists: {target}")
    target.parent.mkdir(parents=True, exist_ok=True)
    source_connection = sqlite3.connect(str(source))
    try:
        destination = sqlite3.connect(str(target))
        try:
            source_connection.backup(destination)
        finally:
            destination.close()
    finally:
        source_connection.close()


def connect_readonly(path: Path) -> sqlite3.Connection:
    resolved = path.expanduser().resolve()
    # SQLite URI mode prevents an accidental creation/write when a path is
    # mistyped.  Keep slash and colon characters for Linux and Windows paths.
    uri = "file:" + urllib.parse.quote(resolved.as_posix(), safe="/:\\") + "?mode=ro"
    connection = sqlite3.connect(uri, uri=True)
    connection.row_factory = sqlite3.Row
    return connection


def table_exists(connection: sqlite3.Connection, name: str) -> bool:
    row = connection.execute(
        "SELECT 1 FROM sqlite_master WHERE type='table' AND name=?", (name,)
    ).fetchone()
    return row is not None


def table_columns(connection: sqlite3.Connection, name: str) -> set[str]:
    if not table_exists(connection, name):
        return set()
    return {
        clean(row[1])
        for row in connection.execute(f'PRAGMA table_info("{name}")').fetchall()
    }


def rows(connection: sqlite3.Connection, query: str) -> list[dict[str, Any]]:
    return [dict(row) for row in connection.execute(query).fetchall()]


def row_count(connection: sqlite3.Connection, query: str, args: Iterable[Any] = ()) -> int:
    row = connection.execute(query, tuple(args)).fetchone()
    return int(row[0]) if row else 0


def source_record(
    source_type: str,
    recording_id: str,
    *,
    evidence: dict[str, Any] | None = None,
) -> dict[str, Any]:
    result: dict[str, Any] = {
        "source_type": source_type,
        "recording_id": recording_id,
    }
    if evidence:
        result["evidence"] = evidence
    return result


def add_source(
    sources: dict[str, list[dict[str, Any]]],
    invalid_sources: dict[str, list[dict[str, Any]]],
    media_file_id: str,
    source: dict[str, Any],
) -> None:
    recording_id = clean(source.get("recording_id"))
    if valid_mbid(recording_id):
        source["recording_id"] = recording_id.lower()
        sources[media_file_id].append(source)
    elif recording_id:
        invalid_sources[media_file_id].append(source)


def build_local_manifest(
    navidrome_db: Path,
    beets_db: Path | None,
) -> tuple[dict[str, Any], list[dict[str, Any]]]:
    nav = connect_readonly(navidrome_db)
    try:
        media_columns = table_columns(nav, "media_file")
        if not media_columns:
            raise RuntimeError("Navidrome database has no media_file table")

        media = rows(
            nav,
            """
            SELECT id, path, title, artist, album, year, genre, duration,
                   mbz_recording_id, missing, library_id, created_at, updated_at
            FROM media_file
            ORDER BY path, id
            """,
        )
        active = [row for row in media if int(row.get("missing") or 0) == 0]
        missing_id = [row for row in active if not clean(row.get("mbz_recording_id"))]
        media_by_id = {clean(row["id"]): row for row in missing_id}

        beets_rows: list[dict[str, Any]] = []
        beets_available = bool(beets_db)
        beets_error = ""
        if beets_db:
            try:
                beets = connect_readonly(beets_db)
                try:
                    if table_exists(beets, "items"):
                        beets_rows = rows(
                            beets,
                            """
                            SELECT id, path, title, artist, album, mb_trackid,
                                   mb_releasetrackid, mb_albumid, mb_artistid,
                                   genres, added, mtime, length
                            FROM items
                            ORDER BY path, id
                            """,
                        )
                    else:
                        beets_available = False
                        beets_error = "beets database has no items table"
                finally:
                    beets.close()
            except (OSError, sqlite3.Error) as exc:
                beets_available = False
                beets_error = f"unable to read beets database: {exc}"

        beets_by_path: dict[str, list[dict[str, Any]]] = defaultdict(list)
        for row in beets_rows:
            beets_by_path[path_key(row.get("path"))].append(row)

        sources: dict[str, list[dict[str, Any]]] = defaultdict(list)
        invalid_sources: dict[str, list[dict[str, Any]]] = defaultdict(list)

        exact_beets_paths = 0
        exact_beets_paths_with_id = 0
        exact_beets_paths_without_id = 0
        for item in missing_id:
            matches = beets_by_path.get(path_key(item.get("path")), [])
            if not matches:
                continue
            exact_beets_paths += 1
            nonblank_ids = [clean(match.get("mb_trackid")) for match in matches if clean(match.get("mb_trackid"))]
            if nonblank_ids:
                exact_beets_paths_with_id += 1
            else:
                exact_beets_paths_without_id += 1
            for match in matches:
                recording_id = clean(match.get("mb_trackid"))
                if recording_id:
                    add_source(
                        sources,
                        invalid_sources,
                        clean(item["id"]),
                        source_record(
                            "beets_exact_path",
                            recording_id,
                            evidence={
                                "beets_item_id": match.get("id"),
                                "beets_path": path_key(match.get("path")),
                                "beets_title": clean(match.get("title")),
                                "beets_artist": clean(match.get("artist")),
                                "beets_album": clean(match.get("album")),
                                "beets_length_seconds": match.get("length"),
                                **duration_evidence(
                                    item.get("duration"),
                                    beets_length_milliseconds(match.get("length")),
                                ),
                            },
                        ),
                    )

        jobs: list[dict[str, Any]] = []
        if table_exists(nav, "music_download_job"):
            jobs = rows(
                nav,
                """
                SELECT id, kind, status, source_id, artist, album, title,
                       origin, media_file_id, radio_item_id, created_at, finished_at
                FROM music_download_job
                WHERE lower(trim(status)) = 'succeeded'
                ORDER BY finished_at, id
                """,
            )
        direct_job_sources = 0
        unmatched_job_sources = 0
        for job in jobs:
            source_id = clean(job.get("source_id"))
            media_file_id = clean(job.get("media_file_id"))
            if clean(job.get("kind")) != "song" or not valid_mbid(source_id):
                continue
            if media_file_id in media_by_id:
                direct_job_sources += 1
                add_source(
                    sources,
                    invalid_sources,
                    media_file_id,
                    source_record(
                        "completed_download_job",
                        source_id,
                        evidence={
                            "job_id": job.get("id"),
                            "job_status": job.get("status"),
                            "job_origin": job.get("origin"),
                            "job_artist": clean(job.get("artist")),
                            "job_title": clean(job.get("title")),
                            "job_album": clean(job.get("album")),
                            "finished_at": job.get("finished_at"),
                        },
                    ),
                )
            else:
                unmatched_job_sources += 1

        direct_radio_sources = 0
        radio_tables = {
            "personal_radio_item": "personal_radio_item",
            "discovery_track": "discovery_track",
        }
        for source_type, table in radio_tables.items():
            if not table_exists(nav, table):
                continue
            columns = table_columns(nav, table)
            if not {"media_file_id", "recording_mbid"}.issubset(columns):
                continue
            radio_rows = rows(
                nav,
                f"""
                SELECT media_file_id, recording_mbid
                FROM "{table}"
                WHERE length(trim(recording_mbid)) > 0
                """,
            )
            for radio in radio_rows:
                media_file_id = clean(radio.get("media_file_id"))
                recording_id = clean(radio.get("recording_mbid"))
                if media_file_id in media_by_id:
                    direct_radio_sources += 1
                    add_source(
                        sources,
                        invalid_sources,
                        media_file_id,
                        source_record(
                            source_type,
                            recording_id,
                            evidence={
                                "media_file_id": media_file_id,
                            },
                        ),
                    )

        candidates: list[dict[str, Any]] = []
        conflicts: list[dict[str, Any]] = []
        no_local_evidence: list[dict[str, Any]] = []
        source_type_counts: Counter[str] = Counter()
        for item in missing_id:
            media_file_id = clean(item["id"])
            item_sources = sources.get(media_file_id, [])
            values = sorted({clean(source["recording_id"]).lower() for source in item_sources})
            base = {
                "media_file_id": media_file_id,
                "path": path_key(item.get("path")),
                "title": clean(item.get("title")),
                "artist": clean(item.get("artist")),
                "album": clean(item.get("album")),
                "year": item.get("year"),
                "duration_seconds": item.get("duration"),
                "genre": clean(item.get("genre")),
                "before_mbz_recording_id": clean(item.get("mbz_recording_id")),
            }
            if len(values) == 1:
                review_reasons = [
                    "validate_recording_title_version_artist_and_duration_before_apply"
                ]
                for source in item_sources:
                    evidence = source.get("evidence") or {}
                    if source["source_type"] == "beets_exact_path":
                        if evidence.get("duration_match") is False:
                            review_reasons.append("beets_duration_missing_or_mismatch")
                        if marker_tokens(item.get("title")) != marker_tokens(
                            evidence.get("beets_title")
                        ):
                            review_reasons.append("version_markers_need_review")
                candidate = {
                    **base,
                    "proposed_mbz_recording_id": values[0],
                    "confidence": "review_required",
                    "decision": "candidate_review",
                    "review_reasons": sorted(set(review_reasons)),
                    "source_evidence": item_sources,
                }
                candidates.append(candidate)
                source_type_counts.update(source["source_type"] for source in item_sources)
            elif len(values) > 1:
                conflicts.append(
                    {
                        **base,
                        "decision": "skip_conflict",
                        "recording_ids": values,
                        "source_evidence": item_sources,
                    }
                )
            else:
                no_local_evidence.append(
                    {
                        **base,
                        "decision": "no_local_evidence",
                        "invalid_source_evidence": invalid_sources.get(media_file_id, []),
                    }
                )

        beets_global_with_id = sum(1 for row in beets_rows if valid_mbid(row.get("mb_trackid")))
        beets_global_without_id = sum(1 for row in beets_rows if not clean(row.get("mb_trackid")))
        coverage: dict[str, Any] = {
            "media_files_total": len(media),
            "media_files_available": len(active),
            "media_files_available_with_recording_mbid": len(active) - len(missing_id),
            "media_files_available_missing_recording_mbid": len(missing_id),
            "missing_recording_mbid_percent_of_available": round(
                (100 * len(missing_id) / len(active)) if active else 0, 2
            ),
            "beets_database_available": beets_available,
            "beets_items_total": len(beets_rows),
            "beets_items_with_valid_mb_trackid": beets_global_with_id,
            "beets_items_without_mb_trackid": beets_global_without_id,
            "missing_rows_with_exact_beets_path": exact_beets_paths,
            "missing_rows_with_exact_beets_path_and_mb_trackid": exact_beets_paths_with_id,
            "missing_rows_with_exact_beets_path_but_no_mb_trackid": exact_beets_paths_without_id,
            "missing_rows_without_exact_beets_path": len(missing_id) - exact_beets_paths,
            "local_evidence_candidates": len(candidates),
            "high_confidence_candidates": 0,
            "all_local_candidates_require_external_version_validation": True,
            "conflicting_rows": len(conflicts),
            "rows_without_local_evidence": len(no_local_evidence),
            "candidate_source_types": dict(sorted(source_type_counts.items())),
            "succeeded_song_jobs_with_direct_media_link": direct_job_sources,
            "succeeded_song_jobs_without_matching_missing_media_link": unmatched_job_sources,
            "direct_radio_or_discovery_sources": direct_radio_sources,
        }
        if beets_error:
            coverage["beets_error"] = beets_error

        manifest: dict[str, Any] = {
            "schema_version": 1,
            "generated_at": datetime.now(timezone.utc).isoformat(),
            "mode": "dry-run",
            "databases": {
                "navidrome": str(navidrome_db),
                "beets": str(beets_db) if beets_db else None,
            },
            "coverage": coverage,
            "policy": {
                "local_join": "exact media_file.path == beets.items.path",
                "accepted_local_sources": [
                    "beets_exact_path",
                    "completed_download_job",
                    "personal_radio_item",
                    "discovery_track",
                ],
                "conflicting_sources_are_skipped": True,
                "local_candidates_require_musicbrainz_version_and_duration_validation": True,
                "existing_mbz_recording_id_is_never_overwritten": True,
                "audio_files_are_never_modified": True,
                "database_is_never_modified": True,
                "sql_output_requires_file_tag_review_and_a_backup": True,
            },
            "candidates": sorted(candidates, key=lambda row: (row["path"], row["media_file_id"])),
            "conflicts": sorted(conflicts, key=lambda row: (row["path"], row["media_file_id"])),
            "no_local_evidence": sorted(
                no_local_evidence, key=lambda row: (row["path"], row["media_file_id"])
            ),
        }
        return manifest, no_local_evidence
    finally:
        nav.close()


def attach_file_states(
    manifest: dict[str, Any],
    records: list[dict[str, Any]],
    music_root: Path,
) -> None:
    """Capture hashes/tag snapshots used by the later file-level apply guard."""

    errors: list[str] = []
    for record in records:
        try:
            path = resolve_media_path(music_root, clean(record.get("path")))
            record["absolute_path"] = str(path)
            record["pre_file"] = audio_file_state(path)
        except (OSError, RuntimeError) as exc:
            record["file_error"] = str(exc)
            errors.append(f"{record.get('media_file_id')}: {exc}")
    manifest["file_state_capture"] = {
        "music_root": str(music_root),
        "records": len(records),
        "errors": errors,
    }


def musicbrainz_suggestions(
    items: list[dict[str, Any]],
    *,
    user_agent: str,
    min_interval: float = 1.1,
    checkpoint: Path | None = None,
) -> tuple[list[dict[str, Any]], list[str]]:
    """Return review-only exact title/artist suggestions from MusicBrainz."""

    suggestions: list[dict[str, Any]] = []
    errors: list[str] = []
    completed: set[str] = set()
    if checkpoint and checkpoint.exists():
        try:
            saved = json.loads(checkpoint.read_text(encoding="utf-8"))
            suggestions = list(saved.get("suggestions", []))
            errors = list(saved.get("errors", []))
            completed = {clean(value) for value in saved.get("completed_media_file_ids", [])}
        except (OSError, ValueError, TypeError) as exc:
            errors.append(f"checkpoint could not be loaded: {exc}")

    def save_checkpoint() -> None:
        if not checkpoint:
            return
        checkpoint.parent.mkdir(parents=True, exist_ok=True)
        checkpoint.write_text(
            json.dumps(
                {
                    "schema_version": 1,
                    "completed_media_file_ids": sorted(completed),
                    "suggestions": suggestions,
                    "errors": errors,
                },
                ensure_ascii=False,
                indent=2,
            )
            + "\n",
            encoding="utf-8",
        )

    last_request = 0.0
    total = len(items)
    for index, item in enumerate(items, start=1):
        media_file_id = clean(item.get("media_file_id"))
        if media_file_id in completed:
            continue
        title = clean(item.get("title"))
        artist = clean(item.get("artist"))
        if not title or not artist:
            completed.add(media_file_id)
            save_checkpoint()
            continue
        query = f'artist:"{artist.replace(chr(92), chr(92) * 2).replace(chr(34), chr(92) + chr(34))}" AND recording:"{title.replace(chr(92), chr(92) * 2).replace(chr(34), chr(92) + chr(34))}"'
        url = "https://musicbrainz.org/ws/2/recording/?" + urllib.parse.urlencode(
            {"query": query, "fmt": "json", "limit": 25}
        )
        payload: dict[str, Any] | None = None
        for attempt in range(3):
            wait = min_interval - (time.monotonic() - last_request)
            if wait > 0:
                time.sleep(wait)
            request = urllib.request.Request(
                url,
                headers={"Accept": "application/json", "User-Agent": user_agent},
            )
            try:
                with urllib.request.urlopen(request, timeout=20) as response:  # nosec B310 - fixed HTTPS host
                    payload = json.load(response)
                last_request = time.monotonic()
                break
            except urllib.error.HTTPError as exc:
                # Keep the global limiter current even when the server rejects
                # a request.  Retry transient rate/service responses only twice
                # with a bounded delay, then preserve the error and continue.
                last_request = time.monotonic()
                if exc.code not in (429, 503) or attempt == 2:
                    errors.append(f"{media_file_id}: HTTP {exc.code} {exc.reason}")
                    break
                retry_after = clean(exc.headers.get("Retry-After"))
                try:
                    delay = float(retry_after)
                except (TypeError, ValueError):
                    delay = 2.0 ** (attempt + 1)
                time.sleep(min(10.0, max(min_interval, delay)))
            except Exception as exc:  # network/service errors belong in the manifest
                last_request = time.monotonic()
                errors.append(f"{media_file_id}: {exc}")
                break

        completed.add(media_file_id)
        print(f"musicbrainz lookup {index}/{total}: {media_file_id}", file=sys.stderr)
        if payload is None:
            save_checkpoint()
            continue

        exact: dict[str, dict[str, Any]] = {}
        for recording in payload.get("recordings", []):
            recording_id = clean(recording.get("id"))
            if not valid_mbid(recording_id):
                continue
            if normalized_key(recording.get("title")) != normalized_key(title):
                continue
            credit_parts: list[str] = []
            for credit in recording.get("artist-credit", []):
                if isinstance(credit, dict):
                    credit_parts.append(clean(credit.get("name")))
                    credit_parts.append(clean(credit.get("joinphrase")))
            credit_name = "".join(credit_parts)
            if normalized_key(credit_name) != normalized_key(artist):
                continue
            exact[recording_id.lower()] = recording
        if len(exact) != 1:
            save_checkpoint()
            continue
        recording_id, recording = next(iter(exact.items()))
        recording_length_ms = recording.get("length")
        duration_seconds = item.get("duration_seconds")
        duration_delta_seconds: float | None = None
        duration_match = False
        try:
            if recording_length_ms is not None and float(duration_seconds or 0) > 0:
                duration_delta_seconds = round(
                    abs(float(duration_seconds) - (float(recording_length_ms) / 1000.0)),
                    3,
                )
                # A small encoder/container discrepancy is normal.  Keep this
                # evidence separate from the exact title/artist gate so a
                # reviewer can see borderline matches instead of hiding them.
                duration_match = duration_delta_seconds <= max(
                    2.0, float(duration_seconds) * 0.02
                )
        except (TypeError, ValueError):
            pass
        suggestions.append(
            {
                **item,
                "proposed_mbz_recording_id": recording_id,
                "confidence": "review_required",
                "decision": "external_suggestion",
                "source_evidence": [
                    {
                        "source_type": "musicbrainz_recording_search",
                        "recording_id": recording_id,
                        "evidence": {
                            "query": query,
                            "score": recording.get("score"),
                            "recording_title": recording.get("title"),
                            "recording_length_ms": recording_length_ms,
                            "media_duration_seconds": duration_seconds,
                            "duration_delta_seconds": duration_delta_seconds,
                            "duration_match": duration_match,
                            "recording_artist_credit": "".join(
                                clean(part.get("name")) + clean(part.get("joinphrase"))
                                for part in recording.get("artist-credit", [])
                                if isinstance(part, dict)
                            ),
                        },
                    }
                ],
            }
        )
        save_checkpoint()
    return suggestions, errors


def sql_quote(value: str) -> str:
    return "'" + value.replace("'", "''") + "'"


def write_sql(path: Path, candidates: list[dict[str, Any]]) -> None:
    lines = [
        "-- Dry-run proposal generated by repair_missing_recording_ids.py",
        "-- Review file-tag evidence, take a database backup, and rescan after any coordinated apply.",
        "-- This file is not executed by the script.",
        "BEGIN;",
    ]
    for candidate in candidates:
        lines.append(
            "UPDATE media_file SET mbz_recording_id = "
            f"{sql_quote(candidate['proposed_mbz_recording_id'])} "
            "WHERE id = "
            f"{sql_quote(candidate['media_file_id'])} "
            "AND missing = 0 AND length(trim(mbz_recording_id)) = 0;"
        )
    lines.append("COMMIT;")
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")


def current_media_row(
    navidrome_db: Path,
    media_file_id: str,
) -> dict[str, Any] | None:
    connection = connect_readonly(navidrome_db)
    try:
        row = connection.execute(
            """
            SELECT id, path, title, artist, album, mbz_recording_id, missing
            FROM media_file WHERE id = ?
            """,
            (media_file_id,),
        ).fetchone()
        return dict(row) if row else None
    finally:
        connection.close()


def beets_rows_for_path(connection: sqlite3.Connection, media_path: str) -> list[dict[str, Any]]:
    return [
        dict(row)
        for row in connection.execute(
            """
            SELECT id, CAST(path AS TEXT) AS path, mb_trackid, title, artist, album
            FROM items WHERE CAST(path AS TEXT) = ? ORDER BY id
            """,
            (media_path,),
        ).fetchall()
    ]


def external_manifest_entries(manifest: dict[str, Any]) -> list[dict[str, Any]]:
    entries = manifest.get("external_suggestions", [])
    if not isinstance(entries, list) or not entries:
        raise RuntimeError("manifest has no external MusicBrainz suggestions to apply")
    selected: list[dict[str, Any]] = []
    for entry in entries:
        if not isinstance(entry, dict):
            raise RuntimeError("manifest contains a malformed external suggestion")
        if entry.get("decision") != "external_suggestion":
            raise RuntimeError("manifest contains a non-suggestion entry in external_suggestions")
        if entry.get("confidence") != "review_required":
            raise RuntimeError("external suggestion confidence is not review_required")
        proposed = clean(entry.get("proposed_mbz_recording_id"))
        if not valid_mbid(proposed):
            raise RuntimeError(f"invalid proposed recording ID for {entry.get('media_file_id')}")
        evidence = entry.get("source_evidence") or []
        if len(evidence) != 1 or evidence[0].get("source_type") != "musicbrainz_recording_search":
            raise RuntimeError(f"external evidence is not singular for {entry.get('media_file_id')}")
        evidence_recording_id = clean(evidence[0].get("recording_id"))
        if not valid_mbid(evidence_recording_id) or evidence_recording_id.lower() != proposed.lower():
            raise RuntimeError(
                f"proposed recording ID does not match source evidence for "
                f"{entry.get('media_file_id')}"
            )
        details = evidence[0].get("evidence") or {}
        if details.get("score") != 100 or details.get("duration_match") is not True:
            raise RuntimeError(
                f"external suggestion lacks score-100 duration evidence for {entry.get('media_file_id')}"
            )
        if entry.get("file_error") or not isinstance(entry.get("pre_file"), dict):
            raise RuntimeError(f"file pre-state is missing for {entry.get('media_file_id')}")
        selected.append(entry)
    return selected


def apply_recording_manifest(
    manifest_path: Path,
    navidrome_db: Path,
    beets_db: Path,
    music_root: Path,
    audio_backup_dir: Path,
    confirm: bool,
) -> None:
    """Apply only reviewed external suggestions to files and blank beets rows."""

    if not confirm:
        raise RuntimeError("apply requires --yes after reviewing the dry-run manifest")
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    if not isinstance(manifest, dict) or manifest.get("mode") != "dry-run":
        raise RuntimeError("apply requires an unmodified dry-run manifest")
    manifest_db = clean((manifest.get("databases") or {}).get("navidrome"))
    if manifest_db and Path(manifest_db).resolve() != navidrome_db.resolve():
        raise RuntimeError(f"manifest Navidrome DB is {manifest_db}, not {navidrome_db.resolve()}")
    entries = external_manifest_entries(manifest)

    root = music_root.expanduser().resolve(strict=False)
    backup_root = audio_backup_dir.expanduser().resolve(strict=False)
    # The tuple's third value is true when the audio writer already completed
    # before an interrupted apply.  Resume must still reconcile Beets rows.
    paths: list[tuple[dict[str, Any], Path, bool]] = []
    existing_apply = manifest.get("apply")
    beets_connection = sqlite3.connect(str(beets_db))
    beets_connection.row_factory = sqlite3.Row
    try:
        if not table_exists(beets_connection, "items"):
            raise RuntimeError("beets database has no items table")
        for entry in entries:
            media_id = clean(entry.get("media_file_id"))
            proposed = clean(entry["proposed_mbz_recording_id"]).lower()
            current = current_media_row(navidrome_db, media_id)
            if current is None:
                raise RuntimeError(f"media-file ID no longer exists: {media_id}")
            for field in ("path", "title", "artist", "album"):
                if clean(current.get(field)) != clean(entry.get(field)):
                    raise RuntimeError(f"media-file {media_id} changed field {field}")
            if int(current.get("missing") or 0) != 0:
                raise RuntimeError(f"media-file {media_id} is now marked missing")
            current_recording_id = clean(current.get("mbz_recording_id"))
            resume_evidence = entry.get("post_file") is not None or bool(
                clean(entry.get("backup_path"))
            )
            if current_recording_id and (
                current_recording_id.lower() != proposed or not resume_evidence
            ):
                raise RuntimeError(
                    f"media-file {media_id} already has a conflicting recording ID"
                )
            path = resolve_media_path(root, clean(entry.get("path")))
            pre_file = entry["pre_file"]
            current_file = audio_file_state(path)
            post_file = entry.get("post_file")
            audio_applied = False
            if post_file is not None and not isinstance(post_file, dict):
                raise RuntimeError(f"malformed post-file state for {path}")

            if post_file is not None or entry.get("backup_path"):
                # A prior apply persisted the rollback path before invoking
                # the writer.  Validate it before treating the audio as
                # complete; it may be needed if this resume later fails.
                backup_name = clean(entry.get("backup_path"))
                if not backup_name:
                    raise RuntimeError(f"resume backup path is missing for {path}")
                backup_path = Path(backup_name).expanduser().resolve(strict=False)
                if not file_inside(backup_root, backup_path):
                    raise RuntimeError(f"resume backup escapes the apply backup directory: {backup_path}")
                if not backup_path.is_file():
                    raise RuntimeError(f"resume backup is unavailable: {backup_path}")
                backup_hash = sha256_file(backup_path)
                if backup_hash != pre_file.get("sha256"):
                    raise RuntimeError(f"resume backup hash mismatch: {backup_path}")
                recorded_backup_hash = clean(entry.get("backup_sha256"))
                if recorded_backup_hash and recorded_backup_hash != backup_hash:
                    raise RuntimeError(f"resume backup hash record mismatch: {backup_path}")

                if current_file.get("recording_id_tags") == [proposed]:
                    if current_file.get("other_tags_sha256") != pre_file.get("other_tags_sha256"):
                        raise RuntimeError(f"non-recording tags changed while resuming: {path}")
                    if post_file is not None:
                        if current_file.get("sha256") != post_file.get("sha256"):
                            raise RuntimeError(f"audio differs from recorded post-state: {path}")
                        if current_file.get("other_tags_sha256") != post_file.get(
                            "other_tags_sha256"
                        ):
                            raise RuntimeError(f"audio tags differ from recorded post-state: {path}")
                    # This covers both a manifest persisted after the writer
                    # and a crash in the small window before post_file was
                    # saved.  Beets is reconciled below in either case.
                    audio_applied = True
                elif current_file.get("recording_id_tags"):
                    raise RuntimeError(f"recording ID differs while resuming: {path}")
                else:
                    # A backup may have been persisted before a crash that
                    # happened before the writer.  The file is still a normal
                    # blank pre-apply input and can be written again.
                    if current_file.get("sha256") != pre_file.get("sha256"):
                        raise RuntimeError(f"media file changed since dry-run: {path}")
                    if current_file.get("other_tags_sha256") != pre_file.get(
                        "other_tags_sha256"
                    ):
                        raise RuntimeError(f"non-recording tags changed since dry-run: {path}")
            else:
                if current_file.get("sha256") != pre_file.get("sha256"):
                    raise RuntimeError(f"media file changed since dry-run: {path}")
                if current_file.get("other_tags_sha256") != pre_file.get("other_tags_sha256"):
                    raise RuntimeError(f"non-recording tags changed since dry-run: {path}")
                if current_file.get("recording_id_tags"):
                    raise RuntimeError(f"recording ID tag is no longer blank: {path}")

            beets_rows = beets_rows_for_path(beets_connection, clean(entry.get("path")))
            for beets_row in beets_rows:
                current_beets_id = clean(beets_row.get("mb_trackid"))
                if current_beets_id and current_beets_id.lower() != proposed:
                    raise RuntimeError(
                        f"beets row {beets_row.get('id')} conflicts with proposed mb_trackid "
                        f"{proposed}: {current_beets_id}"
                    )
            paths.append((entry, path, audio_applied))
    finally:
        beets_connection.close()

    resuming = isinstance(existing_apply, dict) and bool(existing_apply.get("audio_backup_dir"))
    if resuming:
        recorded_backup_dir = Path(clean(existing_apply.get("audio_backup_dir"))).expanduser().resolve(
            strict=False
        )
        if recorded_backup_dir != backup_root:
            raise RuntimeError(
                f"resume backup directory is {recorded_backup_dir}, not {backup_root}"
            )
        if not audio_backup_dir.is_dir() or not any(audio_backup_dir.iterdir()):
            raise RuntimeError(f"resume backup directory is missing or empty: {audio_backup_dir}")
        beets_backup = Path(clean(existing_apply.get("beets_backup"))).expanduser().resolve(
            strict=False
        )
        if not beets_backup.is_file():
            raise RuntimeError(f"resume Beets backup is unavailable: {beets_backup}")
        apply_state = dict(existing_apply)
        apply_state["resumed_at"] = datetime.now(timezone.utc).isoformat()
    else:
        if audio_backup_dir.exists() and any(audio_backup_dir.iterdir()):
            raise RuntimeError(f"audio backup directory already exists and is not empty: {audio_backup_dir}")
        audio_backup_dir.mkdir(parents=True, exist_ok=True)
        beets_backup = audio_backup_dir / "navidrome-beets.db"
        sqlite_backup(beets_db, beets_backup)
        apply_state = {
            "started_at": datetime.now(timezone.utc).isoformat(),
            "audio_backup_dir": str(audio_backup_dir.resolve()),
            "beets_backup": str(beets_backup.resolve()),
            "completed": 0,
            "beets_rows_updated": 0,
        }
    manifest["apply"] = apply_state
    atomic_write_json(manifest_path, manifest)

    beets_connection = sqlite3.connect(str(beets_db))
    beets_connection.row_factory = sqlite3.Row
    modified: list[tuple[Path, Path]] = []
    try:
        beets_connection.execute("BEGIN IMMEDIATE")
        for index, (entry, path, audio_applied) in enumerate(paths, start=1):
            backup_path = (
                Path(clean(entry["backup_path"])).expanduser().resolve(strict=False)
                if clean(entry.get("backup_path"))
                else audio_backup_dir / "files" / f"{index:05d}-{path.name}"
            )
            proposed = clean(entry["proposed_mbz_recording_id"]).lower()
            if not audio_applied:
                if backup_path.exists():
                    if sha256_file(backup_path) != entry["pre_file"]["sha256"]:
                        raise RuntimeError(f"existing audio backup hash mismatch: {backup_path}")
                else:
                    copy_backup(path, backup_path)
            if sha256_file(backup_path) != entry["pre_file"]["sha256"]:
                raise RuntimeError(f"audio backup hash mismatch: {path}")
            if not audio_applied:
                modified.append((path, backup_path))
                entry["backup_path"] = str(backup_path)
                entry["backup_sha256"] = sha256_file(backup_path)
                # Persist the rollback material before the first writer call.
                # If a process stops during tag serialization, the manifest
                # still identifies the exact original bytes needed for recovery.
                atomic_write_json(manifest_path, manifest)
                write_recording_id_tag(path, proposed)
            post_file = audio_file_state(path)
            if post_file.get("recording_id_tags") != [proposed]:
                raise RuntimeError(f"recording ID postcondition failed: {path}")
            if post_file.get("other_tags_sha256") != entry["pre_file"].get("other_tags_sha256"):
                raise RuntimeError(f"non-recording tags changed while writing: {path}")
            beets_rows = beets_rows_for_path(beets_connection, clean(entry.get("path")))
            for beets_row in beets_rows:
                beets_connection.execute(
                    """
                    UPDATE items SET mb_trackid = ?
                    WHERE id = ? AND length(trim(COALESCE(mb_trackid, ''))) = 0
                    """,
                    (proposed, beets_row["id"]),
                )
            verified_beets_rows = beets_rows_for_path(
                beets_connection, clean(entry.get("path"))
            )
            for beets_row in verified_beets_rows:
                if clean(beets_row.get("mb_trackid")).lower() != proposed:
                    raise RuntimeError(f"beets postcondition failed for {entry.get('path')}")
            rows_matching = sum(
                1
                for beets_row in verified_beets_rows
                if clean(beets_row.get("mb_trackid")).lower() == proposed
            )

            entry["post_file"] = post_file
            # Readback keeps a resumed manifest correct when an earlier
            # transaction rolled back or already committed before interruption.
            entry["beets_rows_updated"] = rows_matching
            manifest["apply"]["completed"] = index
            manifest["apply"]["beets_rows_updated"] = sum(
                int(item.get("beets_rows_updated") or 0) for item in entries
            )
            atomic_write_json(manifest_path, manifest)
            print(f"prepared {index}/{len(paths)}: {path}", file=sys.stderr)

        beets_connection.commit()
    except Exception as exc:
        with contextlib.suppress(sqlite3.Error):
            beets_connection.rollback()
        for path, backup_path in reversed(modified):
            with contextlib.suppress(Exception):
                copy_backup(backup_path, path)
        manifest["apply"]["error"] = str(exc)
        atomic_write_json(manifest_path, manifest)
        raise
    finally:
        beets_connection.close()

    manifest["mode"] = "applied"
    manifest["apply"]["finished_at"] = datetime.now(timezone.utc).isoformat()
    atomic_write_json(manifest_path, manifest)
    print(
        f"Applied {len(paths)} recording ID tags and updated "
        f"{manifest['apply']['beets_rows_updated']} beets rows. Run a Navidrome scan after review."
    )


def canonicalize_applied_manifest(
    manifest_path: Path,
    navidrome_db: Path,
    music_root: Path,
    audio_backup_dir: Path,
    confirm: bool,
) -> None:
    """Repair the first pass's non-canonical ID3 representation atomically.

    The original apply wrote a descriptive TXXX frame.  It is retained in the
    manifest as historical evidence, but Navidrome maps the MusicBrainz
    recording ID for ID3 through the UFID owner ``http://musicbrainz.org``.
    This operation is deliberately separate from the initial apply so its
    preconditions can require the exact IDs and hashes produced by that pass.
    """

    if not confirm:
        raise RuntimeError("canonicalization requires --yes")
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    if not isinstance(manifest, dict) or manifest.get("mode") != "applied":
        raise RuntimeError("canonicalization requires a completed applied manifest")
    entries = external_manifest_entries(manifest)
    previous_apply = manifest.get("apply") or {}
    if int(previous_apply.get("completed") or 0) != len(entries):
        raise RuntimeError("applied manifest is incomplete; refusing canonicalization")

    root = music_root.expanduser().resolve(strict=False)
    paths: list[tuple[dict[str, Any], Path, str]] = []
    for entry in entries:
        media_id = clean(entry.get("media_file_id"))
        current = current_media_row(navidrome_db, media_id)
        if current is None:
            raise RuntimeError(f"media-file ID no longer exists: {media_id}")
        for field in ("path", "title", "artist", "album"):
            if clean(current.get(field)) != clean(entry.get(field)):
                raise RuntimeError(f"media-file {media_id} changed field {field}")
        if int(current.get("missing") or 0) != 0:
            raise RuntimeError(f"media-file {media_id} is now marked missing")
        proposed = clean(entry.get("proposed_mbz_recording_id")).lower()
        path = resolve_media_path(root, clean(entry.get("path")))
        current_file = audio_file_state(path)
        if current_file.get("recording_id_tags") != [proposed]:
            raise RuntimeError(
                f"current recording ID differs from the first apply for {path}: "
                f"{current_file.get('recording_id_tags')!r}"
            )
        # The first-pass manifest used an older frame serializer.  Its full
        # audio backup is the authoritative pre-canonicalization snapshot;
        # compare native frames against that backup rather than reinterpreting
        # the legacy hash.
        backup_name = clean(entry.get("backup_path"))
        if not backup_name:
            raise RuntimeError(f"first-pass audio backup is missing for {path}")
        first_backup = Path(backup_name)
        if not first_backup.is_file():
            raise RuntimeError(f"first-pass audio backup is unavailable: {first_backup}")
        first_backup_state = audio_file_state(first_backup)
        if current_file.get("other_tags_sha256") != first_backup_state.get(
            "other_tags_sha256"
        ):
            raise RuntimeError(f"non-recording tags changed since first apply: {path}")
        previous_post = entry.get("post_file") or {}
        if previous_post.get("sha256") and current_file.get("sha256") != previous_post.get(
            "sha256"
        ):
            raise RuntimeError(f"file hash changed since first apply: {path}")
        paths.append((entry, path, proposed))

    if audio_backup_dir.exists() and any(audio_backup_dir.iterdir()):
        raise RuntimeError(f"canonicalization backup directory is not empty: {audio_backup_dir}")
    audio_backup_dir.mkdir(parents=True, exist_ok=True)
    manifest["canonicalization"] = {
        "started_at": datetime.now(timezone.utc).isoformat(),
        "audio_backup_dir": str(audio_backup_dir.resolve()),
        "completed": 0,
    }
    atomic_write_json(manifest_path, manifest)

    modified: list[tuple[Path, Path]] = []
    try:
        for index, (entry, path, proposed) in enumerate(paths, start=1):
            backup_path = audio_backup_dir / "files" / f"{index:05d}-{path.name}"
            copy_backup(path, backup_path)
            if sha256_file(backup_path) != (entry.get("post_file") or {}).get("sha256"):
                raise RuntimeError(f"canonicalization backup hash mismatch: {path}")
            modified.append((path, backup_path))

            entry["canonical_backup_path"] = str(backup_path)
            entry["canonical_backup_sha256"] = sha256_file(backup_path)
            manifest["canonicalization"]["backups_prepared"] = index
            # Persist rollback material before replacing the first file.  The
            # original apply manifest remains the source of the pre-repair
            # bytes; this second backup is the exact TXXX state being replaced.
            atomic_write_json(manifest_path, manifest)
            canonical_pre_other_tags = non_recording_tag_snapshot(path)
            write_recording_id_tag(path, proposed, allow_existing=True)
            post_file = audio_file_state(path)
            if post_file.get("recording_id_tags") != [proposed]:
                raise RuntimeError(f"canonical recording ID postcondition failed: {path}")
            if post_file.get("other_tags_sha256") != canonical_pre_other_tags:
                raise RuntimeError(f"non-recording tags changed while canonicalizing: {path}")

            entry["canonical_pre_other_tags_sha256"] = canonical_pre_other_tags
            entry["canonical_post_file"] = post_file
            manifest["canonicalization"]["completed"] = index
            atomic_write_json(manifest_path, manifest)
            print(f"canonicalized {index}/{len(paths)}: {path}", file=sys.stderr)
    except Exception as exc:
        for path, backup_path in reversed(modified):
            with contextlib.suppress(Exception):
                copy_backup(backup_path, path)
        manifest["canonicalization"]["error"] = str(exc)
        atomic_write_json(manifest_path, manifest)
        raise

    manifest["canonicalization"]["finished_at"] = datetime.now(timezone.utc).isoformat()
    manifest["canonicalization"]["mode"] = "completed"
    atomic_write_json(manifest_path, manifest)
    print(f"Canonicalized {len(paths)} recording ID tags using ID3 UFID owner.")


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--navidrome-db", type=Path, required=True)
    parser.add_argument("--beets-db", type=Path)
    parser.add_argument(
        "--music-root",
        type=Path,
        help="Library root used to capture audio hashes and to apply file tags.",
    )
    parser.add_argument(
        "--manifest",
        type=Path,
        help="Write JSON manifest here; without it, JSON is printed to stdout.",
    )
    parser.add_argument(
        "--sql",
        type=Path,
        help="Write a review-only SQL proposal for high-confidence local candidates.",
    )
    parser.add_argument(
        "--musicbrainz",
        action="store_true",
        help="Query MusicBrainz for review-only exact title/artist suggestions.",
    )
    parser.add_argument(
        "--user-agent",
        default=DEFAULT_USER_AGENT,
        help="User-Agent for optional MusicBrainz requests.",
    )
    parser.add_argument(
        "--checkpoint",
        type=Path,
        help="Resume/write a JSON checkpoint during optional MusicBrainz lookups.",
    )
    parser.add_argument(
        "--apply",
        action="store_true",
        help="Apply reviewed external suggestions from --manifest after all guards pass.",
    )
    parser.add_argument(
        "--canonicalize-applied",
        action="store_true",
        help="Replace the earlier non-canonical ID3 TXXX frame with the Navidrome UFID form.",
    )
    parser.add_argument(
        "--audio-backup-dir",
        type=Path,
        help="New directory for full per-file audio backups during --apply.",
    )
    parser.add_argument(
        "--yes",
        action="store_true",
        help="Required acknowledgement for the explicit file/tag apply operation.",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv or sys.argv[1:])
    if not args.navidrome_db.exists():
        raise SystemExit(f"Navidrome database does not exist: {args.navidrome_db}")
    if args.beets_db and not args.beets_db.exists():
        raise SystemExit(f"beets database does not exist: {args.beets_db}")

    if args.apply or args.canonicalize_applied:
        if not args.manifest or not args.beets_db or not args.music_root or not args.audio_backup_dir:
            raise SystemExit(
                "--apply/--canonicalize-applied requires --manifest, --beets-db, "
                "--music-root, and --audio-backup-dir"
            )
        if args.apply and args.canonicalize_applied:
            raise SystemExit("choose only one of --apply or --canonicalize-applied")
        if args.canonicalize_applied:
            canonicalize_applied_manifest(
                args.manifest,
                args.navidrome_db,
                args.music_root,
                args.audio_backup_dir,
                args.yes,
            )
        else:
            apply_recording_manifest(
                args.manifest,
                args.navidrome_db,
                args.beets_db,
                args.music_root,
                args.audio_backup_dir,
                args.yes,
            )
        return 0

    manifest, no_local_evidence = build_local_manifest(args.navidrome_db, args.beets_db)
    if args.music_root:
        attach_file_states(
            manifest,
            list(manifest.get("candidates", [])) + no_local_evidence,
            args.music_root,
        )
    if args.musicbrainz:
        suggestions, errors = musicbrainz_suggestions(
            no_local_evidence,
            user_agent=args.user_agent,
            checkpoint=args.checkpoint,
        )
        manifest["external_suggestions"] = suggestions
        manifest["external_lookup"] = {
            "provider": "musicbrainz",
            "mode": "review-only",
            "checkpoint": str(args.checkpoint) if args.checkpoint else None,
            "errors": errors,
        }
    if args.sql:
        write_sql(args.sql, manifest["candidates"])
        manifest["sql_proposal"] = str(args.sql)

    encoded = json.dumps(manifest, ensure_ascii=False, indent=2, sort_keys=False) + "\n"
    if args.manifest:
        args.manifest.parent.mkdir(parents=True, exist_ok=True)
        args.manifest.write_text(encoded, encoding="utf-8")
    else:
        sys.stdout.write(encoded)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, sqlite3.Error, RuntimeError) as exc:
        print(f"repair manifest failed: {exc}", file=sys.stderr)
        raise SystemExit(1)
