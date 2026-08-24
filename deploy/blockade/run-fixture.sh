#!/bin/sh
set -eu

IMAGE="${BLOCKADE_IMAGE:-jangolova/blockade:yolo-sam}"
MODEL_CACHE="${BLOCKADE_MODEL_CACHE:-$PWD/.cache/blockade/models}"
YOLO_MODEL="${BLOCKADE_YOLO_MODEL_FILE:-yolo11n.pt}"
SAM_MODEL="${BLOCKADE_SAM_MODEL_FILE:-sam2_b.pt}"

case "$MODEL_CACHE" in
  /*) ;;
  *) echo "BLOCKADE_MODEL_CACHE must be an absolute path: $MODEL_CACHE" >&2; exit 2 ;;
esac

if [ ! -f "$MODEL_CACHE/$YOLO_MODEL" ]; then
  echo "missing YOLO weights: $MODEL_CACHE/$YOLO_MODEL" >&2
  echo "place the model file in the cache or set BLOCKADE_YOLO_MODEL_FILE" >&2
  exit 1
fi
if [ ! -f "$MODEL_CACHE/$SAM_MODEL" ]; then
  echo "missing SAM weights: $MODEL_CACHE/$SAM_MODEL" >&2
  echo "place the model file in the cache or set BLOCKADE_SAM_MODEL_FILE" >&2
  exit 1
fi

docker run --rm -it \
  -p "${BLOCKADE_BIND:-127.0.0.1:8091}:8091" \
  -v "$MODEL_CACHE:/models:ro" \
  -e "BLOCKADE_YOLO_MODEL=/models/$YOLO_MODEL" \
  -e "BLOCKADE_SAM_MODEL=/models/$SAM_MODEL" \
  "$IMAGE"
