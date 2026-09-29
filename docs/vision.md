# Vision

Cymonkey gives agents and applications a common way to use and extend a system
of display and inference interfaces. A caller chooses a target and supplies
the connection; Cymonkey coordinates capabilities, policy, approvals, and
module composition without taking ownership of that target.

```text
caller or agent
  → Cymonkey: enter, authorize, compose, and extend
    → Jangolova: operate and present through display interfaces
    → Blockade: interpret text, image, and sound content with models
```

Jangolova attaches to caller-owned browsers, desktop surfaces, and render
runtimes. Its adapters expose bounded semantic operations through
`hello`, `capabilities`, `describe`, `act`, and `events`. Those operations can
drive existing interfaces or present dynamic ones. Jangolova does not
provision the browser, application, display, or rendering process.

Blockade observes and interprets supplied content using local models and
separately registered cloud adapters. Its scope covers text, images, and sound;
it returns normalized evidence or interpretation without controlling the
source. The image observation request takes an image and optional prompt.
The content interpretation request accepts text, image, and audio parts through
capable provider adapters; no text or audio model ships in the base library.

The modules can be used independently. Cymonkey is the common entry and
extension layer that lets callers combine them under one policy and target
ownership model. For the current image workflow, Cymonkey obtains an approved
screenshot through Jangolova and sends its pixels to Blockade. Text and sound
workflows use Blockade's content contract, without making
Jangolova responsible for interpretation.
