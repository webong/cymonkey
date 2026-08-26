#pragma once

#include "Components/ActorComponent.h"
#include "Dom/JsonValue.h"
#include "CymonkeyProtocol.h"
#include "CymonkeyRegistryComponent.generated.h"

UCLASS(ClassGroup = (Jangolova), meta = (BlueprintSpawnableComponent))
class JANGOLOVACYMONKEY_API UCymonkeyRegistryComponent final : public UActorComponent
{
    GENERATED_BODY()

public:
    UCymonkeyRegistryComponent();

    UPROPERTY(EditAnywhere, BlueprintReadOnly, Category = "Cymonkey")
    TArray<FCymonkeyRegistration> Registrations;

    // Must be invoked on the game thread. A transport wraps the returned value
    // in the Cymonkey response envelope and maps errors to structured errors.
    bool Dispatch(
        const FString& Method,
        const TSharedPtr<FJsonObject>& Params,
        TSharedPtr<FJsonValue>& OutResult,
        FString& OutErrorCode,
        FString& OutErrorMessage);

protected:
    virtual void BeginPlay() override;

private:
    bool BuildAllowlist(FString& OutError);
    TSharedPtr<FJsonValue> Hello() const;
    TSharedPtr<FJsonValue> Capabilities() const;
    TSharedPtr<FJsonValue> Describe() const;
    bool Act(const TSharedPtr<FJsonObject>& Params, TSharedPtr<FJsonValue>& OutResult, FString& OutErrorCode, FString& OutErrorMessage);
    TSharedPtr<FJsonValue> Events(const TSharedPtr<FJsonObject>& Params) const;
    TSharedPtr<FJsonValue> Health() const;
    TSharedPtr<FJsonObject> DescribeResource(const FCymonkeyRegistration& Registration) const;
    void Publish(const FString& Type, const FString& SourceId, const TSharedPtr<FJsonObject>& Data);

    TMap<FString, const FCymonkeyRegistration*> Allowlist;
    TArray<TSharedPtr<FJsonObject>> EventBuffer;
    int64 Revision = 1;
    int64 EventSequence = 0;
    static constexpr int32 MaximumEvents = 256;
};
