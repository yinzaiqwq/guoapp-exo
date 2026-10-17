package com.duanju.duanju_app.playback

internal class DanmakuLanes(
    private val viewport: Float,
    lanes: Int,
    private val duration: Float,
    private val gap: Float = 20f,
) {
    private data class Tail(val start: Long, val width: Float)
    private val tails = arrayOfNulls<Tail>(lanes.coerceAtLeast(1))

    fun place(start: Long, width: Float): Int? {
        for (lane in tails.indices) {
            val previous = tails[lane]
            if (previous != null) {
                val elapsed = (start - previous.start).toFloat()
                if (elapsed < duration) {
                    val previousSpeed = (viewport + previous.width) / duration
                    val speed = (viewport + width) / duration
                    val initialGap = previousSpeed * elapsed - previous.width
                    val overlap = duration - elapsed
                    val finalGap = initialGap + (previousSpeed - speed) * overlap
                    if (minOf(initialGap, finalGap) < gap) continue
                }
            }
            tails[lane] = Tail(start, width)
            return lane
        }
        return null
    }
}
