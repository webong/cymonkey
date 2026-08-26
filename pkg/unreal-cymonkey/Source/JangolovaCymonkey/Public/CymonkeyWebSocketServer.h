#pragma once

#include "CoreMinimal.h"
#include "ICymonkeyTransportHost.h"
#include "UObject/WeakObjectPtr.h"

class UCymonkeyRegistryComponent;
class IWebSocketServer;
class FCymonkeyWebSocketHost;
class IWebSocketClientConnection;

class JANGOLOVACYMONKEY_API FCymonkeyUnrealWebSocketConnection final : public ICymonkeyWebSocketConnection
{
public:
    explicit FCymonkeyUnrealWebSocketConnection(const TSharedRef<IWebSocketClientConnection>& InConnection);
    virtual FString AuthorizationHeader() const override;
    virtual void SetTextHandler(FTextHandler InHandler) override;
    virtual void SendText(const FString& Message) override;
    virtual void Close() override;
    void Deliver(const FString& Message);

private:
    TSharedRef<IWebSocketClientConnection> Connection;
    FTextHandler Handler;
};

// UE 5.8's built-in WebSocketServer module provides the HTTP Upgrade binding.
// This adapter owns only the listener and connection wrappers; Unreal still
// owns the application and world lifecycle.
class JANGOLOVACYMONKEY_API FCymonkeyWebSocketServer final
{
public:
    explicit FCymonkeyWebSocketServer(FString InBearerToken);
    ~FCymonkeyWebSocketServer();

    bool Start(uint16 Port, TWeakObjectPtr<UCymonkeyRegistryComponent> Registry);
    void Stop();
    bool IsListening() const;

private:
    FString BearerToken;
    TSharedPtr<IWebSocketServer> Server;
    TUniquePtr<FCymonkeyWebSocketHost> Host;
    TMap<IWebSocketClientConnection*, TSharedPtr<FCymonkeyUnrealWebSocketConnection>> Connections;
    bool Listening = false;
};
