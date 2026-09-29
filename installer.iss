#ifndef MyAppVersion
  #define MyAppVersion "0.1.0"
#endif
#ifndef SourceDir
  #define SourceDir "staging"
#endif
#ifndef OutputDir
  #define OutputDir "dist"
#endif

#define AppName "Anti-Antigravity"
#define AppShortName "2Ag"

[Setup]
AppId={{F5D0B4CE-6B17-4A06-93A0-2A6000000001}
AppName={#AppName}
AppVersion={#MyAppVersion}
AppPublisher=Anti-Antigravity
DefaultDirName={localappdata}\Programs\Anti-Antigravity
DefaultGroupName={#AppName}
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir={#OutputDir}
OutputBaseFilename=Anti-Antigravity-Setup-x64
SetupIconFile={#SourceDir}\assets\icon.ico
UninstallDisplayIcon={app}\assets\icon.ico
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
ChangesAssociations=no

[Files]
Source: "{#SourceDir}\2ag.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceDir}\2ag.json"; DestDir: "{app}"; Flags: onlyifdoesntexist
Source: "{#SourceDir}\2ag.log"; DestDir: "{app}"; Flags: onlyifdoesntexist skipifsourcedoesntexist
Source: "{#SourceDir}\app\*"; DestDir: "{app}\app"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#SourceDir}\themes\*"; DestDir: "{app}\themes"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#SourceDir}\assets\*"; DestDir: "{app}\assets"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#SourceDir}\plugins\*"; DestDir: "{app}\plugins"; Flags: ignoreversion recursesubdirs createallsubdirs

[Tasks]
Name: "desktopicon"; Description: "创建桌面快捷方式 (&Create a desktop shortcut)"; GroupDescription: "快捷方式设置:"; Flags: checkedonce

[Icons]
Name: "{userdesktop}\2Ag"; Filename: "{app}\2ag.exe"; IconFilename: "{app}\assets\icon.ico"; WorkingDir: "{app}"; Tasks: desktopicon
Name: "{userdesktop}\Anti-Antigravity"; Filename: "{app}\2ag.exe"; IconFilename: "{app}\assets\icon.ico"; WorkingDir: "{app}"; Tasks: desktopicon
Name: "{userprograms}\2Ag"; Filename: "{app}\2ag.exe"; IconFilename: "{app}\assets\icon.ico"; WorkingDir: "{app}"
Name: "{group}\2Ag"; Filename: "{app}\2ag.exe"; IconFilename: "{app}\assets\icon.ico"; WorkingDir: "{app}"
Name: "{group}\Anti-Antigravity"; Filename: "{app}\2ag.exe"; IconFilename: "{app}\assets\icon.ico"; WorkingDir: "{app}"
Name: "{group}\卸载 2Ag"; Filename: "{uninstallexe}"; IconFilename: "{app}\assets\icon.ico"; WorkingDir: "{app}"

[Run]
Filename: "{app}\2ag.exe"; Description: "启动 2Ag (Launch 2Ag)"; Flags: nowait postinstall skipifsilent

[UninstallDelete]
Type: files; Name: "{app}\2ag.json"
Type: files; Name: "{app}\2ag.log"

[Code]
procedure SHChangeNotify(wEventId: Integer; uFlags: Cardinal; dwItem1, dwItem2: Integer);
  external 'SHChangeNotify@shell32.dll stdcall';

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then
  begin
    SHChangeNotify($08000000, $0000, 0, 0); // SHCNE_ASSOCCHANGED
    SHChangeNotify($00001000, $0005, 0, 0); // SHCNE_UPDATEDIR
  end;
end;
