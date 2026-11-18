#pragma once

#include "CoreMinimal.h"
#include "UObject/WeakObjectPtr.h"

class UCymonkeyRegistryComponent;

// Parses and envelopes Cymonkey JSON messages. Semantic dispatch is always
// marshalled to the Unreal game thread; the caller may receive the reply on
// any thread used by its WebSocket implementation.
class JANGOLOVACYMONKEY_API FCymonkeyRequestRouter final
{
public:
    using FReply = TFunction<void(const FString&)>;

    explicit FCymonkeyRequestRouter(TWeakObjectPtr<UCymonkeyRegistryComponent> InRegistry);

    bool HandleText(const FString& Message, FReply Reply, TSharedPtr<FCymonkeyRequestRouter> Self);
    void Stop();

private:
    TWeakObjectPtr<UCymonkeyRegistryComponent> Registry;
    TAtomic<bool> Stopped { false };
};
