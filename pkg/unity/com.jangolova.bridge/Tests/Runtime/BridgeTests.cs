using Newtonsoft.Json.Linq;
using NUnit.Framework;
using UnityEngine;

namespace Jangolova.Unity.Tests
{
    public sealed class BridgeTests
    {
        private GameObject host;
        private JangolovaSceneBridge bridge;

        [SetUp]
        public void SetUp()
        {
            host = new GameObject("Jangolova test host");
            bridge = host.AddComponent<JangolovaSceneBridge>();
        }

        [TearDown]
        public void TearDown()
        {
            foreach (JangolovaObject tracked in
                Object.FindObjectsOfType<JangolovaObject>())
            {
                Object.DestroyImmediate(tracked.gameObject);
            }
            Object.DestroyImmediate(host);
        }

        [Test]
        public void HelloUsesCurrentProtocol()
        {
            JObject hello = (JObject)bridge.Hello();
            Assert.That(
                hello.Value<string>("protocolVersion"),
                Is.EqualTo(BridgeProtocol.Version));
        }

        [Test]
        public void CreateDescribeAndReadCursorEvent()
        {
            JObject created = (JObject)bridge.Act(
                "object.create",
                new JObject
                {
                    ["id"] = "agent-cube",
                    ["type"] = "cube",
                    ["position"] = new JObject { ["x"] = 2, ["y"] = 1 }
                });
            Assert.That(created.Value<string>("id"), Is.EqualTo("agent-cube"));

            JObject state = (JObject)bridge.Describe();
            Assert.That(((JArray)state["objects"]).Count, Is.EqualTo(1));

            JObject firstBatch = (JObject)bridge.Events(new JObject());
            Assert.That(((JArray)firstBatch["events"]).Count, Is.EqualTo(1));
            string cursor = firstBatch.Value<string>("cursor");

            JObject secondBatch = (JObject)bridge.Events(
                new JObject { ["after"] = cursor });
            Assert.That(((JArray)secondBatch["events"]).Count, Is.EqualTo(0));
            Assert.That(
                secondBatch.Value<string>("cursor"),
                Is.EqualTo(cursor));
        }

        [Test]
        public void EnvironmentRejectsNonLoopbackEndpoint()
        {
            Assert.Throws<System.InvalidOperationException>(
                () => JangolovaBridgeClient.ValidateEnvironment(
                    "ws://example.com/bridge",
                    "secret",
                    BridgeProtocol.Version));
        }
    }
}
