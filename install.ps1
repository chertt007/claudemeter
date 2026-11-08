# ClaudeMeter installer for Windows.
#
#   irm https://raw.githubusercontent.com/chertt007/claudemeter/main/install.ps1 | iex
#
# Downloads the latest release for this machine (x64 or ARM64), verifies its
# checksum, installs it to %LOCALAPPDATA%\Programs\ClaudeMeter, adds that
# folder to the user PATH and creates a Start menu shortcut. Run it again to
# update. To uninstall:
#
#   & ([scriptblock]::Create((irm https://raw.githubusercontent.com/chertt007/claudemeter/main/install.ps1))) -Uninstall

param(
    [switch]$Uninstall,
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA 'Programs\ClaudeMeter'),
    [switch]$NoPath,
    [switch]$NoShortcut
)

function Install-ClaudeMeter {
    param([bool]$Uninstall, [string]$InstallDir, [bool]$NoPath, [bool]$NoShortcut)

    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue' # the progress bar makes downloads very slow in PowerShell 5.1
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    $repo = 'chertt007/claudemeter'
    $exe = Join-Path $InstallDir 'claudemeter.exe'
    $shortcut = Join-Path ([Environment]::GetFolderPath('Programs')) 'ClaudeMeter.lnk'

    function Get-UserPath {
        $key = Get-Item 'HKCU:\Environment'
        return [string]$key.GetValue('Path', '', 'DoNotExpandEnvironmentNames')
    }
    function Set-UserPath([string]$value) {
        $kind = 'ExpandString'
        try { $kind = (Get-Item 'HKCU:\Environment').GetValueKind('Path') } catch { }
        Set-ItemProperty -Path 'HKCU:\Environment' -Name 'Path' -Value $value -Type $kind
        # Setting any user variable through .NET broadcasts WM_SETTINGCHANGE,
        # so terminals started from now on see the new PATH.
        [Environment]::SetEnvironmentVariable('CLAUDEMETER_INSTALL', '1', 'User')
        [Environment]::SetEnvironmentVariable('CLAUDEMETER_INSTALL', $null, 'User')
    }
    function Test-SameDir([string]$a, [string]$b) {
        return [Environment]::ExpandEnvironmentVariables($a).TrimEnd('\') -ieq $b.TrimEnd('\')
    }

    if ($Uninstall) {
        Get-Process claudemeter -ErrorAction SilentlyContinue |
            Where-Object { $_.Path -and (Test-SameDir (Split-Path $_.Path) $InstallDir) } |
            Stop-Process -Force
        if (Test-Path $InstallDir) { Remove-Item $InstallDir -Recurse -Force }
        if (Test-Path $shortcut) { Remove-Item $shortcut -Force }
        $parts = (Get-UserPath) -split ';' | Where-Object { $_ -and -not (Test-SameDir $_ $InstallDir) }
        Set-UserPath ($parts -join ';')
        Write-Host 'ClaudeMeter removed. Settings in %APPDATA%\ClaudeMeter were kept.' -ForegroundColor Green
        return
    }

    # The native architecture. $env:PROCESSOR_ARCHITECTURE reports AMD64 inside
    # an emulated x64 PowerShell on ARM, the registry value does not.
    $arch = (Get-ItemProperty 'HKLM:\SYSTEM\CurrentControlSet\Control\Session Manager\Environment').PROCESSOR_ARCHITECTURE
    switch ($arch) {
        'ARM64' { $asset = 'claudemeter_windows_arm64.exe' }
        'AMD64' { $asset = 'claudemeter_windows_amd64.exe' }
        default { throw "Unsupported processor architecture: $arch (ClaudeMeter needs 64-bit Windows)." }
    }

    $base = "https://github.com/$repo/releases/latest/download"
    Write-Host "Downloading $asset ..."
    New-Item -ItemType Directory -Force $InstallDir | Out-Null
    $tmp = Join-Path $InstallDir 'claudemeter.download'
    Invoke-WebRequest "$base/$asset" -OutFile $tmp -UseBasicParsing

    $sums = (Invoke-WebRequest "$base/SHA256SUMS.txt" -UseBasicParsing).Content
    if ($sums -is [byte[]]) { $sums = [Text.Encoding]::UTF8.GetString($sums) }
    $line = ($sums -split "`n") | Where-Object { $_ -match "\s\*?$([regex]::Escape($asset))\s*$" } | Select-Object -First 1
    if (-not $line) { Remove-Item $tmp -Force; throw "No checksum for $asset in the release." }
    $want = ($line -split '\s+')[0]
    $got = (Get-FileHash $tmp -Algorithm SHA256).Hash
    if ($got -ine $want) { Remove-Item $tmp -Force; throw "Checksum mismatch for $asset; nothing was installed." }

    # A running claudemeter.exe can't be overwritten, but it can be renamed.
    $old = Join-Path $InstallDir 'claudemeter.old.exe'
    if (Test-Path $old) { Remove-Item $old -Force -ErrorAction SilentlyContinue }
    $running = $false
    if (Test-Path $exe) {
        try { Move-Item $exe $old -Force } catch { throw "Could not replace $exe. Close ClaudeMeter and run the installer again." }
        $running = [bool](Get-Process claudemeter -ErrorAction SilentlyContinue |
            Where-Object { $_.Path -and (Test-SameDir (Split-Path $_.Path) $InstallDir) })
        if (-not $running) { Remove-Item $old -Force -ErrorAction SilentlyContinue }
    }
    Move-Item $tmp $exe -Force

    if (-not $NoPath) {
        $userPath = Get-UserPath
        if (-not ($userPath -split ';' | Where-Object { $_ -and (Test-SameDir $_ $InstallDir) })) {
            Set-UserPath ((@($userPath.TrimEnd(';')) + $InstallDir | Where-Object { $_ }) -join ';')
        }
        if (-not ($env:Path -split ';' | Where-Object { $_ -and (Test-SameDir $_ $InstallDir) })) {
            $env:Path = $env:Path.TrimEnd(';') + ';' + $InstallDir
        }
    }

    if (-not $NoShortcut) {
        $ws = New-Object -ComObject WScript.Shell
        $lnk = $ws.CreateShortcut($shortcut)
        $lnk.TargetPath = $exe
        $lnk.WorkingDirectory = $InstallDir
        $lnk.Description = 'Claude Code subscription limits'
        $lnk.Save()
    }

    $version = (& $exe --version) -replace '^claudemeter\s*', ''
    Write-Host ''
    Write-Host "ClaudeMeter $version installed to $InstallDir" -ForegroundColor Green
    if ($running) {
        Write-Host 'ClaudeMeter is running: restart it to use the new version.' -ForegroundColor Yellow
    }
    Write-Host 'Run it with:  claudemeter   (or from the Start menu: ClaudeMeter)'
    Write-Host 'It needs Claude Code installed and logged in at least once.'
}

Install-ClaudeMeter -Uninstall:$Uninstall -InstallDir $InstallDir -NoPath:$NoPath -NoShortcut:$NoShortcut
