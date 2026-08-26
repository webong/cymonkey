#include "CymonkeyFixtureGameMode.h"

#include "Engine/World.h"
#include "Misc/CommandLine.h"
#include "Misc/Parse.h"
#include "Misc/PlatformMisc.h"
#include "CymonkeyFixtureActor.h"
#include "PacmanWebSocketServer.h"

void ACymonkeyFixtureGameMode::StartPlay()
{
    Super::StartPlay();
    if (GetWorld() == nullptr) return;
    FActorSpawnParameters SpawnParameters;
    SpawnParameters.Name = TEXT("CymonkeyFixtureActor");
    ACymonkeyFixtureActor* Fixture = GetWorld()->SpawnActor<ACymonkeyFixtureActor>(ACymonkeyFixtureActor::StaticClass(), FTransform::Identity, SpawnParameters);
    const FString Token = FPlatformMisc::GetEnvironmentVariable(TEXT("JANGOLOVA_PACMAN_TOKEN"));
    int32 Port = 8090;
    FParse::Value(FCommandLine::Get(), TEXT("PacmanPort="), Port);
    if (Fixture != nullptr && Fixture->PacmanRegistry != nullptr && !Token.IsEmpty())
    {
        PacmanServer = MakeUnique<FPacmanWebSocketServer>(Token);
        if (!PacmanServer->Start(static_cast<uint16>(Port), Fixture->PacmanRegistry))
        {
            UE_LOG(LogTemp, Error, TEXT("Unable to start Jangolova Pacman WebSocket server on port %d"), Port);
        }
    }
}

void ACymonkeyFixtureGameMode::EndPlay(const EEndPlayReason::Type EndPlayReason)
{
    if (PacmanServer) PacmanServer->Stop();
    PacmanServer.Reset();
    Super::EndPlay(EndPlayReason);
}
