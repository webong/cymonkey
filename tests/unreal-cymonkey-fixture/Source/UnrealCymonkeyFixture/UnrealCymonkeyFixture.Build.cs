using UnrealBuildTool;

public class UnrealPacmanFixture : ModuleRules
{
    public UnrealPacmanFixture(ReadOnlyTargetRules Target) : base(Target)
    {
        PCHUsage = PCHUsageMode.UseExplicitOrSharedPCHs;
        PublicDependencyModuleNames.AddRange(new[] { "Core", "CoreUObject", "Engine", "JangolovaCymonkey" });
    }
}
