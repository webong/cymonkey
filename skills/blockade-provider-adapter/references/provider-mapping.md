# Provider response mapping

Prefer provider-enforced JSON schema or tool output over parsing prose. Keep
the raw provider shape in the provider package and expose only Blockade types
outside its mapper.

## Mapping table

| Provider value | Blockade value | Rule |
| --- | --- | --- |
| detection/object category | `kind: object`, `label` | Preserve the provider label; apply a configured label map only when the integration documents it. |
| OCR span | `kind: text`, `label` | Put recognized text in `label`; use its bounding region. |
| grounded region | `kind: region`, `label` | Use the provider phrase or class as the label. |
| image-level description | `kind: description`, `label` | Use the full image region only when the provider explicitly returned an image-level result. |
| segmentation result | `mask` plus `region` | Encode the binary/alpha mask as base64 PNG and derive or verify its pixel bounds. |
| provider/model provenance | `evidence` | Use a short value such as `provider:model:request-id`; exclude secrets, prompts, and raw responses. |

`kind` remains an open string in the current schema, but prefer the established
values above so callers do not need provider-specific branches.

## Coordinates

Decode the source image dimensions once. Convert all geometry to source-image
pixel coordinates before constructing observations.

- Normalized `[0,1]`: multiply x values by image width and y values by image
  height.
- Normalized `[0,1000]`: divide by 1000, then scale by image dimensions.
- Corner boxes `[x1,y1,x2,y2]`: emit `x=x1`, `y=y1`,
  `width=x2-x1`, `height=y2-y1`.
- Center boxes `[cx,cy,w,h]`: emit `x=cx-w/2`, `y=cy-h/2`.

Normalize reversed corners, reject non-finite numbers, and clamp small numeric
overshoots to the image bounds. Reject geometry that remains invalid after the
documented conversion. Do not mistake coordinates from a resized provider
input for source-image coordinates; reverse the resize/letterbox transform.

An image-level result may use `{x: 0, y: 0, width: imageWidth,
height: imageHeight}`. Do not use that fallback for a supposedly localized
object merely because the provider omitted its box.

## Confidence

Preserve a numeric provider score and convert percentages to `[0,1]`. Do not
silently assign `1.0` when the provider supplies no score. Prefer a structured
provider request that asks for the required score. If the product deliberately
supports scoreless VLM output, require an explicit, documented confidence
policy in Blockade adapter configuration and identify that policy in evidence; never
present a configured default as provider confidence.

Provider-reported VLM confidence may be uncalibrated. Document that limitation
and avoid applying detector thresholds to it unless the integration has an
explicit threshold policy.

## Labels, descriptions, and masks

- Trim labels, reject empty labels, and preserve Unicode.
- Keep descriptions concise enough for the current contract; raw reasoning or
  chain-of-thought is not observation evidence.
- Verify mask dimensions against the source image or its declared region.
- Convert polygon/RLE masks deterministically and test the conversion with a
  small fixture.

## Tests

For each provider response family, test at least one real-shaped redacted
fixture and the corresponding failure modes. Assert semantic output values and
run Blockade response validation; tests that only compare JSON text are not
sufficient.
