using System;
using System.Collections.Generic;
using System.Linq;
using System.Threading.Tasks;

namespace Microsoft.Web.WebView2.Core
{
    // Test doubles for an environment without the Windows WebView2 runtime.
    public sealed class CoreWebView2
    {
        public CoreWebView2Profile Profile { get; } = new CoreWebView2Profile();
        public readonly Dictionary<string, string> Scripts = new Dictionary<string, string>();
        public string LastExecutedScript { get; private set; }
        private int sequence;

        public Task<string> AddScriptToExecuteOnDocumentCreatedAsync(string source)
        {
            var id = (++sequence).ToString();
            Scripts[id] = source;
            return Task.FromResult(id);
        }

        public void RemoveScriptToExecuteOnDocumentCreated(string id) { Scripts.Remove(id); }
        public Task<string> ExecuteScriptAsync(string source)
        {
            LastExecutedScript = source;
            return Task.FromResult("true");
        }
    }

    public sealed class CoreWebView2Profile
    {
        public string ProfileName { get; set; } = "owner-profile";
        public string ProfilePath { get; set; } = "C:\\owner\\profile";
        public readonly List<CoreWebView2BrowserExtension> Extensions = new List<CoreWebView2BrowserExtension>();
        public Task<CoreWebView2BrowserExtension> AddBrowserExtensionAsync(string path)
        {
            var extension = new CoreWebView2BrowserExtension("test-id", "Test extension");
            Extensions.Add(extension);
            return Task.FromResult(extension);
        }
        public Task<IReadOnlyList<CoreWebView2BrowserExtension>> GetBrowserExtensionsAsync()
        {
            return Task.FromResult((IReadOnlyList<CoreWebView2BrowserExtension>)Extensions.Where(item => !item.Removed).ToArray());
        }
    }

    public sealed class CoreWebView2BrowserExtension
    {
        public string Id { get; }
        public string Name { get; }
        public bool IsEnabled { get; private set; } = true;
        public bool Removed { get; private set; }
        public CoreWebView2BrowserExtension(string id, string name) { Id = id; Name = name; }
        public Task EnableAsync(bool enabled) { IsEnabled = enabled; return Task.CompletedTask; }
        public Task RemoveAsync() { Removed = true; return Task.CompletedTask; }
    }
}
