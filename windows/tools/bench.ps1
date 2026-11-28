# Measures memory and idle CPU of fasty.exe plus every WebView2 process it started.
# Uses a throwaway profile (FASTY_PROFILE), never your real data.
param(
  [string]$Exe = "$PSScriptRoot\..\dist\fasty.exe",
  [int]$SleepMinutes = 5,
  [string[]]$Urls = @("https://example.com", "https://en.wikipedia.org/wiki/Web_browser", "https://github.com", "https://www.mozilla.org", "https://news.ycombinator.com"),
  [int]$SettleSeconds = 25,
  [int]$IdleSeconds = 15
)
$profileDir = Join-Path $env:TEMP ("fasty-bench-" + [guid]::NewGuid().ToString("N").Substring(0, 8))
New-Item -ItemType Directory -Force "$profileDir\data" | Out-Null
@{ searchEngine = "duckduckgo"; shield = $true; extendedShield = $false; sleepMinutes = $SleepMinutes; restoreSession = $false; homepage = "" } |
  ConvertTo-Json | Set-Content "$profileDir\data\settings.json"
$env:FASTY_PROFILE = $profileDir
$p = Start-Process $Exe -ArgumentList $Urls -PassThru
Start-Sleep $SettleSeconds

function Get-Tree($rootPid) {
  $all = Get-CimInstance Win32_Process | Select-Object ProcessId, ParentProcessId, Name
  $ids = [System.Collections.Generic.HashSet[int]]::new(); [void]$ids.Add($rootPid)
  do { $n = $ids.Count; foreach ($q in $all) { if ($ids.Contains([int]$q.ParentProcessId)) { [void]$ids.Add([int]$q.ProcessId) } } } while ($ids.Count -ne $n)
  $ids | ForEach-Object { Get-Process -Id $_ -ErrorAction SilentlyContinue }
}
$t0 = Get-Tree $p.Id; $cpu0 = ($t0 | Measure-Object -Property CPU -Sum).Sum
Start-Sleep $IdleSeconds
$t1 = Get-Tree $p.Id; $cpu1 = ($t1 | Measure-Object -Property CPU -Sum).Sum
$ws = [math]::Round((($t1 | Measure-Object -Property WorkingSet64 -Sum).Sum) / 1MB)
$priv = [math]::Round((($t1 | Measure-Object -Property PrivateMemorySize64 -Sum).Sum) / 1MB)
$cores = (Get-CimInstance Win32_ComputerSystem).NumberOfLogicalProcessors
$idlePct = [math]::Round((($cpu1 - $cpu0) / $IdleSeconds / $cores) * 100, 2)
[pscustomobject]@{
  SleepMinutes = $SleepMinutes; Tabs = $Urls.Count; Processes = ($t1 | Measure-Object).Count
  WorkingSetMB = $ws; PrivateMB = $priv; IdleCpuPercentOfAllCores = $idlePct
} | Format-List
$p | Stop-Process -Force
Start-Sleep 2
Get-Process msedgewebview2 -ErrorAction SilentlyContinue | Where-Object { $_.Path -and (Get-CimInstance Win32_Process -Filter "ProcessId=$($_.Id)").CommandLine -like "*$profileDir*" } | Stop-Process -Force -ErrorAction SilentlyContinue
Remove-Item $profileDir -Recurse -Force -ErrorAction SilentlyContinue
