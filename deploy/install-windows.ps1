# Install CookBook on a Windows host as a scheduled task that starts at boot
# as SYSTEM - so it runs with nobody logged in (the lesson from Remoter:
# anything bound to a login session is unreachable after a reboot).
#
#   powershell -ExecutionPolicy Bypass -File .\deploy\install-windows.ps1 -Binary .\server\dist\cookbook-windows-amd64.exe
#
# Idempotent: re-run to upgrade. Data in %ProgramData%\CookBook is never touched.
param(
    [Parameter(Mandatory = $true)][string]$Binary,
    [string]$Importer = '',
    [string]$GeminiKey = '',
    [string]$GroqKey = ''
)
$ErrorActionPreference = 'Stop'

$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    # Self-elevate: shells on this kind of box run unelevated even for admins.
    $argList = @('-ExecutionPolicy', 'Bypass', '-File', "`"$PSCommandPath`"", '-Binary', "`"$(Resolve-Path $Binary)`"")
    if ($Importer) { $argList += @('-Importer', "`"$(Resolve-Path $Importer)`"") }
    if ($GeminiKey) { $argList += @('-GeminiKey', $GeminiKey) }
    if ($GroqKey) { $argList += @('-GroqKey', $GroqKey) }
    Start-Process powershell -Verb RunAs -ArgumentList $argList -Wait
    exit
}

$installDir = Join-Path $env:ProgramFiles 'CookBook'
$dataDir = Join-Path $env:ProgramData 'CookBook'
New-Item -ItemType Directory -Force $installDir, $dataDir | Out-Null

Stop-ScheduledTask -TaskName 'CookBook' -ErrorAction SilentlyContinue
Get-Process cookbook -ErrorAction SilentlyContinue | Stop-Process -Force
Copy-Item $Binary (Join-Path $installDir 'cookbook.exe') -Force
if ($Importer) { Copy-Item $Importer (Join-Path $installDir 'cookbook-import.exe') -Force }

# API keys go into config.json (the scheduled task has no user environment).
$configPath = Join-Path $dataDir 'config.json'
$config = @{}
if (Test-Path $configPath) {
    (Get-Content $configPath -Raw | ConvertFrom-Json).PSObject.Properties | ForEach-Object { $config[$_.Name] = $_.Value }
}
if ($GeminiKey) { $config['gemini_api_key'] = $GeminiKey }
if ($GroqKey) { $config['groq_api_key'] = $GroqKey }
# Typst installed per-user (winget/scoop) is not on SYSTEM's PATH; pin it.
$typst = Get-Command typst -ErrorAction SilentlyContinue
if ($typst -and -not $config.ContainsKey('typst')) { $config['typst'] = $typst.Source }
# Same for ffmpeg (optional: poster frames for uploaded videos).
$ffmpeg = Get-Command ffmpeg -ErrorAction SilentlyContinue
if ($ffmpeg -and -not $config.ContainsKey('ffmpeg')) { $config['ffmpeg'] = $ffmpeg.Source }
if ($config.Count -gt 0) {
    $config | ConvertTo-Json | Out-File -Encoding utf8 $configPath
}

$exe = Join-Path $installDir 'cookbook.exe'
$action = New-ScheduledTaskAction -Execute $exe -Argument "-data `"$dataDir`"" -WorkingDirectory $dataDir
$trigger = New-ScheduledTaskTrigger -AtStartup
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
    -ExecutionTimeLimit ([TimeSpan]::Zero) -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -StartWhenAvailable
$task = New-ScheduledTaskPrincipal -UserId 'SYSTEM' -LogonType ServiceAccount -RunLevel Highest
Register-ScheduledTask -TaskName 'CookBook' -Action $action -Trigger $trigger -Settings $settings -Principal $task -Force | Out-Null

# Inbound rule scoped to the tailnet only.
Remove-NetFirewallRule -DisplayName 'CookBook (Tailscale)' -ErrorAction SilentlyContinue
New-NetFirewallRule -DisplayName 'CookBook (Tailscale)' -Direction Inbound -Protocol TCP -LocalPort 8738 `
    -RemoteAddress 100.64.0.0/10 -Action Allow -Program $exe | Out-Null

Start-ScheduledTask -TaskName 'CookBook'
Start-Sleep -Seconds 2

if (-not $typst -and -not $config.ContainsKey('typst')) {
    Write-Warning 'typst not found: PDFs are disabled. Install with "winget install --id Typst.Typst" and re-run this script.'
}
$token = Get-Content (Join-Path $dataDir 'token') -ErrorAction SilentlyContinue
Write-Host "CookBook installed. Data: $dataDir"
Write-Host "API token: $token"
$ts = Get-Command tailscale -ErrorAction SilentlyContinue
if ($ts) { Write-Host "Tailscale address: $(& tailscale ip -4 | Select-Object -First 1):8738" }
Read-Host 'Press Enter to close'
