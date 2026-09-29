using UnrealBuildTool;

public class JangolovaCymonkey : ModuleRules
{
    public JangolovaCymonkey(ReadOnlyTargetRules Target) : base(Target)
    {
        PCHUsage = PCHUsageMode.UseExplicitOrSharedPCHs;
        PublicDependencyModuleNames.AddRange(new[] { "Core", "CoreUObject", "Engine", "Json" });
        DynamicallyLoadedModuleNames.Add("WebSocketServer");
    }
}
