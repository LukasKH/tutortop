# Installs the latest Tutortop release on Windows:
#
#   irm https://raw.githubusercontent.com/LukasKH/tutortop/main/install.ps1 | iex
#
# Everything is in one script block, so a download cut off halfway runs nothing.
& {
    $ErrorActionPreference = 'Stop'
    # Invoke-WebRequest downloads far slower while it draws a progress bar.
    $ProgressPreference = 'SilentlyContinue'

    if (-not [Environment]::Is64BitOperatingSystem) {
        throw 'Tutortop for Windows is built for 64-bit Windows only.'
    }

    $name = 'tutortop-windows-x64-setup.exe'
    $url = "https://github.com/LukasKH/tutortop/releases/latest/download/$name"
    $installer = Join-Path ([IO.Path]::GetTempPath()) $name

    Write-Host 'Downloading Tutortop'
    Invoke-WebRequest $url -OutFile $installer -UseBasicParsing
    try {
        # /S installs per-user into %LOCALAPPDATA%\Programs\Tutortop without the wizard or UAC.
        Write-Host 'Installing Tutortop'
        $process = Start-Process $installer -ArgumentList '/S' -Wait -PassThru
        if ($process.ExitCode -ne 0) {
            throw "The installer failed with exit code $($process.ExitCode)."
        }
    }
    finally {
        Remove-Item $installer -ErrorAction SilentlyContinue
    }
    Write-Host 'Tutortop is installed. Open it from the Start menu.'
}
