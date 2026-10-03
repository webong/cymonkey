# SweetCookieKit source attribution

`Sources/SweetCookieKit` contains the browser cookie reader sources from
[SweetCookieKit](https://github.com/steipete/SweetCookieKit), tag `v0.5.4`,
commit `b118aa1ddec43f3883b72771b94657e3fbf55e27`.

Copyright (c) 2026 Peter Steinberger. Licensed under MIT; see
[`SweetCookieKit-LICENSE`](SweetCookieKit-LICENSE).

The source is included as a separate Swift target because the upstream
SwiftPM manifest requires Swift 6.2 while this repository builds with Swift
6.0.3. The upstream package manifest, documentation bundle, test suite, and
unrelated Chromium local-storage readers are not included. The Chromium and
Gecko SQLite readers have a small local
change to exclude cookies from partitioned or container-scoped rows when
those schema columns are present. `SQLiteCookieScope.swift` implements the
schema check. The Keychain lookup method reference is written as an explicit
closure to satisfy this repository's Swift 6.0 compiler.
