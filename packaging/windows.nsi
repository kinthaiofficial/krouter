; krouter Windows installer (NSIS)
; Build: makensis packaging/windows.nsi
; Requires: NSIS 3.x, krouter.exe in dist/

!define APPNAME "krouter"
!define APPVERSION "{{VERSION}}"
!define PUBLISHER "kinthai team"
!define WEBSITE "https://kinthai.ai"
!define BINARY_SRC "dist\daemon-windows_windows_amd64_v1\krouter.exe"

Name "${APPNAME} ${APPVERSION}"
OutFile "dist\krouter-${APPVERSION}-setup.exe"

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
  File "${BINARY_SRC}"

  ; Register install path.
  WriteRegStr HKCU "Software\${APPNAME}" "InstallDir" "$INSTDIR"

  ; Register Task Scheduler user task (auto-start at login, no admin).
  ; Pipe the XML via a temp file to avoid command-line quoting issues.
  DetailPrint "Registering Task Scheduler task..."
  nsExec::ExecToLog '"$INSTDIR\krouter.exe" task-install'

  ; Set ANTHROPIC_API_KEY placeholder in HKCU\Environment if not already set.
  ; Users can update this without re-running the installer.
  ReadEnvStr $R0 "ANTHROPIC_API_KEY"
  StrCmp $R0 "" 0 env_already_set
    WriteRegStr HKCU "Environment" "KINTHAI_INSTALLED" "1"
  env_already_set:

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

; ── Uninstaller ────────────────────────────────────────────────────────────────

Section "Uninstall"
  ; Remove Task Scheduler task.
  nsExec::ExecToLog 'schtasks /Delete /TN "krouter-daemon" /F'

  Delete "$INSTDIR\krouter.exe"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"

  DeleteRegKey HKCU "Software\${APPNAME}"
  DeleteRegKey HKCU \
    "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPNAME}"
SectionEnd
