#ifndef MyAppVersion
  #define MyAppVersion "0.2.0-rc.3"
#endif
#ifndef MyAppFileVersion
  #define MyAppFileVersion "0.2.0.3"
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
VersionInfoVersion={#MyAppFileVersion}
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

; 从 0.1.0 就地升级时清理已死文件。
;
; Inno 只覆盖 [Files] 里列出的文件，不会删除「新版本不再提供」的旧文件。0.1.0 曾经
; 把旧的注入链路（assets\inject.js + assets\dream-skin.css）和 assets\assets.go 一起
; 装进目录，其中 inject.js 里写着一台具体开发机的壁纸绝对路径。这条链路在 0.1.1 里
; 已被整体删除（改由 CDP 整段注入 + internal/patcher/injected_hub.js），磁盘上那份
; 文件没有任何代码会读它 —— 但它会一直躺在安装目录里，让「这个目录到底还有没有
; 开发机数据」变成一个需要重新推理的问题。所以升级时按文件名精确清掉。
;
; 只清这五类：携带旧路径的脚本、旧的手写 CSS、不该出现在运行时目录的 .go 源码、
; 外置清单（DPI 清单现在由 go build 直接嵌进 2ag.exe，外置那份不再需要），以及
; 0.1.0 随包发出去的那张 themes\default\wallpaper.jpg —— 它不是产品素材，而是构建机
; Pictures\ 里的某一台机器的图。2Ag 的壁纸只有一个来源：用户自己在 Skin Studio 里
; 选的那张，持久化在 2ag.json 的 wallpaper_path。
; themes\ 与 plugins\ 属于可被用户自行修改的目录，所以这里不删整个目录，只按文件名
; 精确清掉这一个已知残留。
;
; ★ 故意**不**在这里清 {app}\app：0.1.0 / 0.1.1 确实往那儿装过一份冻结宿主。对新装
;   用户它是空的，对老用户它就是「用户已有的 frozen host」—— 升级时必须原样保留，
;   既不删除也不重新复制（重新复制等于替用户重做一次 570 MB 的决定，而且他在那份
;   副本上可能已经有过自己的操作）。
[InstallDelete]
Type: files; Name: "{app}\assets\inject.js"
Type: files; Name: "{app}\assets\dream-skin.css"
Type: files; Name: "{app}\assets\assets.go"
Type: files; Name: "{app}\2ag.exe.manifest"
Type: files; Name: "{app}\themes\default\wallpaper.jpg"

[Files]
Source: "{#SourceDir}\2ag.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceDir}\2ag.json"; DestDir: "{app}"; Flags: onlyifdoesntexist
Source: "{#SourceDir}\2ag.log"; DestDir: "{app}"; Flags: onlyifdoesntexist skipifsourcedoesntexist
; ★ 这里**没有** app\ —— 安装包不携带任何 Google Antigravity 运行时（0.1.0 / 0.1.1
;   曾把构建机上冻结的那份整目录打进来）。增强形态首次启动时，2Ag 会从**用户本机**
;   的官方安装目录复制出一份自己的冻结宿主，见 internal\supervisor\frozen_host.go。
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
; ★ 冻结宿主副本是**派生物**，不是用户的文档：它由 2Ag 从本机官方安装复制而来，
;   官方安装在 %LOCALAPPDATA%\Programs\Antigravity 原样留着，随时能重新建立。
;   不清它就会在卸载后留下约 570 MB 的孤儿 —— 而 0.1.0 / 0.1.1 时代它是由 [Files]
;   装进来的，Inno 的卸载日志本来就会删掉它，所以这里只是补上同一条语义。
Type: filesandordirs; Name: "{app}\app"

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
