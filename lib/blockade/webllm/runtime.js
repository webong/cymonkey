const token = new URL(location.href).searchParams.get("token");
const endpoint = (path) => `${path}${path.includes("?") ? "&" : "?"}token=${encodeURIComponent(token)}`;

async function post(path, body) {
  const response = await fetch(endpoint(path), {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!response.ok) throw new Error(`${path} returned HTTP ${response.status}`);
}

async function complete(body) {
  const response = await fetch(endpoint("/complete"), {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
  });
  // The Blockade request may have timed out while WebLLM was generating. The
  // result is then intentionally discarded and the loaded engine stays usable.
  if (response.status === 404) return;
  if (!response.ok) throw new Error(`/complete returned HTTP ${response.status}`);
}

const safeMessage = (error) => String(error?.message ?? error ?? "unknown failure").slice(0, 512);
const report = (state, detail = "") => post("/status", { state, detail }).catch(() => {});

function imageMime(base64Image) {
  if (base64Image.startsWith("iVBOR")) return "image/png";
  if (base64Image.startsWith("/9j/")) return "image/jpeg";
  throw Object.assign(new Error("WebLLM accepts PNG or JPEG input"), { blockadeKind: "invalid_request" });
}

async function imageDimensions(dataURL) {
  const response = await fetch(dataURL);
  const bitmap = await createImageBitmap(await response.blob());
  const dimensions = { width: bitmap.width, height: bitmap.height };
  bitmap.close();
  return dimensions;
}

const observationSchema = JSON.stringify({
  type: "object",
  properties: {
    observations: {
      type: "array",
      items: {
        type: "object",
        properties: {
          kind: { type: "string", minLength: 1 },
          label: { type: "string", minLength: 1 },
          confidence: { type: "number", minimum: 0, maximum: 1 },
          region: {
            type: "object",
            properties: {
              x: { type: "number", minimum: 0 },
              y: { type: "number", minimum: 0 },
              width: { type: "number", minimum: 0 },
              height: { type: "number", minimum: 0 },
            },
            required: ["x", "y", "width", "height"],
            additionalProperties: false,
          },
        },
        required: ["kind", "label", "confidence", "region"],
        additionalProperties: false,
      },
    },
  },
  required: ["observations"],
  additionalProperties: false,
});

async function start() {
  await report("loading", "Loading WebLLM runtime");
  const configResponse = await fetch(endpoint("/config"));
  if (!configResponse.ok) throw new Error(`runtime config returned HTTP ${configResponse.status}`);
  const config = await configResponse.json();
  const webllm = await import(config.moduleURL);
  const workerSource = `import * as webllm from ${JSON.stringify(config.moduleURL)};\n` +
    `const handler = new webllm.WebWorkerMLCEngineHandler();\n` +
    `self.onmessage = (event) => handler.onmessage(event);\n`;
  const workerURL = URL.createObjectURL(new Blob([workerSource], { type: "text/javascript" }));
  const worker = new Worker(workerURL, { type: "module", name: "blockade-webllm" });
  let lastProgressAt = 0;
  const engine = await webllm.CreateWebWorkerMLCEngine(
    worker,
    config.model,
    {
      logLevel: "WARN",
      initProgressCallback: (progress) => {
        const now = Date.now();
        if (now - lastProgressAt > 1000) {
          lastProgressAt = now;
          report("loading", String(progress.text ?? "Loading WebLLM model"));
        }
      },
    },
    { context_window_size: config.contextWindowSize },
  );
  URL.revokeObjectURL(workerURL);
  await report("ready", `WebLLM model ${config.model} is ready`);

  for (;;) {
    const next = await fetch(endpoint("/next"));
    if (next.status === 204) return;
    if (!next.ok) throw new Error(`next request returned HTTP ${next.status}`);
    const call = await next.json();
    const cancellation = new AbortController();
    fetch(endpoint(`/cancel?id=${encodeURIComponent(call.id)}`), { signal: cancellation.signal })
      .then((response) => {
        if (response.status === 204) engine.interruptGenerate();
      })
      .catch((error) => {
        if (error?.name !== "AbortError") console.warn("Blockade cancellation watcher failed", error);
      });
    try {
      const request = call.request.request;
      const dataURL = `data:${imageMime(request.image)};base64,${request.image}`;
      const dimensions = await imageDimensions(dataURL);
      const objective = request.prompt?.trim() || "Describe the visually important objects, text, controls, and scene details.";
      const prompt = [
        "You are Blockade's read-only visual observation backend.",
        `The image is ${dimensions.width} by ${dimensions.height} pixels.`,
        "Return observations as JSON matching the supplied schema.",
        "Every region must use pixel coordinates in the original image coordinate system.",
        "Use kind values such as object, text, control, or scene. Confidence must be between 0 and 1.",
        "For a whole-image scene observation, use x=0, y=0 and the full image width and height.",
        `Observation objective: ${objective}`,
      ].join("\n");
      const completion = await engine.chat.completions.create({
        model: config.model,
        stream: false,
        temperature: 0.1,
        max_tokens: config.maxTokens,
        response_format: { type: "json_object", schema: observationSchema },
        messages: [{
          role: "user",
          content: [
            { type: "text", text: prompt },
            { type: "image_url", image_url: { url: dataURL } },
          ],
        }],
      });
      const content = completion.choices?.[0]?.message?.content;
      if (typeof content !== "string") {
        throw Object.assign(new Error("WebLLM returned no response content"), { blockadeKind: "invalid_response" });
      }
      let decoded;
      try {
        decoded = JSON.parse(content);
      } catch (error) {
        throw Object.assign(new Error(`WebLLM returned invalid JSON: ${safeMessage(error)}`), { blockadeKind: "invalid_response" });
      }
      if (!Array.isArray(decoded.observations)) {
        throw Object.assign(new Error("WebLLM response has no observations array"), { blockadeKind: "invalid_response" });
      }
      for (const observation of decoded.observations) {
        if (observation === null || typeof observation !== "object" || Array.isArray(observation)) {
          throw Object.assign(new Error("WebLLM returned a malformed observation"), { blockadeKind: "invalid_response" });
        }
        observation.evidence = `webllm:${config.model}`;
      }
      cancellation.abort();
      await complete({
        id: call.id,
        result: {
          apiVersion: "blockade.provider-adapter/v1alpha1",
          response: {
            apiVersion: "blockade.observation/v1alpha1",
            requestId: request.requestId,
            observations: decoded.observations,
          },
        },
      });
    } catch (error) {
      cancellation.abort();
      await complete({
        id: call.id,
        error: { kind: error?.blockadeKind || "unavailable", message: safeMessage(error) },
      });
    }
  }
}

addEventListener("unhandledrejection", (event) => report("error", safeMessage(event.reason)));
start().catch((error) => report("error", safeMessage(error)));
