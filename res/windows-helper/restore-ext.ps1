# Fetch the exact ext Windows packages used by the Cymonkey helper projects.
# ext publishes these NuGet artifacts on GitHub; nuget.org does not carry them.
$ErrorActionPreference = 'Stop'
$feed = Join-Path ([System.IO.Path]::GetTempPath()) 'cymonkey-ext-windows-0.1.0'
New-Item -ItemType Directory -Path $feed -Force | Out-Null
$release = 'https://github.com/webong/ext/releases/download/ext-windows-v0.1.0'
$packages = @(
    @{ Name = 'Ext.Audio.Windows.0.1.0.nupkg'; Sha256 = '7873bb49a34d50e075abe20141b6454e2fce7b952fa95268f2695a9352be9630' },
    @{ Name = 'Ext.Web.Windows.0.1.0.nupkg'; Sha256 = '59b71a06dc5d192f985570e60e5f53284cb719245210e8fe04488b8062a20ea6' }
)
foreach ($package in $packages) {
    $target = Join-Path $feed $package.Name
    if (Test-Path $target) {
        $existing = (Get-FileHash -Path $target -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($existing -ne $package.Sha256) { throw "Cached ext artifact has the wrong checksum: $target" }
        continue
    }
    $download = "$target.download"
    Invoke-WebRequest -Uri "$release/$($package.Name)" -OutFile $download
    $actual = (Get-FileHash -Path $download -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $package.Sha256) { throw "Downloaded ext artifact has the wrong checksum: $($package.Name)" }
    Move-Item -Path $download -Destination $target
}
$projects = @(
    (Join-Path $PSScriptRoot 'WebView2/Cymonkey.Windows.WebView2.csproj'),
    (Join-Path $PSScriptRoot 'Media/Cymonkey.Windows.Media.csproj')
)
foreach ($project in $projects) {
    dotnet restore $project --source $feed --source 'https://api.nuget.org/v3/index.json'
    if ($LASTEXITCODE -ne 0) { throw "dotnet restore failed: $project" }
}
Write-Output "Restored ext Windows helper dependencies from verified packages in $feed"
