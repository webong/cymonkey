import base64
import importlib.util
import json
import os
import sys
import tempfile
import types
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
PROVIDER_PATH = ROOT / "python" / "provider.py"
sys.path.insert(0, str(ROOT / "python"))


class FakeWorker:
    created = []

    def __init__(self, yolo, sam):
        self.created.append((yolo, sam))

    def observe(self, image, request_id):
        return [
            {
                "kind": "object",
                "label": image.decode("utf-8"),
                "confidence": 1,
                "region": {"x": 0, "y": 0, "width": 1, "height": 1},
                "evidence": request_id,
            }
        ]


def load_provider(manifest_path):
    inference = types.ModuleType("inference")
    inference.Worker = FakeWorker
    sys.modules["inference"] = inference
    os.environ["PLUGIN_MANIFEST"] = str(manifest_path)
    spec = importlib.util.spec_from_file_location(
        "blockade_ultralytics_provider", PROVIDER_PATH
    )
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module.Provider()


class ProviderTest(unittest.TestCase):
    def setUp(self):
        FakeWorker.created.clear()
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        self.manifest_path = self.root / "plugin.json"
        self.manifest_path.write_text(
            json.dumps(
                {
                    "apiVersion": "ext.package/v1",
                    "descriptor": {
                        "apiVersion": "ext.plugin/v1",
                        "identity": {"id": "local-ultralytics", "version": "1.0.0", "revision": "test"},
                        "contracts": [
                            {
                                "name": "content.provider",
                                "version": "v1",
                                "operations": [{"name": "start"}],
                            }
                        ],
                    },
                }
            ),
            encoding="utf-8",
        )
        self.model_root = self.root / "model"
        self.model_root.mkdir()
        (self.model_root / "yolo.pt").write_bytes(b"yolo")
        (self.model_root / "sam.pt").write_bytes(b"sam")
        self.model = {
            "manifest": {
                "provider": "local-ultralytics",
                "capabilities": ["object.detect", "object.segment"],
                "artifacts": [
                    {"role": "yolo", "file": "yolo.pt"},
                    {"role": "sam", "file": "sam.pt"},
                ],
            },
            "directory": str(self.model_root),
        }

    def tearDown(self):
        self.temporary.cleanup()

    def test_lifecycle_and_observation_contract(self):
        provider = load_provider(self.manifest_path)
        inspection = provider.call(
            "content.provider.model.inspect", {"settings": {}, "model": self.model}
        )
        self.assertTrue(inspection["compatible"])

        provider.call("content.provider.start", {"settings": {}, "model": self.model})
        self.assertTrue(provider.call("content.provider.health", {})["ready"])
        result = provider.call(
            "content.provider.observe",
            {
                "request": {
                    "apiVersion": "content.adapter/v1",
                    "request": {
                        "apiVersion": "content.observation/v1",
                        "requestId": "request-1",
                        "image": base64.b64encode(b"image").decode("ascii"),
                    },
                }
            },
        )
        self.assertEqual(result["response"]["requestId"], "request-1")
        self.assertEqual(result["response"]["observations"][0]["label"], "image")
        provider.call("content.provider.close", {})
        self.assertFalse(provider.call("content.provider.health", {})["ready"])

    def test_model_artifact_cannot_escape_install_directory(self):
        provider = load_provider(self.manifest_path)
        escaped = json.loads(json.dumps(self.model))
        escaped["manifest"]["artifacts"][0]["file"] = "../outside.pt"
        result = provider.call(
            "content.provider.model.inspect", {"settings": {}, "model": escaped}
        )
        self.assertFalse(result["compatible"])
        self.assertIn("outside", result["detail"])


if __name__ == "__main__":
    unittest.main()
