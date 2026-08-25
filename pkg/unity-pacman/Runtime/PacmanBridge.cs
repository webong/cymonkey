using System;
using System.Collections.Generic;
using System.Globalization;
using System.Linq;
using System.Text.RegularExpressions;
using Newtonsoft.Json.Linq;
using UnityEngine;

namespace Jangolova.Pacman
{
    [DisallowMultipleComponent]
    public sealed class PacmanBridge : MonoBehaviour
    {
        private const int MaximumEvents = 256;
        [SerializeField] private List<PacmanRegistration> registrations = new List<PacmanRegistration>();
        private readonly Dictionary<string, PacmanRegistration> allowlist = new Dictionary<string, PacmanRegistration>(StringComparer.Ordinal);
        private readonly List<JObject> events = new List<JObject>();
        private long revision = 1;
        private long eventSequence;
        [SerializeField] private MonoBehaviour transportHost;
        private IPacmanTransportHost activeTransportHost;
        private static readonly Regex StableId = new Regex("^[a-z][a-z0-9-]{0,31}:[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$", RegexOptions.CultureInvariant);

        private void Awake()
        {
            BuildAllowlist();
            if (transportHost == null) return;
            activeTransportHost = transportHost as IPacmanTransportHost;
            if (activeTransportHost == null)
                throw new InvalidOperationException("Pacman transportHost must implement IPacmanTransportHost.");
            activeTransportHost.StartHost(this);
        }

        private void OnDestroy()
        {
            if (activeTransportHost != null) activeTransportHost.Dispose();
        }

        private void BuildAllowlist()
        {
            allowlist.Clear();
            foreach (PacmanRegistration item in registrations)
            {
                if (item == null) throw new InvalidOperationException("Pacman registrations cannot be null.");
                string kind = WireKind(item.kind);
                if (item.target == null || !StableId.IsMatch(item.id ?? "") || !item.id.StartsWith(kind + ":", StringComparison.Ordinal))
                    throw new InvalidOperationException("Every Pacman registration requires a target and a matching stable kind-prefixed ID.");
                if (allowlist.ContainsKey(item.id))
                    throw new InvalidOperationException("Duplicate Pacman resource ID: " + item.id);
                allowlist.Add(item.id, item);
            }
        }

        public JToken Dispatch(string method, JObject parameters)
        {
            switch (method)
            {
                case "hello": return Hello();
                case "capabilities": return Capabilities();
                case "describe": return Describe();
                case "act": return Act(parameters);
                case "events": return Events(parameters);
                case "health": return Health();
                default: throw new PacmanCallException("method_not_found", "Unsupported Pacman method.");
            }
        }

        private JObject Hello()
        {
            return new JObject {
                ["protocolVersion"] = PacmanProtocol.Version,
                ["compatibleProtocols"] = new JArray(PacmanProtocol.CompatibleProtocols),
                ["implementation"] = new JObject { ["name"] = "jangolova-unity-cymonkey", ["version"] = "0.2.0" },
                ["profiles"] = new JArray(PacmanProtocol.ProfileEngine),
                ["backends"] = new JArray(PacmanProtocol.BackendUnity),
                ["features"] = new JArray("events.cursor", "resources.explicit-allowlist")
            };
        }

        private JArray Capabilities()
        {
            return new JArray {
                Capability("resource.describe", "read", EnumKinds(), new JObject { ["type"] = "object", ["additionalProperties"] = false }),
                Capability("object.active.set", "write", new JArray("object", "ui", "camera"), new JObject {
                    ["type"] = "object", ["properties"] = new JObject { ["active"] = new JObject { ["type"] = "boolean" } },
                    ["required"] = new JArray("active"), ["additionalProperties"] = false
                })
            };
        }

        private JObject Describe()
        {
            return new JObject {
                ["revision"] = revision.ToString(CultureInfo.InvariantCulture),
                ["surfaces"] = new JArray(allowlist.Values.OrderBy(item => item.id, StringComparer.Ordinal).Select(DescribeResource)),
                ["augmentations"] = new JArray()
            };
        }

        private JToken Act(JObject parameters)
        {
            string name = parameters.Value<string>("name");
            JObject input = parameters["input"] as JObject ?? new JObject();
            string targetId = input.Value<string>("targetId") ?? parameters.Value<string>("targetId");
            PacmanRegistration item;
            if (string.IsNullOrWhiteSpace(targetId) || !allowlist.TryGetValue(targetId, out item))
                throw new PacmanCallException("target_not_allowlisted", "Cymonkey target is not allowlisted.");
            if (!item.actions.Contains(name, StringComparer.Ordinal))
                throw new PacmanCallException("action_not_allowlisted", "Cymonkey action is not allowlisted for this target.");
            string expectedRevision = input.Value<string>("expectedRevision");
            if (!string.IsNullOrEmpty(expectedRevision) && expectedRevision != revision.ToString(CultureInfo.InvariantCulture))
                throw new PacmanCallException("stale_revision", $"Revision {expectedRevision} is stale; current revision is {revision}.");
            if (name == "resource.describe") return DescribeResource(item);
            if (name == "object.active.set")
            {
                GameObject value = AsGameObject(item.target);
                bool active = input.Value<bool?>("active")
                    ?? throw new PacmanCallException("invalid_input", "active is required.");
                value.SetActive(active);
                revision++;
                Publish("event:resource-changed", targetId, new JObject { ["active"] = active });
                return new JObject { ["ok"] = true, ["revision"] = revision.ToString(CultureInfo.InvariantCulture) };
            }
            throw new PacmanCallException("action_not_implemented", "Allowlisted action has no Unity handler.");
        }

        private JObject Events(JObject query)
        {
            long after; long.TryParse(query.Value<string>("after"), out after);
            int limit = Math.Min(Math.Max(query.Value<int?>("limit") ?? 100, 1), 1000);
            JArray selected = new JArray(events.Where(item => long.Parse(item.Value<string>("id"), CultureInfo.InvariantCulture) > after).Take(limit).Select(item => item.DeepClone()));
            string cursor = selected.Count == 0 ? after.ToString(CultureInfo.InvariantCulture) : ((JObject)selected.Last).Value<string>("id");
            return new JObject { ["events"] = selected, ["cursor"] = cursor };
        }

        private JObject Health()
        {
            return new JObject { ["status"] = "ready", ["observedAt"] = DateTime.UtcNow.ToString("o", CultureInfo.InvariantCulture) };
        }

        private void Publish(string type, string sourceId, JObject data)
        {
            eventSequence++;
            events.Add(new JObject { ["id"] = eventSequence.ToString(CultureInfo.InvariantCulture), ["type"] = type, ["sourceId"] = sourceId, ["occurredAt"] = DateTime.UtcNow.ToString("o", CultureInfo.InvariantCulture), ["data"] = data });
            if (events.Count > MaximumEvents) events.RemoveAt(0);
        }

        private static JObject DescribeResource(PacmanRegistration item)
        {
            GameObject gameObject = item.target as GameObject;
            Component component = item.target as Component;
            if (gameObject == null && component != null) gameObject = component.gameObject;
            return new JObject { ["id"] = item.id, ["profile"] = PacmanProtocol.ProfileEngine, ["kind"] = WireKind(item.kind), ["label"] = item.label, ["properties"] = new JObject { ["active"] = gameObject == null ? (JToken)JValue.CreateNull() : gameObject.activeSelf } };
        }

        private static JObject Capability(string name, string effect, JArray kinds, JObject schema)
        {
            return new JObject {
                ["name"] = name,
                ["profile"] = PacmanProtocol.ProfileEngine,
                ["backend"] = PacmanProtocol.BackendUnity,
                ["support"] = "native",
                ["lifetime"] = "attachment",
                ["persistence"] = "session",
                ["effect"] = effect,
                ["targetKinds"] = kinds,
                ["inputSchema"] = schema
            };
        }
        private static JArray EnumKinds() { return new JArray(Enum.GetValues(typeof(PacmanResourceKind)).Cast<PacmanResourceKind>().Select(WireKind)); }
        private static string WireKind(PacmanResourceKind kind) { return kind == PacmanResourceKind.@object ? "object" : kind == PacmanResourceKind.@event ? "event" : kind.ToString(); }
        private static GameObject AsGameObject(UnityEngine.Object value) { GameObject result = value as GameObject; Component component = value as Component; if (result == null && component != null) result = component.gameObject; if (result == null) throw new PacmanCallException("invalid_target", "Action requires a GameObject or Component."); return result; }
    }
}
