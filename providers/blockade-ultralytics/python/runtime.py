import contextlib
import os
import sys

import cv2
import numpy as np

os.environ.setdefault("YOLO_VERBOSE", "False")

with contextlib.redirect_stdout(sys.stderr):
    from ultralytics import SAM, YOLO


class Worker:
    def __init__(self):
        with contextlib.redirect_stdout(sys.stderr):
            self.yolo = YOLO(os.environ["BLOCKADE_YOLO_MODEL"])
            self.sam = SAM(os.environ["BLOCKADE_SAM_MODEL"])

    def observe(self, image: bytes, request_id: str):
        array = cv2.imdecode(np.frombuffer(image, np.uint8), cv2.IMREAD_COLOR)
        if array is None:
            raise ValueError("image is not a supported encoded image")
        with contextlib.redirect_stdout(sys.stderr):
            detections = self.yolo(array, verbose=False)[0]
        observations = []
        for index, box in enumerate(detections.boxes):
            x1, y1, x2, y2 = [float(value) for value in box.xyxy[0].cpu().tolist()]
            model_class = int(box.cls[0].cpu().item())
            confidence = float(box.conf[0].cpu().item())
            label = detections.names.get(model_class, str(model_class))
            observations.append({
                "kind": "object",
                "label": label,
                "confidence": confidence,
                "region": {
                    "x": x1,
                    "y": y1,
                    "width": x2 - x1,
                    "height": y2 - y1,
                },
                "evidence": f"{request_id}:detection-{index}",
            })
        if observations:
            boxes = [
                [
                    item["region"]["x"],
                    item["region"]["y"],
                    item["region"]["x"] + item["region"]["width"],
                    item["region"]["y"] + item["region"]["height"],
                ]
                for item in observations
            ]
            with contextlib.redirect_stdout(sys.stderr):
                masks = self.sam(array, bboxes=boxes, verbose=False)[0].masks
            if masks is not None:
                for item, mask in zip(observations, masks.data):
                    png = (mask.cpu().numpy() * 255).astype(np.uint8)
                    ok, encoded = cv2.imencode(".png", png)
                    if ok:
                        import base64

                        item["mask"] = base64.b64encode(encoded.tobytes()).decode("ascii")
        return observations
