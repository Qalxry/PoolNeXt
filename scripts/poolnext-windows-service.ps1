#Requires -Version 5.1
<#
.SYNOPSIS
    Install and manage PoolNeXt as a Windows service through NSSM.

.DESCRIPTION
    PoolNeXt ships a single Windows console executable. This helper wraps it with
    NSSM (https://nssm.cc) so it runs as a background service with a persistent
    SESSION_SECRET, a fixed working directory and rotated stdout/stderr logs.

    NSSM must be installed: pass -NssmPath, put nssm.exe in PATH, next to this
    script, or in C:\nssm\nssm.exe.

    Run the script from an elevated PowerShell session (Run as Administrator)
    for install, uninstall, start, stop, restart and update.

.PARAMETER Action
    install   Register the service, deploy the binary and start it.
    uninstall Stop and remove the service (data is kept unless -RemoveData).
    start     Start the installed service and wait until the WebUI is reachable.
    stop      Stop the installed service.
    restart   Restart the installed service and wait until it is reachable.
    status    Show service state, HTTP status and data/log locations.
    update    Replace the installed binary with -BinaryPath and restart.

.EXAMPLE
    .\poolnext-windows-service.ps1 install -BinaryPath C:\Downloads\poolnext-server-windows-amd64.exe

.EXAMPLE
    .\poolnext-windows-service.ps1 status

.EXAMPLE
    .\poolnext-windows-service.ps1 update -BinaryPath C:\Downloads\poolnext-server-windows-amd64.exe
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [ValidateSet('install', 'uninstall', 'start', 'stop', 'restart', 'status', 'update')]
    [string]$Action,

    [string]$InstallDir = 'C:\PoolNeXt',
    [string]$ServiceName = 'PoolNeXt',
    [int]$Port = 17778,
    [string]$NssmPath = '',
    [string]$BinaryPath = '',
    [string]$SessionSecret = '',
    [switch]$RemoveData,
    [int]$StartupTimeoutSeconds = 90
)

$ErrorActionPreference = 'Stop'

$BinaryName = 'poolnext-server.exe'
$SecretFileName = 'poolnext-secret'
$LogDir = Join-Path $InstallDir 'logs'
$StdoutLog = Join-Path $LogDir 'poolnext.stdout.log'
$StderrLog = Join-Path $LogDir 'poolnext.stderr.log'
$DatabasePath = Join-Path $InstallDir 'poolx.db'
$TargetBinary = Join-Path $InstallDir $BinaryName
$script:ResolvedNssm = $null

function Assert-Administrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal -ArgumentList $identity
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw 'This action requires an elevated PowerShell session. Reopen PowerShell with "Run as Administrator".'
    }
}

function Resolve-NssmExecutable {
    if ($script:ResolvedNssm) { return $script:ResolvedNssm }

    $candidates = New-Object System.Collections.Generic.List[string]
    if ($NssmPath) { $candidates.Add($NssmPath) }
    $command = Get-Command 'nssm.exe' -ErrorAction SilentlyContinue
    if ($command) { $candidates.Add($command.Source) }
    $candidates.Add((Join-Path $PSScriptRoot 'nssm.exe'))
    $programFiles = [Environment]::GetFolderPath('ProgramFiles')
    if ($programFiles) { $candidates.Add((Join-Path $programFiles 'nssm\nssm.exe')) }
    $programFilesX86 = [Environment]::GetEnvironmentVariable('ProgramFiles(x86)')
    if ($programFilesX86) { $candidates.Add((Join-Path $programFilesX86 'nssm\nssm.exe')) }
    $candidates.Add('C:\nssm\nssm.exe')

    foreach ($candidate in $candidates) {
        if ($candidate -and (Test-Path -LiteralPath $candidate -PathType Leaf)) {
            $script:ResolvedNssm = (Resolve-Path -LiteralPath $candidate).Path
            return $script:ResolvedNssm
        }
    }
    throw 'nssm.exe was not found. Install NSSM, add nssm.exe to PATH, or pass -NssmPath.'
}

function Invoke-Nssm {
    param([Parameter(ValueFromRemainingArguments = $true)][string[]]$Arguments)

    $nssm = Resolve-NssmExecutable
    $output = & $nssm @Arguments 2>&1
    if ($LASTEXITCODE -ne 0) {
        $joined = ($Arguments -join ' ')
        $details = ($output | Out-String).Trim()
        throw "nssm $joined failed with exit code $LASTEXITCODE. $details"
    }
    return $output
}

function Get-PoolNeXtService {
    return Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
}

function Assert-ServiceInstalled {
    $service = Get-PoolNeXtService
    if (-not $service) {
        throw "Service '$ServiceName' is not installed. Run this script with -Action install first."
    }
    return $service
}

function Ensure-InstallDirectory {
    $directories = @($InstallDir, $LogDir, (Join-Path $InstallDir 'upload'))
    foreach ($directory in $directories) {
        if (-not (Test-Path -LiteralPath $directory)) {
            New-Item -ItemType Directory -Path $directory -Force | Out-Null
        }
    }
}

function New-AlphanumericSecret {
    param([int]$Length = 48)

    $characters = (48..57) + (65..90) + (97..122)
    $bytes = New-Object byte[] $Length
    $random = [Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $random.GetBytes($bytes)
    } finally {
        $random.Dispose()
    }
    $builder = New-Object System.Text.StringBuilder
    foreach ($byte in $bytes) {
        [void]$builder.Append([char]$characters[$byte % $characters.Count])
    }
    return $builder.ToString()
}

function Test-SessionSecretStrength {
    param([string]$Value)

    if ($Value.Length -lt 32) {
        throw 'SESSION_SECRET must contain at least 32 characters.'
    }
    $uniqueCharacters = ($Value.ToCharArray() | Sort-Object -Unique).Count
    if ($uniqueCharacters -lt 8) {
        throw 'SESSION_SECRET must contain at least 8 distinct characters.'
    }
}

function Resolve-SessionSecret {
    $secretPath = Join-Path $InstallDir $SecretFileName
    if ($SessionSecret) {
        Test-SessionSecretStrength -Value $SessionSecret
        Set-Content -LiteralPath $secretPath -Value $SessionSecret -NoNewline -Encoding ascii
        return $SessionSecret
    }

    if (Test-Path -LiteralPath $secretPath) {
        $existing = (Get-Content -LiteralPath $secretPath -Raw).Trim()
        if ($existing.Length -ge 32) {
            Test-SessionSecretStrength -Value $existing
            return $existing
        }
        Write-Warning "Existing secret at $secretPath is too short and will be regenerated."
    }

    $generated = New-AlphanumericSecret
    Test-SessionSecretStrength -Value $generated
    Set-Content -LiteralPath $secretPath -Value $generated -NoNewline -Encoding ascii
    $null = & icacls.exe $secretPath /inheritance:r /grant:r '*S-1-5-32-544:F' '*S-1-5-18:F' 2>&1
    if ($LASTEXITCODE -ne 0) {
        Write-Warning "Could not restrict permissions on $secretPath; protect it manually."
    }
    Write-Host "Generated SESSION_SECRET and stored it at $secretPath"
    return $generated
}

function Assert-BinaryVersion {
    param([string]$Path)

    $version = & $Path '--version' 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "Version check failed for $Path. Output: $($version | Out-String)"
    }
    Write-Host ("Binary version: {0}" -f (($version | Out-String).Trim()))
}

function Wait-PoolNeXtReady {
    param([int]$TimeoutSeconds)

    $url = "http://127.0.0.1:$Port/api/status"
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    $lastError = $null
    while ((Get-Date) -lt $deadline) {
        try {
            $response = Invoke-RestMethod -Uri $url -TimeoutSec 5
            if ($response.success -and $response.data.version) {
                Write-Host ("PoolNeXt is ready on port {0} (version {1})." -f $Port, $response.data.version)
                return
            }
        } catch {
            $lastError = $_.Exception.Message
        }
        Start-Sleep -Seconds 1
    }

    Show-RecentLogs
    throw "PoolNeXt did not become ready within $TimeoutSeconds seconds. Last error: $lastError"
}

function Show-RecentLogs {
    foreach ($logPath in @($StdoutLog, $StderrLog)) {
        if (Test-Path -LiteralPath $logPath) {
            Write-Host "--- $logPath (tail) ---"
            try {
                Get-Content -LiteralPath $logPath -Tail 40 | ForEach-Object { Write-Host $_ }
            } catch {
                Write-Host "(log is currently locked by the service: $($_.Exception.Message))"
            }
        }
    }
}

function Install-PoolNeXtService {
    Assert-Administrator
    Ensure-InstallDirectory

    if ($BinaryPath) {
        if (-not (Test-Path -LiteralPath $BinaryPath -PathType Leaf)) {
            throw "BinaryPath not found: $BinaryPath"
        }
        $source = (Resolve-Path -LiteralPath $BinaryPath).Path
        $target = [System.IO.Path]::GetFullPath($TargetBinary)
        if ($source -ne $target) {
            Copy-Item -LiteralPath $source -Destination $TargetBinary -Force
        }
    }
    if (-not (Test-Path -LiteralPath $TargetBinary -PathType Leaf)) {
        throw "PoolNeXt binary not found at $TargetBinary. Pass -BinaryPath on first install."
    }
    Assert-BinaryVersion -Path $TargetBinary

    if (Get-PoolNeXtService) {
        throw "Service '$ServiceName' already exists. Use -Action update to replace its binary."
    }

    $secret = Resolve-SessionSecret

    Write-Host "Registering service '$ServiceName' with NSSM..."
    $null = Invoke-Nssm install $ServiceName $TargetBinary
    $null = Invoke-Nssm set $ServiceName DisplayName 'PoolNeXt Proxy Pool'
    $null = Invoke-Nssm set $ServiceName Description 'PoolNeXt proxy pool control plane'
    $null = Invoke-Nssm set $ServiceName AppDirectory $InstallDir
    $null = Invoke-Nssm set $ServiceName AppParameters "--port $Port"
    $null = Invoke-Nssm set $ServiceName AppEnvironmentExtra "SESSION_SECRET=$secret" "SQLITE_PATH=$DatabasePath" 'LOG_LEVEL=info'
    $null = Invoke-Nssm set $ServiceName AppStdout $StdoutLog
    $null = Invoke-Nssm set $ServiceName AppStderr $StderrLog
    $null = Invoke-Nssm set $ServiceName AppRotateFiles 1
    $null = Invoke-Nssm set $ServiceName AppRotateOnline 1
    $null = Invoke-Nssm set $ServiceName AppRotateBytes 10485760
    $null = Invoke-Nssm set $ServiceName AppExit Default Restart
    $null = Invoke-Nssm set $ServiceName AppRestartDelay 5000
    $null = Invoke-Nssm set $ServiceName AppStopMethodConsole 15000

    Set-Service -Name $ServiceName -StartupType Automatic
    Start-Service -Name $ServiceName
    Wait-PoolNeXtReady -TimeoutSeconds $StartupTimeoutSeconds

    Write-Host ''
    Write-Host "Service '$ServiceName' is installed and running."
    Write-Host ("WebUI:  http://127.0.0.1:{0}/" -f $Port)
    Write-Host ("Data:   {0}" -f $InstallDir)
    Write-Host ("Logs:   {0}" -f $LogDir)
    Write-Host ''
    Write-Host 'The one-time root password is printed to the service log during the first start:'
    if (Test-Path -LiteralPath $StdoutLog) {
        try {
            Get-Content -LiteralPath $StdoutLog -Tail 20 | ForEach-Object { Write-Host "  $_" }
        } catch {
            Write-Host "  (log is currently locked by the service: $($_.Exception.Message))"
            Write-Host "  Open $StdoutLog to read the one-time password."
        }
    }
}

function Stop-PoolNeXtService {
    $service = Assert-ServiceInstalled
    $service.Refresh()
    if ($service.Status -ne 'Stopped') {
        Write-Host "Stopping service '$ServiceName'..."
        Stop-Service -Name $ServiceName -Force
        $service.WaitForStatus([System.ServiceProcess.ServiceControllerStatus]::Stopped, [TimeSpan]::FromSeconds(30))
    }
    Write-Host "Service '$ServiceName' is stopped."
}

function Start-PoolNeXtService {
    $service = Assert-ServiceInstalled
    $service.Refresh()
    if ($service.Status -ne 'Running') {
        Write-Host "Starting service '$ServiceName'..."
        Start-Service -Name $ServiceName
    }
    Wait-PoolNeXtReady -TimeoutSeconds $StartupTimeoutSeconds
}

function Restart-PoolNeXtService {
    Stop-PoolNeXtService
    Start-PoolNeXtService
}

function Update-PoolNeXtService {
    Assert-Administrator
    $null = Assert-ServiceInstalled
    if (-not $BinaryPath) {
        throw '-Action update requires -BinaryPath pointing to the new poolnext-server-windows-amd64.exe.'
    }
    if (-not (Test-Path -LiteralPath $BinaryPath -PathType Leaf)) {
        throw "BinaryPath not found: $BinaryPath"
    }
    Assert-BinaryVersion -Path $BinaryPath

    Stop-PoolNeXtService

    if (Test-Path -LiteralPath $TargetBinary) {
        $backupPath = "$TargetBinary.bak-$(Get-Date -Format 'yyyyMMddHHmmss')"
        Copy-Item -LiteralPath $TargetBinary -Destination $backupPath -Force
        Write-Host "Previous binary backed up to $backupPath"
    }
    Copy-Item -LiteralPath $BinaryPath -Destination $TargetBinary -Force
    Start-PoolNeXtService
}

function Show-PoolNeXtStatus {
    $service = Get-PoolNeXtService
    if (-not $service) {
        Write-Host "Service '$ServiceName' is not installed."
        return
    }
    $service.Refresh()

    $version = 'unreachable'
    try {
        $response = Invoke-RestMethod -Uri "http://127.0.0.1:$Port/api/status" -TimeoutSec 5
        if ($response.data.version) { $version = $response.data.version }
    } catch {
        $version = 'unreachable'
    }

    Write-Host ("Service: {0} | Status: {1} | Startup: {2}" -f $ServiceName, $service.Status, $service.StartType)
    Write-Host ("HTTP:    http://127.0.0.1:{0}/ (version: {1})" -f $Port, $version)
    Write-Host ("Binary:  {0}" -f $TargetBinary)
    Write-Host ("Data:    {0}" -f $DatabasePath)
    Write-Host ("Logs:    {0}" -f $LogDir)
}

function Uninstall-PoolNeXtService {
    Assert-Administrator
    $service = Get-PoolNeXtService
    if (-not $service) {
        Write-Host "Service '$ServiceName' is not installed."
        return
    }
    Stop-PoolNeXtService
    $null = Invoke-Nssm remove $ServiceName confirm
    Write-Host "Service '$ServiceName' removed."

    if ($RemoveData) {
        if (Test-Path -LiteralPath $InstallDir) {
            Remove-Item -LiteralPath $InstallDir -Recurse -Force
            Write-Host "Removed $InstallDir"
        }
    } else {
        Write-Host "Data kept at $InstallDir (pass -RemoveData to delete it)."
    }
}

switch ($Action) {
    'install'   { Install-PoolNeXtService }
    'uninstall' { Uninstall-PoolNeXtService }
    'start'     { Start-PoolNeXtService }
    'stop'      { Stop-PoolNeXtService }
    'restart'   { Restart-PoolNeXtService }
    'status'    { Show-PoolNeXtStatus }
    'update'    { Update-PoolNeXtService }
}
