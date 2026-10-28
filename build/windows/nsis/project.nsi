Unicode true

####
## 安装 / 卸载脚本。这是唯一可持久化的定制处：wails_tools.nsh 每次构建都会重写。
##
## 模板替换在这里不生效，下面的默认定义由 wails_tools.nsh 兜底；
## 从 wails3 package 调用时，makensis -D* 会覆盖它们。
####

!include "wails_tools.nsh"
!include "LogicLib.nsh"

VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

ManifestDPIAware true

!include "MUI.nsh"

# ---------------------------------------------------------------------------
# 路径常量。必须与 backend/runtime 的 Resolve 一致。
# ---------------------------------------------------------------------------

!define AUTOSTART_KEY "Software\Microsoft\Windows\CurrentVersion\Run"

# 本应用自己的数据：界面设置、日志、WebView2 缓存。
!define USER_DATA_DIR "$LOCALAPPDATA\${INFO_PROJECTNAME}"

# 旧版 WebView2 profile：早期版本让它落在 %APPDATA%\<exe 名>。
# 现在缓存在 USER_DATA_DIR\webview，这里只为清掉升级残留（纯缓存，直接删）。
!define LEGACY_WEBVIEW_DIR "$APPDATA\${PRODUCT_EXECUTABLE}"

# ---------------------------------------------------------------------------
# 覆盖安装前必须先关掉运行中的实例。
#
# 托盘常驻让「窗口已经关了」不等于「进程已退出」：exe 仍被文件句柄占着，
# 覆写时报 "Error opening file for writing"，对用户是莫名其妙的失败。
# 只用 NSIS 内置指令（这个打包环境不带 nsExec 之类的插件）。
#
# taskkill 找不到目标返回 128、杀掉返回 0，据此决定要不要等它释放句柄。
# 取退出码必须用 ExecWait——Exec 不写 $0，写成 Exec 等于这个判断从来没生效过。
# ---------------------------------------------------------------------------
!macro killRunningInstance
    ExecWait 'taskkill /F /IM ${PRODUCT_EXECUTABLE} /T' $0
    ${If} $0 == 0
        DetailPrint "已关闭正在运行的 ${INFO_PRODUCTNAME}（它可能收在右下角托盘里）"
        ; 给 Windows 一点时间真正释放文件句柄，否则紧接着的覆写仍可能失败
        Sleep 1500
    ${EndIf}
!macroend

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
!define MUI_FINISHPAGE_NOAUTOCLOSE
!define MUI_FINISHPAGE_RUN "$INSTDIR\${PRODUCT_EXECUTABLE}"
!define MUI_FINISHPAGE_RUN_TEXT "立即运行 ${INFO_PRODUCTNAME}"
!define MUI_ABORTWARNING

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
# 数据的去留用组件页的勾选框表达，不弹窗：
# 默认只有主安装项（隐藏、必选），「清空旧数据」默认不勾；
# 静默安装（/S）跳过页面，未勾选的 section 不会执行——无人值守永不删数据。
!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

# 卸载同理：勾选「删除本机数据」才删，默认不勾，静默卸载（/S）保持不删。
!insertmacro MUI_UNPAGE_COMPONENTS
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "SimpChinese"

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe"
!if "${WAILS_INSTALL_SCOPE}" == "user"
    InstallDir "$LOCALAPPDATA\Programs\${INFO_PRODUCTNAME}"
!else
    InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
!endif
ShowInstDetails show

# ---------------------------------------------------------------------------
# 安装
# ---------------------------------------------------------------------------

# 可选项：清空旧数据。/o 前缀 = 默认不勾。
Section /o "清空旧数据（设置、日志、窗口缓存）" SEC_CLEAN_DATA
    RMDir /r "${USER_DATA_DIR}"
    RMDir /r "${LEGACY_WEBVIEW_DIR}"
    DetailPrint "已清空本机数据目录：${USER_DATA_DIR}"
SectionEnd

# 主安装（名字为空 = 隐藏，组件页不显示也不能取消勾选）。
Section "" SEC_CORE
    !insertmacro wails.setShellContext

    !insertmacro wails.webview2runtime

    # 用户可能在 WebView2 安装期间又点了一次图标，覆写 exe 前再探一次。
    !insertmacro killRunningInstance

    SetOutPath $INSTDIR

    !insertmacro wails.files

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro wails.writeUninstaller
SectionEnd

# 组件页的说明文字：勾选框旁边只有一行，后果写在描述区。
!insertmacro MUI_FUNCTION_DESCRIPTION_BEGIN
    !insertmacro MUI_DESCRIPTION_TEXT ${SEC_CLEAN_DATA} "删除 ${USER_DATA_DIR}：界面设置、日志、WebView2 缓存。不清空则安装后沿用现有设置。引擎自己的会话与配置（~/.omp、~/.pi）永远不受影响。"
!insertmacro MUI_FUNCTION_DESCRIPTION_END

Function .onInit
   !insertmacro wails.checkArchitecture
   !insertmacro killRunningInstance
   # 静默安装（/S）不显示组件页，勾选项一律不执行；脚本化安装要清空数据时
   # 用 /CLEANDATA 显式指名。不传这个参数就永远不会删——无人值守安装保守优先。
   ${GetParameters} $R0
   ${GetOptions} $R0 "/CLEANDATA" $R1
   ${IfNot} ${Errors}
       SectionSetFlags ${SEC_CLEAN_DATA} ${SF_SELECTED}
   ${EndIf}
FunctionEnd

# ---------------------------------------------------------------------------
# 卸载
# ---------------------------------------------------------------------------

# 可选项：删除本机数据。/o = 默认不勾，重装后接着用；静默卸载（/S）保持不删。
# 注意：对话、模型、技能等引擎状态在 ~/.omp 与 ~/.pi，永远不从这里删。
Section /o "un.删除本机数据（设置、日志、窗口缓存）" UNSEC_DATA
    RMDir /r "${USER_DATA_DIR}"
    DetailPrint "已删除本机数据目录：${USER_DATA_DIR}"
SectionEnd

Section "un." UNSEC_CORE
    !insertmacro wails.setShellContext

    !insertmacro killRunningInstance
    DeleteRegValue HKCU "${AUTOSTART_KEY}" "${INFO_PRODUCTNAME}"

    # 旧版 WebView2 profile：纯缓存，卸载一律清掉。
    RMDir /r "${LEGACY_WEBVIEW_DIR}"

    RMDir /r $INSTDIR

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.deleteUninstaller
SectionEnd

Function un.onInit
    # 静默卸载同理：/CLEANDATA 才删数据，不传就保留。
    ${GetParameters} $R0
    ${GetOptions} $R0 "/CLEANDATA" $R1
    ${IfNot} ${Errors}
        SectionSetFlags ${UNSEC_DATA} ${SF_SELECTED}
    ${EndIf}
FunctionEnd

!insertmacro MUI_UNFUNCTION_DESCRIPTION_BEGIN
    !insertmacro MUI_DESCRIPTION_TEXT ${UNSEC_DATA} "删除 ${USER_DATA_DIR}：界面设置、日志、WebView2 缓存。对话与模型配置属于引擎（~/.omp、~/.pi），不会被删除。"
!insertmacro MUI_UNFUNCTION_DESCRIPTION_END
