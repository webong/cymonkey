using System;
using System.IO;
using System.Threading.Tasks;
using Cymonkey.Windows.WebView2;
using Microsoft.Web.WebView2.Core;

public static class WebView2BrowserHelperTests
{
    public static int Main()
    {
        try { Run().GetAwaiter().GetResult(); return 0; }
        catch (Exception error) { Console.Error.WriteLine(error); return 1; }
    }

    private static async Task Run()
    {
        var view = new CoreWebView2();
        var allowed = false;
        var helper = new WebView2BrowserHelper(view, request =>
        {
            Assert(request.ProfileName == "owner-profile", "approval must carry the selected profile");
            Assert(request.ProfilePath == "C:\\owner\\profile", "approval must carry the selected profile path");
            return Task.FromResult(allowed);
        });
        await helper.InstallApprovedDocumentScriptAsync("reading", "window.reading = true", new[] { "https://example.com" });
        Assert(view.Scripts.Count == 1, "document script was not registered");
        Assert(await helper.ExecuteApprovedScriptNowAsync("reading"), "approved script did not execute");
        Assert(view.LastExecutedScript.Contains("location.origin"), "script lacks origin guard");
        helper.RemoveApprovedDocumentScript("reading");
        Assert(view.Scripts.Count == 0, "script was not removed");

        var rejected = false;
        try { BrowserScriptPolicy.NormalizeOrigins(new[] { "https://example.com/private" }); }
        catch (ArgumentException) { rejected = true; }
        Assert(rejected, "path-scoped input widened to an origin");

        var extensionDir = Path.Combine(Path.GetTempPath(), "cymonkey-webview2-test-" + Guid.NewGuid().ToString("N"));
        Directory.CreateDirectory(extensionDir);
        try
        {
            File.WriteAllText(Path.Combine(extensionDir, "manifest.json"), "{}");
            rejected = false;
            try { await helper.InstallApprovedExtensionAsync(extensionDir); }
            catch (UnauthorizedAccessException) { rejected = true; }
            Assert(rejected && view.Profile.Extensions.Count == 0, "denied extension was installed");
            allowed = true;
            var installed = await helper.InstallApprovedExtensionAsync(extensionDir);
            Assert(installed.Id == "test-id" && installed.Enabled, "extension was not installed");
            await helper.SetExtensionEnabledAsync(installed.Id, false);
            Assert(!(await helper.ListExtensionsAsync())[0].Enabled, "extension was not disabled");
            await helper.RemoveExtensionAsync(installed.Id);
            Assert((await helper.ListExtensionsAsync()).Count == 0, "extension was not removed");
        }
        finally { Directory.Delete(extensionDir, true); }
        Console.WriteLine("WebView2 helper policy tests passed.");
    }

    private static void Assert(bool condition, string message)
    {
        if (!condition) throw new Exception(message);
    }
}
