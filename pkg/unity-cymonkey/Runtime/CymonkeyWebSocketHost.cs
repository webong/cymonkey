using System;
using System.Threading;
using UnityEngine;

namespace Jangolova.Cymonkey
{
    [DisallowMultipleComponent]
    public sealed class CymonkeyWebSocketHost : MonoBehaviour, ICymonkeyTransportHost
    {
        [SerializeField] private string listenerPrefix;
        private CymonkeyWebSocketServer server;

        public void StartHost(CymonkeyBridge bridge)
        {
            if (server != null) throw new InvalidOperationException("Cymonkey WebSocket host is already running.");
            string prefix = string.IsNullOrWhiteSpace(listenerPrefix)
                ? Environment.GetEnvironmentVariable(CymonkeyProtocol.PrefixEnvironmentVariable)
                : listenerPrefix;
            string token = Environment.GetEnvironmentVariable(CymonkeyProtocol.TokenEnvironmentVariable);
            if (string.IsNullOrWhiteSpace(prefix))
                throw new InvalidOperationException("Cymonkey WebSocket listener prefix is required.");
            server = new CymonkeyWebSocketServer(bridge, prefix, token, SynchronizationContext.Current);
            server.Start();
        }

        public void Dispose()
        {
            if (server == null) return;
            server.Dispose();
            server = null;
        }

        private void OnDestroy() { Dispose(); }
    }
}
