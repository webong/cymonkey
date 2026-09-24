# Cymonkey macOS browser adapter

This Swift library provides the managed runtime controller, configuration
models, and source-free userscript catalog previously embedded in Cymonkey's
Safari app container. It has no app executable, Safari WebExtension, Xcode
project, or bundled browser product. An integrating macOS application can
import `JangolovaMacCore` and supply its own UI and host lifecycle.

```sh
swift test --package-path pkg/macos-browser-adapter
```
