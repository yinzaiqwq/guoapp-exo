package com.duanju.duanju_app

import android.app.Activity
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
import android.widget.FrameLayout
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DataSource
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

        const val RESULT_POSITION_MS = "positionMs"
        const val RESULT_COMPLETED = "completed"

        /** 用户在选集中切换了分集，回传目标下标。 */
        const val RESULT_EPISODE_INDEX = "episodeIndex"
    }

    private var player: ExoPlayer? = null
    private var playerView: PlayerView? = null
    private var danmakuView: DanmakuView? = null
    private var panel: View? = null
    private var panelList: LinearLayout? = null
    private var hint: TextView? = null
    private var startPositionMs = 0L
    private var finished = false
    private var episodeIndex = 0
    private var episodes: List<String> = emptyList()
    private var selectedIndex = 0

    private val handler = Handler(Looper.getMainLooper())
    private val tick = object : Runnable {
        override fun run() {
            danmakuView?.setPosition(player?.currentPosition ?: 0L)
            handler.postDelayed(this, 200)
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
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

        // 加密 MP4：样本级 CENC 解密后交给解码器；明文与 HLS 直接走原始数据源
        val dataFactory: DataSource.Factory =
            if (key != null && !isHls) CencDataSourceFactory(httpFactory, url, referer, key)
            else httpFactory

        // 允许在硬解不可用时回退到软解，避免直接报错
        val renderers = DefaultRenderersFactory(this).setEnableDecoderFallback(true)
        val exo = ExoPlayer.Builder(this, renderers)
            .setLoadControl(loadControl)
            .build()
        player = exo

        val root = FrameLayout(this)

        val view = PlayerView(this).apply {
            useController = true
            resizeMode = AspectRatioFrameLayout.RESIZE_MODE_FIT
            setShowBuffering(PlayerView.SHOW_BUFFERING_WHEN_PLAYING)
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

        setContentView(root)

        intent.getStringExtra(EXTRA_DANMAKU)?.let { danmaku.load(it) }

        val mediaItem = MediaItem.fromUri(Uri.parse(url))
        val source = if (isHls) {
            HlsMediaSource.Factory(httpFactory).createMediaSource(mediaItem)
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
        view.requestFocus()
        handler.post(tick)
    }

    private fun dp(value: Int): Int =
        (value * resources.displayMetrics.density).toInt()

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
            isFocusable = true
            isFocusableInTouchMode = true
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
        val title = TextView(this).apply {
            text = "选集"
            setTextColor(Color.WHITE)
            textSize = 18f
            setPadding(dp(24), dp(16), dp(24), dp(8))
        }
        container.addView(title)
        panel = container
        (window.decorView as ViewGroup).addView(container)
        container.requestFocus()
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
                isFocusable = true
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
            if (index == selectedIndex) row.requestFocus()
        }
    }

    private fun closePanel() {
        panel?.visibility = View.GONE
        playerView?.requestFocus()
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
                putExtra(RESULT_COMPLETED, false)
                putExtra(RESULT_EPISODE_INDEX, index)
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
            putExtra(RESULT_COMPLETED, finished)
        }
        setResult(RESULT_OK, data)
        finish()
    }

    override fun onKeyDown(keyCode: Int, event: KeyEvent): Boolean {
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
            KeyEvent.KEYCODE_DPAD_UP, KeyEvent.KEYCODE_MENU -> {
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
                return true
            }
            KeyEvent.KEYCODE_MEDIA_FAST_FORWARD, KeyEvent.KEYCODE_DPAD_RIGHT -> {
                player?.let {
                    it.seekTo((it.currentPosition + 10_000).coerceAtMost(it.duration.coerceAtLeast(0)))
                    showHint("快进 10 秒")
                }
                return true
            }
            KeyEvent.KEYCODE_MEDIA_REWIND, KeyEvent.KEYCODE_DPAD_LEFT -> {
                player?.let {
                    it.seekTo((it.currentPosition - 10_000).coerceAtLeast(0))
                    showHint("快退 10 秒")
                }
                return true
            }
        }
        return super.onKeyDown(keyCode, event)
    }

    override fun onPause() {
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
        (panel?.parent as? ViewGroup)?.removeView(panel)
        panel = null
        panelList = null
        playerView?.player = null
        player?.release()
        player = null
        super.onDestroy()
    }
}
