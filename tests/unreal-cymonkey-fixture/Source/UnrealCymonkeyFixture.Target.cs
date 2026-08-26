using UnrealBuildTool;

public class UnrealCymonkeyFixtureTarget : TargetRules
{
    public UnrealCymonkeyFixtureTarget(TargetInfo Target) : base(Target)
    {
        Type = TargetType.Game;
        DefaultBuildSettings = BuildSettingsVersion.Latest;
        IncludeOrderVersion = EngineIncludeOrderVersion.Unreal5_8;
        ExtraModuleNames.AddRange(new[] { "UnrealPacmanFixture", "JangolovaCymonkey" });
    }
}
