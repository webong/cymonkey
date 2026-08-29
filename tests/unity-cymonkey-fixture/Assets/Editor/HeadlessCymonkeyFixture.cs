using System;
using System.Reflection;
using Jangolova.Cymonkey;
using Newtonsoft.Json.Linq;
using UnityEditor;
using UnityEngine;
using UObject = UnityEngine.Object;

namespace Jangolova.CymonkeyFixture
{
    public sealed class FixtureTransportHost : MonoBehaviour, ICymonkeyTransportHost
    {
        public bool Started { get; private set; }
        public bool Disposed { get; private set; }

        public void StartHost(CymonkeyBridge bridge)
        {
            Require(bridge != null, "transport received no bridge");
            Started = true;
        }

        public void Dispose()
        {
            Disposed = true;
        }

        private static void Require(bool condition, string message)
        {
            if (!condition) throw new InvalidOperationException(message);
        }
    }

    public static class HeadlessCymonkeyFixture
    {
        public const string ExecuteMethod = "Jangolova.CymonkeyFixture.HeadlessCymonkeyFixture.Run";

        public static void Run()
        {
            int exitCode = 0;
            try
            {
                RunConformance();
                Debug.Log("Unity Cymonkey headless fixture passed.");
            }
            catch (Exception error)
            {
                exitCode = 1;
                Debug.LogError("Unity Cymonkey headless fixture failed: " + error);
            }
            EditorApplication.Exit(exitCode);
        }

        private static void RunConformance()
        {
            GameObject bridgeObject = new GameObject("CymonkeyFixtureBridge");
            GameObject targetObject = new GameObject("CymonkeyFixtureTarget");
            try
            {
                CymonkeyBridge bridge = bridgeObject.AddComponent<CymonkeyBridge>();
                FixtureTransportHost transport = bridgeObject.AddComponent<FixtureTransportHost>();
                Configure(bridge, transport, targetObject);
                InvokeLifecycle(bridge, "Awake");

                Require(transport.Started, "transport was not started");
                JObject hello = RequireObject(bridge.Dispatch("hello", new JObject()), "hello");
                Require((string)hello["protocolVersion"] == CymonkeyProtocol.Version, "protocol version mismatch");
                Require((string)hello["implementation"]?["engine"] == "unity", "hello engine mismatch");

                JArray capabilities = RequireArray(bridge.Dispatch("capabilities", new JObject()), "capabilities");
                Require(ContainsCapability(capabilities, "resource.describe"), "describe capability missing");
                Require(ContainsCapability(capabilities, "object.active.set"), "active capability missing");

                JObject description = RequireObject(bridge.Dispatch("describe", new JObject()), "describe");
                JArray resources = description["resources"] as JArray;
                Require(resources != null && resources.Count == 1, "fixture resource count mismatch");
                Require((string)resources[0]?["id"] == "object:fixture", "stable fixture ID missing");

                JObject action = new JObject {
                    ["name"] = "object.active.set",
                    ["targetId"] = "object:fixture",
                    ["input"] = new JObject { ["active"] = false }
                };
                JObject actionResult = RequireObject(bridge.Dispatch("act", action), "act");
                Require((bool?)actionResult["ok"] == true, "action did not succeed");
                Require(!targetObject.activeSelf, "action did not update the target");

                JObject eventsResult = RequireObject(bridge.Dispatch("events", new JObject { ["after"] = "0", ["limit"] = 10 }), "events");
                JArray events = eventsResult["events"] as JArray;
                Require(events != null && events.Count == 1, "resource change event missing");
                Require((string)events[0]?["type"] == "event:resource-changed", "event type mismatch");

                JObject health = RequireObject(bridge.Dispatch("health", new JObject()), "health");
                Require((string)health["status"] == "ready", "health is not ready");

                InvokeLifecycle(bridge, "OnDestroy");
                Require(transport.Disposed, "transport was not disposed");
                Require(targetObject != null, "disconnect destroyed the target");
            }
            finally
            {
                UObject.DestroyImmediate(bridgeObject);
                UObject.DestroyImmediate(targetObject);
            }
        }

        private static void Configure(CymonkeyBridge bridge, FixtureTransportHost transport, GameObject target)
        {
            SerializedObject serialized = new SerializedObject(bridge);
            SerializedProperty registrations = serialized.FindProperty("registrations");
            Require(registrations != null, "registrations property missing");
            registrations.arraySize = 1;
            SerializedProperty registration = registrations.GetArrayElementAtIndex(0);
            registration.FindPropertyRelative("id").stringValue = "object:fixture";
            registration.FindPropertyRelative("kind").enumValueIndex = (int)CymonkeyResourceKind.@object;
            registration.FindPropertyRelative("label").stringValue = "Headless fixture target";
            registration.FindPropertyRelative("target").objectReferenceValue = target;
            SerializedProperty actions = registration.FindPropertyRelative("actions");
            actions.arraySize = 2;
            actions.GetArrayElementAtIndex(0).stringValue = "resource.describe";
            actions.GetArrayElementAtIndex(1).stringValue = "object.active.set";
            serialized.FindProperty("transportHost").objectReferenceValue = transport;
            serialized.ApplyModifiedPropertiesWithoutUndo();
        }

        private static void InvokeLifecycle(CymonkeyBridge bridge, string methodName)
        {
            MethodInfo method = typeof(CymonkeyBridge).GetMethod(methodName, BindingFlags.Instance | BindingFlags.NonPublic);
            Require(method != null, "Cymonkey lifecycle method missing: " + methodName);
            method.Invoke(bridge, null);
        }

        private static bool ContainsCapability(JArray capabilities, string name)
        {
            foreach (JToken capability in capabilities)
                if ((string)capability?["name"] == name) return true;
            return false;
        }

        private static JObject RequireObject(JToken value, string method)
        {
            JObject result = value as JObject;
            Require(result != null, method + " did not return an object");
            return result;
        }

        private static JArray RequireArray(JToken value, string method)
        {
            JArray result = value as JArray;
            Require(result != null, method + " did not return an array");
            return result;
        }

        private static void Require(bool condition, string message)
        {
            if (!condition) throw new InvalidOperationException(message);
        }
    }
}
