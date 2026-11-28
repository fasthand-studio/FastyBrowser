# Fasty voice helper: offline speech recognition with the recognizer that ships with Windows (SAPI).
# Reads the default microphone and prints one JSON object per line on stdout:
#   {"t":"ready","lang":"en-US"}  {"t":"interim","v":"..."}  {"t":"final","v":"...","c":0.93}
#   {"t":"level","v":0.42}        {"t":"error","v":"..."}
# Fasty stops it by ending the process. Nothing is written to disk and no audio leaves the PC.
param([string]$Lang = "en-US")
$ErrorActionPreference = "Stop"
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
function Emit($o) { [Console]::Out.WriteLine(($o | ConvertTo-Json -Compress)); [Console]::Out.Flush() }
try {
  Add-Type -AssemblyName System.Speech
  $installed = [System.Speech.Recognition.SpeechRecognitionEngine]::InstalledRecognizers()
  if ($installed.Count -eq 0) { Emit @{ t = "error"; v = "No speech recognizer is installed in Windows. Add a language with speech in Settings > Time & language > Speech." }; exit 2 }
  $info = $installed | Where-Object { $_.Culture.Name -ieq $Lang } | Select-Object -First 1
  if (-not $info) { $info = $installed | Where-Object { $_.Culture.TwoLetterISOLanguageName -ieq $Lang.Split("-")[0] } | Select-Object -First 1 }
  if (-not $info) { $info = $installed | Select-Object -First 1 }
  $engine = New-Object System.Speech.Recognition.SpeechRecognitionEngine($info)
  $engine.LoadGrammar((New-Object System.Speech.Recognition.DictationGrammar))
  $engine.SetInputToDefaultAudioDevice()
  $script:lastLevel = [DateTime]::UtcNow
  Register-ObjectEvent -InputObject $engine -EventName SpeechHypothesized -Action {
    Emit @{ t = "interim"; v = $EventArgs.Result.Text }
  } | Out-Null
  Register-ObjectEvent -InputObject $engine -EventName SpeechRecognized -Action {
    Emit @{ t = "final"; v = $EventArgs.Result.Text; c = [math]::Round($EventArgs.Result.Confidence, 2) }
  } | Out-Null
  Register-ObjectEvent -InputObject $engine -EventName AudioLevelUpdated -Action {
    $now = [DateTime]::UtcNow
    if (($now - $script:lastLevel).TotalMilliseconds -ge 50) { $script:lastLevel = $now; Emit @{ t = "level"; v = [math]::Round($EventArgs.AudioLevel / 100.0, 2) } }
  } | Out-Null
  $engine.RecognizeAsync([System.Speech.Recognition.RecognizeMode]::Multiple)
  Emit @{ t = "ready"; lang = $info.Culture.Name }
  while ($true) { Wait-Event -Timeout 1 | Out-Null }
} catch {
  Emit @{ t = "error"; v = $_.Exception.Message }
  exit 1
}
