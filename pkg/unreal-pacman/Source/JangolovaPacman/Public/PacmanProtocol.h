#pragma once

#include "CoreMinimal.h"
#include "PacmanProtocol.generated.h"

namespace Jangolova::Pacman
{
    inline constexpr TCHAR ProtocolVersion[] = TEXT("jangolova.cymonkey/v1alpha2");
    inline constexpr TCHAR CompatibleProtocol[] = TEXT("jangolova.pacman/v1alpha1");
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
enum class EPacmanResourceKind : uint8
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
struct JANGOLOVAPACMAN_API FPacmanRegistration
{
    GENERATED_BODY()

    UPROPERTY(EditAnywhere, BlueprintReadOnly, Category = "Pacman")
    FString StableId;

    UPROPERTY(EditAnywhere, BlueprintReadOnly, Category = "Pacman")
    EPacmanResourceKind Kind = EPacmanResourceKind::Object;

    UPROPERTY(EditAnywhere, BlueprintReadOnly, Category = "Pacman")
    FString Label;

    UPROPERTY(EditAnywhere, BlueprintReadOnly, Category = "Pacman")
    TObjectPtr<UObject> Target = nullptr;

    UPROPERTY(EditAnywhere, BlueprintReadOnly, Category = "Pacman")
    TArray<FString> Actions;
};
