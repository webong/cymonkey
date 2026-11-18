#pragma once

#include "CoreMinimal.h"

class UCymonkeyRegistryComponent;

// A concrete Unreal WebSocket implementation supplies this interface. The
// Cymonkey host owns only the connection and callbacks, never the application.
class JANGOLOVACYMONKEY_API ICymonkeyWebSocketConnection
{
public:
    using FTextHandler = TFunction<void(const FString&)>;

    virtual ~ICymonkeyWebSocketConnection() = default;
    virtual FString AuthorizationHeader() const = 0;
    virtual void SetTextHandler(FTextHandler Handler) = 0;
    virtual void SendText(const FString& Message) = 0;
    virtual void Close() = 0;
};

// Transport implementations authenticate and frame Cymonkey requests. They do
// not own the Unreal application, World, renderer, or target lifecycle.
class JANGOLOVACYMONKEY_API ICymonkeyTransportHost
{
public:
    virtual ~ICymonkeyTransportHost() = default;
    virtual void StartHost(TWeakObjectPtr<UCymonkeyRegistryComponent> Registry) = 0;
    virtual void StopHost() = 0;
};
