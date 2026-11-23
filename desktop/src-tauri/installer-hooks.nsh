; Entries of the installer that live outside the application directory.
;
; The daemon listens on UDP for the paired devices and the discovery, so the
; private and domain networks allow it without the Windows Firewall dialog. The
; public profile stays closed. The startup entry starts the daemon at the login
; of the user that runs the installer; the Startup toggle of the settings keeps
; it afterwards.
;
; The app repeats these steps when the daemon of the installation is missing.
; The names of the rule, the startup entry, the profiles and the protocol are
; shared with it, and a test fails when they drift apart.

!include WinVer.nsh

!define UNISON_FIREWALL_RULE "Unison daemon"
!define UNISON_RUN_KEY "Software\Microsoft\Windows\CurrentVersion\Run"
!define UNISON_RUN_VALUE "Unison"

!macro NSIS_HOOK_PREINSTALL
  ; The app and the daemon are built for Windows 10 and newer, so an older
  ; system gets no files at all.
  ${IfNot} ${AtLeastWin10}
    MessageBox MB_ICONSTOP "Unison requires Windows 10 or later."
    SetErrorLevel 1
    Abort
  ${EndIf}
!macroend

!macro NSIS_HOOK_POSTINSTALL
  WriteRegStr HKCU "${UNISON_RUN_KEY}" "${UNISON_RUN_VALUE}" '"$INSTDIR\unisond.exe"'

  ; A reinstall adds the rule again, so the old one goes first.
  nsExec::ExecToLog 'netsh advfirewall firewall delete rule name="${UNISON_FIREWALL_RULE}"'
  Pop $0
  nsExec::ExecToLog 'netsh advfirewall firewall add rule name="${UNISON_FIREWALL_RULE}" dir=in action=allow program="$INSTDIR\unisond.exe" enable=yes profile=private,domain protocol=UDP'
  Pop $0
  DetailPrint "Firewall rule of the daemon, exit code $0"
!macroend

!macro NSIS_HOOK_PREUNINSTALL
  ; A running executable cannot be replaced or deleted, and the installer does
  ; not ask the daemon to stop.
  nsExec::Exec 'taskkill /F /IM unisond.exe'
  Pop $0

  DeleteRegValue HKCU "${UNISON_RUN_KEY}" "${UNISON_RUN_VALUE}"

  nsExec::ExecToLog 'netsh advfirewall firewall delete rule name="${UNISON_FIREWALL_RULE}"'
  Pop $0
!macroend

!macro NSIS_HOOK_POSTUNINSTALL
  ; The keys, the database and the logs of the daemon stay with the user unless
  ; the uninstaller is asked to delete the application data.
  ${If} $DeleteAppDataCheckboxState = 1
    RmDir /r "$LOCALAPPDATA\Unison"
  ${EndIf}
!macroend
