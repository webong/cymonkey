using UnrealBuildTool;

public class UnrealPacmanFixtureEditorTarget : TargetRules
{
    public UnrealPacmanFixtureEditorTarget(TargetInfo Target) : base(Target)
    {
        Type = TargetType.Editor;
        DefaultBuildSettings = BuildSettingsVersion.Latest;
        IncludeOrderVersion = EngineIncludeOrderVersion.Unreal5_8;
        ExtraModuleNames.AddRange(new[] { "UnrealPacmanFixture", "JangolovaCymonkey" });
    }
}
