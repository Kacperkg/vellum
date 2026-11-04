import base64
import json
from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cd"))
import controller
import release


class ManifestParameterTests(unittest.TestCase):
    def test_production_mode_is_bounded_default_standard_and_never_shell_interpolated(self):
        root = Path(__file__).resolve().parents[1] / "ssm"
        production = json.loads((root / "production.json").read_bytes())
        staging = json.loads((root / "staging.json").read_bytes())
        parameter = production["parameters"]["ApprovalMode"]
        self.assertEqual(parameter["type"], "String")
        self.assertEqual(parameter["default"], "standard")
        self.assertEqual(parameter["allowedValues"], ["standard", "prelaunch"])
        self.assertEqual(parameter["allowedPattern"], "^(standard|prelaunch)$")
        self.assertEqual(parameter["interpolationType"], "ENV_VAR")
        self.assertNotIn("ApprovalMode", staging["parameters"])
        commands = production["mainSteps"][0]["inputs"]["runCommand"]
        self.assertTrue(any("SSM_ApprovalMode:-" in command for command in commands))
        self.assertFalse(any("{{" in command for command in commands))
        self.assertEqual(commands[-1], "exec /usr/bin/python3 -I /usr/local/lib/vellum-cd/cd/controller.py production")

    def test_templates_share_native_size_bounds_and_safe_interpolation(self):
        for environment in ("staging", "production"):
            with self.subTest(environment=environment):
                path = (
                    Path(__file__).resolve().parents[1]
                    / "ssm"
                    / (environment + ".json")
                )
                document = json.loads(path.read_bytes())
                parameter = document["parameters"]["Manifest"]
                self.assertEqual(parameter["type"], "String")
                self.assertEqual(parameter["interpolationType"], "ENV_VAR")
                self.assertEqual(parameter["allowedPattern"], "^[A-Za-z0-9+/=]+$")
                self.assertEqual(parameter["minChars"], 1)
                self.assertEqual(
                    parameter["maxChars"], controller.MAX_ENCODED_MANIFEST_CHARS
                )

    def test_decodes_base64_json(self):
        value = {"schema": 1, "example": "valid transport data"}
        encoded = base64.b64encode(json.dumps(value).encode()).decode()
        self.assertEqual(controller.decode_manifest_parameter(encoded), value)

    def test_rejects_empty_or_oversized_encoded_input_before_decode(self):
        for encoded in ("", "A" * (controller.MAX_ENCODED_MANIFEST_CHARS + 1)):
            with self.subTest(length=len(encoded)):
                with self.assertRaisesRegex(ValueError, "1..45000 characters"):
                    controller.decode_manifest_parameter(encoded)

    def test_decoded_json_limit_accepts_boundary_and_rejects_one_byte_over(self):
        prefix, suffix = b'{"data":"', b'"}'
        for length, accepted in (
            (release.MAX_MANIFEST, True),
            (release.MAX_MANIFEST + 1, False),
        ):
            with self.subTest(length=length):
                payload = prefix + b"A" * (length - len(prefix) - len(suffix)) + suffix
                encoded = base64.b64encode(payload).decode()
                self.assertLessEqual(
                    len(encoded), controller.MAX_ENCODED_MANIFEST_CHARS
                )
                if accepted:
                    self.assertEqual(
                        controller.decode_manifest_parameter(encoded),
                        json.loads(payload),
                    )
                else:
                    with self.assertRaisesRegex(
                        ValueError, "decoded manifest exceeds size limit"
                    ):
                        controller.decode_manifest_parameter(encoded)

    def test_encoded_boundary_still_enforces_decoded_limit(self):
        # Exactly 45000 legal base64 characters decode to more than 32 KiB.
        encoded = "A" * controller.MAX_ENCODED_MANIFEST_CHARS
        with self.assertRaisesRegex(ValueError, "decoded manifest exceeds size limit"):
            controller.decode_manifest_parameter(encoded)

    def test_rejects_malformed_base64_and_invalid_json(self):
        for encoded in (
            "a",
            "====",
            "e30=\n",
            "$(id)",
            "é",
            base64.b64encode(b"not json").decode(),
        ):
            with self.subTest(encoded=encoded):
                with self.assertRaises(ValueError):
                    controller.decode_manifest_parameter(encoded)


if __name__ == "__main__":
    unittest.main()
