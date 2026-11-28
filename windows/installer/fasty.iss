; Inno Setup script for Fasty on Windows.
; Build:  ISCC.exe /DAppVersion=0.1.0 installer\fasty.iss   (run from the windows\ folder, after building dist\fasty.exe)

#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif

[Setup]
AppId={{6D7D1C3A-5B0E-4E57-9F0B-FA57F00D0001}
AppName=Fasty
AppVersion={#AppVersion}
AppPublisher=FastHand Studio
AppPublisherURL=https://fasthandstudio.asia/
AppSupportURL=https://github.com/fasthand-studio/FastyBrowser/issues
AppUpdatesURL=https://github.com/fasthand-studio/FastyBrowser/releases
DefaultDirName={autopf}\Fasty
DefaultGroupName=Fasty
UninstallDisplayIcon={app}\fasty.exe
SetupIconFile=..\assets\fasty.ico
OutputDir=..\dist\installer
OutputBaseFilename=Fasty-Setup-{#AppVersion}-x64
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
; Installs for the current user without admin rights; the user can still choose "all users".
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
; Windows 10 (build 10240) and Windows 11
MinVersion=10.0.10240
CloseApplications=yes
RestartApplications=no
LicenseFile=..\..\LICENSE

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "Create a &desktop shortcut"; GroupDescription: "Shortcuts:"; Flags: unchecked
Name: "defaultapps"; Description: "List Fasty in Windows &Default apps (you still choose it yourself)"; GroupDescription: "Integration:"

[Files]
Source: "..\dist\fasty.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\assets\fasty.ico"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\LICENSE"; DestDir: "{app}"; DestName: "LICENSE.txt"; Flags: ignoreversion
Source: "..\third_party\go-webview2\LICENSE"; DestDir: "{app}\licenses"; DestName: "go-webview2-LICENSE.txt"; Flags: ignoreversion

[Icons]
Name: "{group}\Fasty"; Filename: "{app}\fasty.exe"; IconFilename: "{app}\fasty.ico"
Name: "{group}\Uninstall Fasty"; Filename: "{uninstallexe}"
Name: "{autodesktop}\Fasty"; Filename: "{app}\fasty.exe"; IconFilename: "{app}\fasty.ico"; Tasks: desktopicon

[Registry]
; Makes Fasty appear under Settings > Apps > Default apps. Windows requires the user to pick the
; default browser themselves; nothing here changes the current default.
Root: HKA; Subkey: "Software\Clients\StartMenuInternet\Fasty"; ValueType: string; ValueName: ""; ValueData: "Fasty"; Flags: uninsdeletekey; Tasks: defaultapps
Root: HKA; Subkey: "Software\Clients\StartMenuInternet\Fasty\Capabilities"; ValueType: string; ValueName: "ApplicationName"; ValueData: "Fasty"; Tasks: defaultapps
Root: HKA; Subkey: "Software\Clients\StartMenuInternet\Fasty\Capabilities"; ValueType: string; ValueName: "ApplicationDescription"; ValueData: "A private, lightweight browser from FastHand Studio."; Tasks: defaultapps
Root: HKA; Subkey: "Software\Clients\StartMenuInternet\Fasty\Capabilities\URLAssociations"; ValueType: string; ValueName: "http"; ValueData: "FastyURL"; Tasks: defaultapps
Root: HKA; Subkey: "Software\Clients\StartMenuInternet\Fasty\Capabilities\URLAssociations"; ValueType: string; ValueName: "https"; ValueData: "FastyURL"; Tasks: defaultapps
Root: HKA; Subkey: "Software\Clients\StartMenuInternet\Fasty\DefaultIcon"; ValueType: string; ValueName: ""; ValueData: "{app}\fasty.exe,0"; Tasks: defaultapps
Root: HKA; Subkey: "Software\Clients\StartMenuInternet\Fasty\shell\open\command"; ValueType: string; ValueName: ""; ValueData: """{app}\fasty.exe"""; Tasks: defaultapps
Root: HKA; Subkey: "Software\RegisteredApplications"; ValueType: string; ValueName: "Fasty"; ValueData: "Software\Clients\StartMenuInternet\Fasty\Capabilities"; Flags: uninsdeletevalue; Tasks: defaultapps
Root: HKA; Subkey: "Software\Classes\FastyURL"; ValueType: string; ValueName: ""; ValueData: "Fasty URL"; Flags: uninsdeletekey; Tasks: defaultapps
Root: HKA; Subkey: "Software\Classes\FastyURL"; ValueType: string; ValueName: "URL Protocol"; ValueData: ""; Tasks: defaultapps
Root: HKA; Subkey: "Software\Classes\FastyURL\DefaultIcon"; ValueType: string; ValueName: ""; ValueData: "{app}\fasty.exe,0"; Tasks: defaultapps
Root: HKA; Subkey: "Software\Classes\FastyURL\shell\open\command"; ValueType: string; ValueName: ""; ValueData: """{app}\fasty.exe"" ""%1"""; Tasks: defaultapps

[Run]
Filename: "{app}\fasty.exe"; Description: "Launch Fasty"; Flags: nowait postinstall skipifsilent

[Code]
// Fasty draws pages with the Microsoft Edge WebView2 Runtime. It ships with Windows 11 and with
// current Windows 10 updates; if it is missing, say so instead of installing a browser that cannot start.
function WebView2Installed(): Boolean;
var
  v: String;
begin
  Result :=
    RegQueryStringValue(HKLM32, 'SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}', 'pv', v) or
    RegQueryStringValue(HKLM, 'SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}', 'pv', v) or
    RegQueryStringValue(HKCU, 'Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}', 'pv', v);
  Result := Result and (v <> '') and (v <> '0.0.0.0');
end;

function InitializeSetup(): Boolean;
begin
  Result := True;
  if not WebView2Installed() then
    Result := MsgBox('Fasty needs the Microsoft Edge WebView2 Runtime, which was not found on this PC.' + #13#10#13#10 +
      'You can install it from https://go.microsoft.com/fwlink/p/?LinkId=2124703 and then run this setup again.' + #13#10#13#10 +
      'Install Fasty anyway?', mbConfirmation, MB_YESNO) = IDYES;
end;
