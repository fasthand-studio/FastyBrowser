# Captures ONLY the Fasty window (PrintWindow), never the screen, so nothing else is recorded.
param([string]$Out, [string]$ProcessName = "fasty")
Add-Type -AssemblyName System.Drawing
Add-Type @'
using System; using System.Runtime.InteropServices;
public class PW {
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out R r);
  [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h, IntPtr dc, uint flags);
  public struct R { public int L, T, Rt, B; }
}
'@
$p = Get-Process $ProcessName | Select-Object -First 1
$p.Refresh()
$r = New-Object PW+R
[PW]::GetWindowRect($p.MainWindowHandle, [ref]$r) | Out-Null
$bmp = New-Object System.Drawing.Bitmap ($r.Rt - $r.L), ($r.B - $r.T)
$g = [System.Drawing.Graphics]::FromImage($bmp)
$dc = $g.GetHdc()
[PW]::PrintWindow($p.MainWindowHandle, $dc, 2) | Out-Null
$g.ReleaseHdc($dc)
$bmp.Save($Out)
"saved $($bmp.Width)x$($bmp.Height)"
