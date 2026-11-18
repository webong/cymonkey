using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Threading.Tasks;
using Microsoft.Web.WebView2.Core;

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

    public sealed class BrowserExtensionInfo
    {
        public string Id { get; }
        public string Name { get; }
        public bool Enabled { get; }

        public BrowserExtensionInfo(string id, string name, bool enabled)
        {
            Id = id;
            Name = name;
            Enabled = enabled;
        }
    }

    /// <summary>
    /// In-process binding for the host's CoreWebView2 and its selected profile.
    /// Call on the WebView2 UI thread; the host owns transport and approvals.
    /// </summary>
    public sealed class WebView2BrowserHelper
    {
        private sealed class ApprovedScript
        {
            public string Source;
            public IReadOnlyList<string> Origins;
            public string NativeId;
        }

        private readonly CoreWebView2 webView;
        private readonly Func<ExtensionChangeRequest, Task<bool>> authorizeExtensionChange;
        private readonly Dictionary<string, ApprovedScript> scripts = new Dictionary<string, ApprovedScript>(StringComparer.Ordinal);
        private readonly HashSet<string> pendingScripts = new HashSet<string>(StringComparer.Ordinal);

        public WebView2BrowserHelper(
            CoreWebView2 webView,
            Func<ExtensionChangeRequest, Task<bool>> authorizeExtensionChange)
        {
            this.webView = webView ?? throw new ArgumentNullException(nameof(webView));
            this.authorizeExtensionChange = authorizeExtensionChange ?? throw new ArgumentNullException(nameof(authorizeExtensionChange));
        }

        public string ProfileName => webView.Profile.ProfileName;
        public string ProfilePath => webView.Profile.ProfilePath;

        public async Task InstallApprovedDocumentScriptAsync(string id, string source, IEnumerable<string> allowedOrigins)
        {
            if (string.IsNullOrWhiteSpace(id)) throw new ArgumentException("Script ID is required.", nameof(id));
            var origins = BrowserScriptPolicy.NormalizeOrigins(allowedOrigins);
            var guarded = BrowserScriptPolicy.WrapApprovedSource(source, origins);
            if (scripts.ContainsKey(id) || !pendingScripts.Add(id)) throw new InvalidOperationException("Script ID is already installed.");
            try
            {
                var nativeId = await webView.AddScriptToExecuteOnDocumentCreatedAsync(guarded);
                scripts.Add(id, new ApprovedScript { Source = source, Origins = origins, NativeId = nativeId });
            }
            finally { pendingScripts.Remove(id); }
        }

        public void RemoveApprovedDocumentScript(string id)
        {
            ApprovedScript script;
            if (!scripts.TryGetValue(id, out script)) throw new KeyNotFoundException("Script ID is not installed.");
            webView.RemoveScriptToExecuteOnDocumentCreated(script.NativeId);
            scripts.Remove(id);
        }

        public async Task<bool> ExecuteApprovedScriptNowAsync(string id)
        {
            ApprovedScript script;
            if (!scripts.TryGetValue(id, out script)) throw new KeyNotFoundException("Script ID is not installed.");
            var guarded = BrowserScriptPolicy.WrapApprovedSource(script.Source, script.Origins);
            return await webView.ExecuteScriptAsync(guarded) == "true";
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
            if (!Directory.Exists(fullPath) || !File.Exists(manifest))
                throw new FileNotFoundException("Unpacked extension changed before installation.", manifest);
            var extension = await webView.Profile.AddBrowserExtensionAsync(fullPath);
            return Describe(extension);
        }

        public async Task<IReadOnlyList<BrowserExtensionInfo>> ListExtensionsAsync()
        {
            var installed = await webView.Profile.GetBrowserExtensionsAsync();
            return installed.Select(Describe).OrderBy(value => value.Name, StringComparer.Ordinal).ToArray();
        }

        public async Task SetExtensionEnabledAsync(string extensionId, bool enabled)
        {
            var extension = await FindExtensionAsync(extensionId);
            await RequireApprovalAsync(enabled ? "extension.enable" : "extension.disable", extensionId);
            await extension.EnableAsync(enabled);
        }

        public async Task RemoveExtensionAsync(string extensionId)
        {
            var extension = await FindExtensionAsync(extensionId);
            await RequireApprovalAsync("extension.remove", extensionId);
            await extension.RemoveAsync();
        }

        private async Task<CoreWebView2BrowserExtension> FindExtensionAsync(string id)
        {
            if (string.IsNullOrWhiteSpace(id)) throw new ArgumentException("Extension ID is required.", nameof(id));
            var installed = await webView.Profile.GetBrowserExtensionsAsync();
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

        private static BrowserExtensionInfo Describe(CoreWebView2BrowserExtension extension)
        {
            return new BrowserExtensionInfo(extension.Id, extension.Name, extension.IsEnabled);
        }
    }
}
