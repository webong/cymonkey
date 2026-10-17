# Generation kit

The generation kit holds the neutral contracts for generation: messages,
streaming, structured output, embeddings, speech and transcription, with
validation and an explicitly populated model catalog. It contains no vendor
client or provider switch; hosts register bindings and optional packages such as
`pkg/content-endpoints/generation` supply API dialects.

It is a kit: contracts and registration with no engine behind them, so the host
and guests can both import it. Content observation and interpretation live in
`pkg/contentkit`.
