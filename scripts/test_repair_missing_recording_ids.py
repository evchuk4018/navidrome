from __future__ import annotations

import importlib.util
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("repair_missing_recording_ids.py")
SPEC = importlib.util.spec_from_file_location("repair_missing_recording_ids", SCRIPT)
assert SPEC and SPEC.loader
repair = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(repair)


class FakeUFID:
    def __init__(self, data: bytes) -> None:
        self.data = data


class FakeTextFrame:
    def __init__(self, text: list[str]) -> None:
        self.text = text


class RecordingTagParserTests(unittest.TestCase):
    recording_id = "d9293374-4905-43b3-beb1-63ecefa36343"

    def test_accepts_id3_ufid_frame_data(self) -> None:
        self.assertEqual(
            repair.recording_tag_value(FakeUFID(self.recording_id.encode("ascii"))),
            self.recording_id,
        )

    def test_accepts_mutagen_text_frame(self) -> None:
        self.assertEqual(
            repair.recording_tag_value(FakeTextFrame([self.recording_id])),
            self.recording_id,
        )

    def test_accepts_plain_string_and_lists(self) -> None:
        self.assertEqual(repair.recording_tag_value(self.recording_id), self.recording_id)
        self.assertEqual(
            repair.recording_tag_value([self.recording_id]), self.recording_id
        )
        self.assertEqual(
            repair.recording_tag_value([self.recording_id.encode("ascii")]),
            self.recording_id,
        )

    def test_accepts_mp4_style_bytes_and_track_id_key(self) -> None:
        key = "----:com.apple.iTunes:MusicBrainz Track Id"
        self.assertTrue(repair.is_recording_id_key(key))
        self.assertEqual(
            repair.recording_tag_values({key: [self.recording_id.encode("ascii")]}),
            [self.recording_id],
        )

    def test_ignores_release_track_id(self) -> None:
        key = "----:com.apple.iTunes:MusicBrainz Release Track Id"
        self.assertFalse(repair.is_recording_id_key(key))

    def test_default_user_agent_identifies_repository(self) -> None:
        self.assertIn("https://github.com/evchuk4018/navidrome", repair.DEFAULT_USER_AGENT)

    def test_beets_seconds_are_converted_to_milliseconds(self) -> None:
        self.assertEqual(repair.beets_length_milliseconds("180.25"), 180250.0)
        self.assertTrue(repair.duration_evidence(180.25, repair.beets_length_milliseconds(180.25))["duration_match"])

    def test_external_manifest_binds_proposal_to_evidence_id(self) -> None:
        manifest = {
            "external_suggestions": [
                {
                    "media_file_id": "media-1",
                    "decision": "external_suggestion",
                    "confidence": "review_required",
                    "proposed_mbz_recording_id": self.recording_id,
                    "source_evidence": [
                        {
                            "source_type": "musicbrainz_recording_search",
                            "recording_id": self.recording_id,
                            "evidence": {"score": 100, "duration_match": True},
                        }
                    ],
                    "pre_file": {"sha256": "fixture"},
                }
            ]
        }
        self.assertEqual(len(repair.external_manifest_entries(manifest)), 1)
        manifest["external_suggestions"][0]["source_evidence"][0]["recording_id"] = (
            "bf542b86-1c13-4fb8-9462-6353ac86212a"
        )
        with self.assertRaisesRegex(RuntimeError, "does not match source evidence"):
            repair.external_manifest_entries(manifest)


if __name__ == "__main__":
    unittest.main()
