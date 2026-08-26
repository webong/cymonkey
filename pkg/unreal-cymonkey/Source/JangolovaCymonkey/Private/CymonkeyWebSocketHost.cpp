#include "CymonkeyWebSocketHost.h"

#include "Misc/ScopeLock.h"
#include "Dom/JsonObject.h"
#include "Serialization/JsonReader.h"
#include "Serialization/JsonSerializer.h"
#include "CymonkeyRequestRouter.h"

FCymonkeyWebSocketHost::FCymonkeyWebSocketHost(FString InBearerToken)
    : BearerToken(MoveTemp(InBearerToken))
{
}

FCymonkeyWebSocketHost::~FCymonkeyWebSocketHost()
{
    StopHost();
}

void FCymonkeyWebSocketHost::StartHost(TWeakObjectPtr<UCymonkeyRegistryComponent> InRegistry)
{
    FScopeLock Lock(&Mutex);
    Registry = InRegistry;
    Router = MakeShared<FCymonkeyRequestRouter>(Registry);
    Started = true;
}

void FCymonkeyWebSocketHost::StopHost()
{
    TSharedPtr<ICymonkeyWebSocketConnection> Connection;
    TSharedPtr<FCymonkeyRequestRouter> CurrentRouter;
    {
        FScopeLock Lock(&Mutex);
        if (!Started && !ActiveConnection.IsValid()) return;
        Started = false;
        CurrentRouter = Router;
        Router.Reset();
        Connection = ActiveConnection;
        ActiveConnection.Reset();
        Registry.Reset();
    }
    if (CurrentRouter.IsValid()) CurrentRouter->Stop();
    if (Connection.IsValid()) Connection->Close();
}

bool FCymonkeyWebSocketHost::AcceptConnection(const TSharedRef<ICymonkeyWebSocketConnection>& Connection)
{
    if (BearerToken.IsEmpty())
    {
        Connection->Close();
        return false;
    }
    const bool HeaderAuthenticated = ConstantTimeEquals(Connection->AuthorizationHeader(), FString::Printf(TEXT("Bearer %s"), *BearerToken));
    if (!Connection->AuthorizationHeader().IsEmpty() && !HeaderAuthenticated)
    {
        Connection->Close();
        return false;
    }

    TSharedPtr<FCymonkeyRequestRouter> CurrentRouter;
    TSharedPtr<ICymonkeyWebSocketConnection> Previous;
    {
        FScopeLock Lock(&Mutex);
        if (!Started || !Router.IsValid())
        {
            Connection->Close();
            return false;
        }
        Previous = ActiveConnection;
        ActiveConnection = Connection;
        CurrentRouter = Router;
    }
    if (Previous.IsValid()) Previous->Close();
    TSharedRef<TAtomic<bool>> Authenticated = MakeShared<TAtomic<bool>>(HeaderAuthenticated);
    const FString ExpectedToken = BearerToken;
    Connection->SetTextHandler([CurrentRouter, Connection, Authenticated, ExpectedToken](const FString& Message)
    {
        if (!CurrentRouter.IsValid()) return;
        if (!Authenticated->Load())
        {
            if (!IsAuthMessage(Message, ExpectedToken))
            {
                Connection->Close();
                return;
            }
            Authenticated->Store(true);
            Connection->SendText(TEXT("{\"type\":\"pacman.authenticated\"}"));
            return;
        }
        CurrentRouter->HandleText(Message, [Connection](const FString& Reply)
        {
            Connection->SendText(Reply);
        }, CurrentRouter);
    });
    return true;
}

bool FCymonkeyWebSocketHost::IsAuthMessage(const FString& Message, const FString& Token)
{
    const TSharedRef<TJsonReader<TCHAR>> Reader = TJsonReaderFactory<TCHAR>::Create(Message);
    TSharedPtr<FJsonObject> Object;
    if (!FJsonSerializer::Deserialize(Reader, Object) || !Object.IsValid()) return false;
    FString Type;
    FString Candidate;
    return Object->TryGetStringField(TEXT("type"), Type)
        && Object->TryGetStringField(TEXT("token"), Candidate)
        && Type == TEXT("auth")
        && ConstantTimeEquals(Candidate, Token);
}

bool FCymonkeyWebSocketHost::ConstantTimeEquals(const FString& Left, const FString& Right)
{
    if (Left.Len() != Right.Len()) return false;
    uint32 Difference = 0;
    for (int32 Index = 0; Index < Left.Len(); ++Index)
    {
        Difference |= static_cast<uint32>(Left[Index]) ^ static_cast<uint32>(Right[Index]);
    }
    return Difference == 0;
}
