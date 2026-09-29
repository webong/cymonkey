#pragma once

#include "CoreMinimal.h"
#include "ICymonkeyTransportHost.h"
#include "UObject/WeakObjectPtr.h"

class FCymonkeyRequestRouter;

// Authenticates and routes an already accepted caller-owned WebSocket. A
// platform/server binding calls AcceptConnection after performing its HTTP
// upgrade; this class owns no listen socket or Unreal process lifecycle.
class JANGOLOVACYMONKEY_API FCymonkeyWebSocketHost final : public ICymonkeyTransportHost
{
public:
    explicit FCymonkeyWebSocketHost(FString InBearerToken);
    virtual ~FCymonkeyWebSocketHost() override;

    virtual void StartHost(TWeakObjectPtr<UCymonkeyRegistryComponent> Registry) override;
    virtual void StopHost() override;

    bool AcceptConnection(const TSharedRef<ICymonkeyWebSocketConnection>& Connection);

private:
    static bool ConstantTimeEquals(const FString& Left, const FString& Right);
    static bool IsAuthMessage(const FString& Message, const FString& Token);

    FString BearerToken;
    TWeakObjectPtr<UCymonkeyRegistryComponent> Registry;
    TSharedPtr<FCymonkeyRequestRouter> Router;
    TSharedPtr<ICymonkeyWebSocketConnection> ActiveConnection;
    FCriticalSection Mutex;
    bool Started = false;
};
