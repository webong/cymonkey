//
//  AppDelegate.swift
//  Jangolova
//
//  Created by Wisdom Ebong on 02/08/2026.
//

import ApplicationServices
import Cocoa
import JangolovaMacCore
import SafariServices

@main
class AppDelegate: NSObject, NSApplicationDelegate {
    private let statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
    private let runtime = ManagedRuntimeController()
    private let catalog = UserscriptCatalog(appGroup: "group.dev.jangolova.shared")
    private var runtimeStatus = ManagedRuntimeStatus.stopped

    func applicationDidFinishLaunching(_ notification: Notification) {
        statusItem.button?.title = "C"
        rebuildMenu()
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        return false
    }

    private func rebuildMenu() {
        let menu = NSMenu()
        let title = NSMenuItem(title: "Cymonkey for macOS", action: nil, keyEquivalent: "")
        title.isEnabled = false
        menu.addItem(title)
        let runtimeItem = NSMenuItem(title: runtimeLabel, action: nil, keyEquivalent: "")
        runtimeItem.isEnabled = false
        menu.addItem(runtimeItem)
        let accessibility = NSMenuItem(title: "Accessibility: \(AXIsProcessTrusted() ? "Granted" : "Not Granted")", action: nil, keyEquivalent: "")
        accessibility.isEnabled = false
        menu.addItem(accessibility)
        menu.addItem(.separator())
        let toggle = NSMenuItem(title: runtimeStatus == .stopped ? "Start Managed Cymonkey" : "Stop Managed Cymonkey", action: #selector(toggleRuntime), keyEquivalent: "")
        toggle.target = self
        menu.addItem(toggle)
        let userscripts = NSMenuItem(title: "Userscripts", action: nil, keyEquivalent: "")
        userscripts.submenu = NSMenu(title: "Userscripts")
        menu.addItem(userscripts)
        let safari = NSMenuItem(title: "Safari Extension Preferences…", action: #selector(openSafariPreferences), keyEquivalent: "")
        safari.target = self
        menu.addItem(safari)
        menu.addItem(.separator())
        let quit = NSMenuItem(title: "Quit Cymonkey", action: #selector(quit), keyEquivalent: "q")
        quit.target = self
        menu.addItem(quit)
        statusItem.menu = menu
        Task { await populateUserscripts(userscripts.submenu) }
    }

    private var runtimeLabel: String {
        switch runtimeStatus {
        case .stopped: return "Cymonkey: Stopped"
        case .connecting: return "Cymonkey: Connecting"
        case .connected: return "Cymonkey: Connected"
        case .failed: return "Cymonkey: Failed"
        }
    }

    @objc private func toggleRuntime() {
        if runtimeStatus != .stopped {
            Task { await runtime.stop() }
            runtimeStatus = .stopped
            rebuildMenu()
            return
        }
        runtimeStatus = .connecting
        rebuildMenu()
        Task {
            do {
                let configuration = try ManagedRuntimeConfiguration(environment: ProcessInfo.processInfo.environment)
                try await runtime.start(configuration: configuration)
                runtimeStatus = await runtime.status
            } catch {
                runtimeStatus = .failed("managed runtime configuration failed")
            }
            rebuildMenu()
        }
    }

    private func populateUserscripts(_ menu: NSMenu?) async {
        guard let menu else { return }
        let values = (try? await catalog.list()) ?? []
        menu.removeAllItems()
        if values.isEmpty {
            let empty = NSMenuItem(title: "No Safari userscripts", action: nil, keyEquivalent: "")
            empty.isEnabled = false
            menu.addItem(empty)
            return
        }
        for value in values {
            let item = NSMenuItem(title: "\(value.enabled ? "✓" : "–") \(value.name)", action: nil, keyEquivalent: "")
            item.isEnabled = false
            menu.addItem(item)
        }
        let unavailable = NSMenuItem(title: "Safari execution is unavailable on this build", action: nil, keyEquivalent: "")
        unavailable.isEnabled = false
        menu.addItem(.separator())
        menu.addItem(unavailable)
    }

    @objc private func openSafariPreferences() {
        SFSafariApplication.showPreferencesForExtension(withIdentifier: "dev.jangolova.Jangolova.Extension") { _ in }
    }

    @objc private func quit() {
        Task { await runtime.stop() }
        NSApplication.shared.terminate(nil)
    }

}
