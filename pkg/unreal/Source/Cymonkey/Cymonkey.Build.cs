using UnrealBuildTool;

public class Cymonkey : ModuleRules
{
    public Cymonkey(ReadOnlyTargetRules Target) : base(Target)
    {
        PCHUsage = PCHUsageMode.UseExplicitOrSharedPCHs;
        PublicDependencyModuleNames.AddRange(new[] { "Core", "CoreUObject", "Engine", "Json" });
        DynamicallyLoadedModuleNames.Add("WebSocketServer");
    }
}
