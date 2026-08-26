#include "Misc/AutomationTest.h"

#include "CymonkeyFixtureActor.h"
#include "CymonkeyRegistryComponent.h"

#if WITH_DEV_AUTOMATION_TESTS
IMPLEMENT_SIMPLE_AUTOMATION_TEST(
    FCymonkeyFixtureRegistrationTest,
    "Jangolova.Cymonkey.Fixture.ExplicitRegistration",
    EAutomationTestFlags::EditorContext | EAutomationTestFlags::EngineFilter)

bool FCymonkeyFixtureRegistrationTest::RunTest(const FString& Parameters)
{
    const ACymonkeyFixtureActor* Defaults = GetDefault<ACymonkeyFixtureActor>();
    TestNotNull(TEXT("Fixture actor defaults exist"), Defaults);
    if (Defaults == nullptr || Defaults->CymonkeyRegistry == nullptr) return false;
    TestEqual(TEXT("Exactly one resource is registered"), Defaults->CymonkeyRegistry->Registrations.Num(), 1);
    if (Defaults->CymonkeyRegistry->Registrations.Num() != 1) return false;
    const FCymonkeyRegistration& Registration = Defaults->CymonkeyRegistry->Registrations[0];
    TestEqual(TEXT("Stable fixture ID"), Registration.StableId, FString(TEXT("object:fixture")));
    TestTrue(TEXT("Fixture target is explicitly assigned"), Registration.Target != nullptr);
    TestTrue(TEXT("Visibility action is explicitly allowlisted"), Registration.Actions.Contains(TEXT("object.visibility.set")));
    return true;
}
#endif
