# Install the latest release into .\plugins\windows\amd64. Run it in the CLIProxyAPI working directory.
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$repo = "haowang02/cpa-plugin-codex-candy-eval"
$name = "cpa-codex-candy-eval"

if ($env:PROCESSOR_ARCHITECTURE -ne "AMD64") {
    throw "${name}: only Windows x64 is supported"
}

$tag = (Invoke-RestMethod -UseBasicParsing "https://api.github.com/repos/$repo/releases/latest").tag_name
$version = $tag.TrimStart('v')
$asset = "${name}_${version}_windows_amd64.zip"
$base = "https://github.com/$repo/releases/download/$tag"
$dir = Join-Path (Join-Path "plugins" "windows") "amd64"
$file = "$name-v$version.dll"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("$name." + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $tmp | Out-Null

try {
    Write-Host "Downloading $asset..."
    Invoke-WebRequest -UseBasicParsing "$base/$asset" -OutFile (Join-Path $tmp $asset)
    Invoke-WebRequest -UseBasicParsing "$base/checksums.txt" -OutFile (Join-Path $tmp "checksums.txt")

    $line = Select-String -Path (Join-Path $tmp "checksums.txt") -Pattern ("\s" + [regex]::Escape($asset) + "$")
    $actual = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp $asset)).Hash
    if (-not $line -or $line.Line.Split(" ")[0] -ne $actual) {
        throw "${name}: checksum mismatch for $asset"
    }

    Expand-Archive -LiteralPath (Join-Path $tmp $asset) -DestinationPath $tmp -Force
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    Move-Item -Force (Join-Path $tmp "$name.dll") (Join-Path $dir $file)
    Write-Host "Installed: $(Resolve-Path (Join-Path $dir $file))"
}
finally {
    Remove-Item -Recurse -Force $tmp
}
