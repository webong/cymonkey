//
//  SafariWebExtensionHandler.swift
//  Jangolova Extension
//
//  Created by Wisdom Ebong on 02/08/2026.
//

import SafariServices

class SafariWebExtensionHandler: NSObject, NSExtensionRequestHandling {

    func beginRequest(with context: NSExtensionContext) {
        let request = context.inputItems.first as? NSExtensionItem

        let message: Any?
        if #available(iOS 15.0, macOS 11.0, *) {
            message = request?.userInfo?[SFExtensionMessageKey]
        } else {
            message = request?.userInfo?["message"]
        }

        let response = NSExtensionItem()
        let result = handle(message)
        if #available(iOS 15.0, macOS 11.0, *) {
            response.userInfo = [ SFExtensionMessageKey: result ]
        } else {
            response.userInfo = [ "message": result ]
        }

        context.completeRequest(returningItems: [ response ], completionHandler: nil)
    }

    private func handle(_ message: Any?) -> [String: Any] {
        guard let request = message as? [String: Any],
              request["method"] as? String == "userscripts.catalog.replace",
              let rawValues = request["values"] as? [[String: Any]] else {
            return ["ok": false, "error": "unsupported native message"]
        }
        let values = rawValues.prefix(500).compactMap(sanitize)
        guard JSONSerialization.isValidJSONObject(values),
              let encoded = try? JSONSerialization.data(withJSONObject: values),
              let defaults = UserDefaults(suiteName: "group.dev.jangolova.shared") else {
            return ["ok": false, "error": "userscript catalog is invalid"]
        }
        defaults.set(encoded, forKey: "cymonkey.userscripts.catalog.v1")
        return ["ok": true, "count": values.count]
    }

    private func sanitize(_ value: [String: Any]) -> [String: Any]? {
        guard let id = value["id"] as? String, id.count <= 128,
              let name = value["name"] as? String, name.count <= 128,
              let revision = value["revision"] as? String, revision.count <= 128,
              let enabled = value["enabled"] as? Bool,
              let matches = value["matches"] as? [String], matches.count <= 128,
              matches.allSatisfy({ $0.count <= 512 }) else { return nil }
        return ["id": id, "name": name, "revision": revision, "enabled": enabled, "matches": matches]
    }

}
