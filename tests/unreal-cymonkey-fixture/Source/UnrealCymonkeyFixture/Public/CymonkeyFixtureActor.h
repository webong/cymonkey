#pragma once

#include "CoreMinimal.h"
#include "GameFramework/Actor.h"
#include "CymonkeyFixtureActor.generated.h"

class UCymonkeyRegistryComponent;

UCLASS()
class UNREALPACMANFIXTURE_API ACymonkeyFixtureActor final : public AActor
{
    GENERATED_BODY()

public:
    ACymonkeyFixtureActor();

    UPROPERTY(VisibleAnywhere, BlueprintReadOnly, Category = "Pacman")
    TObjectPtr<UCymonkeyRegistryComponent> CymonkeyRegistry;
};
