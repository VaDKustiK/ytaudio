Unicode True
!include "MUI2.nsh"
Name "ytaudio"
OutFile "${OUTFILE}"
InstallDir "$LOCALAPPDATA\Programs\ytaudio"
RequestExecutionLevel user
!define MUI_ICON "${ICON}"
!define MUI_UNICON "${ICON}"
!define MUI_ABORTWARNING
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_RUN "$INSTDIR\ytaudio.exe"
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"
Section "Install"
 SetOutPath "$INSTDIR"
 File /r "${STAGE}\*"
 CreateDirectory "$SMPROGRAMS\ytaudio"
 CreateShortcut "$SMPROGRAMS\ytaudio\ytaudio.lnk" "$INSTDIR\ytaudio.exe"
 WriteUninstaller "$INSTDIR\Uninstall.exe"
 WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\ytaudio" "DisplayName" "ytaudio"
 WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\ytaudio" "DisplayVersion" "${VERSION}"
 WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\ytaudio" "UninstallString" '$"$INSTDIR\Uninstall.exe$"'
SectionEnd
Section "Uninstall"
 Delete "$SMPROGRAMS\ytaudio\ytaudio.lnk"
 RMDir "$SMPROGRAMS\ytaudio"
 RMDir /r "$INSTDIR"
 DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\ytaudio"
SectionEnd
