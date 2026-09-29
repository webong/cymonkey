import base64
import json
import sys

from runtime import Worker


def main():
    model_worker = Worker()
    for line in sys.stdin:
        request = {}
        try:
            request = json.loads(line)
            request_id = request["id"]
            method = request.get("method")
            if method == "health":
                response = {"id": request_id, "ok": True}
            elif method == "observe":
                observations = model_worker.observe(
                    base64.b64decode(request["image"]), request_id
                )
                response = {
                    "id": request_id,
                    "ok": True,
                    "apiVersion": "blockade.observation/v1alpha1",
                    "observations": observations,
                }
            else:
                raise ValueError(f"unsupported worker method {method!r}")
        except Exception as exc:
            response = {
                "id": request.get("id", ""),
                "ok": False,
                "error": str(exc),
            }
        sys.stdout.write(json.dumps(response, separators=(",", ":")) + "\n")
        sys.stdout.flush()


if __name__ == "__main__":
    main()
