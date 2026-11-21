using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Threading.Tasks;
using Microsoft.Web.WebView2.Core;
using Webong.Ext.Web.Windows;

namespace Cymonkey.Windows.WebView2
{
    public sealed class ExtensionChangeRequest
    {
        public string Operation { get; }
        public string Subject { get; }
        public string ProfileName { get; }
        public string ProfilePath { get; }

        public ExtensionChangeRequest(string operation, string subject, string profileName, string profilePath)
        {
            Operation = operation;
            Subject = subject;
            ProfileName = profileName;
            ProfilePath = profilePath;
        }
    }

    /// <summary>
    /// In-process binding for the host's CoreWebView2 and its selected profile.
    /// Call on the WebView2 UI thread; the host owns transport and approvals.
    /// </summary>
    public sealed class WebView2BrowserHelper : IAsyncDisposable
    {
        private readonly WebView2Workspace workspace;
        private readonly Func<ExtensionChangeRequest, Task<bool>> authorizeExtensionChange;

        public WebView2BrowserHelper(
            CoreWebView2 webView,
            Func<ExtensionChangeRequest, Task<bool>> authorizeExtensionChange)
        {
            workspace = new WebView2Workspace(webView ?? throw new ArgumentNullException(nameof(webView)));
            this.authorizeExtensionChange = authorizeExtensionChange ?? throw new ArgumentNullException(nameof(authorizeExtensionChange));
        }

        public string ProfileName => workspace.ProfileName;
        public string ProfilePath => workspace.ProfilePath;

        public async Task InstallApprovedDocumentScriptAsync(string id, string source, IEnumerable<string> allowedOrigins)
        {
            if (string.IsNullOrWhiteSpace(id)) throw new ArgumentException("Script ID is required.", nameof(id));
            var origins = BrowserScriptPolicy.NormalizeOrigins(allowedOrigins);
            var guarded = BrowserScriptPolicy.WrapApprovedSource(source, origins);
            await workspace.InstallDocumentScriptAsync(id, guarded);
        }

        public void RemoveApprovedDocumentScript(string id)
        {
            workspace.RemoveDocumentScript(id);
        }

        public async Task<bool> ExecuteApprovedScriptNowAsync(string id)
        {
            return await workspace.ExecuteDocumentScriptNowAsync(id) == "true";
        }

        public async Task<BrowserExtensionInfo> InstallApprovedExtensionAsync(string unpackedDirectory)
        {
            if (string.IsNullOrWhiteSpace(unpackedDirectory) || !Path.IsPathRooted(unpackedDirectory))
                throw new ArgumentException("An absolute unpacked extension directory is required.", nameof(unpackedDirectory));
            var fullPath = Path.GetFullPath(unpackedDirectory);
            var manifest = Path.Combine(fullPath, "manifest.json");
            if (!Directory.Exists(fullPath) || !File.Exists(manifest))
                throw new FileNotFoundException("Unpacked extension manifest.json was not found.", manifest);
            await RequireApprovalAsync("extension.install", fullPath);
            return await workspace.InstallExtensionAsync(fullPath);
        }

        public async Task<IReadOnlyList<BrowserExtensionInfo>> ListExtensionsAsync()
        {
            return await workspace.ListExtensionsAsync();
        }

        public async Task SetExtensionEnabledAsync(string extensionId, bool enabled)
        {
            await FindExtensionAsync(extensionId);
            await RequireApprovalAsync(enabled ? "extension.enable" : "extension.disable", extensionId);
            await workspace.SetExtensionEnabledAsync(extensionId, enabled);
        }

        public async Task RemoveExtensionAsync(string extensionId)
        {
            await FindExtensionAsync(extensionId);
            await RequireApprovalAsync("extension.remove", extensionId);
            await workspace.RemoveExtensionAsync(extensionId);
        }

        public ValueTask DisposeAsync() => workspace.DisposeAsync();

        private async Task<BrowserExtensionInfo> FindExtensionAsync(string id)
        {
            if (string.IsNullOrWhiteSpace(id)) throw new ArgumentException("Extension ID is required.", nameof(id));
            var installed = await workspace.ListExtensionsAsync();
            var extension = installed.FirstOrDefault(item => item.Id == id);
            if (extension == null) throw new KeyNotFoundException("Extension is not installed in this WebView2 profile.");
            return extension;
        }

        private async Task RequireApprovalAsync(string operation, string subject)
        {
            var request = new ExtensionChangeRequest(operation, subject, ProfileName, ProfilePath);
            if (!await authorizeExtensionChange(request))
                throw new UnauthorizedAccessException("WebView2 extension change was not approved.");
        }

    }
}
