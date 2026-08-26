# Blockade model cache

Model weights are deliberately not committed to the repository. Put the
files required by the local fixture in this directory or point the launcher at
another absolute directory with `BLOCKADE_MODEL_CACHE`.

Default filenames:

- `yolo11n.pt`
- `sam2_b.pt`

The files must be compatible with the Ultralytics version pinned in
`deploy/blockade/requirements.txt`. Keep the cache outside Git and do not put
credentials in model paths or configuration.
