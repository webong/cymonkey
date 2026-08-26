using System;
using Newtonsoft.Json;
using Newtonsoft.Json.Linq;

namespace Jangolova.Unity
{
    public static class BridgeProtocol
    {
        public const string Version = "jangolova.bridge/v1alpha1";
        public const string UrlEnvironmentVariable = "JANGOLOVA_BRIDGE_URL";
        public const string TokenEnvironmentVariable = "JANGOLOVA_BRIDGE_TOKEN";
        public const string ProtocolEnvironmentVariable = "JANGOLOVA_BRIDGE_PROTOCOL";
        public const int MaximumMessageBytes = 4 * 1024 * 1024;
    }

    public sealed class BridgeRequest
    {
        [JsonProperty("id")]
        public ulong Id { get; set; }

        [JsonProperty("method")]
        public string Method { get; set; }

        [JsonProperty("params")]
        public JObject Params { get; set; }
    }

    public sealed class BridgeResponse
    {
        [JsonProperty("id")]
        public ulong Id { get; set; }

        [JsonProperty("result", NullValueHandling = NullValueHandling.Ignore)]
        public JToken Result { get; set; }

        [JsonProperty("error", NullValueHandling = NullValueHandling.Ignore)]
        public BridgeError Error { get; set; }
    }

    public sealed class BridgeError
    {
        [JsonProperty("code")]
        public string Code { get; set; }

        [JsonProperty("message")]
        public string Message { get; set; }
    }

    public sealed class BridgeCallException : Exception
    {
        public BridgeCallException(string code, string message)
            : base(message)
        {
            Code = code;
        }

        public string Code { get; private set; }
    }

    public interface IJangolovaBridgeHandler
    {
        JToken Hello();
        JToken Capabilities();
        JToken Describe();
        JToken Act(string name, JObject input);
        JToken Events(JObject query);
    }
}
