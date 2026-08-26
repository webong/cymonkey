using System;
using System.IO;
using System.Net;
using System.Net.WebSockets;
using System.Text;
using System.Threading;
using System.Threading.Tasks;
using Newtonsoft.Json;
using Newtonsoft.Json.Linq;

namespace Jangolova.Cymonkey
{
    // The Unity application owns this listener. Disposing it detaches Pacman;
    // it never exits or otherwise controls the application.
    internal sealed class CymonkeyWebSocketServer : IDisposable
    {
        private readonly CymonkeyBridge bridge;
        private readonly string token;
        private readonly SynchronizationContext unityContext;
        private readonly HttpListener listener = new HttpListener();
        private readonly CancellationTokenSource lifetime = new CancellationTokenSource();

        internal CymonkeyWebSocketServer(CymonkeyBridge bridge, string prefix, string token, SynchronizationContext unityContext)
        {
            if (string.IsNullOrWhiteSpace(token)) throw new InvalidOperationException("Pacman token is required when a listener prefix is configured.");
            this.bridge = bridge; this.token = token; this.unityContext = unityContext;
            listener.Prefixes.Add(prefix);
        }

        internal void Start() { listener.Start(); _ = AcceptLoop(); }

        private async Task AcceptLoop()
        {
            while (!lifetime.IsCancellationRequested)
            {
                HttpListenerContext context;
                try { context = await listener.GetContextAsync(); }
                catch (Exception) when (lifetime.IsCancellationRequested) { return; }
                if (!ConstantTimeEquals(context.Request.Headers["Authorization"], "Bearer " + token)) { context.Response.StatusCode = 401; context.Response.Close(); continue; }
                if (!context.Request.IsWebSocketRequest) { context.Response.StatusCode = 400; context.Response.Close(); continue; }
                WebSocketContext upgraded = await context.AcceptWebSocketAsync(null);
                _ = Serve(upgraded.WebSocket);
            }
        }

        private async Task Serve(WebSocket socket)
        {
            byte[] buffer = new byte[16 * 1024];
            try
            {
                while (socket.State == WebSocketState.Open && !lifetime.IsCancellationRequested)
                {
                    using (MemoryStream message = new MemoryStream())
                    {
                        WebSocketReceiveResult read;
                        do { read = await socket.ReceiveAsync(new ArraySegment<byte>(buffer), lifetime.Token); if (read.MessageType == WebSocketMessageType.Close) return; if (message.Length + read.Count > CymonkeyProtocol.MaximumMessageBytes) throw new InvalidDataException("Pacman message is too large."); message.Write(buffer, 0, read.Count); } while (!read.EndOfMessage);
                        WireRequest request = JsonConvert.DeserializeObject<WireRequest>(Encoding.UTF8.GetString(message.ToArray()));
                        JObject reply = await OnUnityThread(() => Invoke(request));
                        byte[] payload = Encoding.UTF8.GetBytes(reply.ToString(Formatting.None));
                        await socket.SendAsync(new ArraySegment<byte>(payload), WebSocketMessageType.Text, true, lifetime.Token);
                    }
                }
            }
            catch (OperationCanceledException) { }
            finally { socket.Dispose(); }
        }

        private JObject Invoke(WireRequest request)
        {
            try { return new JObject { ["id"] = request.Id, ["result"] = bridge.Dispatch(request.Method, request.Params ?? new JObject()) }; }
            catch (Exception error) { CymonkeyCallException known = error as CymonkeyCallException; return new JObject { ["id"] = request == null ? 0 : request.Id, ["error"] = new JObject { ["code"] = known == null ? "pacman_error" : known.Code, ["message"] = error.Message } }; }
        }

        private Task<JObject> OnUnityThread(Func<JObject> operation) { TaskCompletionSource<JObject> result = new TaskCompletionSource<JObject>(TaskCreationOptions.RunContinuationsAsynchronously); unityContext.Post(_ => { try { result.SetResult(operation()); } catch (Exception error) { result.SetException(error); } }, null); return result.Task; }
        private static bool ConstantTimeEquals(string left, string right) { if (left == null || left.Length != right.Length) return false; int difference = 0; for (int i = 0; i < left.Length; i++) difference |= left[i] ^ right[i]; return difference == 0; }
        public void Dispose() { lifetime.Cancel(); listener.Close(); lifetime.Dispose(); }
    }
}
