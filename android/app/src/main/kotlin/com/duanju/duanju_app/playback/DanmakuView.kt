package com.duanju.duanju_app.playback

import android.content.Context
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.Typeface
import android.view.View

/**
 * 弹幕渲染层，叠加在 SurfaceView 之上。
 *
 * 数据由 Flutter 侧按集预取后传入（原生侧不联网），按播放进度计算位置：
 * 每条弹幕从右向左匀速穿过屏幕，进度由 [setPosition] 推进。
 *
 * 无状态渲染：位置完全由「当前进度 - 弹幕时间」推导，因此 seek 后画面
 * 立即正确，不需要重建活动列表。
 */
internal class DanmakuView(context: Context) : View(context) {

    private data class Item(val timeMs: Long, val text: String)

    private var items: List<Item> = emptyList()
    private var positionMs = 0L
    private var enabled = true

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

    /** 一条弹幕穿过屏幕所需时间；越短越快。 */
    private val travelMs = 9_000f
    private val laneHeight = 46f
    private val topMargin = 24f

    init {
        // 不参与焦点与触摸，避免遮挡遥控器操作
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
        invalidate()
    }

    /** 开关弹幕显示。命名避开 View.setEnabled，那是控件可用状态。 */
    fun setDanmakuEnabled(value: Boolean) {
        enabled = value
        if (!value) items = emptyList()
        invalidate()
    }

    /** 由播放进度驱动重绘。 */
    fun setPosition(ms: Long) {
        positionMs = ms
        if (items.isNotEmpty()) invalidate()
    }

    override fun onDraw(canvas: Canvas) {
        super.onDraw(canvas)
        if (!enabled || items.isEmpty()) return
        val width = width.toFloat()
        if (width <= 0f) return
        val lanes = maxOf(1, ((height - topMargin) / laneHeight).toInt())

        for ((index, item) in items.withIndex()) {
            val elapsed = positionMs - item.timeMs
            // 只画屏幕上正在飞的，以及即将出现的少量
            if (elapsed < 0 || elapsed > travelMs.toLong()) continue
            val textWidth = paint.measureText(item.text)
            val x = width - (width + textWidth) * (elapsed / travelMs)
            if (x > width || x + textWidth < 0f) continue
            val lane = index % lanes
            val y = topMargin + lane * laneHeight + paint.textSize
            canvas.drawText(item.text, x, y, strokePaint)
            canvas.drawText(item.text, x, y, paint)
        }
    }
}
