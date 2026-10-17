import base64
import json
import os

import ext_plugin
from inference import Worker

ADAPTER_API = "content.adapter/v1"
OBSERVATION_API = "content.observation/v1"
CONTRACT = "content.provider"
CONTRACT_VERSION = "v1"


class Provider:
    def __init__(self):
        manifest_path = os.environ["PLUGIN_MANIFEST"]
        with open(manifest_path, "r", encoding="utf-8") as source:
            self.manifest = json.load(source)
        self.name = self.manifest["descriptor"]["identity"]["id"]
        contracts = self.manifest["descriptor"]["contracts"]
        if not any(
            item.get("name") == CONTRACT and item.get("version") == CONTRACT_VERSION
            for item in contracts
        ):
            raise ValueError("package does not declare a supported content contract")
        self.worker = None

    def call(self, method, params):
        adapter_api = ADAPTER_API
        observation_api = OBSERVATION_API
        if method == "content.provider.model.inspect":
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
        if method == "content.provider.start":
            if self.worker is not None:
                raise ValueError("provider is already started")
            self._require_empty_settings(params.get("settings", {}))
            model = params.get("model")
            if model is None:
                raise ValueError("Ultralytics provider requires an installed model")
            yolo, sam = self._model_paths(model)
            self.worker = Worker(yolo, sam)
            return {}
        if method == "content.provider.capabilities":
            return {
                "apiVersion": adapter_api,
                "capabilities": [
                    "image.observe",
                    "object.detect",
                    "object.segment",
                ],
            }
        if method == "content.provider.health":
            return {"apiVersion": adapter_api, "ready": self.worker is not None}
        if method == "content.provider.observe":
            if self.worker is None:
                raise ValueError("provider has not been started")
            wrapped = params["request"]
            if wrapped.get("apiVersion") != adapter_api:
                raise ValueError("unsupported provider adapter apiVersion")
            request = wrapped["request"]
            if request.get("apiVersion") != OBSERVATION_API:
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
        if method == "content.provider.close":
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
        plugin = manifest.get("provider")
        if plugin != self.name:
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
    manifest_path = os.environ["PLUGIN_MANIFEST"]
    provider = Provider()
    ext_plugin.serve_stdio(
        manifest_path,
        lambda contract, operation, payload: provider.call(
            contract + "." + operation, payload or {}
        ),
    )


if __name__ == "__main__":
    main()
