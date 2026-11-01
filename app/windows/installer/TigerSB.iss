; The TigerSB installer for Windows (Inno Setup 6). CI builds it once per
; architecture:
;   iscc /DArch=x64 /DAppVersion=0.1.0 /DAppBuild=12 /DAppDir=... /DServiceDir=... TigerSB.iss
; AppDir is Flutter's Release folder; ServiceDir holds tigersb-service.exe,
; wintun.dll and wintun-LICENSE.txt of the same architecture.
;
; It installs the app, registers the VPN service and the tigersb:// links.
; An update installs over it: the service is stopped first and started again.

#ifndef Arch
  #define Arch "x64"
#endif
#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif
#ifndef AppBuild
  #define AppBuild "0"
#endif

#if Arch == "arm64"
  #define Allowed "arm64"
#else
  ; Not on ARM64 Windows, which needs the ARM64 driver of Wintun.
  #define Allowed "x64os"
#endif

[Setup]
AppId={{6F3D1C2A-7B4E-4E1B-9A57-2C8E5D0B7F31}
AppName=TigerSB
AppVersion={#AppVersion}
AppVerName=TigerSB {#AppVersion}
VersionInfoVersion={#AppVersion}.{#AppBuild}
AppPublisher=TigerSB
AppPublisherURL=https://github.com/Stilltiger45/TigerSB-client
AppSupportURL=https://github.com/Stilltiger45/TigerSB-client/issues
DefaultDirName={autopf}\TigerSB
DisableProgramGroupPage=yes
DisableDirPage=yes
PrivilegesRequired=admin
ArchitecturesAllowed={#Allowed}
ArchitecturesInstallIn64BitMode={#Allowed}
MinVersion=10.0.17763
OutputDir=.
OutputBaseFilename=TigerSB-windows-{#Arch}
SetupIconFile=..\runner\resources\app_icon.ico
UninstallDisplayIcon={app}\TigerSB.exe
UninstallDisplayName=TigerSB
WizardStyle=modern
Compression=lzma2/max
SolidCompression=yes
; The app is closed for the update and opened again after it.
CloseApplications=force
RestartApplications=no
ShowLanguageDialog=no

[Languages]
Name: "ru"; MessagesFile: "compiler:Languages\Russian.isl"

[Tasks]
Name: "desktopicon"; Description: "Значок на рабочем столе"

[Files]
Source: "{#AppDir}\*"; DestDir: "{app}"; Flags: recursesubdirs ignoreversion
Source: "{#ServiceDir}\tigersb-service.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#ServiceDir}\wintun.dll"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#ServiceDir}\wintun-LICENSE.txt"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\TigerSB"; Filename: "{app}\TigerSB.exe"
Name: "{autodesktop}\TigerSB"; Filename: "{app}\TigerSB.exe"; Tasks: desktopicon

[Registry]
; tigersb:// links open the app with the key.
Root: HKA; Subkey: "Software\Classes\tigersb"; ValueType: string; ValueData: "URL:TigerSB"; Flags: uninsdeletekey
Root: HKA; Subkey: "Software\Classes\tigersb"; ValueType: string; ValueName: "URL Protocol"; ValueData: ""
Root: HKA; Subkey: "Software\Classes\tigersb\DefaultIcon"; ValueType: string; ValueData: "{app}\TigerSB.exe,0"
Root: HKA; Subkey: "Software\Classes\tigersb\shell\open\command"; ValueType: string; ValueData: """{app}\TigerSB.exe"" ""%1"""
; «Запуск с системой» writes this value; the uninstaller removes it.
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: none; ValueName: "TigerSB"; Flags: uninsdeletevalue dontcreatekey

[Run]
Filename: "{app}\tigersb-service.exe"; Parameters: "install"; Flags: runhidden waituntilterminated; StatusMsg: "Запускаю службу VPN…"
; Without administrator rights, as the user who installed it.
Filename: "{app}\TigerSB.exe"; Description: "Открыть TigerSB"; Flags: nowait postinstall runasoriginaluser

[UninstallRun]
Filename: "{app}\tigersb-service.exe"; Parameters: "uninstall"; Flags: runhidden waituntilterminated; RunOnceId: "RemoveService"

[UninstallDelete]
Type: filesandordirs; Name: "{commonappdata}\TigerSB\data\geodata"

[Code]
// The running service holds its files: stop it before they are replaced.
function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  Code, I: Integer;
begin
  Result := '';
  if Exec(ExpandConstant('{sys}\sc.exe'), 'stop TigerSB', '', SW_HIDE, ewWaitUntilTerminated, Code) and (Code = 0) then
    for I := 1 to 30 do
    begin
      Sleep(500);
      Exec(ExpandConstant('{cmd}'), '/c sc.exe query TigerSB | find "STOPPED"', '', SW_HIDE, ewWaitUntilTerminated, Code);
      if Code = 0 then
        Break;
    end;
end;
