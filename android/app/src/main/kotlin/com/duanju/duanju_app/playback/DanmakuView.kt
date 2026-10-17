package com.duanju.duanju_app.playback

import android.content.Context
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.Typeface
import android.os.SystemClock
import android.view.View

/**
 * 弹幕渲染层，叠加在 SurfaceView 之上。
 *
 * 两条设计要点：
 *
 * 1. 用单调时钟自行驱动重绘。播放进度每 200ms 才上报一次，若只在该回调里
 *    invalidate，弹幕会一顿一顿地跳；这里按 SystemClock.uptimeMillis 计算
 *    平滑位移，配合 postInvalidateOnAnimation 逐帧绘制。
 *
 * 2. 只遍历当前可见的时间窗口。整集弹幕可达数百条，逐帧遍历全部会显著掉帧；
 *    条目按时间排序后用二分定位窗口起点，只处理窗口内的少量条目。
 */
internal class DanmakuView(context: Context) : View(context) {

    private data class Item(val timeMs: Long, val text: String)

    private var items: List<Item> = emptyList()
    private var enabled = true
    private var areaFraction = 0.33f
    private var fontSize = 34f
    private var opacity = 1f
    private var travelMs = 9_000f
    private var blockedWords: List<String> = emptyList()

    /** 播放进度对应的时钟基准：positionMs 对应 uptimeMs 时刻。 */
    private var anchorPositionMs = 0L
    private var anchorUptimeMs = 0L
    private var playing = true

    /** 是否有内容需要逐帧绘制。 */
    private var animating = false

    private val paint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = Color.WHITE
        textSize = 34f
        typeface = Typeface.DEFAULT_BOLD
        setShadowLayer(3f, 1.5f, 1.5f, Color.argb(200, 0, 0, 0))
    }
    private val strokePaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = Color.argb(190, 0, 0, 0)
        textSize = 34f
        typeface = Typeface.DEFAULT_BOLD
        style = Paint.Style.STROKE
        strokeWidth = 4f
    }

    /** 一条弹幕穿过屏幕所需时间。 */
    private val topMargin = 24f

    init {
        isFocusable = false
        isClickable = false
        setWillNotDraw(false)
    }

    /**
     * 载入弹幕。[payload] 每行格式为 `时间毫秒\t文本`，由 Dart 侧序列化。
     */
    fun load(payload: String) {
        val parsed = ArrayList<Item>()
        for (line in payload.split('\n')) {
            if (line.isEmpty()) continue
            val tab = line.indexOf('\t')
            if (tab <= 0 || tab >= line.length - 1) continue
            val time = line.substring(0, tab).toLongOrNull() ?: continue
            val text = line.substring(tab + 1).trim()
            if (text.isEmpty()) continue
            parsed.add(Item(time, text))
        }
        parsed.sortBy { it.timeMs }
        items = parsed
        restart()
    }

    /** 开关弹幕显示。命名避开 View.setEnabled，那是控件可用状态。 */
    fun setDanmakuEnabled(value: Boolean) {
        enabled = value
        if (!value) {
            animating = false
            invalidate()
        } else {
            restart()
        }
    }

    fun configure(area: Float, font: Float, alpha: Float, durationMs: Float, blocked: String) {
        areaFraction = area.coerceIn(0.2f, 0.5f)
        fontSize = font.coerceIn(24f, 48f)
        opacity = alpha.coerceIn(0.2f, 1f)
        travelMs = durationMs.coerceIn(5_000f, 15_000f)
        blockedWords = blocked.split(',', '，', '\n').map { it.trim() }.filter { it.isNotEmpty() }
        paint.textSize = fontSize
        paint.alpha = (255 * opacity).toInt()
        paint.setShadowLayer(3f, 1.5f, 1.5f, Color.argb((200 * opacity).toInt(), 0, 0, 0))
        strokePaint.textSize = fontSize
        strokePaint.alpha = (190 * opacity).toInt()
        invalidate()
    }

    /**
     * 上报播放进度，重建时钟基准。
     *
     * 播放中每 200ms 调用一次；两次调用之间由 uptimeMillis 推算当前位置，
     * 因此位移是连续的而不是阶梯式的。
     */
    fun setPosition(ms: Long, isPlaying: Boolean = true) {
        anchorPositionMs = ms
        anchorUptimeMs = SystemClock.uptimeMillis()
        playing = isPlaying
        restart()
    }

    /** 启动逐帧绘制。 */
    private fun restart() {
        if (!enabled || items.isEmpty() || animating) return
        animating = true
        postInvalidateOnAnimation()
    }

    /** 当前播放位置：暂停时用锚点，播放中按单调时钟外推。 */
    private fun currentPositionMs(): Long {
        if (!playing) return anchorPositionMs
        val elapsed = SystemClock.uptimeMillis() - anchorUptimeMs
        return anchorPositionMs + elapsed
    }

    override fun onDraw(canvas: Canvas) {
        super.onDraw(canvas)
        if (!enabled || items.isEmpty()) {
            animating = false
            return
        }
        val width = width.toFloat()
        if (width <= 0f) {
            animating = false
            return
        }

        val now = currentPositionMs()
        // 只处理 [now - travelMs, now] 窗口内的条目：早于窗口的已飞出屏幕，
        // 晚于 now 的还没进场。二分定位窗口起点。
        val windowStart = now - travelMs.toLong()
        var low = 0
        var high = items.size
        while (low < high) {
            val middle = (low + high) ushr 1
            if (items[middle].timeMs < windowStart) low = middle + 1 else high = middle
        }

        val laneHeight = fontSize + 12f
        val lanes = ((height * areaFraction - topMargin) / laneHeight).toInt().coerceIn(1, 4)
        var drawn = 0
        for (index in low until items.size) {
            val item = items[index]
            if (blockedWords.any { item.text.contains(it, ignoreCase = true) }) continue
            val elapsed = now - item.timeMs
            if (elapsed < 0) break
            if (elapsed > travelMs.toLong()) continue
            val textWidth = paint.measureText(item.text)
            val x = width - (width + textWidth) * (elapsed / travelMs)
            if (x > width || x + textWidth < 0f) continue
            val lane = index % lanes
            val y = topMargin + lane * laneHeight + fontSize
            canvas.drawText(item.text, x, y, strokePaint)
            canvas.drawText(item.text, x, y, paint)
            drawn++
        }

        // 还有内容在窗口内就继续逐帧刷新；否则停下来省电。
        val hasPending = items.lastOrNull()?.timeMs?.let { it > now } ?: false
        if (drawn > 0 || hasPending) {
            postInvalidateOnAnimation()
        } else {
            animating = false
        }
    }
}
