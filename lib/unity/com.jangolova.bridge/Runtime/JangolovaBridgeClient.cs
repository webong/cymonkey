using System;
using System.IO;
using System.Net;
using System.Net.WebSockets;
using System.Text;
using System.Threading;
using System.Threading.Tasks;
using Newtonsoft.Json;
using Newtonsoft.Json.Linq;

namespace Jangolova.Unity
{
    public sealed class JangolovaBridgeClient : IDisposable
    {
        private readonly IJangolovaBridgeHandler handler;
        private readonly SynchronizationContext unityContext;
        private readonly ClientWebSocket socket;
        private readonly SemaphoreSlim sendLock = new SemaphoreSlim(1, 1);
        private readonly CancellationTokenSource lifetime = new CancellationTokenSource();
        private int disposed;

        public JangolovaBridgeClient(
            IJangolovaBridgeHandler handler,
            SynchronizationContext unityContext)
        {
            this.handler = handler ?? throw new ArgumentNullException(nameof(handler));
            this.unityContext = unityContext
                ?? throw new ArgumentNullException(nameof(unityContext));
            socket = new ClientWebSocket();
        }

        public bool IsConnected
        {
            get { return socket.State == WebSocketState.Open; }
        }

        public static bool HasBridgeEnvironment()
        {
            return !string.IsNullOrWhiteSpace(
                Environment.GetEnvironmentVariable(
                    BridgeProtocol.UrlEnvironmentVariable));
        }

        public async Task ConnectFromEnvironmentAsync(
            CancellationToken cancellationToken)
        {
            string endpoint = Environment.GetEnvironmentVariable(
                BridgeProtocol.UrlEnvironmentVariable);
            string token = Environment.GetEnvironmentVariable(
                BridgeProtocol.TokenEnvironmentVariable);
            string protocol = Environment.GetEnvironmentVariable(
                BridgeProtocol.ProtocolEnvironmentVariable);

            Uri uri = ValidateEnvironment(endpoint, token, protocol);
            socket.Options.SetRequestHeader(
                HttpRequestHeader.Authorization.ToString(),
                "Bearer " + token);

            using (CancellationTokenSource linked =
                CancellationTokenSource.CreateLinkedTokenSource(
                    cancellationToken,
                    lifetime.Token))
            {
                await socket.ConnectAsync(uri, linked.Token)
                    .ConfigureAwait(false);
                await ReceiveLoopAsync(linked.Token).ConfigureAwait(false);
            }
        }

        public static Uri ValidateEnvironment(
            string endpoint,
            string token,
            string protocol)
        {
            if (string.IsNullOrWhiteSpace(endpoint))
                throw new InvalidOperationException("Jangolova bridge URL is required.");
            if (string.IsNullOrWhiteSpace(token))
                throw new InvalidOperationException("Jangolova bridge token is required.");
            if (!string.Equals(
                protocol,
                BridgeProtocol.Version,
                StringComparison.Ordinal))
            {
                throw new InvalidOperationException(
                    "Unsupported Jangolova bridge protocol.");
            }

            Uri uri;
            if (!Uri.TryCreate(endpoint, UriKind.Absolute, out uri) ||
                !string.Equals(uri.Scheme, "ws", StringComparison.OrdinalIgnoreCase))
            {
                throw new InvalidOperationException(
                    "Jangolova bridge URL must be an absolute ws:// URL.");
            }

            if (!string.IsNullOrEmpty(uri.UserInfo))
            {
                throw new InvalidOperationException(
                    "Jangolova bridge URL cannot contain user information.");
            }

            string host = uri.Host.Trim('[', ']');
            IPAddress address;
            bool isLoopback = string.Equals(
                host,
                "localhost",
                StringComparison.OrdinalIgnoreCase);
            if (IPAddress.TryParse(host, out address))
                isLoopback = IPAddress.IsLoopback(address);
            if (!isLoopback)
            {
                throw new InvalidOperationException(
                    "Jangolova bridge URL must use a loopback host.");
            }
            return uri;
        }

        private async Task ReceiveLoopAsync(CancellationToken cancellationToken)
        {
            byte[] buffer = new byte[16 * 1024];
            while (!cancellationToken.IsCancellationRequested &&
                socket.State == WebSocketState.Open)
            {
                using (MemoryStream message = new MemoryStream())
                {
                    WebSocketReceiveResult received;
                    do
                    {
                        received = await socket.ReceiveAsync(
                            new ArraySegment<byte>(buffer),
                            cancellationToken).ConfigureAwait(false);
                        if (received.MessageType == WebSocketMessageType.Close)
                        {
                            await socket.CloseOutputAsync(
                                WebSocketCloseStatus.NormalClosure,
                                "closing",
                                cancellationToken).ConfigureAwait(false);
                            return;
                        }
                        if (received.MessageType != WebSocketMessageType.Text)
                            throw new InvalidDataException(
                                "Jangolova bridge accepts text messages only.");
                        if (message.Length + received.Count >
                            BridgeProtocol.MaximumMessageBytes)
                        {
                            throw new InvalidDataException(
                                "Jangolova bridge message exceeds the size limit.");
                        }
                        message.Write(buffer, 0, received.Count);
                    }
                    while (!received.EndOfMessage);

                    string json = Encoding.UTF8.GetString(message.ToArray());
                    await HandleRequestAsync(json, cancellationToken)
                        .ConfigureAwait(false);
                }
            }
        }

        private async Task HandleRequestAsync(
            string json,
            CancellationToken cancellationToken)
        {
            BridgeRequest request = null;
            BridgeResponse response;
            try
            {
                request = JsonConvert.DeserializeObject<BridgeRequest>(json);
                if (request == null || string.IsNullOrWhiteSpace(request.Method))
                    throw new BridgeCallException(
                        "invalid_request",
                        "Bridge method is required.");
                JToken result = await InvokeOnUnityThreadAsync(
                    () => Dispatch(request)).ConfigureAwait(false);
                response = new BridgeResponse
                {
                    Id = request.Id,
                    Result = result ?? JValue.CreateNull()
                };
            }
            catch (Exception exception)
            {
                BridgeCallException bridgeError = exception as BridgeCallException;
                response = new BridgeResponse
                {
                    Id = request == null ? 0 : request.Id,
                    Error = new BridgeError
                    {
                        Code = bridgeError == null
                            ? "bridge_error"
                            : bridgeError.Code,
                        Message = exception.Message
                    }
                };
            }

            string payload = JsonConvert.SerializeObject(
                response,
                Formatting.None);
            byte[] bytes = Encoding.UTF8.GetBytes(payload);
            await sendLock.WaitAsync(cancellationToken).ConfigureAwait(false);
            try
            {
                await socket.SendAsync(
                    new ArraySegment<byte>(bytes),
                    WebSocketMessageType.Text,
                    true,
                    cancellationToken).ConfigureAwait(false);
            }
            finally
            {
                sendLock.Release();
            }
        }

        private JToken Dispatch(BridgeRequest request)
        {
            JObject parameters = request.Params ?? new JObject();
            switch (request.Method)
            {
                case "hello":
                    return handler.Hello();
                case "capabilities":
                    return handler.Capabilities();
                case "describe":
                    return handler.Describe();
                case "act":
                    string name = parameters.Value<string>("name");
                    JObject input = parameters["input"] as JObject ?? new JObject();
                    if (string.IsNullOrWhiteSpace(name))
                        throw new BridgeCallException(
                            "invalid_action",
                            "Action name is required.");
                    return handler.Act(name, input);
                case "events":
                    return handler.Events(parameters);
                default:
                    throw new BridgeCallException(
                        "method_not_found",
                        "Unsupported bridge method \"" + request.Method + "\".");
            }
        }

        private Task<JToken> InvokeOnUnityThreadAsync(Func<JToken> operation)
        {
            TaskCompletionSource<JToken> completion =
                new TaskCompletionSource<JToken>(
                    TaskCreationOptions.RunContinuationsAsynchronously);
            unityContext.Post(
                _ =>
                {
                    try
                    {
                        completion.SetResult(operation());
                    }
                    catch (Exception exception)
                    {
                        completion.SetException(exception);
                    }
                },
                null);
            return completion.Task;
        }

        public void Dispose()
        {
            if (Interlocked.Exchange(ref disposed, 1) != 0)
                return;
            lifetime.Cancel();
            socket.Dispose();
            sendLock.Dispose();
            lifetime.Dispose();
        }
    }
}
