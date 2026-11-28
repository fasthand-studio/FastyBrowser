# Builds Fasty for Windows: tests, executable, and (optionally) the installer and portable zip.
#   .\build.ps1                       test + build dist\fasty.exe
#   .\build.ps1 -Installer -Zip       also build dist\installer\Fasty-Setup-<ver>-x64.exe and the zip
#   .\build.ps1 -SelfTest             also run the in-app self-test (opens a window for ~30 s)
param(
  [string]$Version = "0.1.0",
  [switch]$Installer,
  [switch]$Zip,
  [switch]$SelfTest
)
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot
$env:CGO_ENABLED = "0"   # pure Go: no C compiler or Windows SDK needed

Write-Host "== go vet / go test"
go vet ./...
go test ./...

Write-Host "== resources (icon + manifest)"
# rsrc turns assets\fasty.ico and assets\fasty.manifest into a .syso that `go build` links in.
$rsrc = (Get-Command rsrc -ErrorAction SilentlyContinue).Source
if (-not $rsrc) { go install github.com/akavel/rsrc@latest; $rsrc = Join-Path (go env GOPATH) "bin\rsrc.exe" }
& $rsrc -manifest assets\fasty.manifest -ico assets\fasty.ico -arch amd64 -o rsrc_windows_amd64.syso

Write-Host "== build dist\fasty.exe"
New-Item -ItemType Directory -Force dist | Out-Null
go build -trimpath -ldflags "-s -w -H windowsgui" -o dist\fasty.exe .
"{0:N1} MB" -f ((Get-Item dist\fasty.exe).Length / 1MB)

if ($SelfTest) {
  Write-Host "== self-test"
  $p = Start-Process dist\fasty.exe -ArgumentList "--selftest" -PassThru
  if (-not $p.WaitForExit(180000)) { $p.Kill(); throw "self-test timed out" }
  if ($p.ExitCode -ne 0) { throw "self-test failed (exit $($p.ExitCode)); see fasty.log in %TEMP%\fasty-selftest-*" }
  Write-Host "self-test passed"
}

if ($Installer) {
  Write-Host "== installer"
  $iscc = (Get-Command ISCC.exe -ErrorAction SilentlyContinue).Source
  if (-not $iscc) { $iscc = @("$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe", "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe") | Where-Object { Test-Path $_ } | Select-Object -First 1 }
  if (-not $iscc) { throw "Inno Setup 6 not found. Install it with: winget install JRSoftware.InnoSetup" }
  & $iscc /Q "/DAppVersion=$Version" installer\fasty.iss
}

if ($Zip) {
  Write-Host "== portable zip"
  $stage = "dist\zip\Fasty-$Version-x64"
  Remove-Item $stage -Recurse -Force -ErrorAction SilentlyContinue
  New-Item -ItemType Directory -Force $stage | Out-Null
  Copy-Item dist\fasty.exe, assets\fasty.ico $stage
  Copy-Item ..\LICENSE "$stage\LICENSE.txt"
  Compress-Archive -Path "$stage\*" -DestinationPath "dist\Fasty-$Version-x64-portable.zip" -Force
}
Write-Host "done"
