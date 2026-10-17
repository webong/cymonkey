using System;
using System.Collections.Generic;
using System.Globalization;
using System.Linq;
using System.Threading;
using Newtonsoft.Json.Linq;
using UnityEngine;

namespace Jangolova.Unity
{
    [DisallowMultipleComponent]
    public sealed class JangolovaSceneBridge :
        MonoBehaviour,
        IJangolovaBridgeHandler
    {
        private const int MaximumEvents = 256;
        private readonly Dictionary<string, JangolovaObject> objects =
            new Dictionary<string, JangolovaObject>();
        private readonly List<JObject> events = new List<JObject>();
        private CancellationTokenSource lifetime;
        private JangolovaBridgeClient client;
        private long eventSequence;

        public bool IsConnected
        {
            get { return client != null && client.IsConnected; }
        }

        private async void Start()
        {
            IndexExistingObjects();
            if (!JangolovaBridgeClient.HasBridgeEnvironment())
                return;

            lifetime = new CancellationTokenSource();
            client = new JangolovaBridgeClient(
                this,
                SynchronizationContext.Current);
            try
            {
                await client.ConnectFromEnvironmentAsync(lifetime.Token);
            }
            catch (OperationCanceledException)
            {
            }
            catch (Exception exception)
            {
                if (lifetime != null && !lifetime.IsCancellationRequested)
                {
                    Debug.LogError(
                        "Jangolova bridge stopped: " + exception.Message,
                        this);
                }
            }
        }

        private void Update()
        {
#if ENABLE_LEGACY_INPUT_MANAGER
            if (!Input.GetMouseButtonDown(0) || Camera.main == null)
                return;
            Ray ray = Camera.main.ScreenPointToRay(Input.mousePosition);
            RaycastHit hit;
            if (!Physics.Raycast(ray, out hit))
                return;
            JangolovaObject selected =
                hit.collider.GetComponentInParent<JangolovaObject>();
            if (selected == null)
                return;
            Publish(
                "object.selected",
                new JObject
                {
                    ["objectId"] = selected.ObjectId,
                    ["x"] = Input.mousePosition.x,
                    ["y"] = Input.mousePosition.y
                });
#endif
        }

        private void OnDestroy()
        {
            if (lifetime != null)
                lifetime.Cancel();
            if (client != null)
                client.Dispose();
            if (lifetime != null)
                lifetime.Dispose();
        }

        public JToken Hello()
        {
            return new JObject
            {
                ["protocolVersion"] = BridgeProtocol.Version,
                ["implementation"] = new JObject
                {
                    ["name"] = "jangolova-unity",
                    ["version"] = "0.1.0"
                },
                ["features"] = new JArray("events.cursor")
            };
        }

        public JToken Capabilities()
        {
            return new JArray
            {
                Capability(
                    "scene.describe",
                    "Return the current Unity scene state.",
                    "read",
                    EmptySchema()),
                Capability(
                    "object.create",
                    "Create a tracked primitive GameObject.",
                    "write",
                    ObjectSchema(
                        new JObject
                        {
                            ["id"] = StringSchema(),
                            ["type"] = new JObject
                            {
                                ["type"] = "string",
                                ["enum"] = new JArray("cube", "sphere", "plane")
                            },
                            ["position"] = VectorSchema(),
                            ["rotation"] = VectorSchema(),
                            ["scale"] = VectorSchema()
                        },
                        "id",
                        "type")),
                Capability(
                    "object.update",
                    "Update a tracked GameObject transform.",
                    "write",
                    ObjectSchema(
                        new JObject
                        {
                            ["id"] = StringSchema(),
                            ["position"] = VectorSchema(),
                            ["rotation"] = VectorSchema(),
                            ["scale"] = VectorSchema()
                        },
                        "id")),
                Capability(
                    "object.remove",
                    "Remove a tracked GameObject.",
                    "write",
                    ObjectSchema(
                        new JObject { ["id"] = StringSchema() },
                        "id")),
                Capability(
                    "camera.update",
                    "Update the main camera transform.",
                    "write",
                    ObjectSchema(
                        new JObject
                        {
                            ["position"] = VectorSchema(),
                            ["rotation"] = VectorSchema(),
                            ["fieldOfView"] = new JObject
                            {
                                ["type"] = "number",
                                ["minimum"] = 1,
                                ["maximum"] = 179
                            }
                        }))
            };
        }

        public JToken Describe()
        {
            PruneDestroyedObjects();
            JArray describedObjects = new JArray(
                objects.Values
                    .OrderBy(value => value.ObjectId, StringComparer.Ordinal)
                    .Select(DescribeObject));
            JObject state = new JObject
            {
                ["scene"] = gameObject.scene.name,
                ["objects"] = describedObjects
            };
            if (Camera.main != null)
                state["camera"] = DescribeCamera(Camera.main);
            return state;
        }

        public JToken Act(string name, JObject input)
        {
            switch (name)
            {
                case "scene.describe":
                    return Describe();
                case "object.create":
                    return CreateObject(input);
                case "object.update":
                    return UpdateObject(input);
                case "object.remove":
                    return RemoveObject(input);
                case "camera.update":
                    return UpdateCamera(input);
                default:
                    throw new BridgeCallException(
                        "invalid_action",
                        "Unsupported Unity action \"" + name + "\".");
            }
        }

        public JToken Events(JObject query)
        {
            long after = ParseCursor(query.Value<string>("after"));
            int limit = query.Value<int?>("limit") ?? 100;
            if (limit <= 0)
                limit = 100;
            if (limit > 1000)
                throw new BridgeCallException(
                    "invalid_event_query",
                    "Event limit cannot exceed 1000.");
            HashSet<string> types = new HashSet<string>(
                (query["types"] as JArray ?? new JArray())
                    .Values<string>(),
                StringComparer.Ordinal);

            JArray selected = new JArray();
            long cursor = after;
            foreach (JObject item in events)
            {
                long sequence = ParseCursor(item.Value<string>("id"));
                if (sequence <= after)
                    continue;
                cursor = sequence;
                if (types.Count != 0 &&
                    !types.Contains(item.Value<string>("type")))
                    continue;
                selected.Add(item.DeepClone());
                if (selected.Count >= limit)
                    break;
            }
            return new JObject
            {
                ["events"] = selected,
                ["cursor"] = cursor.ToString(CultureInfo.InvariantCulture)
            };
        }

        private JToken CreateObject(JObject input)
        {
            string id = RequiredString(input, "id");
            string type = RequiredString(input, "type").ToLowerInvariant();
            PruneDestroyedObjects();
            if (objects.ContainsKey(id))
                throw new BridgeCallException(
                    "object_exists",
                    "Object \"" + id + "\" already exists.");

            PrimitiveType primitive;
            switch (type)
            {
                case "cube":
                    primitive = PrimitiveType.Cube;
                    break;
                case "sphere":
                    primitive = PrimitiveType.Sphere;
                    break;
                case "plane":
                    primitive = PrimitiveType.Plane;
                    break;
                default:
                    throw new BridgeCallException(
                        "invalid_object_type",
                        "Object type must be cube, sphere, or plane.");
            }

            Vector3 position = ReadVector(input["position"], Vector3.zero);
            Vector3 rotation = ReadVector(input["rotation"], Vector3.zero);
            Vector3 scale = ReadVector(input["scale"], Vector3.one);
            GameObject created = GameObject.CreatePrimitive(primitive);
            created.name = id;
            created.transform.position = position;
            created.transform.eulerAngles = rotation;
            created.transform.localScale = scale;
            JangolovaObject tracked = created.AddComponent<JangolovaObject>();
            tracked.Initialize(id, type);
            objects.Add(id, tracked);
            Publish("object.created", DescribeObject(tracked));
            return DescribeObject(tracked);
        }

        private JToken UpdateObject(JObject input)
        {
            JangolovaObject tracked = RequiredObject(
                RequiredString(input, "id"));
            Vector3 position = ReadVector(
                input["position"],
                tracked.transform.position);
            Vector3 rotation = ReadVector(
                input["rotation"],
                tracked.transform.eulerAngles);
            Vector3 scale = ReadVector(
                input["scale"],
                tracked.transform.localScale);
            tracked.transform.position = position;
            tracked.transform.eulerAngles = rotation;
            tracked.transform.localScale = scale;
            Publish("object.updated", DescribeObject(tracked));
            return DescribeObject(tracked);
        }

        private JToken RemoveObject(JObject input)
        {
            string id = RequiredString(input, "id");
            JangolovaObject tracked = RequiredObject(id);
            objects.Remove(id);
            if (Application.isPlaying)
                Destroy(tracked.gameObject);
            else
                DestroyImmediate(tracked.gameObject);
            JObject result = new JObject
            {
                ["ok"] = true,
                ["id"] = id
            };
            Publish("object.removed", result);
            return result;
        }

        private JToken UpdateCamera(JObject input)
        {
            Camera camera = Camera.main;
            if (camera == null)
                throw new BridgeCallException(
                    "camera_not_found",
                    "The scene has no camera tagged MainCamera.");
            Vector3 position = ReadVector(
                input["position"],
                camera.transform.position);
            Vector3 rotation = ReadVector(
                input["rotation"],
                camera.transform.eulerAngles);
            float? fieldOfView = input.Value<float?>("fieldOfView");
            if (fieldOfView.HasValue)
            {
                if (fieldOfView.Value < 1 || fieldOfView.Value > 179)
                    throw new BridgeCallException(
                        "invalid_camera",
                        "fieldOfView must be between 1 and 179.");
            }
            camera.transform.position = position;
            camera.transform.eulerAngles = rotation;
            if (fieldOfView.HasValue)
                camera.fieldOfView = fieldOfView.Value;
            JObject result = DescribeCamera(camera);
            Publish("camera.updated", result);
            return result;
        }

        private void IndexExistingObjects()
        {
            objects.Clear();
            foreach (JangolovaObject tracked in
                FindObjectsOfType<JangolovaObject>())
            {
                if (!string.IsNullOrWhiteSpace(tracked.ObjectId))
                    objects[tracked.ObjectId] = tracked;
            }
        }

        private void PruneDestroyedObjects()
        {
            foreach (string id in objects
                .Where(pair => pair.Value == null)
                .Select(pair => pair.Key)
                .ToArray())
            {
                objects.Remove(id);
            }
        }

        private JangolovaObject RequiredObject(string id)
        {
            PruneDestroyedObjects();
            JangolovaObject tracked;
            if (!objects.TryGetValue(id, out tracked) || tracked == null)
                throw new BridgeCallException(
                    "object_not_found",
                    "Object \"" + id + "\" was not found.");
            return tracked;
        }

        private void Publish(string type, JToken data)
        {
            eventSequence++;
            events.Add(
                new JObject
                {
                    ["id"] = eventSequence.ToString(
                        CultureInfo.InvariantCulture),
                    ["type"] = type,
                    ["occurredAt"] = DateTime.UtcNow.ToString(
                        "o",
                        CultureInfo.InvariantCulture),
                    ["data"] = data.DeepClone()
                });
            if (events.Count > MaximumEvents)
                events.RemoveRange(0, events.Count - MaximumEvents);
        }

        private static JObject DescribeObject(JangolovaObject tracked)
        {
            return new JObject
            {
                ["id"] = tracked.ObjectId,
                ["type"] = tracked.ObjectType,
                ["name"] = tracked.gameObject.name,
                ["position"] = WriteVector(tracked.transform.position),
                ["rotation"] = WriteVector(tracked.transform.eulerAngles),
                ["scale"] = WriteVector(tracked.transform.localScale)
            };
        }

        private static JObject DescribeCamera(Camera camera)
        {
            return new JObject
            {
                ["position"] = WriteVector(camera.transform.position),
                ["rotation"] = WriteVector(camera.transform.eulerAngles),
                ["fieldOfView"] = camera.fieldOfView
            };
        }

        private static JObject WriteVector(Vector3 value)
        {
            return new JObject
            {
                ["x"] = value.x,
                ["y"] = value.y,
                ["z"] = value.z
            };
        }

        private static Vector3 ReadVector(JToken token, Vector3 fallback)
        {
            if (token == null)
                return fallback;
            JObject value = token as JObject;
            if (value == null)
                throw new BridgeCallException(
                    "invalid_vector",
                    "Vector values must be JSON objects.");
            return new Vector3(
                value.Value<float?>("x") ?? fallback.x,
                value.Value<float?>("y") ?? fallback.y,
                value.Value<float?>("z") ?? fallback.z);
        }

        private static string RequiredString(JObject input, string name)
        {
            string value = input.Value<string>(name);
            if (string.IsNullOrWhiteSpace(value))
                throw new BridgeCallException(
                    "invalid_input",
                    name + " is required.");
            return value;
        }

        private static long ParseCursor(string cursor)
        {
            if (string.IsNullOrWhiteSpace(cursor))
                return 0;
            long value;
            if (!long.TryParse(
                cursor,
                NumberStyles.None,
                CultureInfo.InvariantCulture,
                out value) ||
                value < 0)
            {
                throw new BridgeCallException(
                    "invalid_cursor",
                    "Event cursor is invalid.");
            }
            return value;
        }

        private static JObject Capability(
            string name,
            string description,
            string effect,
            JObject schema)
        {
            return new JObject
            {
                ["name"] = name,
                ["description"] = description,
                ["inputSchema"] = schema,
                ["effect"] = effect
            };
        }

        private static JObject EmptySchema()
        {
            return ObjectSchema(new JObject());
        }

        private static JObject ObjectSchema(
            JObject properties,
            params string[] required)
        {
            JObject schema = new JObject
            {
                ["type"] = "object",
                ["properties"] = properties,
                ["additionalProperties"] = false
            };
            if (required.Length != 0)
                schema["required"] = new JArray(required);
            return schema;
        }

        private static JObject StringSchema()
        {
            return new JObject
            {
                ["type"] = "string",
                ["minLength"] = 1
            };
        }

        private static JObject VectorSchema()
        {
            return new JObject
            {
                ["type"] = "object",
                ["properties"] = new JObject
                {
                    ["x"] = new JObject { ["type"] = "number" },
                    ["y"] = new JObject { ["type"] = "number" },
                    ["z"] = new JObject { ["type"] = "number" }
                },
                ["additionalProperties"] = false
            };
        }
    }
}
