#pragma once

#include "GameFramework/GameModeBase.h"
#include "CymonkeyFixtureGameMode.generated.h"

UCLASS()
class UNREALPACMANFIXTURE_API ACymonkeyFixtureGameMode final : public AGameModeBase
{
    GENERATED_BODY()

protected:
    virtual void StartPlay() override;
    virtual void EndPlay(const EEndPlayReason::Type EndPlayReason) override;

private:
    TUniquePtr<class FPacmanWebSocketServer> PacmanServer;
};
