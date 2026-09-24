# Vision

Cymonkey gives agents and applications a common way to use and extend a system
of display and inference interfaces. A caller chooses a target and supplies
the connection; Cymonkey coordinates capabilities, policy, approvals, and
module composition without taking ownership of that target.

```text
caller or agent
  → Cymonkey: enter, authorize, compose, and extend
    → Jangolova: operate and present through display interfaces
    → Blockade: infer through image and sound interfaces
```

Jangolova attaches to caller-owned browsers, desktop surfaces, and render
runtimes. Its adapters expose bounded semantic operations through
`hello`, `capabilities`, `describe`, `act`, and `events`. Those operations can
drive existing interfaces or present dynamic ones. Jangolova does not
provision the browser, application, display, or rendering process.

Blockade accepts inference requests and returns normalized evidence without
controlling the source. Its boundary includes local models and separately
registered cloud adapters. The implemented request today takes an image and
optional prompt; sound inference is an intended next interface and has no
public request contract yet.

The modules can be used independently. Cymonkey is the common entry and
extension layer that lets callers combine them under one policy and target
ownership model. For the current image workflow, Cymonkey obtains an approved
screenshot through Jangolova and sends its pixels to Blockade. Future sound
workflows should use an explicit sound contract under Blockade, without making
Jangolova responsible for inference.
