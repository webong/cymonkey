#include "CymonkeyRequestRouter.h"

#include "Async/Async.h"
#include "Dom/JsonObject.h"
#include "Policies/CondensedJsonPrintPolicy.h"
#include "Serialization/JsonReader.h"
#include "Serialization/JsonSerializer.h"
#include "CymonkeyRegistryComponent.h"

namespace
{
    constexpr int32 MaximumMessageBytes = Jangolova::Cymonkey::MaximumMessageBytes;

    FString SerializeObject(const TSharedPtr<FJsonObject>& Object)
    {
        FString Text;
        const TSharedRef<TJsonWriter< TCHAR, TCondensedJsonPrintPolicy<TCHAR> >> Writer =
            TJsonWriterFactory<TCHAR, TCondensedJsonPrintPolicy<TCHAR>>::Create(&Text);
        FJsonSerializer::Serialize(Object.ToSharedRef(), Writer);
        Writer->Close();
        return Text;
    }

    FString ErrorResponse(const TSharedPtr<FJsonValue>& Id, const FString& Code, const FString& Message)
    {
        TSharedPtr<FJsonObject> Response = MakeShared<FJsonObject>();
        Response->SetField(TEXT("id"), Id.IsValid() ? Id : MakeShared<FJsonValueNumber>(0));
        TSharedPtr<FJsonObject> Error = MakeShared<FJsonObject>();
        Error->SetStringField(TEXT("code"), Code);
        Error->SetStringField(TEXT("message"), Message);
        Response->SetObjectField(TEXT("error"), Error);
        return SerializeObject(Response);
    }

    FString SuccessResponse(const TSharedPtr<FJsonValue>& Id, const TSharedPtr<FJsonValue>& Result)
    {
        TSharedPtr<FJsonObject> Response = MakeShared<FJsonObject>();
        Response->SetField(TEXT("id"), Id);
        Response->SetField(TEXT("result"), Result);
        return SerializeObject(Response);
    }
}

FCymonkeyRequestRouter::FCymonkeyRequestRouter(TWeakObjectPtr<UCymonkeyRegistryComponent> InRegistry)
    : Registry(InRegistry)
{
}

bool FCymonkeyRequestRouter::HandleText(const FString& Message, FReply Reply, TSharedPtr<FCymonkeyRequestRouter> Self)
{
    if (!Reply || Stopped.Load()) return false;
    FTCHARToUTF8 Utf8(*Message);
    if (Utf8.Length() > MaximumMessageBytes)
    {
        Reply(ErrorResponse(nullptr, TEXT("message_too_large"), TEXT("Cymonkey message is too large")));
        return false;
    }

    const TSharedRef<TJsonReader<TCHAR>> Reader = TJsonReaderFactory<TCHAR>::Create(Message);
    TSharedPtr<FJsonObject> Request;
    if (!FJsonSerializer::Deserialize(Reader, Request) || !Request.IsValid())
    {
        Reply(ErrorResponse(nullptr, TEXT("invalid_json"), TEXT("Cymonkey request must be a JSON object")));
        return false;
    }
    const TSharedPtr<FJsonValue>* Id = Request->Values.Find(TEXT("id"));
    FString Method;
    if (Id == nullptr || !Request->TryGetStringField(TEXT("method"), Method) || Method.IsEmpty())
    {
        Reply(ErrorResponse(Id == nullptr ? nullptr : *Id, TEXT("invalid_request"), TEXT("Cymonkey request requires id and method")));
        return false;
    }

    TSharedPtr<FJsonObject> Params = MakeShared<FJsonObject>();
    const TSharedPtr<FJsonObject>* ParsedParams = nullptr;
    if (Request->TryGetObjectField(TEXT("params"), ParsedParams) && ParsedParams != nullptr)
    {
        Params = *ParsedParams;
    }

    const TSharedPtr<FJsonValue> RequestId = *Id;
    TWeakObjectPtr<UCymonkeyRegistryComponent> WeakRegistry = Registry;
    TWeakPtr<FCymonkeyRequestRouter> WeakRouter = Self;
    AsyncTask(ENamedThreads::GameThread, [WeakRouter, WeakRegistry, RequestId, Method, Params, Reply = MoveTemp(Reply)]() mutable
    {
        const TSharedPtr<FCymonkeyRequestRouter> Router = WeakRouter.Pin();
        if (!Router.IsValid() || Router->Stopped.Load()) return;
        UCymonkeyRegistryComponent* RegistryObject = WeakRegistry.Get();
        if (RegistryObject == nullptr)
        {
            Reply(ErrorResponse(RequestId, TEXT("target_unavailable"), TEXT("Cymonkey target is unavailable")));
            return;
        }
        TSharedPtr<FJsonValue> Result;
        FString ErrorCode;
        FString ErrorMessage;
        if (!RegistryObject->Dispatch(Method, Params, Result, ErrorCode, ErrorMessage))
        {
            Reply(ErrorResponse(RequestId, ErrorCode.IsEmpty() ? TEXT("pacman_error") : ErrorCode, ErrorMessage));
            return;
        }
        Reply(SuccessResponse(RequestId, Result));
    });
    return true;
}

void FCymonkeyRequestRouter::Stop()
{
    Stopped.Store(true);
}
