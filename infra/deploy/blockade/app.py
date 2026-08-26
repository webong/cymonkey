import base64
import binascii
import contextlib
import os
import sys
import uuid
from contextlib import asynccontextmanager

import cv2
import numpy as np
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel

os.environ.setdefault("YOLO_VERBOSE", "False")

with contextlib.redirect_stdout(sys.stderr):
    from ultralytics import SAM, YOLO


class ObserveRequest(BaseModel):
    apiVersion: str | None = None
    requestId: str | None = None
    image: str
    prompt: str | None = None


class Worker:
    def __init__(self):
        with contextlib.redirect_stdout(sys.stderr):
            self.yolo = YOLO(os.getenv("BLOCKADE_YOLO_MODEL", "yolo11n.pt"))
            self.sam = SAM(os.getenv("BLOCKADE_SAM_MODEL", "sam2_b.pt"))

    def observe(self, image: bytes, request_id: str):
        array = cv2.imdecode(np.frombuffer(image, np.uint8), cv2.IMREAD_COLOR)
        if array is None:
            raise ValueError("image is not a supported encoded image")
        with contextlib.redirect_stdout(sys.stderr):
            detections = self.yolo(array, verbose=False)[0]
        observations = []
        for index, box in enumerate(detections.boxes):
            x1, y1, x2, y2 = [float(value) for value in box.xyxy[0].cpu().tolist()]
            cls = int(box.cls[0].cpu().item())
            confidence = float(box.conf[0].cpu().item())
            label = detections.names.get(cls, str(cls))
            observations.append({
                "kind": "object", "label": label, "confidence": confidence,
                "region": {"x": x1, "y": y1, "width": x2 - x1, "height": y2 - y1},
                "evidence": f"{request_id}:detection-{index}",
            })
        if observations:
            boxes = [
                [item["region"]["x"], item["region"]["y"],
                 item["region"]["x"] + item["region"]["width"],
                 item["region"]["y"] + item["region"]["height"]]
                for item in observations
            ]
            with contextlib.redirect_stdout(sys.stderr):
                masks = self.sam(array, bboxes=boxes, verbose=False)[0].masks
            if masks is not None:
                for item, mask in zip(observations, masks.data):
                    png = (mask.cpu().numpy() * 255).astype(np.uint8)
                    ok, encoded = cv2.imencode(".png", png)
                    if ok:
                        item["mask"] = base64.b64encode(encoded.tobytes()).decode("ascii")
        return observations


worker = None


@asynccontextmanager
async def lifespan(_app):
    global worker
    worker = Worker()
    yield


app = FastAPI(title="Blockade vision worker", version="0.1.0", lifespan=lifespan)


@app.get("/healthz")
def healthz():
    return {"status": "ok", "provider": "yolo-sam"}


@app.get("/capabilities")
def capabilities():
    return {"apiVersion": "blockade.observation/v1alpha1", "provider": "yolo-sam",
            "capabilities": ["image.observe", "object.detect", "object.segment"]}


@app.post("/v1/observe")
def observe(request: ObserveRequest):
    if request.apiVersion not in (None, "blockade.observation/v1alpha1"):
        raise HTTPException(400, "unsupported apiVersion")
    try:
        image = base64.b64decode(request.image, validate=True)
        request_id = request.requestId or str(uuid.uuid4())
        observations = worker.observe(image, request_id)
        return {"apiVersion": "blockade.observation/v1alpha1", "requestId": request_id,
                "observations": observations}
    except (ValueError, binascii.Error) as exc:
        raise HTTPException(400, str(exc)) from exc
