#include "CymonkeyRegistryComponent.h"

#include "Components/SceneComponent.h"
#include "Dom/JsonObject.h"
#include "GameFramework/Actor.h"
#include "Internationalization/Regex.h"
#include "Misc/DateTime.h"

namespace
{
    FString WireKind(const ECymonkeyResourceKind Kind)
    {
        switch (Kind)
        {
        case ECymonkeyResourceKind::Scene: return TEXT("scene");
        case ECymonkeyResourceKind::Object: return TEXT("object");
        case ECymonkeyResourceKind::UI: return TEXT("ui");
        case ECymonkeyResourceKind::Camera: return TEXT("camera");
        case ECymonkeyResourceKind::Material: return TEXT("material");
        case ECymonkeyResourceKind::Animation: return TEXT("animation");
        case ECymonkeyResourceKind::Timeline: return TEXT("timeline");
        case ECymonkeyResourceKind::Artifact: return TEXT("artifact");
        case ECymonkeyResourceKind::Event: return TEXT("event");
        }
        return TEXT("object");
    }

    TSharedPtr<FJsonValue> ObjectValue(const TSharedPtr<FJsonObject>& Value)
    {
        return MakeShared<FJsonValueObject>(Value);
    }

    TSharedPtr<FJsonValue> ArrayValue(TArray<TSharedPtr<FJsonValue>> Values)
    {
        return MakeShared<FJsonValueArray>(MoveTemp(Values));
    }

    TSharedPtr<FJsonObject> ObjectSchema()
    {
        TSharedPtr<FJsonObject> Schema = MakeShared<FJsonObject>();
        Schema->SetStringField(TEXT("type"), TEXT("object"));
        Schema->SetBoolField(TEXT("additionalProperties"), false);
        return Schema;
    }

    TSharedPtr<FJsonValue> Capability(
        const FString& Name,
        const FString& Effect,
        const TArray<FString>& Kinds,
        const TSharedPtr<FJsonObject>& InputSchema)
    {
        TSharedPtr<FJsonObject> Value = MakeShared<FJsonObject>();
        Value->SetStringField(TEXT("name"), Name);
        Value->SetStringField(TEXT("domain"), Jangolova::Cymonkey::DomainRender);
        Value->SetStringField(TEXT("runtime"), Jangolova::Cymonkey::RuntimeUnreal);
        Value->SetStringField(TEXT("driver"), Jangolova::Cymonkey::DriverWebSocket);
        Value->SetStringField(TEXT("support"), TEXT("native"));
        Value->SetStringField(TEXT("lifetime"), TEXT("attachment"));
        Value->SetStringField(TEXT("persistence"), TEXT("session"));
        Value->SetStringField(TEXT("effect"), Effect);
        TArray<TSharedPtr<FJsonValue>> TargetKinds;
        for (const FString& Kind : Kinds)
        {
            TargetKinds.Add(MakeShared<FJsonValueString>(Kind));
        }
        Value->SetArrayField(TEXT("resourceKinds"), TargetKinds);
        Value->SetObjectField(TEXT("inputSchema"), InputSchema);
        return ObjectValue(Value);
    }
}

UCymonkeyRegistryComponent::UCymonkeyRegistryComponent()
{
    PrimaryComponentTick.bCanEverTick = false;
}

void UCymonkeyRegistryComponent::BeginPlay()
{
    Super::BeginPlay();
    FString Error;
    if (!BuildAllowlist(Error))
    {
        UE_LOG(LogTemp, Error, TEXT("Cymonkey registry is invalid: %s"), *Error);
        SetComponentTickEnabled(false);
    }
}

bool UCymonkeyRegistryComponent::BuildAllowlist(FString& OutError)
{
    static const FRegexPattern StableIdPattern(TEXT("^[a-z][a-z0-9-]{0,31}:[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$"));
    Allowlist.Empty();
    for (const FCymonkeyRegistration& Registration : Registrations)
    {
        FRegexMatcher Matcher(StableIdPattern, Registration.StableId);
        const FString Prefix = WireKind(Registration.Kind) + TEXT(":");
        if (!IsValid(Registration.Target) || !Matcher.FindNext() || !Registration.StableId.StartsWith(Prefix, ESearchCase::CaseSensitive))
        {
            OutError = TEXT("Every registration requires a target and matching stable kind-prefixed ID");
            Allowlist.Empty();
            return false;
        }
        if (Allowlist.Contains(Registration.StableId))
        {
            OutError = FString::Printf(TEXT("Duplicate Cymonkey resource ID: %s"), *Registration.StableId);
            Allowlist.Empty();
            return false;
        }
        Allowlist.Add(Registration.StableId, &Registration);
    }
    return true;
}

bool UCymonkeyRegistryComponent::Dispatch(
    const FString& Method,
    const TSharedPtr<FJsonObject>& Params,
    TSharedPtr<FJsonValue>& OutResult,
    FString& OutErrorCode,
    FString& OutErrorMessage)
{
    check(IsInGameThread());
    const TSharedPtr<FJsonObject> SafeParams = Params.IsValid() ? Params : MakeShared<FJsonObject>();
    if (Method == Jangolova::Cymonkey::MethodHello) OutResult = Hello();
    else if (Method == Jangolova::Cymonkey::MethodCapabilities) OutResult = Capabilities();
    else if (Method == Jangolova::Cymonkey::MethodDescribe) OutResult = Describe();
    else if (Method == Jangolova::Cymonkey::MethodAct) return Act(SafeParams, OutResult, OutErrorCode, OutErrorMessage);
    else if (Method == Jangolova::Cymonkey::MethodEvents) OutResult = Events(SafeParams);
    else if (Method == Jangolova::Cymonkey::MethodHealth) OutResult = Health();
    else
    {
        OutErrorCode = TEXT("method_not_found");
        OutErrorMessage = TEXT("Unsupported Cymonkey method");
        return false;
    }
    return true;
}

TSharedPtr<FJsonValue> UCymonkeyRegistryComponent::Hello() const
{
    TSharedPtr<FJsonObject> Implementation = MakeShared<FJsonObject>();
    Implementation->SetStringField(TEXT("name"), TEXT("jangolova-unreal-cymonkey"));
    Implementation->SetStringField(TEXT("version"), TEXT("0.2.0"));

    TSharedPtr<FJsonObject> Value = MakeShared<FJsonObject>();
    Value->SetStringField(TEXT("protocolVersion"), Jangolova::Cymonkey::ProtocolVersion);
    Value->SetField(TEXT("implementation"), MakeShared<FJsonValueObject>(Implementation));
    Value->SetArrayField(TEXT("domains"), { MakeShared<FJsonValueString>(Jangolova::Cymonkey::DomainRender) });
    Value->SetArrayField(TEXT("runtimes"), { MakeShared<FJsonValueString>(Jangolova::Cymonkey::RuntimeUnreal) });
    Value->SetArrayField(TEXT("drivers"), { MakeShared<FJsonValueString>(Jangolova::Cymonkey::DriverWebSocket) });
    Value->SetArrayField(TEXT("features"), {
        MakeShared<FJsonValueString>(TEXT("events.cursor")),
        MakeShared<FJsonValueString>(TEXT("resources.explicit-allowlist"))
    });
    return ObjectValue(Value);
}

TSharedPtr<FJsonValue> UCymonkeyRegistryComponent::Capabilities() const
{
    const TArray<FString> AllKinds = {
        TEXT("scene"), TEXT("object"), TEXT("ui"), TEXT("camera"), TEXT("material"),
        TEXT("animation"), TEXT("timeline"), TEXT("artifact"), TEXT("event")
    };
    TSharedPtr<FJsonObject> VisibilitySchema = ObjectSchema();
    TSharedPtr<FJsonObject> Properties = MakeShared<FJsonObject>();
    TSharedPtr<FJsonObject> Visible = MakeShared<FJsonObject>();
    Visible->SetStringField(TEXT("type"), TEXT("boolean"));
    Properties->SetObjectField(TEXT("visible"), Visible);
    VisibilitySchema->SetObjectField(TEXT("properties"), Properties);
    VisibilitySchema->SetArrayField(TEXT("required"), { MakeShared<FJsonValueString>(TEXT("visible")) });

    TArray<TSharedPtr<FJsonValue>> Values;
    Values.Add(Capability(TEXT("resource.describe"), TEXT("read"), AllKinds, ObjectSchema()));
    Values.Add(Capability(TEXT("object.visibility.set"), TEXT("write"), { TEXT("object"), TEXT("ui"), TEXT("camera") }, VisibilitySchema));
    return ArrayValue(MoveTemp(Values));
}

TSharedPtr<FJsonValue> UCymonkeyRegistryComponent::Describe() const
{
    TArray<FString> Ids;
    Allowlist.GetKeys(Ids);
    Ids.Sort();
    TArray<TSharedPtr<FJsonValue>> Resources;
    for (const FString& Id : Ids)
    {
        Resources.Add(ObjectValue(DescribeResource(*Allowlist.FindChecked(Id))));
    }
    TSharedPtr<FJsonObject> Value = MakeShared<FJsonObject>();
    Value->SetStringField(TEXT("revision"), LexToString(Revision));
    Value->SetArrayField(TEXT("surfaces"), Resources);
    Value->SetArrayField(TEXT("augmentations"), {});
    return ObjectValue(Value);
}

bool UCymonkeyRegistryComponent::Act(
    const TSharedPtr<FJsonObject>& Params,
    TSharedPtr<FJsonValue>& OutResult,
    FString& OutErrorCode,
    FString& OutErrorMessage)
{
    FString Name;
    FString TargetId;
    Params->TryGetStringField(TEXT("name"), Name);
    const TSharedPtr<FJsonObject>* InputObject = nullptr;
    Params->TryGetObjectField(TEXT("input"), InputObject);
    if (InputObject == nullptr)
    {
        TargetId.Empty();
    }
    else
    {
        (*InputObject)->TryGetStringField(TEXT("targetId"), TargetId);
    }
    if (TargetId.IsEmpty())
    {
        Params->TryGetStringField(TEXT("targetId"), TargetId);
    }
    const FCymonkeyRegistration* const* Found = Allowlist.Find(TargetId);
    if (Found == nullptr)
    {
        OutErrorCode = TEXT("target_not_allowlisted");
        OutErrorMessage = TEXT("Cymonkey target is not allowlisted");
        return false;
    }
    const FCymonkeyRegistration& Registration = **Found;
    if (!Registration.Actions.Contains(Name))
    {
        OutErrorCode = TEXT("action_not_allowlisted");
        OutErrorMessage = TEXT("Cymonkey action is not allowlisted for this target");
        return false;
    }
    if (InputObject != nullptr)
    {
        FString ExpectedRevision;
        if ((*InputObject)->TryGetStringField(TEXT("expectedRevision"), ExpectedRevision) && !ExpectedRevision.IsEmpty())
        {
            const FString CurrentRevision = LexToString(Revision);
            if (ExpectedRevision != CurrentRevision)
            {
                OutErrorCode = TEXT("stale_revision");
                OutErrorMessage = FString::Printf(TEXT("Revision %s is stale; current revision is %s"), *ExpectedRevision, *CurrentRevision);
                return false;
            }
        }
    }
    if (Name == TEXT("resource.describe"))
    {
        OutResult = ObjectValue(DescribeResource(Registration));
        return true;
    }
    if (Name == TEXT("object.visibility.set"))
    {
        const TSharedPtr<FJsonObject>* Input = InputObject;
        bool Visible = false;
        if (Input == nullptr || !(*Input)->TryGetBoolField(TEXT("visible"), Visible))
        {
            OutErrorCode = TEXT("invalid_input");
            OutErrorMessage = TEXT("visible is required");
            return false;
        }
        if (AActor* Actor = Cast<AActor>(Registration.Target))
        {
            Actor->SetActorHiddenInGame(!Visible);
        }
        else if (USceneComponent* Component = Cast<USceneComponent>(Registration.Target))
        {
            Component->SetVisibility(Visible, true);
        }
        else
        {
            OutErrorCode = TEXT("invalid_target");
            OutErrorMessage = TEXT("Visibility requires an Actor or SceneComponent");
            return false;
        }
        Revision++;
        TSharedPtr<FJsonObject> Data = MakeShared<FJsonObject>();
        Data->SetBoolField(TEXT("visible"), Visible);
        Publish(TEXT("event:resource-changed"), TargetId, Data);
        TSharedPtr<FJsonObject> Value = MakeShared<FJsonObject>();
        Value->SetBoolField(TEXT("ok"), true);
        Value->SetStringField(TEXT("revision"), LexToString(Revision));
        OutResult = ObjectValue(Value);
        return true;
    }
    OutErrorCode = TEXT("action_not_implemented");
    OutErrorMessage = TEXT("Allowlisted action has no Unreal handler");
    return false;
}

TSharedPtr<FJsonValue> UCymonkeyRegistryComponent::Events(const TSharedPtr<FJsonObject>& Params) const
{
    FString AfterText;
    Params->TryGetStringField(TEXT("after"), AfterText);
    const int64 After = FCString::Atoi64(*AfterText);
    double RequestedLimit = 100;
    Params->TryGetNumberField(TEXT("limit"), RequestedLimit);
    const int32 Limit = FMath::Clamp(static_cast<int32>(RequestedLimit), 1, 1000);
    TArray<TSharedPtr<FJsonValue>> Selected;
    FString Cursor = LexToString(After);
    for (const TSharedPtr<FJsonObject>& Event : EventBuffer)
    {
        FString EventId;
        Event->TryGetStringField(TEXT("id"), EventId);
        if (FCString::Atoi64(*EventId) <= After) continue;
        Selected.Add(ObjectValue(Event));
        Cursor = EventId;
        if (Selected.Num() >= Limit) break;
    }
    TSharedPtr<FJsonObject> Value = MakeShared<FJsonObject>();
    Value->SetArrayField(TEXT("events"), Selected);
    Value->SetStringField(TEXT("cursor"), Cursor);
    return ObjectValue(Value);
}

TSharedPtr<FJsonValue> UCymonkeyRegistryComponent::Health() const
{
    TSharedPtr<FJsonObject> Value = MakeShared<FJsonObject>();
    Value->SetStringField(TEXT("status"), TEXT("ready"));
    Value->SetStringField(TEXT("observedAt"), FDateTime::UtcNow().ToIso8601());
    return ObjectValue(Value);
}

TSharedPtr<FJsonObject> UCymonkeyRegistryComponent::DescribeResource(const FCymonkeyRegistration& Registration) const
{
    TSharedPtr<FJsonObject> Properties = MakeShared<FJsonObject>();
    if (const AActor* Actor = Cast<AActor>(Registration.Target))
    {
        Properties->SetBoolField(TEXT("visible"), !Actor->IsHidden());
    }
    else if (const USceneComponent* Component = Cast<USceneComponent>(Registration.Target))
    {
        Properties->SetBoolField(TEXT("visible"), Component->IsVisible());
    }
    TSharedPtr<FJsonObject> Value = MakeShared<FJsonObject>();
    Value->SetStringField(TEXT("id"), Registration.StableId);
    Value->SetStringField(TEXT("domain"), Jangolova::Cymonkey::DomainRender);
    Value->SetStringField(TEXT("runtime"), Jangolova::Cymonkey::RuntimeUnreal);
    Value->SetStringField(TEXT("kind"), WireKind(Registration.Kind));
    if (!Registration.Label.IsEmpty()) Value->SetStringField(TEXT("label"), Registration.Label);
    Value->SetObjectField(TEXT("properties"), Properties);
    return Value;
}

void UCymonkeyRegistryComponent::Publish(
    const FString& Type,
    const FString& SourceId,
    const TSharedPtr<FJsonObject>& Data)
{
    EventSequence++;
    TSharedPtr<FJsonObject> Event = MakeShared<FJsonObject>();
    Event->SetStringField(TEXT("id"), LexToString(EventSequence));
    Event->SetStringField(TEXT("type"), Type);
    Event->SetStringField(TEXT("domain"), Jangolova::Cymonkey::DomainRender);
    Event->SetStringField(TEXT("runtime"), Jangolova::Cymonkey::RuntimeUnreal);
    Event->SetStringField(TEXT("driver"), Jangolova::Cymonkey::DriverWebSocket);
    Event->SetStringField(TEXT("sourceId"), SourceId);
    Event->SetStringField(TEXT("occurredAt"), FDateTime::UtcNow().ToIso8601());
    Event->SetObjectField(TEXT("data"), Data);
    EventBuffer.Add(Event);
    if (EventBuffer.Num() > MaximumEvents) EventBuffer.RemoveAt(0);
}
