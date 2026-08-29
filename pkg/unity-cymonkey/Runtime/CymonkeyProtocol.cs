using System;
using Newtonsoft.Json;
using Newtonsoft.Json.Linq;

namespace Jangolova.Cymonkey
{
    public static class CymonkeyProtocol
    {
        public const string Version = "jangolova.cymonkey/v1alpha2";
        public const string DomainRender = "render";
        public const string RuntimeUnity = "unity";
        public const string DriverCymonkeyWebSocket = "cymonkey-ws";
        public const string PrefixEnvironmentVariable = "JANGOLOVA_CYMONKEY_PREFIX";
        public const string TokenEnvironmentVariable = "JANGOLOVA_CYMONKEY_TOKEN";
        public const int MaximumMessageBytes = 4 * 1024 * 1024;
    }

    [Serializable]
    public sealed class CymonkeyRegistration
    {
        [Tooltip("Stable kind-prefixed ID, for example object:hero.")]
        public string id;
        public CymonkeyResourceKind kind;
        public string label;
        [Tooltip("The explicitly exposed Unity object. Unregistered objects are invisible to Cymonkey.")]
        public UnityEngine.Object target;
        [Tooltip("Explicit action allowlist for this resource.")]
        public string[] actions = Array.Empty<string>();
    }

    public enum CymonkeyResourceKind
    {
        scene, @object, ui, camera, material, animation, timeline, artifact, @event
    }

    public sealed class CymonkeyCallException : Exception
    {
        public CymonkeyCallException(string code, string message) : base(message) { Code = code; }
        public string Code { get; private set; }
    }

    internal sealed class WireRequest
    {
        [JsonProperty("id")] public ulong Id { get; set; }
        [JsonProperty("method")] public string Method { get; set; }
        [JsonProperty("params")] public JObject Params { get; set; }
    }
}
