package com.duanju.duanju_app

import android.app.Activity
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.view.KeyEvent
import android.view.View
import android.view.WindowManager
import android.widget.FrameLayout
import androidx.media3.common.MediaItem
import androidx.media3.common.MimeTypes
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DefaultHttpDataSource
import androidx.media3.exoplayer.DefaultLoadControl
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.hls.HlsMediaSource
import androidx.media3.exoplayer.source.ProgressiveMediaSource
import androidx.media3.ui.AspectRatioFrameLayout
import androidx.media3.ui.PlayerView

/**
 * 原生全屏播放器：ExoPlayer + PlayerView(SurfaceView)。
 *
 * 用独立 Activity 承载，视频帧经 SurfaceView 由显示控制器直接合成，
 * 不进入 Flutter 的 GPU 合成管线 —— 这是电视/投影仪上最省 GPU 的路径。
 */
@UnstableApi
class NativePlayerActivity : Activity() {

    companion object {
        const val EXTRA_URL = "url"
        const val EXTRA_TITLE = "title"
        const val EXTRA_REFERER = "referer"
        const val EXTRA_POSITION_MS = "positionMs"
        const val RESULT_POSITION_MS = "positionMs"
        const val RESULT_COMPLETED = "completed"
    }

    private var player: ExoPlayer? = null
    private var playerView: PlayerView? = null
    private var startPositionMs = 0L
    private var finished = false

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

        // 加大缓冲：电视端网络抖动时不易断流
        val loadControl = DefaultLoadControl.Builder()
            .setBufferDurationsMs(
                60_000,   // minBufferMs
                180_000,  // maxBufferMs
                2_500,    // bufferForPlaybackMs
                5_000     // bufferForPlaybackAfterRebufferMs
            )
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

        val exo = ExoPlayer.Builder(this)
            .setLoadControl(loadControl)
            .build()
        player = exo

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
        setContentView(view)

        val mediaItem = MediaItem.fromUri(Uri.parse(url))
        val source = if (url.contains(".m3u8", ignoreCase = true)) {
            HlsMediaSource.Factory(httpFactory).createMediaSource(mediaItem)
        } else {
            ProgressiveMediaSource.Factory(httpFactory).createMediaSource(mediaItem)
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
                setResult(
                    RESULT_FIRST_USER,
                    Intent().putExtra("error", error.errorCodeName)
                )
                finish()
            }
        })
        view.requestFocus()
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
        when (keyCode) {
            KeyEvent.KEYCODE_MEDIA_PLAY_PAUSE, KeyEvent.KEYCODE_SPACE -> {
                player?.let { if (it.isPlaying) it.pause() else it.play() }
                return true
            }
            KeyEvent.KEYCODE_MEDIA_FAST_FORWARD -> {
                player?.let { it.seekTo((it.currentPosition + 10_000).coerceAtMost(it.duration)) }
                return true
            }
            KeyEvent.KEYCODE_MEDIA_REWIND -> {
                player?.let { it.seekTo((it.currentPosition - 10_000).coerceAtLeast(0)) }
                return true
            }
        }
        return super.onKeyDown(keyCode, event)
    }

    override fun onPause() {
        super.onPause()
        player?.pause()
    }

    override fun onDestroy() {
        playerView?.player = null
        player?.release()
        player = null
        super.onDestroy()
    }
}
