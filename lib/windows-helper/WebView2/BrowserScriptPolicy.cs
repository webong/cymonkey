using System;
using System.Collections.Generic;
using System.Globalization;
using System.Linq;
using System.Text;

namespace Cymonkey.Windows.WebView2
{
    /// <summary>Pure scope checks shared by immediate and future-document injection.</summary>
    public static class BrowserScriptPolicy
    {
        public static IReadOnlyList<string> NormalizeOrigins(IEnumerable<string> approvedOrigins)
        {
            if (approvedOrigins == null) throw new ArgumentNullException(nameof(approvedOrigins));
            var input = approvedOrigins.ToArray();
            if (input.Length == 0) throw new ArgumentException("At least one HTTPS origin is required.");
            var result = new SortedSet<string>(StringComparer.Ordinal);
            foreach (var value in input)
            {
                Uri uri;
                if (!Uri.TryCreate(value, UriKind.Absolute, out uri) ||
                    uri.Scheme != Uri.UriSchemeHttps ||
                    string.IsNullOrEmpty(uri.IdnHost) ||
                    !string.IsNullOrEmpty(uri.UserInfo) ||
                    uri.AbsolutePath != "/" ||
                    !string.IsNullOrEmpty(uri.Query) ||
                    !string.IsNullOrEmpty(uri.Fragment))
                {
                    throw new ArgumentException("Approvals must be exact HTTPS origins.", nameof(approvedOrigins));
                }
                var host = uri.HostNameType == UriHostNameType.IPv6
                    ? "[" + uri.IdnHost.ToLowerInvariant() + "]"
                    : uri.IdnHost.ToLowerInvariant();
                result.Add("https://" + host + (uri.IsDefaultPort ? "" : ":" + uri.Port.ToString(CultureInfo.InvariantCulture)));
            }
            return result.ToArray();
        }

        public static string WrapApprovedSource(string source, IReadOnlyList<string> origins)
        {
            if (string.IsNullOrEmpty(source)) throw new ArgumentException("Approved script source is required.", nameof(source));
            if (origins == null || origins.Count == 0) throw new ArgumentException("Origins are required.", nameof(origins));
            var checks = string.Join(" && ", origins.Select(origin => "location.origin !== " + JavaScriptString(origin)));
            return "(() => { if (" + checks + ") return false;\n" + source + "\nreturn true; })()";
        }

        private static string JavaScriptString(string value)
        {
            var output = new StringBuilder("\"");
            foreach (var ch in value)
            {
                switch (ch)
                {
                    case '"': output.Append("\\\""); break;
                    case '\\': output.Append("\\\\"); break;
                    case '\n': output.Append("\\n"); break;
                    case '\r': output.Append("\\r"); break;
                    case '\t': output.Append("\\t"); break;
                    default:
                        if (ch < 0x20 || ch == '\u2028' || ch == '\u2029')
                            output.Append("\\u" + ((int)ch).ToString("x4", CultureInfo.InvariantCulture));
                        else output.Append(ch);
                        break;
                }
            }
            return output.Append('"').ToString();
        }
    }
}
