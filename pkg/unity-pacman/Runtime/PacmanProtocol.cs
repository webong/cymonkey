using System;
using Newtonsoft.Json;
using Newtonsoft.Json.Linq;

namespace Jangolova.Pacman
{
    public static class PacmanProtocol
    {
        public const string Version = "jangolova.cymonkey/v1alpha2";
        public static readonly string[] CompatibleProtocols = { "jangolova.pacman/v1alpha1" };
        public const string ProfileEngine = "engine";
        public const string BackendUnity = "engine-unity";
        public const string PrefixEnvironmentVariable = "JANGOLOVA_PACMAN_PREFIX";
        public const string TokenEnvironmentVariable = "JANGOLOVA_PACMAN_TOKEN";
        public const int MaximumMessageBytes = 4 * 1024 * 1024;
    }

    [Serializable]
    public sealed class PacmanRegistration
    {
        [Tooltip("Stable kind-prefixed ID, for example object:hero.")]
        public string id;
        public PacmanResourceKind kind;
        public string label;
        [Tooltip("The explicitly exposed Unity object. Unregistered objects are invisible to Pacman.")]
        public UnityEngine.Object target;
        [Tooltip("Explicit action allowlist for this resource.")]
        public string[] actions = Array.Empty<string>();
    }

    public enum PacmanResourceKind
    {
        scene, @object, ui, camera, material, animation, timeline, artifact, @event
    }

    public sealed class PacmanCallException : Exception
    {
        public PacmanCallException(string code, string message) : base(message) { Code = code; }
        public string Code { get; private set; }
    }

    internal sealed class WireRequest
    {
        [JsonProperty("id")] public ulong Id { get; set; }
        [JsonProperty("method")] public string Method { get; set; }
        [JsonProperty("params")] public JObject Params { get; set; }
    }
}
