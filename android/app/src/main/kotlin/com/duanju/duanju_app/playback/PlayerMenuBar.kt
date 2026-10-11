package com.duanju.duanju_app.playback

import android.content.Context
import android.graphics.Color
import android.graphics.drawable.GradientDrawable
import android.view.Gravity
import android.view.KeyEvent
import android.view.View
import android.widget.FrameLayout
import android.widget.LinearLayout
import android.widget.TextView

/**
 * 底部操作条：弹幕开关、清晰度、选集、倍速等入口。
 *
 * 与选集面板一样放在原生侧：SurfaceView 由显示控制器直接合成，Flutter
 * 图层无法叠加其上。只负责呈现与回调，具体行为由 Activity 决定。
 */
internal class PlayerMenuBar(
    context: Context,
    private val onToggleDanmaku: () -> Unit,
    private val onQuality: () -> Unit,
    private val onEpisodes: () -> Unit,
    private val onSpeed: () -> Unit,
) : FrameLayout(context) {

    private val items = mutableListOf<TextView>()
    private var focused = 0
    private var danmakuOn = true

    private val accent = Color.parseColor("#FF634C")

    init {
        setBackgroundColor(Color.argb(200, 0, 0, 0))
        isFocusable = true
        isFocusableInTouchMode = true
        val row = LinearLayout(context).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER
        }
        addView(
            row,
            LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.WRAP_CONTENT, Gravity.CENTER)
                .apply { topMargin = dp(16); bottomMargin = dp(16) }
        )

        fun addItem(label: String, action: () -> Unit) {
            val view = TextView(context).apply {
                text = label
                textSize = 17f
                setTextColor(Color.WHITE)
                setPadding(dp(20), dp(10), dp(20), dp(10))
                isFocusable = true
                setOnClickListener { action() }
            }
            items.add(view)
            row.addView(view)
        }

        addItem("弹幕 开", onToggleDanmaku)
        addItem("清晰度", onQuality)
        addItem("选集", onEpisodes)
        addItem("倍速 1.0x", onSpeed)
        highlight()
    }

    /** 更新弹幕按钮文案，反映当前开关状态。 */
    fun setDanmakuEnabled(enabled: Boolean) {
        danmakuOn = enabled
        items.getOrNull(0)?.text = if (enabled) "弹幕 开" else "弹幕 关"
        highlight()
    }

    fun setSpeed(speed: Float) {
        val label = if (speed == speed.toInt().toFloat()) "${speed.toInt()}.0x" else "${speed}x"
        items.getOrNull(3)?.text = "倍速 $label"
        highlight()
    }

    private fun highlight() {
        items.forEachIndexed { index, view ->
            val active = index == focused
            view.setTextColor(if (active) accent else Color.WHITE)
            view.background = GradientDrawable().apply {
                setColor(if (active) Color.argb(60, 255, 99, 76) else Color.TRANSPARENT)
                cornerRadius = dp(8).toFloat()
            }
            if (active) view.requestFocus()
        }
    }

    /** 返回 true 表示事件已被消费。 */
    fun handleKey(keyCode: Int): Boolean {
        when (keyCode) {
            KeyEvent.KEYCODE_DPAD_LEFT -> {
                focused = (focused - 1 + items.size) % items.size
                highlight()
                return true
            }
            KeyEvent.KEYCODE_DPAD_RIGHT -> {
                focused = (focused + 1) % items.size
                highlight()
                return true
            }
            KeyEvent.KEYCODE_DPAD_CENTER, KeyEvent.KEYCODE_ENTER, KeyEvent.KEYCODE_NUMPAD_ENTER -> {
                when (focused) {
                    0 -> {
                        danmakuOn = !danmakuOn
                        onToggleDanmaku()
                    }
                    1 -> onQuality()
                    2 -> onEpisodes()
                    3 -> onSpeed()
                }
                return true
            }
        }
        return false
    }

    /** 外部改变焦点项后同步高亮（如清晰度面板返回）。 */
    fun requestFocusAt(index: Int) {
        focused = index.coerceIn(0, items.size - 1)
        highlight()
    }

    private fun dp(value: Int): Int = (value * resources.displayMetrics.density).toInt()
}
