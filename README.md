# 红果鉴 / 真果鉴

> ⚠️ **免责声明**：本项目源码来自网上大名鼎鼎的**鱼佬**（原作者）。我只是把它拿来打包、测试着玩，方便自己用，**不保证任何可用性，随时可能删库**。

Flutter 多端独立短剧 / 影视应用，原名「短剧库 APP」。站源请求、解析、下载和播放均在设备上完成，不依赖自建服务。当前源码版本：**0.2.74+99**。

本轮在 0.2.50 基线上新增两个原生站源：**韩小圈**（`hanxiaoquan`，韩剧 / 韩国电影 / 韩国综艺 / 韩国动漫）与**鬼片网**（`guipian`，鬼片 / 电视剧 / 动漫）。默认可见站源顺序为：**红果 → 韩小圈 → 鬼片 → 青空**。

> 说明：应用长期按「只维护源码与定向测试，集中验证另行安排」的方式推进。未完成集中验证与真机验收的项目保持未验收状态；历史版本的检查记录不能作为后续新增功能的验收结论。

| 编译方式 | 应用名称 | 可用站源 |
| --- | --- | --- |
| 默认（不加参数） | 红果鉴 | 仅红果 |
| 加 `--all-sources` | 真果鉴 | 红果、韩小圈、鬼片、青空，以及黄豆、剧果、野果、帝果、黄果视频、黄果 AI、黄果旧版 |

默认可见站源（红果、韩小圈、鬼片、青空）无需密码；其余站源默认隐藏，在站源管理里通过密码锁解锁后显示。已有受限用户不会自动获得新增站源权限，管理员可在用户管理中勾选。

这是**编译选项**，应用内不能切换版本。标题、Android 桌面名称与电视横幅、Windows 窗口与分发文件名、iOS 显示名随编译选项变化。界面、站源调用、原生下载调度同时限制可用站源；红果版不会访问或继续执行其他站源的旧任务。

两版保留原 Android / iOS 应用标识及数据目录；Android 使用同一签名可相互覆盖升级，不能作为两个独立正式应用并排安装。切换版本保留追剧、观看记录、用户权限和下载记录；备份格式保持兼容。

## 使用

| 功能 | 操作 |
| --- | --- |
| 浏览 | 标题栏依次提供排序与筛选、榜单、多选下载和展开搜索。分类保持独立一行，内容区左右滑动切类；支持默认、名称、自然季号、上线日期、热度、播放量排序及连载状态筛选。列表下滑接近底部自动加载下一页，底部「加载更多」为兜底 |
| 搜索 | 红果合并官网与名称索引，输入停顿 300 毫秒显示最多 10 条联想；电视端支持拼音首字母与手机扫码输入。韩小圈、剧果、野果、鬼片调用各自的在线搜索并按分页继续加载。部分失败保留有效结果；最近 20 次搜索按用户保存 |
| 推荐 | 仅红果分类栏在「全部」后显示「推荐」，可切换真人剧、漫剧、AI 剧并分别记住位置 |
| 详情 | 普通点剧直接进入播放；下载和兜底入口保留详情页。详情页浏览封面、资料、追剧状态与可展开简介；底部固定「立即播放 / 继续播放」和下载入口。选集默认折叠，展开后每组 50 集，可切换范围、定位当前或输入集数跳转；宽屏与电视采用资料、选集分栏 |
| 追剧与历史 | 按想看 / 在看 / 已看 / 有更新筛选；红果详情会自动查找同系列剧集，已追剧发现新季后进入「有更新」。追剧和历史均可搜索。每个用户独立保存，每 5 秒及退出播放时记录真实进度 |
| 卡片与批量下载 | 卡片「更多」提供收藏、状态和下载选集；电脑右键，电视菜单键。发现页长按卡片或点标题栏「多选下载」，一次最多 50 部 |
| VIP | 全站源版浏览黄豆或全部站源时显示 VIP 图标，仅过滤黄豆；默认隐藏已确认的 VIP。未知、免费、VIP 分别保存，未知不会覆盖已知状态 |
| 手机播放 | 上滑下一集、下滑上一集；长按 350 毫秒临时 3 倍速。竖屏轻点播放 / 暂停，横屏轻点控制条，双击播放 / 暂停。视频下方为「选集 / 简介 / 下载」Tab |
| 画中画 | Android 手机 / 平板播放器接入画中画按钮；进入前隐藏自绘控制层与弹幕，小窗只保留画面。Android TV、Windows、iOS 暂未接入 |
| 画质增强 | 仅 Windows 桌面端显示并运行增强链路（关闭 / 自动 / 省电增强 / 清晰优先）；移动端与电视端隐藏 |
| 弹幕 | 红果在线播放默认开启；播放器侧边圆形「弹」字按钮可关闭、查看状态及失败重试，Android 原生播放器会在起播后异步接收弹幕。TV 原生播放器默认只在画面上方 1/3 显示，可通过遥控器按钮设置显示区域、字号、透明度、滚动速度和屏蔽词，设置会保存。本地播放不加载弹幕 |
| 下载 | 播放页「下载」Tab 和详情页下载入口均可选择分集与画质；支持批量暂停、继续、重试、删除及清理任务但保留视频。TV 下载合集提供直接可聚焦的「删除整部」按钮，确认后删除该剧所有分集任务和文件。「更新本剧」补新增或缺失分集 |
| 服务器下载 | 设置中的服务器下载列表可直接播放已下载分集、切换同剧其他集，并保留分集删除和整部删除入口 |
| 更多 | 站源管理、用户管理、设置与备份、界面模式、关于 |

| 站源 | 浏览与播放 | 搜索 |
| --- | --- | --- |
| 红果 | 真人剧、漫剧、AI 剧及分集 | 联网搜索与官网搜索联想 |
| 韩小圈 | MacCMS 模板：最新韩剧 / 韩国电影 / 韩国综艺 / 韩国动漫，多线路分集 | 站源在线搜索 |
| 鬼片 | MacCMS 站点：鬼片 / 电视剧 / 动漫，多线路分集 | RSS 最新条目标题匹配（站点搜索已停用） |
| 青空 | 番剧、剧场动画、特摄及分集 | 站源在线搜索 |
| 黄豆 | 列表、VIP 标记及分集 | 筛选已加载短剧 |
| 剧果 | 热门、最新、接口分类、详情及可播放分集；签名 Cookie 接入在线播放、预加载和下载 | 站源在线搜索，支持继续加载 |
| 野果 | 接口实际分类、目录、详情及真实分集；按分集重新取流，保留 H.264 / H.265 地址 | 站源在线分页搜索 |
| 帝果 | 网页分类、目录、详情及分集；vplayer 签名解析 | 站源在线分页搜索 |
| 黄果视频 / 黄果 AI / 黄果旧版 | 列表、分类、详情及分集 | 筛选已加载短剧 |

站源可用性、清晰度和区域限制取决于源站及网络；应用不解除源站 VIP 或其他授权限制。

### 网络与资源设置

管理员从「设置与备份 → 网络与资源」选择自动、直连或手动代理，手动地址支持 HTTP、HTTPS、SOCKS5 / SOCKS5h。代理地址默认遮蔽、不进入备份；设备内播放服务始终绕过代理。自动模式读取系统静态代理与排除列表，恢复前台及每 30 秒更新；代理不会关闭 TLS 校验。

目录请求并发可设 1–6、间隔 0–5000 毫秒（默认 3 / 250ms）；下载并发默认 2、可设 1–6。仍保留前台优先、后台让行、取消、退避及请求数量边界。

### 局域网互联：追剧同步与推送播放（已接入，待验证）

两台设备在同一局域网直连，自动同步追剧记录或接续播放，不依赖账号服务器。服务类型 `_zgj-link._tcp`（DNS-SD / mDNS），按设备 ID 合并多网卡地址，首次配对记录证书指纹，后续连接固定校验。

入口：「追剧 → 标题栏同步」或「设置与备份 → 设备互联」。手动同步默认「双向合并」，可选覆盖对方 / 覆盖本机，先「查看预览」再执行；自动同步仅发送改动条目与删除标记，播放期间约每 10 秒发送进度。冲突记录保留候选，用户从同步页逐项处理。

## 安装包与平台状态

| 平台 | 包与状态 |
| --- | --- |
| Android 8.0+ | 三架构（arm64-v8a / armeabi-v7a / x86_64）APK；同一签名可覆盖升级 |
| Windows 10/11 x64 | 完整 ZIP 解压后运行 `hongguojian.exe` / `zhenguojian.exe`，保留所有 DLL 与 `data`；局域网原生发现依赖 Windows 10 1903+ |
| Android TV | 与手机共用源码，自动识别电视模式并保持横屏；待电视 / 盒子实机验收 |
| iOS 15.1+ | 已加入工程、Go 核心链接、媒体依赖、文件管理与构建脚本；iOS 播放页禁用 media_kit_video 硬件纹理加速以规避 libmpv 渲染崩溃；待 Xcode 构建与真机验收，无已签名 IPA |

`INSTALL_FAILED_NO_MATCHING_ABIS` 表示 APK 与设备架构不匹配，请更换对应架构安装包。

### 0.2.74 TV 定向修复与验证（2026-10-18）

原生播放器现在每 5 秒以及进入后台时回传进度，退出、切集、切换清晰度时同时回传播放位置与总时长，写入当前用户观看记录。避免用未播放的 mpv 状态覆盖原生进度；最近观看、续播集号与位置使用同一记录。旧版本未保存的观看位置无法恢复。

下载合集的展开区与删除按钮改成独立焦点区域；整部删除包含当前筛选之外的同剧任务。弹幕改为碰撞安全的轨道分配，同时检查入口间距与长文本追尾风险；没有空闲轨道时跳过该条，不改变已有显示范围、字号、透明度、速度和屏蔽词设置。

测试环境为 Android 14 x86_64 模拟器（1920×1080、160 dpi），使用 TV 布局、真实原生 ExoPlayer、合成视频和弹幕，不请求站源图片、不使用外部测试账号。

| 范围 | 实际验证 | 结论 |
| --- | --- | --- |
| 最近观看与续播 | `tv_progress_test.dart` 实际进入原生播放，播放中检查记录，ADB 两次返回退出，重建存储并以保存位置重新播放，确认进度继续增长 | 已测试且可用；未覆盖进程强杀与实体电视 |
| 整部删除 | 组件测试从按键遍历进入按钮，左右键离开再返回，确认包含筛选外的分集；模拟器通过 ADB `DPAD_CENTER` 打开确认框并确认删除，两集从模拟仓库清除 | 已测试且可用；本次未删除真实用户文件或服务器文件 |
| 密集弹幕 | 4 项 Kotlin 测试覆盖同时间多条、入口间距、长弹幕追尾与连续混合长度；模拟器输入同时间 100 条合成弹幕，画面分成 6 行且无叠字 | 已测试且可用（定向场景）；未覆盖全部字体、设备及真实弹幕规模 |
| 相关 Flutter 回归 | 开启 `DISABLE_REMOTE_IMAGES=true` 后两个相关测试文件共 17 项通过、4 项失败 | 未全量通过 |

4 个失败项为旧入口相关的窄屏下载筛选、本地播放按钮、TV 长队列导航，以及手机播放工具间距断言；不得据定向通过宣称所有页面与平台验收通过。Android ARMv7、ARM64、x86_64 三架构 release APK 构建通过，静态分析仍为 108 条 info，无 error/warning。设备测试日志及合成截图位于本地忽略目录 `build/device-test/results/tv-0.2.74` 与 `build/tv-*-device.log`。

设备用例运行前向 `/data/local/tmp/danmaku-test.mp4` 放置超过两分钟的合成视频，并带 `--dart-define=DISABLE_REMOTE_IMAGES=true` 运行 `integration_test/tv_progress_test.dart`。看到 `TV_NATIVE_BACK` 时用 ADB 在两秒内发送两次返回；看到 `TV_DELETE_REMOTE_READY` 和 `TV_DELETE_CONFIRM_READY` 时分别发送一次 `DPAD_CENTER`。这两个用例需要宿主机按键配合，不是无人值守测试。

### 0.2.73 定向验证（2026-10-17）

修复 TV 弹幕设置面板只出现关闭按钮的问题，选项改为原生可聚焦按钮，并显示当前值；去掉固定四行的弹幕限制，使显示区域实际生效。红果前台目录请求每分类只取一页，避免沿用批量扫描的默认 50 页上限；目录与分类请求最多等待 15 秒。修复带密钥的 HLS 二进制分片被当成播放列表的问题，恢复分片 Range 响应。

测试环境：Windows 主机、TV AVD `hgtv_tv`，补充 Android 14 x86_64 模拟器（1920×1080、160 dpi）的同一原生播放器；应用为 0.2.73+98 调试构建。弹幕用合成视频与文本，无外部测试账号。以下结论不替代实体电视验收。

| 功能 | 操作和实际结果 | 状态 |
| --- | --- | --- |
| 弹幕开关 | 遥控选择关闭，终止进程重进后仍关闭，再开启成功 | 已测试且可用 |
| 显示区域 | 遥控逐一选择上方 1/4、1/3、1/2，保存值正确；1/4 与 1/2 合成弹幕占用高度不同 | 已测试且可用 |
| 字号 | 小、中、大逐档选择，分别保存 26、34、42，重进保留；合成截图中文字高度由小号 21 px 变为大号 34 px | 已测试且可用（中档未单独量化） |
| 透明度 | 30%、60%、100% 逐档选择并保存，重进保留；同一背景下 30% 的像素亮度差明显低于 100% | 已测试且可用（60% 未单独量化） |
| 滚动速度 | 快、中、慢逐档选择并保存 6000、9000、12000 ms；快慢合成弹幕位移不同 | 已测试且可用 |
| 屏蔽词 | 保存 TEST,SKIP 后过滤对应合成文本；取消修改保留原值，清空保存后为空 | 已测试且可用 |
| 设置持久化 | 同时更改全部设置，强制停止并重新打开，保存值与界面一致 | 已测试且可用 |
| 未提供的设置 | 当前没有密度、防挡脸、智能云屏蔽、恢复默认或多端同步入口 | 无法验证，功能未实现 |
| 输入边界 | 屏蔽词空值已覆盖；超长文本、特殊字符及大规模弹幕压力未覆盖 | 未测试/未覆盖 |
| 本机合集删除 | TV 遥控触发确认与整部删除的组件测试通过；原生临时文件测试批量删除两集，逐集确认任务和目录清除 | 已测试且可用（定向自动化） |
| 服务器下载 | 模拟器实际进入原生播放，选集后重新起播；整部删除用模拟仓库验证确认和刷新流程 | 播放已验证，真实服务器删除未执行 |
| 首页失败反馈 | 模拟器断网显示无法加载、重试与站源诊断；前台单页浏览、续页、游标重启恢复及并发元数据保存测试通过 | 已测试且可用（定向场景） |

定向 Flutter 服务器下载、TV 整部/分集删除测试及 Go 播放地址、文件删除、红果目录游标测试通过。Android 三架构构建通过；静态分析有 108 条 info，无 error/warning。修复 HLS 分片处理，并修正目录/榜单测试夹具和 Windows 文件权限断言后，`go test ./core -count=1` 全部通过。实体 ARM 电视、全部播放源与跨设备同步仍未验收。

### GitHub Actions

推送 `main` / `master`、`v*` 标签、提交 PR，或手动运行 **Build app packages**，会先检查再构建两版（默认与 `--all-sources`）：

| 产物 | 内容 |
| --- | --- |
| `*-android` | 三种架构 APK 和 SHA256 |
| `*-windows` | 完整 ZIP 和 SHA256 |
| `*-ios-unsigned` | 未签名 `.app` ZIP 和 SHA256，不能直接当已签名 IPA 安装 |

推送 `main` 且 android / ios / windows 全部构建成功时，自动创建 / 更新 GitHub Release（tag `app-v{version}`）。发布新版本前需先在 `pubspec.yaml` 提升 `version`，否则会覆盖同名 tag 的 Release。

Android 正式发布使用同一签名并递增构建号，在仓库 Secrets 配置：`ANDROID_KEYSTORE_BASE64`、`ANDROID_KEYSTORE_PASSWORD`、`ANDROID_KEY_ALIAS`、`ANDROID_KEY_PASSWORD`。未配置时生成 debug 签名预览包。

本地不入库的 `android/key.properties`：

~~~properties
storeFile=/absolute/path/zhenguojian-release.jks
storePassword=你的密码
keyAlias=zhenguojian
keyPassword=你的密码
~~~

## 开发与构建

Flutter `3.47.4`、Dart `3.12+`、Go `1.24.1+`、Python `3.10+`。Android 需要 JDK 17、SDK 36、NDK `28.2.13676358`；Windows 需要 Visual Studio C++ 桌面组件及 MinGW-w64 x64；iOS 需要 macOS、完整 Xcode 和 CocoaPods。

构建脚本对子进程默认设置 `GOPROXY=https://goproxy.cn,direct`、`GOSUMDB=off`，同名环境变量可覆盖。

~~~sh
python3 scripts/build_android.py                 # 红果鉴
python3 scripts/build_android.py --all-sources   # 真果鉴
python3 scripts/build_android.py --abi arm64-v8a
python3 scripts/build_android.py --cn-mirrors    # 国内镜像
~~~

~~~powershell
.\scripts\build_windows.ps1
.\scripts\build_windows.ps1 -AllSources
.\scripts\build_windows.ps1 -ChinaMirrors
~~~

~~~sh
python3 scripts/build_ios.py
python3 scripts/build_ios.py --all-sources
python3 scripts/build_ios.py --core-only [--simulator]
python3 scripts/build_ios.py --export-options /path/to/ExportOptions.plist
~~~

产物在 `dist/android`、`dist/windows`、`dist/ios`，红果版以 `hongguojian-` 开头，全站源版以 `zhenguojian-` 开头。

首次 Android 调试先编译对应架构核心，再运行：

~~~sh
python3 scripts/build_native.py --platform android --abi arm64-v8a
flutter pub get --enforce-lockfile
flutter run
~~~

调试全站源版：先给 `build_native.py` 加 `--all-sources`，再 `flutter run --dart-define=ALL_SOURCES=true`；iOS 对应 `build_ios.py --core-only --all-sources`。脚本会同步设置 Dart 常量和 Go 编译参数，应用启动时校验二者一致，避免混装原生库。

播放器使用 [media_kit](https://github.com/media-kit/media-kit) / libmpv，合并和导出使用 [FFmpegKit min-gpl](https://github.com/sk3llo/ffmpeg_kit_flutter)（含 GPL 媒体组件）。FFmpegKit 不参与正常播放或下载的转码。

### 集中检查与真机回归

~~~sh
python3 -m unittest discover -s scripts -p 'test_*.py'
dart format --output=none --set-exit-if-changed lib test integration_test test_driver
dart analyze --fatal-infos lib test integration_test test_driver
flutter test --dart-define=DISABLE_REMOTE_IMAGES=true
flutter test --dart-define=DISABLE_REMOTE_IMAGES=true --dart-define=ALL_SOURCES=true
cd native
go test -race ./...
go test -race -ldflags="-X duanjuapp/native/core.buildAllSources=true" ./...
~~~

Android 设备回归（连接并授权 USB 调试、保持解锁）：

~~~sh
python3 scripts/create_test_media.py
python3 scripts/serve_test_media.py
adb reverse tcp:38473 tcp:38473
flutter drive --driver=test_driver/playback.dart --target=integration_test/playback_test.dart \
  --dart-define=DISABLE_REMOTE_IMAGES=true --dart-define=FIXTURE_BASE_URL=http://127.0.0.1:38473
~~~

结果在 `build/device-test/results/`；结束后停服务并 `adb reverse --remove tcp:38473`。

### 源码同步

~~~sh
python3 scripts/finish_task.py --message "本次实际完成的变更"
python3 scripts/sync_source.py --check
~~~

脚本只同步纯源码到同级 `../guoapp`，并生成 `真果·鉴-YYYYMMDDHHMM.zip` 源码压缩包；不执行 Git 提交、分支或推送。

## 目录结构

| 目录 | 内容 |
| --- | --- |
| `lib` | 页面、播放器、本地用户、FFI、下载和媒体处理 |
| `native/core`、`native/bridge` | 独立站源核心、缓存、下载、目录迁移及 C ABI |
| `android`、`windows`、`ios` | 平台工程与必要资源 |
| `assets/video_enhancement`、`packages/media_kit_libs_windows_video` | 增强 Shader 与许可、固定 Windows 媒体依赖插件 |
| `scripts`、`.github/workflows` | 构建、签名、验证、同步和版本快照 |
| `test`、`integration_test` | 自动化与设备回归 |

## 站源开发约定

站源是 Go 原生 provider（`native/core/provider_*.go`），不是运行期加载的 Python 源。新增站源需接入：`provider_huangguo.go`（常量 / 白名单 / `canonicalProviderSource` / `GetHuangguoChapters`）、`provider_media.go`（baseURL / host 反查 / 播放分派）、`app_categories.go`、`app_cover_metadata.go`、`app_runtime.go`（Config 字段 + 目录 / 搜索 / 详情分派 + 空页放行），并在 `lib/models.dart` 登记 `SourceSite`。

注意：Go RE2 正则**不支持 lookahead**。解析多线路播放列表时不能用 `(?=...)` 做分段，否则非捕获组会消耗下一段开头；应改用显式字符串截断（从容器标记之后查找下一段标记）。
