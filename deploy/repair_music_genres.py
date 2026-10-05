#!/usr/bin/env python3
"""Plan and safely apply MusicBrainz-backed genre repairs.

This utility deliberately separates discovery from mutation:

* ``--dry-run`` reads the Navidrome database and audio tags, then writes a
  reviewable manifest. MusicBrainz responses are cached and requests are
  serialized through a shared rate-state file so another repair job can use
  the same cache without exceeding the public API's request policy.
* ``--apply`` accepts only a dry-run manifest. It verifies every media-file
  identity, path, tag snapshot, and file hash again, copies each file to a
  manifest-controlled backup, and changes only the file's genre tag. It does
  not write Navidrome's database; run a scanner after reviewing the result.
* ``--rollback`` restores files from the per-file backups after checking that
  the current file is still the one produced by the apply operation.
* ``--reviewed-genres`` supplies source-backed, per-file proposals to a
  ``--dry-run``. The proposals are checked against the current media-file
  identity and SHA-256 before MusicBrainz discovery starts.

The genre evidence is intentionally conservative. A library artist group is
eligible only when a representative recording has exactly one MusicBrainz
artist credit whose normalized name equals the library artist. Multi-artist,
ambiguous, and unmatched recordings are skipped. The artist's positive
MusicBrainz genre votes are used as an artist-level fallback, with at most two
genres written to each category-only or blank track. Existing musical genres
are never replaced.

The script requires ``requests`` and ``mutagen`` for a real repair run. The
``--self-test-ytdlp`` mode additionally requires the image's ``yt_dlp`` and
``ffmpeg`` installations but performs all work in a temporary directory.
"""

from __future__ import annotations

import argparse
import contextlib
import copy
import enum
import hashlib
import json
import os
import re
import shutil
import sqlite3
import stat
import subprocess
import sys
import tempfile
import time
import unicodedata
from collections import Counter, defaultdict
from pathlib import Path
from typing import Any, Iterable
from urllib.parse import quote, urlparse


MANIFEST_VERSION = 1
DEFAULT_BASE_URL = "https://musicbrainz.org/ws/2"
DEFAULT_USER_AGENT = "NavidromeGenreRepair/1.0 (https://github.com/evchuk4018/navidrome)"
DEFAULT_RATE_SECONDS = 1.1

# These are YouTube/video categories observed in the Navidrome library. They
# are deliberately kept separate from the broader list of generic MusicBrainz
# tags below: a future source value matching a real genre must not be cleared
# merely because it was not seen in this one library.
VIDEO_CATEGORY_GENRES = frozenset(
    {
        "music",
        "people & blogs",
        "people and blogs",
        "entertainment",
        "gaming",
        "travel & events",
        "travel and events",
        "comedy",
        "education",
        "film & animation",
        "film and animation",
        "howto & style",
        "how to and style",
        "howto and style",
        "pets & animals",
        "pets and animals",
    }
)

# Artist tags are folksonomy data. Keep obvious non-genre labels out even
# when a user has voted for them; positive counts alone should not turn a
# podcast/category label into a song genre.
GENERIC_NON_GENRES = frozenset(
    {
        "audio",
        "audiobook",
        "blog",
        "blogs",
        "comedy",
        "educational",
        "entertainment",
        "film",
        "interview",
        "live",
        "music",
        "music video",
        "news",
        "non music",
        "non-music",
        "podcast",
        "spoken word",
        "talk",
        "video",
    }
)

# A reviewed proposal may carry the source file's edit/format label in its
# title, but those labels are not musical genres. Keep this list separate
# from the existing MusicBrainz filters so the reviewed-input contract is
# explicit and cannot accidentally broaden automatic artist fallback.
FORMAT_LABELS = frozenset(
    {
        "edit",
        "edit audio",
        "guitar remix",
        "remix",
        "slowed",
        "slowed reverb",
        "slowed + reverb",
        "slowed and reverb",
        "sped up",
        "speed up",
        "super slowed",
        "ultra slowed",
    }
)


class RepairError(RuntimeError):
    """A user-actionable repair refusal or validation failure."""


def eprint(message: str) -> None:
    print(message, file=sys.stderr)


def utc_now() -> str:
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())


def atomic_write_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temporary = tempfile.mkstemp(prefix=f".{path.name}.", suffix=".tmp", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            json.dump(value, handle, indent=2, sort_keys=True, ensure_ascii=False)
            handle.write("\n")
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, path)
    finally:
        with contextlib.suppress(FileNotFoundError):
            os.unlink(temporary)


def load_json(path: Path) -> Any:
    try:
        with path.open("r", encoding="utf-8") as handle:
            return json.load(handle)
    except (OSError, json.JSONDecodeError) as exc:
        raise RepairError(f"read JSON {path}: {exc}") from exc


def normalize_text(value: str) -> str:
    value = unicodedata.normalize("NFKC", value or "").casefold().strip()
    output: list[str] = []
    for character in value:
        category = unicodedata.category(character)
        if category.startswith(("P", "S")):
            output.append(" ")
        else:
            output.append(character)
    return " ".join("".join(output).split())


def normalize_artist(value: str) -> str:
    value = unicodedata.normalize("NFKC", value or "").strip()
    suffix = " - topic"
    if len(value) > len(suffix) and value.casefold().endswith(suffix):
        value = value[: -len(suffix)]
    return normalize_text(value)


def normalize_genre(value: str) -> str:
    return normalize_text(value)


NORMALIZED_VIDEO_CATEGORY_GENRES = frozenset(normalize_genre(item) for item in VIDEO_CATEGORY_GENRES)
NORMALIZED_GENERIC_NON_GENRES = frozenset(normalize_genre(item) for item in GENERIC_NON_GENRES)
NORMALIZED_FORMAT_LABELS = frozenset(normalize_genre(item) for item in FORMAT_LABELS)


def clean_values(value: Any) -> list[str]:
    if value is None:
        return []
    if isinstance(value, str):
        value = [value]
    if isinstance(value, dict):
        # Navidrome stores tag facets as objects such as
        # {"id": "...", "value": "Music"}; older rows may contain plain
        # strings. Preserve only the display value and ignore facet IDs.
        if "value" in value:
            return clean_values(value["value"])
        if "name" in value:
            return clean_values(value["name"])
        return []
    if not isinstance(value, (list, tuple)):
        value = [value]
    result: list[str] = []
    for item in value:
        if isinstance(item, (dict, list, tuple)):
            result.extend(clean_values(item))
        elif str(item).strip():
            result.append(str(item).strip())
    return result


def unique_values(values: Iterable[str]) -> list[str]:
    result: list[str] = []
    seen: set[str] = set()
    for value in values:
        value = str(value).strip()
        key = normalize_genre(value)
        if not value or not key or key in seen:
            continue
        seen.add(key)
        result.append(value)
    return result


def is_video_category(value: str) -> bool:
    return normalize_genre(value) in NORMALIZED_VIDEO_CATEGORY_GENRES


def is_supported_genre(value: str, minimum_votes: int) -> bool:
    normalized = normalize_genre(value)
    if not normalized or normalized in NORMALIZED_GENERIC_NON_GENRES:
        return False
    # MusicBrainz tags can contain URLs or free-form descriptions. Those are
    # not safe to place in a library's genre facet.
    if "http://" in normalized or "https://" in normalized or len(normalized) > 80:
        return False
    return minimum_votes > 0


def is_reviewed_genre(value: str) -> bool:
    """Return whether a human-reviewed value is a musical genre label.

    Reviewed values are intentionally stricter than the existing
    MusicBrainz folksonomy filter. They must be plain, bounded strings and
    may not be a video category, generic metadata label, or edit/format
    marker such as ``slowed`` or ``guitar remix``.
    """
    if not isinstance(value, str):
        return False
    value = value.strip()
    normalized = normalize_genre(value)
    if not normalized or len(normalized) > 80:
        return False
    if normalized in NORMALIZED_VIDEO_CATEGORY_GENRES:
        return False
    if normalized in NORMALIZED_GENERIC_NON_GENRES:
        return False
    if normalized in NORMALIZED_FORMAT_LABELS:
        return False
    if any(label in normalized for label in NORMALIZED_FORMAT_LABELS):
        return False
    parsed = urlparse(value)
    if parsed.scheme or parsed.netloc or "http://" in normalized or "https://" in normalized:
        return False
    return True


def genre_values_from_json(raw: Any) -> list[str]:
    if raw in (None, "", b""):
        return []
    try:
        parsed = json.loads(raw) if isinstance(raw, (str, bytes)) else raw
    except (TypeError, json.JSONDecodeError):
        return []
    if not isinstance(parsed, dict):
        return []
    return unique_values(clean_values(parsed.get("genre")))


def file_inside(root: Path, path: Path) -> bool:
    try:
        path.relative_to(root)
        return True
    except ValueError:
        return False


def resolve_media_path(library_path: str, media_path: str, override_root: Path | None) -> tuple[Path | None, str]:
    if not media_path:
        return None, "empty media path"
    raw = Path(media_path)
    root = (override_root or Path(library_path)).expanduser()
    try:
        root = root.resolve(strict=False)
    except OSError as exc:
        return None, f"resolve library path: {exc}"
    if raw.is_absolute():
        candidate = raw
    else:
        candidate = root / raw
    try:
        resolved = candidate.resolve(strict=False)
    except OSError as exc:
        return None, f"resolve media path: {exc}"
    if not file_inside(root, resolved):
        return None, f"path escapes library root {root}"
    return resolved, ""


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    try:
        with path.open("rb") as handle:
            for chunk in iter(lambda: handle.read(1024 * 1024), b""):
                digest.update(chunk)
    except OSError as exc:
        raise RepairError(f"hash {path}: {exc}") from exc
    return digest.hexdigest()


def import_mutagen() -> Any:
    try:
        from mutagen import File as mutagen_file
    except ImportError as exc:
        raise RepairError("mutagen is required; install it in the repair environment") from exc
    return mutagen_file


def mutagen_tags(path: Path) -> Any:
    mutagen_file = import_mutagen()
    try:
        audio = mutagen_file(path, easy=True)
    except Exception as exc:  # mutagen exposes format-specific exceptions
        raise RepairError(f"read audio tags {path}: {exc}") from exc
    if audio is None:
        raise RepairError(f"unsupported audio format or missing tags: {path}")
    return audio


def raw_mutagen_tags(path: Path) -> Any:
    """Read the native Mutagen tag object, including frames Easy tags hide."""
    mutagen_file = import_mutagen()
    try:
        audio = mutagen_file(path)
    except Exception as exc:  # mutagen exposes format-specific exceptions
        raise RepairError(f"read native audio tags {path}: {exc}") from exc
    if audio is None:
        raise RepairError(f"unsupported audio format or missing tags: {path}")
    return audio


def read_file_genres(path: Path) -> list[str]:
    audio = mutagen_tags(path)
    tags = audio.tags
    if tags is None:
        return []
    try:
        values = tags.get("genre", [])
    except Exception as exc:
        raise RepairError(f"read genre tag {path}: {exc}") from exc
    return unique_values(clean_values(values))


def tag_value_for_snapshot(value: Any) -> Any:
    # Mutagen uses small enum/int-like helper objects (for example ID3's
    # Encoding) inside frame payloads. Handle scalar values before asking for
    # __dict__; some of those helpers expose a descriptor that raises
    # TypeError when introspected.
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
    # Native Mutagen frames keep their payload in __dict__. Capturing the
    # payload instead of repr(frame) makes the comparison independent of
    # object addresses while still covering artwork, comments, and private
    # frames that Easy tags do not expose.
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


def tag_snapshot(path: Path) -> str:
    audio = raw_mutagen_tags(path)
    tags = audio.tags
    if tags is None:
        values: dict[str, Any] = {}
    else:
        values = {}
        for key in sorted(tags.keys(), key=str):
            key_text = str(key)
            if key_text.casefold() in {"genre", "tcon", "©gen"}:
                continue
            if hasattr(tags, "getall"):
                values[key_text] = [tag_value_for_snapshot(item) for item in tags.getall(key_text)]
            else:
                values[key_text] = tag_value_for_snapshot(tags[key])
    encoded = json.dumps(values, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()
    return hashlib.sha256(encoded).hexdigest()


def file_state(path: Path) -> dict[str, Any]:
    try:
        stat_result = path.stat()
    except OSError as exc:
        raise RepairError(f"stat {path}: {exc}") from exc
    if not path.is_file() or path.is_symlink():
        raise RepairError(f"media path is not a regular file: {path}")
    # A file with no writable tag container is not a safe candidate. The
    # repair intentionally skips it instead of asking Mutagen to invent a
    # new tag format while it is touching the library.
    if mutagen_tags(path).tags is None:
        raise RepairError(f"audio has no writable tags: {path}")
    frame_hash = tag_snapshot(path)
    return {
        "sha256": sha256_file(path),
        "size": stat_result.st_size,
        "mtime_ns": stat_result.st_mtime_ns,
        "mode": stat.S_IMODE(stat_result.st_mode),
        "genres": read_file_genres(path),
        # This is a native Mutagen frame hash with only TCON/genre removed;
        # it covers artwork, comments, and frames Easy tags omit.
        "native_tags_sha256": frame_hash,
    }


def open_database(path: Path) -> sqlite3.Connection:
    try:
        connection = sqlite3.connect(f"file:{path.resolve()}?mode=ro", uri=True)
        connection.row_factory = sqlite3.Row
        return connection
    except sqlite3.Error as exc:
        raise RepairError(f"open SQLite database read-only {path}: {exc}") from exc


def load_tracks(db_path: Path, music_root: Path | None) -> list[dict[str, Any]]:
    with open_database(db_path) as connection:
        try:
            rows = connection.execute(
                """
                SELECT mf.id, mf.library_id, mf.path, mf.title, mf.artist,
                       mf.mbz_recording_id, mf.missing, mf.tags,
                       COALESCE(lib.path, '') AS library_path
                FROM media_file AS mf
                LEFT JOIN library AS lib ON lib.id = mf.library_id
                ORDER BY mf.id
                """
            ).fetchall()
        except sqlite3.Error as exc:
            raise RepairError(f"read media_file rows: {exc}") from exc

    tracks: list[dict[str, Any]] = []
    for row in rows:
        record = dict(row)
        record["db_genres"] = genre_values_from_json(record.get("tags"))
        path, path_error = resolve_media_path(
            str(record.get("library_path") or ""), str(record.get("path") or ""), music_root
        )
        record["absolute_path"] = str(path) if path else ""
        record["path_error"] = path_error
        tracks.append(record)
    return tracks


def _reviewed_url(value: Any, index: int) -> str:
    if not isinstance(value, str) or not value.strip():
        raise RepairError(f"reviewed genre entry {index} has a non-string source URL")
    value = value.strip()
    parsed = urlparse(value)
    if parsed.scheme not in {"http", "https"} or not parsed.netloc:
        raise RepairError(f"reviewed genre entry {index} has an unsafe source URL: {value!r}")
    return value


def load_reviewed_genres(
    path: Path,
    tracks: list[dict[str, Any]],
    music_root: Path | None,
    maximum_genres: int,
) -> dict[str, dict[str, Any]]:
    """Load and validate per-file genre proposals before any MB lookups.

    The input is deliberately validated against the current read-only
    Navidrome snapshot and the current audio-file hash. This means a review
    cannot silently drift onto a moved, replaced, or differently identified
    file between inventory and dry-run. The returned values contain only the
    reviewed proposal and provenance needed by the manifest; the caller still
    decides whether the file's current genre class permits a replacement.
    """
    raw = load_json(path)
    if not isinstance(raw, dict) or raw.get("version") != 1:
        raise RepairError(f"reviewed genres must be a version 1 object: {path}")
    raw_entries = raw.get("entries")
    if not isinstance(raw_entries, list):
        raise RepairError(f"reviewed genres entries must be a list: {path}")

    tracks_by_id: dict[str, dict[str, Any]] = {}
    for track in tracks:
        media_id = str(track.get("id") or "").strip()
        if media_id:
            tracks_by_id[media_id] = track

    reviewed: dict[str, dict[str, Any]] = {}
    for index, item in enumerate(raw_entries, start=1):
        if not isinstance(item, dict):
            raise RepairError(f"reviewed genre entry {index} must be an object")
        raw_media_id = item.get("media_file_id")
        if not isinstance(raw_media_id, str) or not raw_media_id.strip():
            raise RepairError(
                f"reviewed genre entry {index} media_file_id must be a non-empty string"
            )
        media_id = raw_media_id.strip()
        if media_id in reviewed:
            raise RepairError(f"duplicate reviewed genre media_file_id: {media_id}")
        track = tracks_by_id.get(media_id)
        if track is None:
            raise RepairError(f"unknown reviewed genre media_file_id: {media_id}")

        genres = item.get("genres")
        if not isinstance(genres, list) or not 1 <= len(genres) <= 2:
            raise RepairError(
                f"reviewed genre entry {media_id} must contain one or two genre strings"
            )
        if len(genres) > maximum_genres:
            raise RepairError(
                f"reviewed genre entry {media_id} exceeds --maximum-genres={maximum_genres}"
            )
        cleaned_genres: list[str] = []
        for genre in genres:
            if not isinstance(genre, str) or not genre.strip() or not is_reviewed_genre(genre):
                raise RepairError(
                    f"reviewed genre entry {media_id} contains a non-musical genre: {genre!r}"
                )
            cleaned_genres.append(genre.strip())
        cleaned_genres = unique_values(cleaned_genres)
        if not 1 <= len(cleaned_genres) <= 2:
            raise RepairError(f"reviewed genre entry {media_id} has duplicate or empty genres")

        sources = item.get("sources")
        if not isinstance(sources, list) or not sources:
            raise RepairError(f"reviewed genre entry {media_id} must include source URLs")
        cleaned_sources: list[str] = []
        for source in sources:
            cleaned_sources.append(_reviewed_url(source, index))
        evidence_note = item.get("evidence_note")
        if not isinstance(evidence_note, str) or not evidence_note.strip():
            raise RepairError(f"reviewed genre entry {media_id} must include evidence_note")

        expected = item.get("expected")
        if not isinstance(expected, dict):
            raise RepairError(f"reviewed genre entry {media_id} must include expected identity")
        expected_path = expected.get("path")
        expected_title = expected.get("title")
        expected_artist = expected.get("artist")
        expected_hash = expected.get("file_sha256")
        if not isinstance(expected_path, str) or not expected_path.strip():
            raise RepairError(f"reviewed genre entry {media_id} expected.path is required")
        if not isinstance(expected_title, str) or not isinstance(expected_artist, str):
            raise RepairError(f"reviewed genre entry {media_id} expected title/artist are required")
        if not isinstance(expected_hash, str) or not re.fullmatch(r"[0-9a-fA-F]{64}", expected_hash):
            raise RepairError(f"reviewed genre entry {media_id} expected.file_sha256 is invalid")

        current_relative_path = str(track.get("path") or "")
        current_absolute_path = str(track.get("absolute_path") or "")
        accepted_paths = {current_relative_path}
        if current_absolute_path:
            accepted_paths.add(str(Path(current_absolute_path).resolve(strict=False)))
        expected_path_text = expected_path.strip()
        if expected_path_text not in accepted_paths:
            raise RepairError(f"reviewed genre entry {media_id} expected path does not match current media_file")
        if expected_title != str(track.get("title") or ""):
            raise RepairError(f"reviewed genre entry {media_id} expected title does not match current media_file")
        if expected_artist != str(track.get("artist") or ""):
            raise RepairError(f"reviewed genre entry {media_id} expected artist does not match current media_file")
        if track.get("missing") or track.get("path_error") or not current_absolute_path:
            raise RepairError(f"reviewed genre entry {media_id} cannot fingerprint an unavailable media file")
        current_path = Path(current_absolute_path)
        if not current_path.exists() or current_path.is_symlink() or not current_path.is_file():
            raise RepairError(f"reviewed genre entry {media_id} media file is unavailable")
        current_state = file_state(current_path)
        if current_state["sha256"].casefold() != expected_hash.casefold():
            raise RepairError(f"reviewed genre entry {media_id} expected file hash does not match current media_file")

        reviewed[media_id] = {
            "genres": cleaned_genres,
            "sources": cleaned_sources,
            "evidence_note": evidence_note.strip(),
            "provenance": "reviewed per-file proposal",
            "expected": {
                "path": expected_path_text,
                "title": expected_title,
                "artist": expected_artist,
                "file_sha256": expected_hash.lower(),
            },
        }
    return reviewed


class SharedRateLimiter:
    """Serialize requests and persist the next permitted request time.

    The lock/state pair is intentionally a filesystem contract so the MBID
    repair utility and this genre utility can share one request budget.
    """

    def __init__(self, lock_path: Path, interval: float) -> None:
        self.lock_path = lock_path
        self.state_path = lock_path.with_suffix(lock_path.suffix + ".json")
        self.interval = max(interval, 0.1)

    @contextlib.contextmanager
    def request_slot(self) -> Iterable[None]:
        self.lock_path.parent.mkdir(parents=True, exist_ok=True)
        with self.lock_path.open("a+", encoding="utf-8") as lock_handle:
            flock = None
            try:
                import fcntl

                flock = fcntl
                flock.flock(lock_handle.fileno(), flock.LOCK_EX)
            except ImportError:
                # Production runs on Linux. On platforms without flock the
                # lock still serializes requests within this process.
                pass
            try:
                state: dict[str, Any] = {}
                with contextlib.suppress(OSError, json.JSONDecodeError):
                    state = json.loads(self.state_path.read_text(encoding="utf-8"))
                now = time.time()
                next_at = float(state.get("next_request_at", 0))
                if next_at > now:
                    time.sleep(next_at - now)
                try:
                    yield
                finally:
                    # Persist the interval even when the HTTP call raises, so
                    # a retry or another repair process cannot immediately
                    # follow a failed request.
                    atomic_write_json(self.state_path, {"next_request_at": time.time() + self.interval})
            finally:
                if flock is not None:
                    flock.flock(lock_handle.fileno(), flock.LOCK_UN)


class MusicBrainzClient:
    def __init__(
        self,
        cache_dir: Path,
        lock_path: Path,
        base_url: str,
        user_agent: str,
        rate_seconds: float,
        timeout: float,
    ) -> None:
        try:
            import requests
        except ImportError as exc:
            raise RepairError("requests is required for MusicBrainz lookups") from exc
        self.requests = requests
        self.cache_dir = cache_dir
        self.cache_dir.mkdir(parents=True, exist_ok=True)
        self.limiter = SharedRateLimiter(lock_path, rate_seconds)
        self.base_url = base_url.rstrip("/")
        self.user_agent = user_agent
        self.timeout = timeout
        self.request_count = 0

    def _cache_path(self, kind: str, identifier: str) -> Path:
        safe = re.sub(r"[^A-Za-z0-9._-]", "_", identifier)
        return self.cache_dir / f"{kind}-{safe}.json"

    def fetch(self, kind: str, identifier: str, params: dict[str, str]) -> tuple[dict[str, Any], bool]:
        cache_path = self._cache_path(kind, identifier)
        if cache_path.exists():
            value = load_json(cache_path)
            if isinstance(value, dict) and isinstance(value.get("response"), dict):
                return value["response"], True
        endpoint = f"{self.base_url}/{kind}/{quote(identifier, safe='')}"
        request_params = {"fmt": "json", **params}
        last_error: Exception | None = None
        for attempt in range(3):
            try:
                with self.limiter.request_slot():
                    response = self.requests.get(
                        endpoint,
                        params=request_params,
                        headers={"User-Agent": self.user_agent, "Accept": "application/json"},
                        timeout=self.timeout,
                    )
                    self.request_count += 1
                    if response.status_code in (429, 500, 502, 503, 504):
                        retry_after = response.headers.get("Retry-After", "")
                        delay = float(retry_after) if retry_after.isdigit() else min(30.0, 2**attempt)
                        last_error = RepairError(f"MusicBrainz HTTP {response.status_code}")
                    else:
                        response.raise_for_status()
                        value = response.json()
                        if not isinstance(value, dict):
                            raise RepairError(f"MusicBrainz returned non-object JSON for {endpoint}")
                        atomic_write_json(
                            cache_path,
                            {
                                "cached_at": utc_now(),
                                "endpoint": endpoint,
                                "params": request_params,
                                "response": value,
                            },
                        )
                        return value, False
                if last_error is not None:
                    time.sleep(delay)
            except Exception as exc:  # requests exposes several network exception types
                last_error = exc
                if attempt < 2:
                    time.sleep(min(30.0, 2**attempt))
        raise RepairError(f"MusicBrainz lookup failed for {kind}/{identifier}: {last_error}") from last_error


def recording_evidence(client: MusicBrainzClient, recording_id: str, group_artist: str) -> dict[str, Any]:
    data, cached = client.fetch("recording", recording_id, {"inc": "artist-credits"})
    credits = [item for item in data.get("artist-credit", []) if isinstance(item, dict)]
    if len(credits) != 1:
        raise RepairError(f"recording has {len(credits)} artist credits; refusing ambiguous match")
    credit = credits[0]
    artist = credit.get("artist") if isinstance(credit.get("artist"), dict) else {}
    artist_id = str(artist.get("id") or "").strip()
    artist_name = str(credit.get("name") or artist.get("name") or "").strip()
    if not artist_id or not artist_name:
        raise RepairError("recording has no complete primary artist credit")
    if normalize_artist(artist_name) != group_artist:
        raise RepairError(f"recording artist {artist_name!r} does not exactly match library artist group")
    return {
        "recording_id": recording_id,
        "recording_url": f"https://musicbrainz.org/recording/{quote(recording_id, safe='')}",
        "artist_id": artist_id,
        "artist_name": artist_name,
        "artist_url": f"https://musicbrainz.org/artist/{quote(artist_id, safe='')}",
        "recording_cached": cached,
    }


def artist_genre_evidence(
    client: MusicBrainzClient, artist_id: str, minimum_votes: int, maximum_genres: int
) -> list[dict[str, Any]]:
    data, cached = client.fetch("artist", artist_id, {"inc": "genres"})
    candidates: list[dict[str, Any]] = []
    for item in data.get("genres", []):
        if not isinstance(item, dict):
            continue
        name = str(item.get("name") or "").strip()
        try:
            count = int(item.get("count") or 0)
        except (TypeError, ValueError):
            count = 0
        if count < minimum_votes or not is_supported_genre(name, count):
            continue
        candidates.append({"name": name, "count": count})
    candidates.sort(key=lambda item: (-item["count"], normalize_genre(item["name"])))
    selected: list[dict[str, Any]] = []
    seen: set[str] = set()
    for item in candidates:
        normalized = normalize_genre(item["name"])
        if normalized in seen:
            continue
        seen.add(normalized)
        item = dict(item)
        item["artist_cached"] = cached
        selected.append(item)
        if len(selected) >= maximum_genres:
            break
    return selected


def classify_existing_genres(values: list[str]) -> str:
    values = unique_values(values)
    if not values:
        return "blank"
    if all(is_video_category(value) for value in values):
        return "video-category-only"
    return "musical-or-unknown"


def previous_group_evidence(
    previous: dict[str, Any] | None, group: str, current_recording_ids: set[str]
) -> dict[str, Any] | None:
    if not previous or previous.get("version") != MANIFEST_VERSION:
        return None
    groups = previous.get("groups")
    if not isinstance(groups, dict):
        return None
    value = groups.get(group)
    if not isinstance(value, dict) or value.get("status") != "ok":
        return None
    representative_id = str(value.get("representative_recording_id") or "")
    evidence = value.get("evidence")
    evidence_id = str(evidence.get("recording_id") or "") if isinstance(evidence, dict) else ""
    if not current_recording_ids.intersection({representative_id, evidence_id}):
        return None
    return copy.deepcopy(evidence) if isinstance(evidence, dict) else None


def make_manifest(
    db_path: Path,
    tracks: list[dict[str, Any]],
    client: MusicBrainzClient,
    manifest_path: Path,
    music_root: Path | None,
    minimum_votes: int,
    maximum_genres: int,
    resume: bool,
    reviewed: dict[str, dict[str, Any]] | None = None,
    reviewed_path: Path | None = None,
) -> dict[str, Any]:
    """Build a dry-run manifest for the supplied current track snapshot.

    ``reviewed`` is an override map, not a row selector: callers that operate
    on a frozen inventory should filter the result of :func:`load_tracks`
    before calling this function. Every supplied reviewed ID is still checked
    against that filtered snapshot by :func:`load_reviewed_genres`.
    """
    reviewed = reviewed or {}
    previous = load_json(manifest_path) if resume and manifest_path.exists() else None
    entries: list[dict[str, Any]] = []
    eligible_groups: dict[str, list[dict[str, Any]]] = defaultdict(list)

    for index, track in enumerate(tracks, start=1):
        entry: dict[str, Any] = {
            "media_file_id": str(track.get("id") or ""),
            "library_id": track.get("library_id"),
            "library_path": str(track.get("library_path") or ""),
            "relative_path": str(track.get("path") or ""),
            "path": str(track.get("absolute_path") or ""),
            "title": str(track.get("title") or ""),
            "artist": str(track.get("artist") or ""),
            "mbz_recording_id": str(track.get("mbz_recording_id") or ""),
            "db_genres": unique_values(track["db_genres"]),
            "file_genres": [],
            "genre_class": "unknown",
            "status": "pending",
            "reason": "",
        }
        path = Path(entry["path"]) if entry["path"] else None
        if track.get("missing"):
            entry.update(status="skip-missing-db-row", reason="Navidrome marks this file missing")
        elif track.get("path_error"):
            entry.update(status="skip-path", reason=track["path_error"])
        elif path is None or not path.exists():
            entry.update(status="skip-file-missing", reason="file does not exist")
        elif path.is_symlink() or not path.is_file():
            entry.update(status="skip-file-type", reason="file is not a regular file")
        else:
            try:
                state = file_state(path)
                entry["pre_file"] = state
                entry["file_genres"] = state["genres"]
                entry["genre_class"] = classify_existing_genres(state["genres"])
                reviewed_proposal = reviewed.get(entry["media_file_id"])
                if reviewed_proposal is not None:
                    entry["reviewed"] = copy.deepcopy(reviewed_proposal)
                if entry["genre_class"] == "musical-or-unknown":
                    entry.update(status="skip-existing-genre", reason="preserve existing non-category genre")
                    if reviewed_proposal is not None:
                        entry["reason"] = "preserve existing non-category genre; reviewed proposal not applied"
                        entry["reviewed"]["applied"] = False
                elif reviewed_proposal is not None:
                    entry.update(
                        status="candidate",
                        reason="replace category-only/blank genre from reviewed per-file proposal",
                        replacement_genres=list(reviewed_proposal["genres"]),
                        source={
                            "sources": list(reviewed_proposal["sources"]),
                            "evidence_note": reviewed_proposal["evidence_note"],
                            "provenance": reviewed_proposal["provenance"],
                        },
                    )
                    entry["reviewed"]["applied"] = True
                else:
                    group = normalize_artist(entry["artist"])
                    if not group:
                        entry.update(status="skip-empty-artist", reason="artist is empty after normalization")
                    else:
                        entry["status"] = "eligible-group"
                        entry["group_artist"] = group
                        eligible_groups[group].append(entry)
            except RepairError as exc:
                entry.update(status="skip-tag-read", reason=str(exc))
        entries.append(entry)
        if index == 1 or index % 100 == 0 or index == len(tracks):
            eprint(f"scanned {index}/{len(tracks)} files; eligible groups {len(eligible_groups)}")

    groups_manifest: dict[str, Any] = {}
    ordered_groups = sorted(eligible_groups)
    for group_index, group in enumerate(ordered_groups, start=1):
        group_entries = sorted(eligible_groups[group], key=lambda item: (item["media_file_id"], item["path"]))
        anchored_entries = [entry for entry in group_entries if entry.get("mbz_recording_id")]
        anchor_candidates: list[dict[str, Any]] = []
        seen_recordings: set[str] = set()
        for entry in anchored_entries:
            recording_id = str(entry["mbz_recording_id"])
            if recording_id in seen_recordings:
                continue
            seen_recordings.add(recording_id)
            anchor_candidates.append(entry)
            if len(anchor_candidates) >= 3:
                break
        representative = anchor_candidates[0] if anchor_candidates else group_entries[0]
        current_recording_ids = {
            str(entry.get("mbz_recording_id")) for entry in anchored_entries if entry.get("mbz_recording_id")
        }
        cached_evidence = previous_group_evidence(previous, group, current_recording_ids)
        group_result: dict[str, Any] = {
            "status": "failed",
            "library_artist": representative["artist"],
            "normalized_artist": group,
            "representative_media_file_id": representative["media_file_id"],
            "representative_recording_id": representative["mbz_recording_id"],
            "reason": "",
        }
        try:
            if cached_evidence is not None:
                evidence = copy.deepcopy(cached_evidence)
                evidence["resumed"] = True
            else:
                evidence = None
                anchor_errors: list[str] = []
                for anchor in anchor_candidates:
                    try:
                        evidence = recording_evidence(client, anchor["mbz_recording_id"], group)
                        group_result["representative_media_file_id"] = anchor["media_file_id"]
                        group_result["representative_recording_id"] = anchor["mbz_recording_id"]
                        break
                    except RepairError as exc:
                        anchor_errors.append(f"{anchor['mbz_recording_id']}: {exc}")
                if evidence is None:
                    if not anchor_candidates:
                        raise RepairError("no recording MBID in this artist group")
                    raise RepairError("no exact single-artist recording anchor: " + " | ".join(anchor_errors))
                evidence["genres"] = artist_genre_evidence(
                    client, evidence["artist_id"], minimum_votes, maximum_genres
                )
            genres = unique_values(item["name"] for item in evidence.get("genres", []))
            if not genres:
                raise RepairError("MusicBrainz artist has no positive supported genre votes")
            group_result.update(
                status="ok",
                evidence=evidence,
                replacement_genres=genres,
                provenance="MusicBrainz artist-level fallback",
            )
            for entry in group_entries:
                entry["status"] = "candidate"
                entry["reason"] = "replace category-only/blank genre from verified artist evidence"
                entry["replacement_genres"] = genres
                entry["source"] = {
                    "recording_url": evidence["recording_url"],
                    "artist_url": evidence["artist_url"],
                    "recording_id": evidence["recording_id"],
                    "artist_id": evidence["artist_id"],
                    "artist_name": evidence["artist_name"],
                    "genres": evidence["genres"],
                    "provenance": "MusicBrainz artist-level fallback",
                }
        except RepairError as exc:
            group_result["reason"] = str(exc)
            for entry in group_entries:
                entry["status"] = "skip-evidence"
                entry["reason"] = str(exc)
        groups_manifest[group] = group_result
        eprint(f"MusicBrainz groups {group_index}/{len(ordered_groups)}: {group} -> {group_result['status']}")

    counts = Counter(entry["status"] for entry in entries)
    audit = {
        "media_file_rows": len(tracks),
        "missing_internal_id": sum(not str(track.get("id") or "").strip() for track in tracks),
        "missing_recording_id": sum(not str(track.get("mbz_recording_id") or "").strip() for track in tracks),
        "file_blank_or_video_category": sum(
            entry["genre_class"] in ("blank", "video-category-only") for entry in entries
        ),
        "file_preserved_musical_or_unknown": sum(
            entry["genre_class"] == "musical-or-unknown" for entry in entries
        ),
        "reviewed_entries": len(reviewed),
        "reviewed_candidates": sum(
            entry.get("reviewed", {}).get("applied") is True for entry in entries
        ),
        "reviewed_preserved_existing_genre": sum(
            entry.get("reviewed", {}).get("applied") is False for entry in entries
        ),
    }
    manifest = {
        "version": MANIFEST_VERSION,
        "created_at": utc_now(),
        "mode": "dry-run",
        "db_path": str(db_path.resolve()),
        "music_root": str(music_root.resolve()) if music_root else "",
        "musicbrainz": {
            "base_url": client.base_url,
            "user_agent": client.user_agent,
            "minimum_votes": minimum_votes,
            "maximum_genres": maximum_genres,
            "cache_dir": str(client.cache_dir.resolve()),
            "request_count": client.request_count,
        },
        "reviewed_genres": {
            "path": str(reviewed_path.resolve()) if reviewed_path else "",
            "version": 1 if reviewed_path else None,
            "entry_count": len(reviewed),
            "provenance": "reviewed per-file proposals" if reviewed_path else "",
        },
        "groups": groups_manifest,
        "summary": dict(counts),
        "audit": audit,
        "entries": entries,
    }
    atomic_write_json(manifest_path, manifest)
    return manifest


def ensure_manifest(path: Path) -> dict[str, Any]:
    value = load_json(path)
    if not isinstance(value, dict) or value.get("version") != MANIFEST_VERSION:
        raise RepairError(f"unsupported or invalid manifest {path}")
    if not isinstance(value.get("entries"), list):
        raise RepairError(f"manifest has no entries: {path}")
    return value


def backup_target(backup_dir: Path, index: int, path: Path) -> Path:
    # Prefixing with the manifest entry number avoids collisions while keeping
    # the original filename useful to a human inspecting the backup.
    return backup_dir / "files" / f"{index:05d}-{path.name}"


def copy_backup(source: Path, target: Path) -> None:
    target.parent.mkdir(parents=True, exist_ok=True)
    temporary = target.with_name(f".{target.name}.tmp")
    try:
        shutil.copy2(source, temporary)
        os.replace(temporary, target)
    finally:
        with contextlib.suppress(FileNotFoundError):
            temporary.unlink()


def _read_id3v1_footer(path: Path) -> bytes | None:
    """Return an existing ID3v1 footer, without interpreting its fields."""
    try:
        with path.open("rb") as handle:
            handle.seek(0, os.SEEK_END)
            if handle.tell() < 128:
                return None
            handle.seek(-128, os.SEEK_END)
            footer = handle.read(128)
    except OSError as exc:
        raise RepairError(f"read MP3 ID3v1 footer {path}: {exc}") from exc
    if len(footer) == 128 and footer[:3] == b"TAG":
        return footer
    return None


def _restore_id3v1_non_genre_bytes(path: Path, original_footer: bytes) -> None:
    """Restore ID3v1 bytes except byte 127, which stores the new genre."""
    if len(original_footer) != 128 or original_footer[:3] != b"TAG":
        raise RepairError(f"invalid original MP3 ID3v1 footer {path}")
    try:
        with path.open("r+b") as handle:
            handle.seek(0, os.SEEK_END)
            if handle.tell() < 128:
                raise RepairError(f"MP3 ID3v1 footer disappeared after write {path}")
            handle.seek(-128, os.SEEK_END)
            current_footer = handle.read(128)
            if len(current_footer) != 128 or current_footer[:3] != b"TAG":
                raise RepairError(f"MP3 ID3v1 footer disappeared after write {path}")
            handle.seek(-128, os.SEEK_END)
            handle.write(original_footer[:127] + current_footer[127:])
            handle.flush()
            os.fsync(handle.fileno())
    except RepairError:
        raise
    except OSError as exc:
        raise RepairError(f"restore MP3 ID3v1 footer {path}: {exc}") from exc


def _write_genres_in_place(path: Path, replacement: list[str]) -> None:
    """Change only the genre frame/field on a temporary audio copy."""
    if path.suffix.casefold() == ".mp3":
        try:
            from mutagen.id3 import ID3, TCON
        except ImportError as exc:
            raise RepairError("mutagen.id3 is required for MP3 genre repair") from exc
        original_footer = _read_id3v1_footer(path)
        try:
            tags = ID3(path)
        except Exception as exc:
            raise RepairError(f"read MP3 ID3 tags {path}: {exc}") from exc
        tags.delall("TCON")
        if replacement:
            tags.add(TCON(encoding=3, text=replacement))
        try:
            tags.save(path)
            if original_footer is not None:
                _restore_id3v1_non_genre_bytes(path, original_footer)
        except Exception as exc:
            raise RepairError(f"write MP3 genre tag {path}: {exc}") from exc
        return

    audio = mutagen_tags(path)
    if audio.tags is None:
        raise RepairError(f"audio has no writable tags: {path}")
    try:
        if replacement:
            audio.tags["genre"] = replacement
        else:
            with contextlib.suppress(KeyError):
                del audio.tags["genre"]
        audio.save()
    except Exception as exc:
        raise RepairError(f"write genre tag {path}: {exc}") from exc


def _temporary_audio_copy(path: Path) -> Path:
    # Keep the original extension so Mutagen chooses the same parser for the
    # temporary file. It is created next to the source and atomically renamed
    # only after all tag and frame checks succeed.
    fd, temporary = tempfile.mkstemp(
        prefix=f".{path.stem}.genre-repair-", suffix=path.suffix, dir=path.parent
    )
    os.close(fd)
    return Path(temporary)


def _touch_for_scan(path: Path) -> None:
    # The scanner uses file changes as its trigger. Keep the original mode,
    # but give the replaced inode a fresh timestamp so a later scan observes
    # the tag update. Backups retain the original timestamp.
    now = time.time_ns()
    os.utime(path, ns=(now, now), follow_symlinks=False)


def replace_genres(path: Path, replacement: list[str]) -> dict[str, Any]:
    before = file_state(path)
    temporary = _temporary_audio_copy(path)
    try:
        shutil.copy2(path, temporary)
        _write_genres_in_place(temporary, replacement)
        candidate = file_state(temporary)
        if candidate["native_tags_sha256"] != before["native_tags_sha256"]:
            raise RepairError(f"non-genre tags changed while writing {path}")
        if [normalize_genre(item) for item in candidate["genres"]] != [
            normalize_genre(item) for item in replacement
        ]:
            raise RepairError(f"genre verification failed for {path}: {candidate['genres']!r}")
        os.chmod(temporary, before["mode"], follow_symlinks=False)
        os.replace(temporary, path)
        _touch_for_scan(path)
        after = file_state(path)
        if after["native_tags_sha256"] != before["native_tags_sha256"]:
            raise RepairError(f"non-genre tags changed after replacing {path}")
        if [normalize_genre(item) for item in after["genres"]] != [
            normalize_genre(item) for item in replacement
        ]:
            raise RepairError(f"genre verification failed after replacing {path}: {after['genres']!r}")
        return after
    finally:
        with contextlib.suppress(FileNotFoundError):
            temporary.unlink()


def current_db_entry(db_path: Path, media_file_id: str) -> dict[str, Any] | None:
    with open_database(db_path) as connection:
        row = connection.execute(
            """
            SELECT mf.id, mf.library_id, mf.path, mf.title, mf.artist,
                   mf.mbz_recording_id, mf.missing, mf.tags,
                   COALESCE(lib.path, '') AS library_path
            FROM media_file AS mf
            LEFT JOIN library AS lib ON lib.id = mf.library_id
            WHERE mf.id = ?
            """,
            (media_file_id,),
        ).fetchone()
    if row is None:
        return None
    result = dict(row)
    result["db_genres"] = genre_values_from_json(result.get("tags"))
    return result


def validate_apply_entry(
    db_path: Path,
    entry: dict[str, Any],
    music_root: Path | None = None,
    allow_post_db_genres: bool = False,
    allow_any_content: bool = False,
) -> Path:
    media_id = str(entry.get("media_file_id") or "")
    current = current_db_entry(db_path, media_id)
    if current is None:
        raise RepairError(f"media-file ID no longer exists: {media_id}")
    # The database stores a library-relative path while the manifest stores
    # the resolved absolute path. Compare the relative path first, then
    # resolve it again below to catch a moved library or symlink escape.
    for key in ("library_id", "path", "title", "artist", "mbz_recording_id", "library_path"):
        expected_value = entry.get("relative_path") if key == "path" else entry.get(key)
        if str(current.get(key) or "") != str(expected_value or ""):
            raise RepairError(f"media-file {media_id} changed field {key}")
    if current.get("missing"):
        raise RepairError(f"media-file {media_id} is now marked missing")
    current_db_genres = [normalize_genre(item) for item in current["db_genres"]]
    original_db_genres = [normalize_genre(item) for item in entry.get("db_genres", [])]
    replacement_db_genres = [normalize_genre(item) for item in entry.get("replacement_genres", [])]
    allowed_db_genres = {tuple(original_db_genres)}
    if allow_post_db_genres:
        allowed_db_genres.add(tuple(replacement_db_genres))
    if tuple(current_db_genres) not in allowed_db_genres:
        raise RepairError(f"media-file {media_id} database genre tags changed")
    resolved, path_error = resolve_media_path(
        str(current.get("library_path") or ""), str(current.get("path") or ""), music_root
    )
    if resolved is None or path_error:
        raise RepairError(f"media-file {media_id} path is unsafe: {path_error}")
    manifest_path = Path(str(entry.get("path") or "")).resolve(strict=False)
    if resolved != manifest_path:
        raise RepairError(f"media-file {media_id} resolved path changed: {resolved} != {manifest_path}")
    path = resolved
    if not path.exists() or path.is_symlink() or not path.is_file():
        if not allow_any_content and path.exists():
            raise RepairError(f"media-file {media_id} path is unavailable: {path}")
        if path.exists() and (path.is_symlink() or not path.is_file()):
            raise RepairError(f"media-file {media_id} path is unavailable: {path}")
        if not path.exists() and not allow_any_content:
            raise RepairError(f"media-file {media_id} path is unavailable: {path}")
        return path
    if allow_any_content:
        return path
    state = file_state(path)
    expected = entry.get("pre_file") or {}
    post = entry.get("post_file") if isinstance(entry.get("post_file"), dict) else {}
    allowed_hashes = {str(expected.get("sha256") or "")}
    if post.get("sha256"):
        allowed_hashes.add(str(post["sha256"]))
    if state["sha256"] not in allowed_hashes:
        raise RepairError(f"media-file {media_id} changed on disk since dry-run")
    if state.get("native_tags_sha256") != expected.get("native_tags_sha256"):
        raise RepairError(f"media-file {media_id} native tags changed since dry-run")
    expected_genres = post.get("genres") if post and state["sha256"] == post.get("sha256") else entry.get("file_genres", [])
    if [normalize_genre(item) for item in state["genres"]] != [normalize_genre(item) for item in expected_genres]:
        raise RepairError(f"media-file {media_id} genre tags changed since dry-run")
    return path


def manifest_music_root(manifest: dict[str, Any]) -> Path | None:
    value = str(manifest.get("music_root") or "").strip()
    return Path(value).resolve(strict=False) if value else None


def manifest_library_roots(manifest: dict[str, Any]) -> list[Path]:
    roots: set[Path] = set()
    override = manifest_music_root(manifest)
    if override is not None:
        roots.add(override)
    for entry in manifest.get("entries", []):
        value = str(entry.get("library_path") or "").strip()
        if value:
            roots.add(Path(value).resolve(strict=False))
    return sorted(roots, key=str)


def assert_backup_dir_safe(target_dir: Path, manifest: dict[str, Any]) -> Path:
    target = target_dir.resolve(strict=False)
    for root in manifest_library_roots(manifest):
        if file_inside(root, target):
            raise RepairError(f"backup directory must be outside the music library: {target} is under {root}")
    return target


def assert_backup_path(backup: Path, target_dir: Path) -> None:
    try:
        backup.resolve(strict=False).relative_to(target_dir.resolve(strict=False))
    except ValueError as exc:
        raise RepairError(f"manifest backup path escapes backup directory: {backup}") from exc


def backup_matches_entry(backup: Path, entry: dict[str, Any]) -> bool:
    expected = str(entry.get("backup_sha256") or "")
    return bool(expected and backup.exists() and backup.is_file() and sha256_file(backup) == expected)


def apply_manifest(manifest_path: Path, db_path: Path, backup_dir: Path | None, confirm: bool) -> None:
    if not confirm:
        raise RepairError("apply requires --yes after a dry-run manifest has been reviewed")
    manifest = ensure_manifest(manifest_path)
    if manifest.get("mode") != "dry-run":
        raise RepairError("apply requires a dry-run manifest")
    expected_db = Path(str(manifest.get("db_path") or "")).resolve()
    if expected_db != db_path.resolve():
        raise RepairError(f"manifest DB is {expected_db}, but --db is {db_path.resolve()}")
    configured_backup = str(manifest.get("apply", {}).get("backup_dir") or "").strip()
    if backup_dir is not None:
        target_dir = backup_dir
    elif configured_backup:
        target_dir = Path(configured_backup)
    else:
        target_dir = manifest_path.parent / f"genre-repair-backup-{time.strftime('%Y%m%d-%H%M%S')}"
    target_dir = assert_backup_dir_safe(target_dir, manifest)
    apply_record = manifest.get("apply") if isinstance(manifest.get("apply"), dict) else {}
    existing_status = str(apply_record.get("status") or "")
    if target_dir.exists() and any(target_dir.iterdir()):
        if not configured_backup or Path(configured_backup).resolve(strict=False) != target_dir or existing_status not in (
            "in-progress",
            "failed",
        ):
            raise RepairError(f"backup directory already exists and is not resumable: {target_dir}")
    target_dir.mkdir(parents=True, exist_ok=True)
    if configured_backup and Path(configured_backup).resolve(strict=False) != target_dir:
        raise RepairError("configured backup directory changed while resuming apply")
    manifest["apply"] = {
        **apply_record,
        "status": "in-progress",
        "started_at": apply_record.get("started_at") or utc_now(),
        "backup_dir": str(target_dir),
    }
    atomic_write_json(manifest_path, manifest)
    applied = 0
    try:
        for index, entry in enumerate(manifest["entries"], start=1):
            if entry.get("status") != "candidate":
                continue
            music_root = manifest_music_root(manifest)
            path = validate_apply_entry(db_path, entry, music_root)
            backup_value = str(entry.get("backup_path") or "").strip()
            if backup_value:
                backup = Path(backup_value).resolve(strict=False)
                assert_backup_path(backup, target_dir)
                if not backup_matches_entry(backup, entry):
                    raise RepairError(f"recorded backup is missing or changed: {backup}")
                # A process may have been interrupted after replacement but
                # before its post-state was recorded. Refuse to guess which
                # content is live; the recorded backup remains recoverable.
                current_hash = sha256_file(path)
                if entry.get("post_file") and current_hash == entry["post_file"].get("sha256"):
                    applied += 1
                    continue
                if current_hash != entry["pre_file"].get("sha256"):
                    raise RepairError(f"media-file {path} is neither its pre-apply nor post-apply content")
            else:
                backup = backup_target(target_dir, index, path).resolve(strict=False)
                assert_backup_path(backup, target_dir)
                copy_backup(path, backup)
                backup_hash = sha256_file(backup)
                if backup_hash != entry["pre_file"]["sha256"]:
                    raise RepairError(f"backup hash mismatch for {path}")
                # Persist the complete backup record before touching the live
                # inode. A crash at the next instruction is recoverable.
                entry["backup_path"] = str(backup)
                entry["backup_sha256"] = backup_hash
                entry["backup_created_at"] = utc_now()
                manifest["apply"]["backed_up"] = int(manifest["apply"].get("backed_up", 0)) + 1
                atomic_write_json(manifest_path, manifest)
            try:
                post = replace_genres(path, unique_values(entry.get("replacement_genres", [])))
            except Exception:
                copy_backup(backup, path)
                _touch_for_scan(path)
                manifest["apply"]["status"] = "failed"
                manifest["apply"]["error"] = f"failed while changing {path}"
                atomic_write_json(manifest_path, manifest)
                raise
            entry["post_file"] = post
            entry["applied_at"] = utc_now()
            applied += 1
            manifest["apply"]["completed"] = applied
            atomic_write_json(manifest_path, manifest)
            eprint(f"applied {applied} genre repairs: {path}")
    except Exception as exc:
        manifest["apply_error"] = str(exc)
        manifest["apply"]["status"] = "failed"
        atomic_write_json(manifest_path, manifest)
        raise
    manifest["mode"] = "applied"
    manifest["apply"]["status"] = "complete"
    manifest["apply"]["finished_at"] = utc_now()
    atomic_write_json(manifest_path, manifest)
    print(f"Applied {applied} genre repairs. Run a Navidrome scan after reviewing the files.")


def rollback_manifest(manifest_path: Path, db_path: Path, confirm: bool, force: bool) -> None:
    if not confirm:
        raise RepairError("rollback requires --yes")
    manifest = ensure_manifest(manifest_path)
    apply_record = manifest.get("apply") if isinstance(manifest.get("apply"), dict) else {}
    partial_failed = manifest.get("mode") == "dry-run" and apply_record.get("status") == "failed"
    if manifest.get("mode") != "applied" and not partial_failed:
        raise RepairError("rollback requires an applied or failed apply manifest")
    expected_db = Path(str(manifest.get("db_path") or "")).resolve()
    if expected_db != db_path.resolve():
        raise RepairError(f"manifest DB is {expected_db}, but --db is {db_path.resolve()}")
    backup_dir_value = str(manifest.get("apply", {}).get("backup_dir") or "").strip()
    if not backup_dir_value:
        raise RepairError("applied manifest has no backup directory")
    target_dir = Path(backup_dir_value).resolve(strict=False)
    assert_backup_dir_safe(target_dir, manifest)
    restored = 0
    for entry in manifest["entries"]:
        backup_name = entry.get("backup_path")
        if not backup_name:
            continue
        path = validate_apply_entry(
            db_path,
            entry,
            manifest_music_root(manifest),
            allow_post_db_genres=True,
            allow_any_content=force,
        )
        backup = Path(backup_name).resolve(strict=False)
        assert_backup_path(backup, target_dir)
        if not backup.exists() or not backup.is_file():
            raise RepairError(f"backup is missing: {backup}")
        if not backup_matches_entry(backup, entry):
            raise RepairError(f"backup hash changed: {backup}")
        if path.exists() and not force:
            current_hash = sha256_file(path)
            if current_hash == entry.get("backup_sha256"):
                entry["rolled_back_at"] = entry.get("rolled_back_at") or utc_now()
                atomic_write_json(manifest_path, manifest)
                continue
            expected_post = (entry.get("post_file") or {}).get("sha256")
            if expected_post and current_hash != expected_post:
                raise RepairError(f"refusing rollback because file changed after apply: {path}")
        copy_backup(backup, path)
        _touch_for_scan(path)
        restored_hash = sha256_file(path)
        if restored_hash != entry.get("backup_sha256"):
            raise RepairError(f"rollback hash mismatch for {path}")
        entry["rolled_back_at"] = utc_now()
        restored += 1
        atomic_write_json(manifest_path, manifest)
        eprint(f"restored {restored}: {path}")
    manifest["mode"] = "rolled-back"
    manifest["rollback"] = {"finished_at": utc_now(), "restored": restored}
    atomic_write_json(manifest_path, manifest)
    print(f"Restored {restored} files. Run a Navidrome scan to restore database tags.")


def self_test_ytdlp() -> None:
    """Exercise the exact FFmpegMetadataPP path without downloading anything."""
    if shutil.which("ffmpeg") is None:
        raise RepairError("ffmpeg is required for --self-test-ytdlp")
    try:
        from mutagen.id3 import COMM, ID3, TCON, TIT2, TPE1
        from yt_dlp import YoutubeDL
        from yt_dlp.postprocessor.ffmpeg import FFmpegMetadataPP
    except ImportError as exc:
        raise RepairError("yt_dlp and mutagen are required for --self-test-ytdlp") from exc

    with tempfile.TemporaryDirectory(prefix="navidrome-genre-self-test-") as temporary:
        path = Path(temporary) / "probe.mp3"
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
                str(path),
            ],
            check=True,
        )
        tags = ID3(path)
        tags.add(TIT2(encoding=3, text="Probe Song"))
        tags.add(TPE1(encoding=3, text="Probe Artist"))
        tags.add(TCON(encoding=3, text="Music"))
        tags.add(COMM(encoding=3, lang="eng", desc="", text="https://youtu.be/probe"))
        tags.save(path)

        # Use yt-dlp's real YoutubeDL object rather than a partial mock. The
        # FFmpeg postprocessor invokes downloader progress hooks internally;
        # no extraction or download is performed here.
        with YoutubeDL(
            {
                "quiet": True,
                "no_warnings": True,
                "skip_download": True,
                "postprocessor_args": {"metadata+ffmpeg_o": ["-metadata", "genre="]},
            }
        ) as downloader:
            processor = FFmpegMetadataPP(
                downloader, add_metadata=True, add_chapters=False, add_infojson=False
            )
            processor.run(
                {
                    "filepath": str(path),
                    "ext": "mp3",
                    "title": "Probe Song",
                    "artist": "Probe Artist",
                    "webpage_url": "https://youtu.be/probe",
                    # This is the value produced by the production
                    # --parse-metadata %(webpage_url)s:%(meta_comment)s flag.
                    "meta_comment": "https://youtu.be/probe",
                    "categories": ["Music"],
                    "vcodec": "none",
                    "acodec": "mp3",
                }
            )

        result = ID3(path)
        assert str(result.get("TIT2")) == "Probe Song", result.get("TIT2")
        assert str(result.get("TPE1")) == "Probe Artist", result.get("TPE1")
        assert "TCON" not in result, result.getall("TCON")
        comments = [text for frame in result.getall("COMM") for text in frame.text]
        assert any("https://youtu.be/probe" in text for text in comments), comments
    print("yt-dlp FFmpegMetadataPP self-test passed: category genre cleared; title, artist, and source comment retained")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--dry-run", action="store_true", help="build a reviewable manifest without mutating files")
    mode.add_argument("--apply", action="store_true", help="apply a reviewed dry-run manifest")
    mode.add_argument("--rollback", action="store_true", help="restore files from an applied manifest")
    mode.add_argument("--self-test-ytdlp", action="store_true", help="run the temporary silent-MP3 integration test")
    parser.add_argument("--db", type=Path, default=Path("/data/navidrome.db"), help="Navidrome SQLite database")
    parser.add_argument("--manifest", type=Path, default=Path("genre-repair-manifest.json"))
    parser.add_argument(
        "--reviewed-genres",
        type=Path,
        default=None,
        help="version 1 per-file reviewed genre proposals to apply during --dry-run",
    )
    parser.add_argument("--music-root", type=Path, default=None, help="override library root when resolving media paths")
    parser.add_argument("--cache-dir", type=Path, default=Path("musicbrainz-genre-cache"))
    parser.add_argument("--rate-lock", type=Path, default=None, help="shared MusicBrainz rate lock file")
    parser.add_argument("--base-url", default=DEFAULT_BASE_URL)
    parser.add_argument("--user-agent", default=DEFAULT_USER_AGENT)
    parser.add_argument("--rate-seconds", type=float, default=DEFAULT_RATE_SECONDS)
    parser.add_argument("--timeout", type=float, default=30.0)
    parser.add_argument("--minimum-votes", type=int, default=1)
    parser.add_argument("--maximum-genres", type=int, default=2)
    parser.add_argument("--resume", action="store_true", help="reuse group evidence from an existing manifest")
    parser.add_argument("--backup-dir", type=Path, default=None)
    parser.add_argument("--yes", action="store_true", help="confirm apply or rollback after review")
    parser.add_argument("--force", action="store_true", help="allow rollback after a file changed")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        if args.self_test_ytdlp:
            self_test_ytdlp()
            return 0
        if args.maximum_genres < 1 or args.maximum_genres > 2:
            raise RepairError("--maximum-genres must be 1 or 2")
        if args.minimum_votes < 1:
            raise RepairError("--minimum-votes must be positive")
        if args.apply:
            if args.reviewed_genres:
                raise RepairError("--reviewed-genres is only valid with --dry-run")
            apply_manifest(args.manifest, args.db, args.backup_dir, args.yes)
            return 0
        if args.rollback:
            if args.reviewed_genres:
                raise RepairError("--reviewed-genres is only valid with --dry-run")
            rollback_manifest(args.manifest, args.db, args.yes, args.force)
            return 0
        tracks = load_tracks(args.db, args.music_root)
        reviewed = (
            load_reviewed_genres(args.reviewed_genres, tracks, args.music_root, args.maximum_genres)
            if args.reviewed_genres
            else {}
        )
        lock_path = args.rate_lock or args.cache_dir / "musicbrainz.rate.lock"
        client = MusicBrainzClient(
            args.cache_dir,
            lock_path,
            args.base_url,
            args.user_agent,
            args.rate_seconds,
            args.timeout,
        )
        manifest = make_manifest(
            args.db.resolve(),
            tracks,
            client,
            args.manifest,
            args.music_root,
            args.minimum_votes,
            args.maximum_genres,
            args.resume,
            reviewed,
            args.reviewed_genres,
        )
        print(json.dumps(manifest["summary"], sort_keys=True))
        print(f"Dry-run manifest: {args.manifest.resolve()}")
        print("No audio files or database rows were changed.")
        return 0
    except (RepairError, AssertionError, OSError, sqlite3.Error) as exc:
        eprint(f"error: {exc}")
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
