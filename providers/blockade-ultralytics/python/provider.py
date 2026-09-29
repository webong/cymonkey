import base64
import json
import os
import sys

from inference import Worker

ADAPTER_API = "blockade.provider-adapter/v1alpha1"
OBSERVATION_API = "blockade.observation/v1alpha1"
MAX_FRAME = 24 << 20


class Provider:
    def __init__(self):
        manifest_path = os.environ["BLOCKADE_PLUGIN_MANIFEST"]
        with open(manifest_path, "r", encoding="utf-8") as source:
            self.manifest = json.load(source)
        contracts = self.manifest.get("contracts", [])
        if not any(
            item.get("name") == "blockade.provider"
            and item.get("version") == "v1alpha1"
            for item in contracts
        ):
            raise ValueError("package does not declare blockade.provider@v1alpha1")
        self.worker = None

    def call(self, method, params):
        if method == "plugin.hello":
            return self.manifest
        if method == "blockade.model.inspect":
            try:
                self._require_empty_settings(params.get("settings", {}))
                yolo, sam = self._model_paths(params["model"])
                worker = Worker(yolo, sam)
                del worker
                return {"apiVersion": ADAPTER_API, "compatible": True}
            except Exception as exc:
                return {
                    "apiVersion": ADAPTER_API,
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
                "apiVersion": ADAPTER_API,
                "capabilities": [
                    "image.observe",
                    "object.detect",
                    "object.segment",
                ],
            }
        if method == "blockade.health":
            return {"apiVersion": ADAPTER_API, "ready": self.worker is not None}
        if method == "blockade.observe":
            if self.worker is None:
                raise ValueError("provider has not been started")
            wrapped = params["request"]
            if wrapped.get("apiVersion") != ADAPTER_API:
                raise ValueError("unsupported provider adapter apiVersion")
            request = wrapped["request"]
            request_id = request.get("requestId", "")
            observations = self.worker.observe(
                base64.b64decode(request["image"], validate=True), request_id
            )
            return {
                "apiVersion": ADAPTER_API,
                "response": {
                    "apiVersion": OBSERVATION_API,
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
                "Ultralytics provider has no provider-specific settings; configure workers in Blockade"
            )

    def _model_paths(self, model):
        manifest = model["manifest"]
        if manifest.get("provider") != self.manifest["name"]:
            raise ValueError("model provider does not match this provider package")
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
