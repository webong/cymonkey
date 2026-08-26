using System;

namespace Jangolova.Cymonkey
{
    // A transport host only carries Pacman request/response envelopes. It does
    // not define semantic methods or own the Unity application lifecycle.
    public interface ICymonkeyTransportHost : IDisposable
    {
        void StartHost(CymonkeyBridge bridge);
    }
}
