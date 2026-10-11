package com.duanju.duanju_app.playback

import android.content.Context
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.RectF
import android.graphics.Typeface
import android.view.View

/**
 * 电视风格的播放进度条。
 *
 * 自绘而非使用 media3 的 PlayerView 控制器：后者会消费方向键与确定键，
 * 导致 Activity 收不到遥控器事件。这里只负责显示，按键仍由 Activity 处理。
 *
 * 大字号、粗进度、居中时间，兼顾三米外观看与遥控器操作反馈。
 */
internal class ProgressBarView(context: Context) : View(context) {

    private var positionMs = 0L
    private var durationMs = 0L
    private var bufferedMs = 0L

    private val trackPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = Color.argb(90, 255, 255, 255)
    }
    private val bufferPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = Color.argb(120, 255, 255, 255)
    }
    private val progressPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = Color.parseColor("#FF634C")
    }
    private val knobPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = Color.WHITE
    }
    private val textPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = Color.WHITE
        textSize = 30f
        typeface = Typeface.DEFAULT_BOLD
        setShadowLayer(4f, 1.5f, 1.5f, Color.argb(220, 0, 0, 0))
    }

    /** 进度条高度与时间文字的垂直间距。 */
    private val barHeight = 8f
    private val labelGap = 16f
    private val knobRadius = 11f

    init {
        isFocusable = false
        isClickable = false
        setWillNotDraw(false)
    }

    fun update(position: Long, duration: Long, buffered: Long) {
        val changed = position != positionMs || duration != durationMs || buffered != bufferedMs
        positionMs = position
        durationMs = duration
        bufferedMs = buffered
        if (changed) invalidate()
    }

    /** 把毫秒格式化为 `mm:ss`；超过一小时补上小时位。 */
    private fun format(ms: Long): String {
        if (ms <= 0) return "00:00"
        val total = ms / 1000
        val hours = total / 3600
        val minutes = (total % 3600) / 60
        val seconds = total % 60
        return if (hours > 0) {
            String.format("%d:%02d:%02d", hours, minutes, seconds)
        } else {
            String.format("%02d:%02d", minutes, seconds)
        }
    }

    override fun onDraw(canvas: Canvas) {
        super.onDraw(canvas)
        val width = width.toFloat()
        if (width <= 0f || durationMs <= 0L) return

        // 时间文字画在进度条上方
        val label = "${format(positionMs)} / ${format(durationMs)}"
        val barTop = height - barHeight - dp(8)
        val textY = barTop - labelGap
        canvas.drawText(label, 0f, textY, textPaint)

        val fraction = (positionMs.toDouble() / durationMs).coerceIn(0.0, 1.0)
        val bufferFraction = (bufferedMs.toDouble() / durationMs).coerceIn(0.0, 1.0)
        val radius = barHeight / 2
        val top = barTop

        canvas.drawRoundRect(RectF(0f, top, width, top + barHeight), radius, radius, trackPaint)
        if (bufferFraction > fraction) {
            canvas.drawRoundRect(
                RectF(0f, top, (width * bufferFraction).toFloat(), top + barHeight),
                radius, radius, bufferPaint
            )
        }
        val played = (width * fraction).toFloat()
        if (played > 0f) {
            canvas.drawRoundRect(RectF(0f, top, played, top + barHeight), radius, radius, progressPaint)
        }
        canvas.drawCircle(played, top + radius, knobRadius, knobPaint)
    }

    private fun dp(value: Int): Float = value * resources.displayMetrics.density
}
