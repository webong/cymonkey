import base64
import json
import os
import sys

from inference import Worker

ADAPTER_API = "blockade.provider-adapter/v1alpha1"
OBSERVATION_API = "blockade.observation/v1alpha1"
CYMONKEY_ADAPTER_API = "cymonkey.content-adapter/v1alpha1"
CYMONKEY_OBSERVATION_REQUEST_API = "cymonkey.observation/v1alpha1"
CYMONKEY_OBSERVATION_API = "cymonkey.observation-result/v1alpha1"
MAX_FRAME = 24 << 20


class Provider:
    def __init__(self):
        manifest_path = os.environ.get("CYMONKEY_PLUGIN_MANIFEST") or os.environ[
            "BLOCKADE_PLUGIN_MANIFEST"
        ]
        with open(manifest_path, "r", encoding="utf-8") as source:
            self.manifest = json.load(source)
        contracts = self.manifest.get("contracts", [])
        if not any(
            item.get("name") in {"blockade.provider", "cymonkey.content"}
            and item.get("version") == "v1alpha1"
            for item in contracts
        ):
            raise ValueError("package does not declare a supported content contract")
        self.public = any(
            item.get("name") == "cymonkey.content"
            and item.get("version") == "v1alpha1"
            for item in contracts
        )
        self.worker = None

    def call(self, method, params):
        if method == "plugin.hello":
            return self.manifest
        public = method.startswith("cymonkey.content.")
        if public:
            method = "blockade." + method.removeprefix("cymonkey.content.")
        adapter_api = CYMONKEY_ADAPTER_API if public else ADAPTER_API
        observation_api = CYMONKEY_OBSERVATION_API if public else OBSERVATION_API
        if method == "blockade.model.inspect":
            try:
                self._require_empty_settings(params.get("settings", {}))
                yolo, sam = self._model_paths(params["model"])
                worker = Worker(yolo, sam)
                del worker
                return {"apiVersion": adapter_api, "compatible": True}
            except Exception as exc:
                return {
                    "apiVersion": adapter_api,
                    "compatible": False,
                    "detail": str(exc),
                }
        if method == "blockade.start":
            if self.worker is not None:
                raise ValueError("provider is already started")
            self._require_empty_settings(params.get("settings", {}))
            model = params.get("model")
            if model is None:
                raise ValueError("Ultralytics provider requires an installed model")
            yolo, sam = self._model_paths(model)
            self.worker = Worker(yolo, sam)
            return {}
        if method == "blockade.capabilities":
            return {
                "apiVersion": adapter_api,
                "capabilities": [
                    "image.observe",
                    "object.detect",
                    "object.segment",
                ],
            }
        if method == "blockade.health":
            return {"apiVersion": adapter_api, "ready": self.worker is not None}
        if method == "blockade.observe":
            if self.worker is None:
                raise ValueError("provider has not been started")
            wrapped = params["request"]
            if wrapped.get("apiVersion") != adapter_api:
                raise ValueError("unsupported provider adapter apiVersion")
            request = wrapped["request"]
            expected_request_api = (
                CYMONKEY_OBSERVATION_REQUEST_API if public else OBSERVATION_API
            )
            if request.get("apiVersion") != expected_request_api:
                raise ValueError("unsupported observation request apiVersion")
            request_id = request.get("requestId", "")
            observations = self.worker.observe(
                base64.b64decode(request["image"], validate=True), request_id
            )
            return {
                "apiVersion": adapter_api,
                "response": {
                    "apiVersion": observation_api,
                    "requestId": request_id,
                    "observations": observations,
                },
            }
        if method == "blockade.close":
            self.worker = None
            return {}
        raise ValueError(f"unsupported method {method!r}")

    @staticmethod
    def _require_empty_settings(settings):
        if settings:
            raise ValueError(
                "Ultralytics plugin has no plugin-specific settings; configure workers in Cymonkey"
            )

    def _model_paths(self, model):
        manifest = model["manifest"]
        plugin = manifest.get("plugin") if self.public else manifest.get("provider")
        if plugin != self.manifest["name"]:
            raise ValueError("model plugin does not match this plugin package")
        capabilities = set(manifest.get("capabilities", []))
        if not {"object.detect", "object.segment"}.issubset(capabilities):
            raise ValueError("model must declare object.detect and object.segment")
        return (
            self._artifact_path(model, "yolo", ".pt"),
            self._artifact_path(model, "sam", ".pt"),
        )

    @staticmethod
    def _artifact_path(model, role, extension):
        matches = [
            item["file"]
            for item in model["manifest"].get("artifacts", [])
            if item.get("role") == role
        ]
        if len(matches) != 1:
            raise ValueError(f"model needs exactly one {role!r} artifact")
        if not matches[0].lower().endswith(extension):
            raise ValueError(f"model artifact {matches[0]!r} must use {extension}")
        root = os.path.realpath(model["directory"])
        path = os.path.realpath(os.path.join(root, matches[0]))
        if os.path.dirname(path) != root or not os.path.isfile(path):
            raise ValueError("model artifact is outside its installed model directory")
        return path


def main():
    provider = Provider()
    for line in sys.stdin:
        request = {}
        try:
            if len(line.encode("utf-8")) > MAX_FRAME:
                raise ValueError("plugin request exceeds frame limit")
            request = json.loads(line)
            result = provider.call(request.get("method"), request.get("params") or {})
            response = {"id": request["id"], "result": result}
        except Exception as exc:
            response = {"id": request.get("id", 0), "error": str(exc)}
        sys.stdout.write(json.dumps(response, separators=(",", ":")) + "\n")
        sys.stdout.flush()


if __name__ == "__main__":
    main()
