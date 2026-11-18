#include "CymonkeyWebSocketServer.h"

#include "IWebSocketClientConnection.h"
#include "IWebSocketServer.h"
#include "CymonkeyRegistryComponent.h"
#include "CymonkeyWebSocketHost.h"
#include "WebSocketServerModule.h"

namespace
{
}

FCymonkeyUnrealWebSocketConnection::FCymonkeyUnrealWebSocketConnection(const TSharedRef<IWebSocketClientConnection>& InConnection)
    : Connection(InConnection)
{
}

FString FCymonkeyUnrealWebSocketConnection::AuthorizationHeader() const { return FString(); }
void FCymonkeyUnrealWebSocketConnection::SetTextHandler(FTextHandler InHandler) { Handler = MoveTemp(InHandler); }
void FCymonkeyUnrealWebSocketConnection::SendText(const FString& Message) { Connection->SendText(Message); }
void FCymonkeyUnrealWebSocketConnection::Close() { Connection->Close(1000, TEXT("Cymonkey transport closed")); }
void FCymonkeyUnrealWebSocketConnection::Deliver(const FString& Message) { if (Handler) Handler(Message); }

FCymonkeyWebSocketServer::FCymonkeyWebSocketServer(FString InBearerToken)
    : BearerToken(MoveTemp(InBearerToken))
{
}

FCymonkeyWebSocketServer::~FCymonkeyWebSocketServer()
{
    Stop();
}

bool FCymonkeyWebSocketServer::Start(uint16 Port, TWeakObjectPtr<UCymonkeyRegistryComponent> Registry)
{
    if (Listening || Port == 0 || BearerToken.IsEmpty() || !Registry.IsValid()) return false;
    Host = MakeUnique<FCymonkeyWebSocketHost>(BearerToken);
    Host->StartHost(Registry);
    Server = FWebSocketServerModule::Get().GetWebSocketServer(Port);
    if (!Server.IsValid()) return false;

    Server->OnConnected([this](TSharedRef<IWebSocketClientConnection> Connection)
    {
        const TSharedPtr<FCymonkeyUnrealWebSocketConnection> Wrapped = MakeShared<FCymonkeyUnrealWebSocketConnection>(Connection);
        Connections.Add(&Connection.Get(), Wrapped);
        if (!Host->AcceptConnection(Wrapped.ToSharedRef()))
        {
            Connections.Remove(&Connection.Get());
            return;
        }
    });
    Server->OnMessage([this](TSharedRef<IWebSocketClientConnection> Connection, const FString& Message)
    {
        if (const TSharedPtr<FCymonkeyUnrealWebSocketConnection>* Wrapped = Connections.Find(&Connection.Get()))
        {
            (*Wrapped)->Deliver(Message);
        }
    });
    Server->OnDisconnected([this](TSharedRef<IWebSocketClientConnection> Connection)
    {
        Connections.Remove(&Connection.Get());
    });
    FWebSocketServerModule::Get().StartAllServers();
    Listening = Server->IsListening();
    return Listening;
}

void FCymonkeyWebSocketServer::Stop()
{
    if (Server.IsValid() && Listening) Server->StopListening();
    Listening = false;
    if (Host) Host->StopHost();
    Connections.Empty();
    Host.Reset();
    Server.Reset();
}

bool FCymonkeyWebSocketServer::IsListening() const
{
    return Listening && Server.IsValid() && Server->IsListening();
}
