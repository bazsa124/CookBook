# Cross-compile the server and importer for every supported host into dist\.
# Same as build.sh, for a Windows box without a POSIX shell.
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot
$version = if ($env:VERSION) { $env:VERSION } else { '0.1.0' }
New-Item -ItemType Directory -Force dist | Out-Null
$env:CGO_ENABLED = '0'
foreach ($target in 'linux/amd64', 'linux/arm64', 'linux/386', 'windows/amd64') {
    $os, $arch = $target.Split('/')
    $ext = if ($os -eq 'windows') { '.exe' } else { '' }
    foreach ($cmd in 'cookbook', 'cookbook-import') {
        Write-Host "building $cmd $os/$arch"
        $env:GOOS = $os; $env:GOARCH = $arch
        go build -trimpath -ldflags "-s -w -X main.version=$version" -o "dist/$cmd-$os-$arch$ext" "./cmd/$cmd"
        if ($LASTEXITCODE -ne 0) { throw "build failed: $cmd $target" }
    }
}
Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED
