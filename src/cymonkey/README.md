# Cymonkey core

This directory is the standalone core of the Jangolova Cymonkey product. It
can be extracted into its own repository without bringing Jangolova's browser
extension, native helper, target lifecycle, credentials, or driver clients.

The public wire namespace remains `jangolova.cymonkey/v1alpha2`. That is
product identity, not a dependency on the Jangolova host implementation.

## Owns

- portable target, endpoint, policy, caller, attachment, module, and registry
  contracts;
- composite capability routing and portable conformance checks;
- the canonical schemas in `protocol/`;
- the conformance suite in `conformance/`.

## Does not own

- CDP, WebDriver BiDi, Playwright, Puppeteer, Safari MCP, or WebExtension
  clients;
- browser-extension packaging, userscript installation, storage, or network
  permissions;
- macOS process launch, Apple Events, Accessibility consent, or native helper
  signing;
- runtime lifecycle, credentials, or target discovery.

Those are host responsibilities. In this repository Jangolova's reference
integrations live in `integrations/jangolova/cymonkey/`.

## Verify

```bash
go test ./src/cymonkey
node --test src/cymonkey/conformance/contract.mjs
```
