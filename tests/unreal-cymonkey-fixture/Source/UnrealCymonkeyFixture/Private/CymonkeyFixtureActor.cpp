#include "CymonkeyFixtureActor.h"

#include "Components/SceneComponent.h"
#include "CymonkeyProtocol.h"
#include "CymonkeyRegistryComponent.h"

ACymonkeyFixtureActor::ACymonkeyFixtureActor()
{
    PrimaryActorTick.bCanEverTick = false;
    RootComponent = CreateDefaultSubobject<USceneComponent>(TEXT("FixtureRoot"));
    CymonkeyRegistry = CreateDefaultSubobject<UCymonkeyRegistryComponent>(TEXT("CymonkeyRegistry"));

    FCymonkeyRegistration Registration;
    Registration.StableId = TEXT("object:fixture");
    Registration.Kind = ECymonkeyResourceKind::Object;
    Registration.Label = TEXT("Cymonkey fixture actor");
    Registration.Target = this;
    Registration.Actions = { TEXT("resource.describe"), TEXT("object.visibility.set") };
    CymonkeyRegistry->Registrations.Add(Registration);
}
