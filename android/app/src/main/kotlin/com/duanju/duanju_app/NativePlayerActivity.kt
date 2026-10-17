package com.duanju.duanju_app

import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.graphics.Color
import android.graphics.drawable.GradientDrawable
import android.net.Uri
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.Gravity
import android.view.KeyEvent
import android.view.View
import android.view.ViewGroup
import android.view.WindowManager
import android.widget.Button
import android.widget.FrameLayout
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DataSource
import androidx.media3.datasource.DefaultDataSource
import androidx.media3.datasource.DefaultHttpDataSource
import androidx.media3.exoplayer.DefaultLoadControl
import androidx.media3.exoplayer.DefaultRenderersFactory
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.hls.HlsMediaSource
import androidx.media3.exoplayer.source.ProgressiveMediaSource
import androidx.media3.ui.AspectRatioFrameLayout
import androidx.media3.ui.PlayerView
import com.duanju.duanju_app.playback.CencDataSourceFactory
import com.duanju.duanju_app.playback.DanmakuView
import com.duanju.duanju_app.playback.PlayerMenuBar
import com.duanju.duanju_app.playback.ProgressBarView

/**
 * 原生全屏播放器：ExoPlayer + PlayerView(SurfaceView)。
 *
 * 用独立 Activity 承载，视频帧经 SurfaceView 由显示控制器直接合成，
 * 不进入 Flutter 的 GPU 合成路径 —— 这是电视/投影仪上最省 GPU 的方式。
 *
 * 弹幕与选集在此 Activity 内自绘：SurfaceView 由显示控制器直接合成，
 * Flutter 的图层无法叠加其上，因此这两项 UI 必须落在原生侧。
 * 弹幕数据由 Flutter 侧预取后随 Intent 传入，原生侧不联网。
 */
@UnstableApi
class NativePlayerActivity : Activity() {

    companion object {
        const val EXTRA_URL = "url"
        const val EXTRA_TITLE = "title"
        const val EXTRA_REFERER = "referer"
        const val EXTRA_POSITION_MS = "positionMs"

        /** CENC 内容密钥，十六进制 AES-128；为空表示明文播放。 */
        const val EXTRA_KEY = "key"

        /** 分集标题，以 \n 分隔；为空则不显示选集入口。 */
        const val EXTRA_EPISODES = "episodes"

        /** 当前集下标，用于在选集中标记。 */
        const val EXTRA_EPISODE_INDEX = "episodeIndex"

        /** 弹幕载荷，每行 `时间毫秒\t文本`。 */
        const val EXTRA_DANMAKU = "danmaku"

        /** 可用清晰度档位，以逗号分隔；为空则不显示清晰度入口。 */
        const val EXTRA_QUALITIES = "qualities"

        /** 当前清晰度，用于在面板中标记。 */
        const val EXTRA_QUALITY = "quality"

        const val RESULT_POSITION_MS = "positionMs"
        const val RESULT_DURATION_MS = "durationMs"
        var progressListener: ((Long, Long) -> Unit)? = null
        const val RESULT_COMPLETED = "completed"

        /** 用户在选集中切换了分集，回传目标下标。 */
        const val RESULT_EPISODE_INDEX = "episodeIndex"

        /** 用户切换了清晰度，回传目标档位。 */
        const val RESULT_QUALITY = "quality"

        /** 用户调整了倍速，回传当前值；Flutter 侧持久化，切集后沿用。 */
        const val RESULT_SPEED = "speed"

        /** 初始倍速，由 Flutter 侧传入。 */
        const val EXTRA_SPEED = "speed"

        /** 可选倍速档位。 */
        val SPEED_OPTIONS = floatArrayOf(1.0f, 1.25f, 1.5f, 2.0f, 0.75f)

        /**
         * 当前正在运行的播放器实例。弹幕在起播后异步取回，需要由外部推入。
         * 用弱引用避免 Activity 泄漏；同一时刻只允许一个播放器。
         */
        @Volatile
        private var current: java.lang.ref.WeakReference<NativePlayerActivity>? = null

        @Volatile
        private var pendingDanmaku: String? = null

        @Volatile
        private var acceptingDanmaku = false

        @JvmStatic
        fun beginSession() {
            pendingDanmaku = null
            acceptingDanmaku = true
        }

        /** 向正在运行的播放器推送弹幕载荷；未运行时忽略。 */
        @JvmStatic
        fun pushDanmaku(payload: String?) {
            if (payload.isNullOrEmpty()) return
            val activity = current?.get()
            if (activity == null) {
                if (acceptingDanmaku) pendingDanmaku = payload
                return
            }
            activity.runOnUiThread {
                val view = activity.danmakuView
                if (view == null) {
                    if (acceptingDanmaku) pendingDanmaku = payload
                } else {
                    view.load(payload)
                    pendingDanmaku = null
                }
            }
        }
    }

    private var player: ExoPlayer? = null
    private var playerView: PlayerView? = null
    private var danmakuView: DanmakuView? = null
    private var progressBar: ProgressBarView? = null
    private var menuBarRef: PlayerMenuBar? = null
    private var panel: View? = null
    private var panelList: LinearLayout? = null
    private var panelScroll: ScrollView? = null
    private var qualityPanel: View? = null
    private var qualityList: LinearLayout? = null
    private var hint: TextView? = null
    private var startPositionMs = 0L
    private var finished = false
    private var episodeIndex = 0
    private var episodes: List<String> = emptyList()
    private var selectedIndex = 0
    private var danmakuEnabled = true
    private var playbackSpeed = 1.0f
    private var qualities: List<Int> = emptyList()
    private var currentQuality = 0
    private var selectedQuality = 0
    private var backArmed = false
    private val danmakuPreferences by lazy { getSharedPreferences("tv_danmaku", MODE_PRIVATE) }

    private fun applyDanmakuSettings() {
        danmakuView?.configure(
            danmakuPreferences.getFloat("area", 0.33f),
            danmakuPreferences.getFloat("font", 34f),
            danmakuPreferences.getFloat("opacity", 1f),
            danmakuPreferences.getFloat("duration", 9_000f),
            danmakuPreferences.getString("blocked", "").orEmpty(),
        )
    }

    private fun chooseDanmakuValue(title: String, key: String, labels: Array<String>, values: FloatArray) {
        val initial = when (key) {
            "area" -> 0.33f
            "font" -> 34f
            "opacity" -> 1f
            else -> 9_000f
        }
        val current = danmakuPreferences.getFloat(key, initial)
        val selected = values.indexOfFirst { it == current }.coerceAtLeast(0)
        val content = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(20), dp(8), dp(20), dp(8))
        }
        lateinit var dialog: AlertDialog
        labels.forEachIndexed { index, label ->
            content.addView(Button(this).apply {
                text = if (index == selected) "✓ $label" else label
                textSize = 20f
                isAllCaps = false
                isFocusable = true
                minHeight = dp(56)
                setOnClickListener {
                    danmakuPreferences.edit().putFloat(key, values[index]).apply()
                    applyDanmakuSettings()
                    dialog.dismiss()
                    this@NativePlayerActivity.handler.postDelayed({ showDanmakuSettings() }, 150)
                }
            })
        }
        dialog = AlertDialog.Builder(this)
            .setTitle(title)
            .setView(content)
            .setNegativeButton("返回") { _, _ -> handler.postDelayed({ showDanmakuSettings() }, 150) }
            .create()
        dialog.setOnShowListener { content.getChildAt(selected)?.requestFocus() }
        dialog.show()
    }

    private fun showDanmakuSettings() {
        val area = when (danmakuPreferences.getFloat("area", 0.33f)) {
            0.25f -> "上方 1/4"
            0.5f -> "上方 1/2"
            else -> "上方 1/3"
        }
        val font = when (danmakuPreferences.getFloat("font", 34f)) {
            26f -> "小"
            42f -> "大"
            else -> "中"
        }
        val opacity = when (danmakuPreferences.getFloat("opacity", 1f)) {
            0.3f -> "30%"
            0.6f -> "60%"
            else -> "100%"
        }
        val speed = when (danmakuPreferences.getFloat("duration", 9_000f)) {
            6_000f -> "快"
            12_000f -> "慢"
            else -> "中"
        }
        val blocked = if (danmakuPreferences.getString("blocked", "").isNullOrBlank()) "未设置" else "已设置"
        val content = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(20), dp(8), dp(20), dp(8))
        }
        lateinit var dialog: AlertDialog
        fun option(label: String, action: () -> Unit) {
            content.addView(Button(this).apply {
                text = label
                textSize = 20f
                isAllCaps = false
                isFocusable = true
                minHeight = dp(56)
                setOnClickListener {
                    dialog.dismiss()
                    this@NativePlayerActivity.handler.postDelayed({ action() }, 150)
                }
            })
        }
        option("显示区域 · $area") {
            chooseDanmakuValue("显示区域", "area", arrayOf("上方 1/4", "上方 1/3", "上方 1/2"), floatArrayOf(0.25f, 0.33f, 0.5f))
        }
        option("字号 · $font") {
            chooseDanmakuValue("字号", "font", arrayOf("小", "中", "大"), floatArrayOf(26f, 34f, 42f))
        }
        option("透明度 · $opacity") {
            chooseDanmakuValue("透明度", "opacity", arrayOf("30%", "60%", "100%"), floatArrayOf(0.3f, 0.6f, 1f))
        }
        option("滚动速度 · $speed") {
            chooseDanmakuValue("滚动速度", "duration", arrayOf("快", "中", "慢"), floatArrayOf(6_000f, 9_000f, 12_000f))
        }
        option("屏蔽词 · $blocked") {
            val input = EditText(this).apply {
                setSingleLine(false)
                setText(danmakuPreferences.getString("blocked", ""))
                hint = "多个关键词用逗号分隔"
            }
            AlertDialog.Builder(this)
                .setTitle("屏蔽词")
                .setView(input)
                .setPositiveButton("保存") { _, _ ->
                    danmakuPreferences.edit().putString("blocked", input.text.toString()).apply()
                    applyDanmakuSettings()
                }
                .setNegativeButton("取消", null)
                .show()
        }
        dialog = AlertDialog.Builder(this)
            .setTitle("弹幕设置")
            .setView(content)
            .setNegativeButton("关闭", null)
            .create()
        dialog.setOnShowListener { content.getChildAt(0)?.requestFocus() }
        dialog.show()
    }

    private val handler = Handler(Looper.getMainLooper())
    private var lastProgressAt = 0L

    private fun reportProgress(force: Boolean = false) {
        val exo = player ?: return
        val now = android.os.SystemClock.elapsedRealtime()
        if (exo.duration <= 0 || exo.currentPosition < 100) return
        if (!force && now - lastProgressAt < 5_000L) return
        lastProgressAt = now
        progressListener?.invoke(exo.currentPosition, exo.duration)
    }

    private val tick = object : Runnable {
        override fun run() {
            val exo = player
            if (exo != null) {
                val position = exo.currentPosition
                // 传入播放状态：暂停时弹幕停在原位，不随墙钟继续漂移
                danmakuView?.setPosition(position, exo.isPlaying)
                progressBar?.update(position, exo.duration.coerceAtLeast(0), exo.bufferedPosition)
                reportProgress()
            }
            handler.postDelayed(this, 200)
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        current = java.lang.ref.WeakReference(this)
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)

        val url = intent.getStringExtra(EXTRA_URL).orEmpty()
        if (url.isEmpty()) {
            finish()
            return
        }
        val referer = intent.getStringExtra(EXTRA_REFERER).orEmpty()
        startPositionMs = intent.getLongExtra(EXTRA_POSITION_MS, 0L)
        val key = decodeKey(intent.getStringExtra(EXTRA_KEY))
        val isHls = url.contains(".m3u8", ignoreCase = true)
        episodeIndex = intent.getIntExtra(EXTRA_EPISODE_INDEX, 0)
        selectedIndex = episodeIndex
        episodes = intent.getStringExtra(EXTRA_EPISODES)
            ?.split('\n')
            ?.filter { it.isNotEmpty() }
            ?: emptyList()
        qualities = intent.getStringExtra(EXTRA_QUALITIES)
            ?.split(',')
            ?.mapNotNull { it.trim().toIntOrNull() }
            ?.filter { it > 0 }
            ?: emptyList()
        currentQuality = intent.getIntExtra(EXTRA_QUALITY, 0)
        selectedQuality = currentQuality
        playbackSpeed = intent.getFloatExtra(EXTRA_SPEED, 1.0f)
            .takeIf { candidate -> SPEED_OPTIONS.any { kotlin.math.abs(it - candidate) < 0.01f } }
            ?: 1.0f

        // 缓冲对齐实测流畅的电视端配置：小缓冲 + 快速起播。
        // 大缓冲会让电视长时间停在加载态，观感上就是卡；这里不是越大越稳。
        val loadControl = DefaultLoadControl.Builder()
            .setBufferDurationsMs(
                15_000,   // minBufferMs
                30_000,   // maxBufferMs
                1_000,    // bufferForPlaybackMs
                2_000     // bufferForPlaybackAfterRebufferMs
            )
            .setTargetBufferBytes(12 * 1024 * 1024)
            .build()

        val httpFactory = DefaultHttpDataSource.Factory()
            .setUserAgent(
                "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 " +
                    "(KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"
            )
            .setConnectTimeoutMs(15_000)
            .setReadTimeoutMs(30_000)
            .setAllowCrossProtocolRedirects(true)
            .apply { if (referer.isNotEmpty()) setDefaultRequestProperties(mapOf("Referer" to referer)) }

        // 明文内容用 DefaultDataSource：它按 URI scheme 分派，
        // 本地文件走 FileDataSource，网络请求走上面配好 UA/Referer 的 httpFactory。
        // 加密内容仍用 CencDataSourceFactory（仅支持网络地址，本地加密文件由调用方排除）。
        val localCapable: DataSource.Factory = DefaultDataSource.Factory(this, httpFactory)
        val dataFactory: DataSource.Factory =
            if (key != null && !isHls) CencDataSourceFactory(httpFactory, url, referer, key)
            else localCapable

        // 允许在硬解不可用时回退到软解，避免直接报错
        val renderers = DefaultRenderersFactory(this).setEnableDecoderFallback(true)
        val exo = ExoPlayer.Builder(this, renderers)
            .setLoadControl(loadControl)
            .build()
        player = exo

        val root = FrameLayout(this)

        val view = PlayerView(this).apply {
            // 关闭 media3 自带控制器：它会消费方向键与确定键，使 Activity 收不到
            // 遥控器事件。进度与操作提示由下方自绘层承担。
            useController = false
            resizeMode = AspectRatioFrameLayout.RESIZE_MODE_FIT
            layoutParams = FrameLayout.LayoutParams(
                FrameLayout.LayoutParams.MATCH_PARENT,
                FrameLayout.LayoutParams.MATCH_PARENT
            )
            player = exo
        }
        playerView = view
        root.addView(view)

        // 弹幕层：置于画面之上，但不参与焦点与触摸
        val danmaku = DanmakuView(this).apply {
            layoutParams = FrameLayout.LayoutParams(
                FrameLayout.LayoutParams.MATCH_PARENT,
                FrameLayout.LayoutParams.MATCH_PARENT
            )
        }
        danmakuView = danmaku
        danmakuEnabled = danmakuPreferences.getBoolean("enabled", true)
        applyDanmakuSettings()
        root.addView(danmaku)

        // 底部提示条：只在操作时短暂出现
        val hintView = TextView(this).apply {
            setTextColor(Color.WHITE)
            textSize = 13f
            setPadding(dp(16), dp(8), dp(16), dp(8))
            background = GradientDrawable().apply {
                setColor(Color.argb(150, 0, 0, 0))
                cornerRadius = dp(6).toFloat()
            }
            layoutParams = FrameLayout.LayoutParams(
                FrameLayout.LayoutParams.WRAP_CONTENT,
                FrameLayout.LayoutParams.WRAP_CONTENT,
                Gravity.BOTTOM or Gravity.CENTER_HORIZONTAL
            ).apply { bottomMargin = dp(28) }
            visibility = View.GONE
        }
        hint = hintView
        root.addView(hintView)

        // 底部操作条：弹幕开关、清晰度、选集、倍速。
        // 回调里通过 menuBarRef 取自身，避免在初始化表达式中引用尚未赋值的局部变量。
        val menuBar = PlayerMenuBar(
            this,
            onToggleDanmaku = {
                danmakuEnabled = !danmakuEnabled
                danmakuPreferences.edit().putBoolean("enabled", danmakuEnabled).apply()
                danmaku.setDanmakuEnabled(danmakuEnabled)
                menuBarRef?.setDanmakuEnabled(danmakuEnabled)
                showHint(if (danmakuEnabled) "弹幕已开启" else "弹幕已关闭")
            },
            onDanmakuSettings = { showDanmakuSettings() },
            onQuality = { toggleQualityPanel() },
            onEpisodes = { togglePanel() },
            onSpeed = {
                playbackSpeed = nextSpeed(playbackSpeed)
                exo.setPlaybackSpeed(playbackSpeed)
                menuBarRef?.setSpeed(playbackSpeed)
                showHint("倍速 ${playbackSpeed}x")
            },
        ).apply {
            visibility = View.GONE
            layoutParams = FrameLayout.LayoutParams(
                FrameLayout.LayoutParams.MATCH_PARENT,
                FrameLayout.LayoutParams.WRAP_CONTENT,
                Gravity.BOTTOM
            )
        }
        menuBarRef = menuBar
        root.addView(menuBar)

        // 进度条：操作时显示，静置 3 秒后自动隐藏，避免长时间遮挡画面
        val progress = ProgressBarView(this).apply {
            visibility = View.GONE
            layoutParams = FrameLayout.LayoutParams(
                FrameLayout.LayoutParams.MATCH_PARENT,
                dp(64),
                Gravity.BOTTOM
            ).apply { leftMargin = dp(24); rightMargin = dp(24); bottomMargin = dp(16) }
        }
        progressBar = progress
        root.addView(progress)

        setContentView(root)

        val initialDanmaku = intent.getStringExtra(EXTRA_DANMAKU)
            ?.takeIf { it.isNotEmpty() }
            ?: pendingDanmaku
        initialDanmaku?.let { danmaku.load(it) }
        pendingDanmaku = null
        danmaku.setDanmakuEnabled(danmakuEnabled)
        menuBar.setDanmakuEnabled(danmakuEnabled)

        val mediaItem = MediaItem.fromUri(Uri.parse(url))
        val source = if (isHls) {
            HlsMediaSource.Factory(localCapable).createMediaSource(mediaItem)
        } else {
            ProgressiveMediaSource.Factory(dataFactory).createMediaSource(mediaItem)
        }
        exo.setMediaSource(source)
        if (startPositionMs > 0) exo.seekTo(startPositionMs)
        exo.prepare()
        exo.playWhenReady = true

        exo.addListener(object : Player.Listener {
            override fun onPlaybackStateChanged(state: Int) {
                if (state == Player.STATE_ENDED) {
                    finished = true
                    finishWithResult()
                }
            }

            override fun onPlayerError(error: PlaybackException) {
                // 回传错误，交给 Flutter 侧决定是否降级到内置播放器
                val reason = error.cause?.message?.takeIf { it.isNotBlank() }
                setResult(
                    RESULT_FIRST_USER,
                    Intent().putExtra("error", reason ?: nativeErrorText(error))
                )
                finish()
            }
        })
        exo.setPlaybackSpeed(playbackSpeed)
        menuBarRef?.setSpeed(playbackSpeed)
        view.requestFocus()
        handler.post(tick)
        revealProgress()
    }

    private fun dp(value: Int): Int =
        (value * resources.displayMetrics.density).toInt()

    /** 显示进度条并在 3 秒后自动隐藏。 */
    private fun revealProgress() {
        val bar = progressBar ?: return
        bar.visibility = View.VISIBLE
        handler.removeCallbacks(hideProgress)
        handler.postDelayed(hideProgress, 3_000)
    }

    private val hideProgress = Runnable {
        if (menuBarRef?.visibility != View.VISIBLE) {
            progressBar?.visibility = View.GONE
        }
    }

    /** 短暂显示一行提示，2 秒后自动隐藏。 */
    private fun showHint(text: String) {
        val view = hint ?: return
        view.text = text
        view.visibility = View.VISIBLE
        handler.removeCallbacks(hideHint)
        handler.postDelayed(hideHint, 2_000)
    }

    private val hideHint = Runnable { hint?.visibility = View.GONE }

    /** 选集面板：方向键移动、确定键切换、返回键收起。 */
    private fun togglePanel() {
        if (episodes.isEmpty()) return
        // 面板与操作条都消费方向键，同时可见时 onKeyDown 会先命中操作条，
        // 导致面板收不到输入。这里统一收起，调用点无需各自处理。
        menuBarRef?.visibility = View.GONE
        val existing = panel
        if (existing != null && existing.visibility == View.VISIBLE) {
            closePanel()
            return
        }
        val list = panelList ?: buildPanel()
        if (panel != null) {
            panel?.visibility = View.VISIBLE
            highlightSelected()
            return
        }
        val container = FrameLayout(this).apply {
            setBackgroundColor(Color.argb(215, 0, 0, 0))
            layoutParams = FrameLayout.LayoutParams(
                FrameLayout.LayoutParams.MATCH_PARENT,
                FrameLayout.LayoutParams.MATCH_PARENT
            )
            // 不聚焦：按键统一由 Activity.onKeyDown 处理
            isFocusable = false
        }
        val scroll = ScrollView(this).apply { isVerticalScrollBarEnabled = false }
        scroll.addView(list)
        panelScroll = scroll
        container.addView(
            scroll,
            FrameLayout.LayoutParams(
                FrameLayout.LayoutParams.MATCH_PARENT,
                FrameLayout.LayoutParams.WRAP_CONTENT
            ).apply { topMargin = dp(48); bottomMargin = dp(48) }
        )
        val title = TextView(this).apply {
            text = "选集"
            setTextColor(Color.WHITE)
            textSize = 18f
            setPadding(dp(24), dp(16), dp(24), dp(8))
        }
        container.addView(title)
        panel = container
        (window.decorView as ViewGroup).addView(container)
        highlightSelected()
    }

    private fun buildPanel(): LinearLayout {
        val list = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(16), 0, dp(16), dp(16))
        }
        panelList = list
        for ((index, name) in episodes.withIndex()) {
            val row = TextView(this).apply {
                text = name
                textSize = 16f
                setPadding(dp(20), dp(12), dp(20), dp(12))
                setTextColor(Color.WHITE)
                // 不聚焦：可聚焦子项会自行消费方向键，使 Activity.onKeyDown
                // 收不到事件。焦点与高亮由 highlightSelected 统一管理。
                isFocusable = false
                setOnClickListener { chooseEpisode(index) }
            }
            list.addView(row)
        }
        return list
    }

    private fun highlightSelected() {
        val list = panelList ?: return
        for (index in 0 until list.childCount) {
            val row = list.getChildAt(index) as? TextView ?: continue
            row.setTextColor(if (index == selectedIndex) Color.parseColor("#FF634C") else Color.WHITE)
            // 行不可聚焦（否则会抢走方向键），滚动改由这里驱动：
            // 把当前项滚入视野，否则超出首屏的集数无法选中。
            if (index == selectedIndex) scrollIntoView(row)
        }
    }

    /** 把目标行滚动到面板可视区域内。 */
    private fun scrollIntoView(row: View) {
        val scroll = panelScroll ?: return
        scroll.post {
            val top = row.top - (scroll.height - row.height) / 2
            scroll.smoothScrollTo(0, top.coerceAtLeast(0))
        }
    }

    private fun closePanel() {
        panel?.visibility = View.GONE
        playerView?.requestFocus()
    }

    /** 清晰度面板：列出可用档位，选择后回传由 Flutter 重新取流。 */
    private fun toggleQualityPanel() {
        if (qualities.isEmpty()) {
            showHint("本集没有可切换的清晰度")
            return
        }
        // 同 togglePanel：避免操作条抢走方向键
        menuBarRef?.visibility = View.GONE
        val existing = qualityPanel
        if (existing != null && existing.visibility == View.VISIBLE) {
            existing.visibility = View.GONE
            return
        }
        if (existing != null) {
            existing.visibility = View.VISIBLE
            highlightQuality()
            return
        }
        val list = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(16), 0, dp(16), dp(16))
        }
        qualityList = list
        for (value in qualities) {
            val row = TextView(this).apply {
                text = if (value > 0) "${value}P" else "自动"
                textSize = 16f
                setPadding(dp(20), dp(12), dp(20), dp(12))
                setTextColor(Color.WHITE)
                // 同选集：不聚焦，避免消费方向键
                isFocusable = false
                setOnClickListener { chooseQuality(value) }
            }
            list.addView(row)
        }
        val container = FrameLayout(this).apply {
            setBackgroundColor(Color.argb(215, 0, 0, 0))
            layoutParams = FrameLayout.LayoutParams(
                FrameLayout.LayoutParams.MATCH_PARENT,
                FrameLayout.LayoutParams.MATCH_PARENT
            )
        }
        val scroll = ScrollView(this).apply { isVerticalScrollBarEnabled = false }
        scroll.addView(list)
        container.addView(
            scroll,
            FrameLayout.LayoutParams(
                FrameLayout.LayoutParams.MATCH_PARENT,
                FrameLayout.LayoutParams.WRAP_CONTENT
            ).apply { topMargin = dp(48); bottomMargin = dp(48) }
        )
        container.addView(TextView(this).apply {
            text = "清晰度"
            setTextColor(Color.WHITE)
            textSize = 18f
            setPadding(dp(24), dp(16), dp(24), dp(8))
        })
        qualityPanel = container
        (window.decorView as ViewGroup).addView(container)
        highlightQuality()
    }

    private fun highlightQuality() {
        selectedQuality = currentQuality
        highlightQualitySelection()
    }

    /** 按 selectedQuality 高亮并聚焦，供面板打开与方向键移动时复用。 */
    private fun highlightQualitySelection() {
        val list = qualityList ?: return
        val index = qualities.indexOf(selectedQuality)
        for (position in 0 until list.childCount) {
            val row = list.getChildAt(position) as? TextView ?: continue
            row.setTextColor(
                if (position == index) Color.parseColor("#FF634C") else Color.WHITE
            )
        }
    }

    private fun chooseQuality(value: Int) {
        qualityPanel?.visibility = View.GONE
        if (value == currentQuality) return
        // 切换清晰度需要重新取流，交回 Flutter 处理
        setResult(
            RESULT_OK,
            Intent().apply {
                putExtra(RESULT_POSITION_MS, player?.currentPosition ?: 0L)
                putExtra(RESULT_DURATION_MS, player?.duration?.coerceAtLeast(0L) ?: 0L)
                putExtra(RESULT_COMPLETED, false)
                putExtra(RESULT_QUALITY, value)
                putExtra(RESULT_SPEED, playbackSpeed)
            }
        )
        finish()
    }

    /** 倍速档位循环。 */
    private fun nextSpeed(current: Float): Float {
        val index = SPEED_OPTIONS.indexOfFirst { kotlin.math.abs(it - current) < 0.01f }
        return SPEED_OPTIONS[(index + 1).mod(SPEED_OPTIONS.size)]
    }

    private fun chooseEpisode(index: Int) {
        if (index == episodeIndex) {
            closePanel()
            return
        }
        // 交给 Flutter 侧切换：原生 Activity 结束并回传目标下标
        setResult(
            RESULT_OK,
            Intent().apply {
                putExtra(RESULT_POSITION_MS, player?.currentPosition ?: 0L)
                putExtra(RESULT_DURATION_MS, player?.duration?.coerceAtLeast(0L) ?: 0L)
                putExtra(RESULT_COMPLETED, false)
                putExtra(RESULT_EPISODE_INDEX, index)
                putExtra(RESULT_SPEED, playbackSpeed)
            }
        )
        finish()
    }

    /** 解析十六进制 AES-128 内容密钥；缺失或格式不符时返回 null，按明文播放。 */
    private fun decodeKey(value: String?): ByteArray? {
        val text = value?.trim().orEmpty()
        if (text.length != 32) return null
        val key = ByteArray(16)
        for (index in 0 until 16) {
            val high = Character.digit(text[index * 2], 16)
            val low = Character.digit(text[index * 2 + 1], 16)
            if (high < 0 || low < 0) return null
            key[index] = ((high shl 4) or low).toByte()
        }
        return key
    }

    /** 把 ExoPlayer 错误码翻译成用户能看懂的原因。 */
    private fun nativeErrorText(error: PlaybackException): String = when (error.errorCode) {
        PlaybackException.ERROR_CODE_IO_NETWORK_CONNECTION_FAILED,
        PlaybackException.ERROR_CODE_IO_NETWORK_CONNECTION_TIMEOUT ->
            "网络连接失败，请检查网络后重试"
        PlaybackException.ERROR_CODE_IO_BAD_HTTP_STATUS ->
            "媒体地址已失效，请重新解析播放"
        PlaybackException.ERROR_CODE_PARSING_CONTAINER_MALFORMED,
        PlaybackException.ERROR_CODE_PARSING_MANIFEST_MALFORMED ->
            "视频格式不受支持，请切换线路或清晰度"
        PlaybackException.ERROR_CODE_DECODER_INIT_FAILED,
        PlaybackException.ERROR_CODE_DECODING_FAILED ->
            "设备无法硬解该编码，请切换清晰度或使用内置播放器"
        else -> "系统播放器播放失败（${error.errorCodeName}）"
    }

    private fun finishWithResult() {
        val data = Intent().apply {
            putExtra(RESULT_POSITION_MS, player?.currentPosition ?: 0L)
            putExtra(RESULT_DURATION_MS, player?.duration?.coerceAtLeast(0L) ?: 0L)
            putExtra(RESULT_COMPLETED, finished)
            putExtra(RESULT_SPEED, playbackSpeed)
        }
        setResult(RESULT_OK, data)
        finish()
    }

    override fun onKeyDown(keyCode: Int, event: KeyEvent): Boolean {
        // 清晰度面板优先：与选集面板同为全屏遮罩
        if (qualityPanel?.visibility == View.VISIBLE) {
            when (keyCode) {
                KeyEvent.KEYCODE_DPAD_DOWN -> {
                    val index = qualities.indexOf(selectedQuality)
                    val next = (index + 1).coerceAtMost(qualities.size - 1)
                    if (next >= 0) selectedQuality = qualities[next]
                    highlightQualitySelection()
                    return true
                }
                KeyEvent.KEYCODE_DPAD_UP -> {
                    val index = qualities.indexOf(selectedQuality)
                    val next = (index - 1).coerceAtLeast(0)
                    if (next >= 0) selectedQuality = qualities[next]
                    highlightQualitySelection()
                    return true
                }
                KeyEvent.KEYCODE_DPAD_CENTER, KeyEvent.KEYCODE_ENTER, KeyEvent.KEYCODE_NUMPAD_ENTER -> {
                    chooseQuality(selectedQuality)
                    return true
                }
                KeyEvent.KEYCODE_BACK -> {
                    qualityPanel?.visibility = View.GONE
                    menuBarRef?.visibility = View.VISIBLE
                    menuBarRef?.selectItem(2)
                    return true
                }
            }
            return true
        }
        // 底部操作条展开时，方向键在其内部移动
        val menu = menuBarRef
        if (menu?.visibility == View.VISIBLE) {
            when (keyCode) {
                KeyEvent.KEYCODE_DPAD_LEFT, KeyEvent.KEYCODE_DPAD_RIGHT,
                KeyEvent.KEYCODE_DPAD_CENTER, KeyEvent.KEYCODE_ENTER,
                KeyEvent.KEYCODE_NUMPAD_ENTER -> {
                    if (menu.handleKey(keyCode)) return true
                }
                KeyEvent.KEYCODE_DPAD_DOWN, KeyEvent.KEYCODE_BACK -> {
                    menu.visibility = View.GONE
                    playerView?.requestFocus()
                    return true
                }
            }
        }
        val panelOpen = panel?.visibility == View.VISIBLE
        if (panelOpen) {
            when (keyCode) {
                KeyEvent.KEYCODE_DPAD_DOWN -> {
                    selectedIndex = (selectedIndex + 1).coerceAtMost(episodes.size - 1)
                    highlightSelected()
                    return true
                }
                KeyEvent.KEYCODE_DPAD_UP -> {
                    selectedIndex = (selectedIndex - 1).coerceAtLeast(0)
                    highlightSelected()
                    return true
                }
                KeyEvent.KEYCODE_DPAD_CENTER, KeyEvent.KEYCODE_ENTER, KeyEvent.KEYCODE_NUMPAD_ENTER -> {
                    chooseEpisode(selectedIndex)
                    return true
                }
                KeyEvent.KEYCODE_BACK -> {
                    closePanel()
                    return true
                }
            }
            return true
        }
        when (keyCode) {
            // 上键展开底部操作条；再按一次收起
            KeyEvent.KEYCODE_DPAD_UP -> {
                val bar = menuBarRef ?: return super.onKeyDown(keyCode, event)
                if (bar.visibility == View.VISIBLE) {
                    bar.visibility = View.GONE
                    progressBar?.visibility = View.GONE
                    playerView?.requestFocus()
                } else {
                    bar.visibility = View.VISIBLE
                    bar.selectItem(0)
                    revealProgress()
                }
                return true
            }
            KeyEvent.KEYCODE_MENU -> {
                if (episodes.isNotEmpty()) {
                    togglePanel()
                    return true
                }
            }
            KeyEvent.KEYCODE_MEDIA_PLAY_PAUSE, KeyEvent.KEYCODE_SPACE, KeyEvent.KEYCODE_DPAD_CENTER -> {
                player?.let {
                    if (it.isPlaying) {
                        it.pause()
                        showHint("已暂停")
                    } else {
                        it.play()
                        showHint("播放中")
                    }
                }
                revealProgress()
                return true
            }
            KeyEvent.KEYCODE_MEDIA_FAST_FORWARD, KeyEvent.KEYCODE_DPAD_RIGHT -> {
                player?.let {
                    it.seekTo((it.currentPosition + 10_000).coerceAtMost(it.duration.coerceAtLeast(0)))
                    showHint("快进 10 秒")
                }
                revealProgress()
                return true
            }
            KeyEvent.KEYCODE_MEDIA_REWIND, KeyEvent.KEYCODE_DPAD_LEFT -> {
                player?.let {
                    it.seekTo((it.currentPosition - 10_000).coerceAtLeast(0))
                    showHint("快退 10 秒")
                }
                revealProgress()
                return true
            }
            // 无面板时：第一次按返回收起操作条，再按一次才退出播放。
            // 直接透传会导致误触返回键立即退出整集。
            KeyEvent.KEYCODE_BACK -> {
                if (menuBarRef?.visibility == View.VISIBLE) {
                    menuBarRef?.visibility = View.GONE
                    playerView?.requestFocus()
                    return true
                }
                if (backArmed) {
                    finishWithResult()
                    return true
                }
                backArmed = true
                showHint("再按一次返回退出播放")
                handler.removeCallbacks(disarmBack)
        handler.removeCallbacks(hideProgress)
                handler.postDelayed(disarmBack, 2_000)
                return true
            }
        }
        return super.onKeyDown(keyCode, event)
    }

    /** 返回键的二次确认状态；2 秒内未再按则失效。 */
    private val disarmBack = Runnable {
        backArmed = false
        hint?.visibility = View.GONE
    }

    override fun onPause() {
        reportProgress(force = true)
        super.onPause()
        player?.pause()
        handler.removeCallbacks(tick)
    }

    override fun onResume() {
        super.onResume()
        handler.post(tick)
    }

    override fun onDestroy() {
        handler.removeCallbacks(tick)
        handler.removeCallbacks(hideHint)
        handler.removeCallbacks(disarmBack)
        handler.removeCallbacks(hideProgress)
        if (current?.get() === this) current = null
        acceptingDanmaku = false
        pendingDanmaku = null
        (panel?.parent as? ViewGroup)?.removeView(panel)
        panel = null
        panelList = null
        playerView?.player = null
        player?.release()
        player = null
        super.onDestroy()
    }
}
