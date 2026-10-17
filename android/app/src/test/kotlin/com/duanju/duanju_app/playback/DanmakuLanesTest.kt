package com.duanju.duanju_app.playback

import org.junit.Assert.*
import org.junit.Test

class DanmakuLanesTest {
    @Test fun simultaneousCommentsUseDifferentLanes() {
        val lanes = DanmakuLanes(1920f, 3, 9000f)
        assertEquals(0, lanes.place(0, 200f))
        assertEquals(1, lanes.place(0, 200f))
        assertEquals(2, lanes.place(0, 200f))
        assertNull(lanes.place(0, 200f))
    }

    @Test fun followingCommentWaitsForEntryGap() {
        val lanes = DanmakuLanes(1920f, 1, 9000f)
        assertEquals(0, lanes.place(0, 200f))
        assertNull(lanes.place(100, 200f))
        assertEquals(0, lanes.place(1000, 200f))
    }

    @Test fun longerFasterCommentCannotCatchPrevious() {
        val lanes = DanmakuLanes(1920f, 1, 9000f)
        assertEquals(0, lanes.place(0, 100f))
        assertNull(lanes.place(1000, 1800f))
        assertEquals(0, lanes.place(9000, 1800f))
    }

    @Test fun acceptedCommentsRemainSeparatedDuringTheirOverlap() {
        val duration = 9000f
        val lanes = DanmakuLanes(1920f, 4, duration)
        val accepted = mutableListOf<Triple<Long, Float, Int>>()
        for (index in 0..200) {
            val time = index * 137L
            val width = 50f + (index * 173 % 1600)
            val lane = lanes.place(time, width) ?: continue
            for ((start, previousWidth, previousLane) in accepted) {
                if (lane != previousLane || time - start >= duration) continue
                for (step in 0..100) {
                    val elapsed = (duration - (time - start)) * step / 100
                    val previousRight = 1920f - (1920f + previousWidth) * ((time - start + elapsed) / duration) + previousWidth
                    val nextLeft = 1920f - (1920f + width) * (elapsed / duration)
                    assertTrue("same-lane collision", nextLeft - previousRight >= 19.9f)
                }
            }
            accepted.add(Triple(time, width, lane))
        }
        assertTrue(accepted.size > 4)
    }
}
