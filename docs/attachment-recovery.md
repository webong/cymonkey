# Interaction attachment recovery

Cymonkey preserves an interaction instance when its adapter worker, relay
connection, or target endpoint becomes unavailable. Recovery replaces only the
Cymonkey-owned attachment. It never starts, stops, or restarts a browser,
Unity or Unreal application, container, VM, display, relay, or Xallet session.

## Recovery triggers

The interaction provider begins recovery when:

- an adapter emits a terminal `failed`, `exited`, or unexpected
  `disconnected` lifecycle event;
- an adapter event stream closes unexpectedly; or
- two consecutive active instance health probes report `unhealthy`.

The provider changes the instance to `recovering`, releases the failed adapter
instance, and calls the same adapter with the same engine specification and
prepared caller-owned target descriptor. Failed attachment attempts use
exponential backoff from 250 milliseconds up to 10 seconds. A successful
attachment returns the existing provider instance to `connected`.

Recovery produces provider events in addition to the adapter's original
terminal event:

| Event | Meaning |
| --- | --- |
| `instance.recovering` | Cymonkey started replacing its attachment. |
| `instance.recovery.retrying` | An attachment attempt failed and will be retried. |
| `instance.recovered` | A replacement attachment passed the adapter handshake. |

Resolved credentials and TLS material remain in memory and use the existing
redaction rules. Deleting the instance or shutting down the provider cancels
recovery before releasing that connection material.

## Action safety

Cymonkey does not automatically replay a failed semantic call. In particular,
an `act` request may have reached the target before its response connection
failed, so replay could perform a write twice. The caller observes the error,
waits for `instance.recovered`, describes the current target state, and decides
whether the action is safe to issue again.

## Process restart boundary

Attachment recovery is intentionally in-process. If the Cymonkey provider
itself restarts, the caller that owns desired state reconnects its instances by
submitting their target descriptors again. Persisting or discovering target
placement inside Cymonkey would make it a target supervisor and violate the
architecture boundary. Xallet may perform this reconciliation, but the same
rule applies to a native launcher or any other caller.

Credential and TLS renewal are separate from failure recovery: renewal swaps
connection material on a live instance, while attachment recovery replaces a
failed adapter instance.
