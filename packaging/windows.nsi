; krouter Windows installer (NSIS)
; Build: makensis packaging/windows.nsi
; Requires: NSIS 3.x, krouter.exe and krouter-installer.exe in dist/

!define APPNAME "krouter"
!define APPVERSION "{{VERSION}}"
!define PUBLISHER "kinthai team"
!define WEBSITE "https://kinthai.ai"
!define DAEMON_SRC    "dist/krouter-windows.exe"
!define INSTALLER_SRC "dist/krouter-installer-windows.exe"

Name "${APPNAME} ${APPVERSION}"
OutFile "dist/krouter-${APPVERSION}-setup.exe"

; No admin required — install to LOCALAPPDATA.
RequestExecutionLevel user

; Default install dir: %LOCALAPPDATA%\kinthai
InstallDir "$LOCALAPPDATA\kinthai"
InstallDirRegKey HKCU "Software\${APPNAME}" "InstallDir"

; Modern UI.
!include "MUI2.nsh"
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

; ── Installer ──────────────────────────────────────────────────────────────────

Section "Install"
  SetOutPath "$INSTDIR"
  File /oname=krouter.exe           "${DAEMON_SRC}"
  File /oname=krouter-installer.exe "${INSTALLER_SRC}"

  ; Register install path.
  WriteRegStr HKCU "Software\${APPNAME}" "InstallDir" "$INSTDIR"

  ; Register krouter daemon as a Task Scheduler user task (auto-start, no admin).
  DetailPrint "Registering daemon service..."
  nsExec::ExecToLog '"$INSTDIR\krouter.exe" task-install'

  ; Write uninstaller.
  WriteUninstaller "$INSTDIR\uninstall.exe"

  ; Add to Add/Remove Programs.
  WriteRegStr HKCU \
    "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPNAME}" \
    "DisplayName" "${APPNAME} ${APPVERSION}"
  WriteRegStr HKCU \
    "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPNAME}" \
    "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKCU \
    "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPNAME}" \
    "Publisher" "${PUBLISHER}"
  WriteRegStr HKCU \
    "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPNAME}" \
    "URLInfoAbout" "${WEBSITE}"

SectionEnd

; After the install dialog closes, launch the browser-based wizard.
; krouter-installer is built with -H windowsgui so no console window appears.
Function .onInstSuccess
  Exec '"$INSTDIR\krouter-installer.exe"'
FunctionEnd

; ── Uninstaller ────────────────────────────────────────────────────────────────

Section "Uninstall"
  ; Stop daemon and remove the Task Scheduler task.
  nsExec::ExecToLog 'schtasks /End /TN "krouter-daemon"'
  nsExec::ExecToLog 'schtasks /Delete /TN "krouter-daemon" /F'

  Delete "$INSTDIR\krouter.exe"
  Delete "$INSTDIR\krouter-installer.exe"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"

  DeleteRegKey HKCU "Software\${APPNAME}"
  DeleteRegKey HKCU \
    "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPNAME}"
SectionEnd
