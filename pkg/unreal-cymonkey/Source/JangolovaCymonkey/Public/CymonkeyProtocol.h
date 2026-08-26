#pragma once

#include "CoreMinimal.h"
#include "CymonkeyProtocol.generated.h"

namespace Jangolova::Cymonkey
{
    inline constexpr TCHAR ProtocolVersion[] = TEXT("jangolova.cymonkey/v1alpha2");
    inline constexpr TCHAR ProfileEngine[] = TEXT("engine");
    inline constexpr TCHAR BackendUnreal[] = TEXT("engine-unreal");
    inline constexpr TCHAR MethodHello[] = TEXT("hello");
    inline constexpr TCHAR MethodCapabilities[] = TEXT("capabilities");
    inline constexpr TCHAR MethodDescribe[] = TEXT("describe");
    inline constexpr TCHAR MethodAct[] = TEXT("act");
    inline constexpr TCHAR MethodEvents[] = TEXT("events");
    inline constexpr TCHAR MethodHealth[] = TEXT("health");
    inline constexpr int32 MaximumMessageBytes = 4 * 1024 * 1024;
}

UENUM(BlueprintType)
enum class ECymonkeyResourceKind : uint8
{
    Scene,
    Object,
    UI,
    Camera,
    Material,
    Animation,
    Timeline,
    Artifact,
    Event
};

USTRUCT(BlueprintType)
struct JANGOLOVACYMONKEY_API FCymonkeyRegistration
{
    GENERATED_BODY()

    UPROPERTY(EditAnywhere, BlueprintReadOnly, Category = "Cymonkey")
    FString StableId;

    UPROPERTY(EditAnywhere, BlueprintReadOnly, Category = "Cymonkey")
    ECymonkeyResourceKind Kind = ECymonkeyResourceKind::Object;

    UPROPERTY(EditAnywhere, BlueprintReadOnly, Category = "Cymonkey")
    FString Label;

    UPROPERTY(EditAnywhere, BlueprintReadOnly, Category = "Cymonkey")
    TObjectPtr<UObject> Target = nullptr;

    UPROPERTY(EditAnywhere, BlueprintReadOnly, Category = "Cymonkey")
    TArray<FString> Actions;
};
