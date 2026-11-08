package systems.pkt.lingon.terminal

import android.os.SystemClock
import android.view.MotionEvent
import android.view.View
import androidx.compose.ui.graphics.Color
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestCoroutineScheduler
import org.junit.Test
import org.junit.runner.RunWith
import systems.pkt.lingon.DefaultTerminalZoom
import systems.pkt.lingon.protocol.ScrollbackRow

@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(AndroidJUnit4::class)
class TerminalSelectionScrollInstrumentedTest {
    private fun snapshot(rows: Int = 30): TerminalSnapshot {
        val cols = 40
        val runes = IntArray(rows * cols) { ' '.code }
        repeat(rows) { row -> "row%02d".format(row).forEachIndexed { col, c -> runes[row * cols + col] = c.code } }
        return TerminalSnapshot(cols, rows, runes, IntArray(runes.size), IntArray(runes.size), IntArray(runes.size), null, 0, 0, false, 0, "")
    }
    private fun update(view: TerminalGridView, snap: TerminalSnapshot, offset: Int = 0, seq: Long = 1) {
        view.update(snap, 14, 8, TerminalPalette(Color.White, Color.Black), seq,
            snap.cols, snap.rows - offset, false, DefaultTerminalZoom, 0, offset, false, false)
    }
    private fun view(snap: TerminalSnapshot): TerminalGridView {
        val view = TerminalGridView(InstrumentationRegistry.getInstrumentation().targetContext)
        view.measure(View.MeasureSpec.makeMeasureSpec(240, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(160, View.MeasureSpec.EXACTLY))
        view.layout(0, 0, 240, 160)
        update(view, snap)
        view.restoreViewportState(view.captureViewportState().copy(cameraOffsetYPx = 0f, mode = TerminalViewportMode.Manual))
        view.setSelectionMode(true)
        return view
    }
    private fun event(view: View, action: Int, points: List<Pair<Float, Float>>, time: Long, index: Int = 0) {
        val props = points.indices.map { id -> MotionEvent.PointerProperties().apply { this.id = id; toolType = MotionEvent.TOOL_TYPE_FINGER } }.toTypedArray()
        val coords = points.map { (px, py) -> MotionEvent.PointerCoords().apply { x = px; y = py; pressure = 1f; size = 1f } }.toTypedArray()
        val e = MotionEvent.obtain(time, time, action or (index shl MotionEvent.ACTION_POINTER_INDEX_SHIFT), points.size, props, coords, 0, 0, 1f, 1f, 0, 0, 0, 0)
        try { view.dispatchTouchEvent(e) } finally { e.recycle() }
    }
    private fun select(view: TerminalGridView, lift: Boolean = true) {
        val w = view.getScaledCellWidthForTesting()
        val h = view.getScaledCellHeightForTesting()
        val time = SystemClock.uptimeMillis()
        event(view, MotionEvent.ACTION_DOWN, listOf(w * 0.5f to h * 0.5f), time)
        event(view, MotionEvent.ACTION_MOVE, listOf(w * 4.5f to h * 0.5f), time + 16)
        if (lift) event(view, MotionEvent.ACTION_UP, listOf(w * 4.5f to h * 0.5f), time + 32)
        assertEquals("row00", view.selectedText())
    }
    @Test
    fun secondFingerDuringSelectionPreservesNewRangeAndCancellation() {
        InstrumentationRegistry.getInstrumentation().runOnMainSync {
            val view = view(snapshot())
            select(view, lift = false)
            val time = SystemClock.uptimeMillis() + 100
            event(view, MotionEvent.ACTION_POINTER_DOWN, listOf(90f to 90f, 140f to 90f), time, 1)
            assertEquals("row00", view.selectedText())
            event(view, MotionEvent.ACTION_MOVE, listOf(50f to 50f, 100f to 50f), time + 16)
            assertTrue(view.getCameraOffsetYForTesting() > 0)
            assertEquals("row00", view.selectedText())
            event(view, MotionEvent.ACTION_CANCEL, listOf(50f to 50f, 100f to 50f), time + 32)
            assertEquals("row00", view.selectedText())
        }
    }
    @Test
    fun holdBeforeSecondFingerAndExpandedHistoryKeepSelection() {
        InstrumentationRegistry.getInstrumentation().runOnMainSync {
            val live = snapshot()
            val view = view(live)
            select(view)
            val time = SystemClock.uptimeMillis() + 100
            event(view, MotionEvent.ACTION_DOWN, listOf(100f to 100f), time)
            assertEquals("first finger must not replace a range before a drag", "row00", view.selectedText())
            event(view, MotionEvent.ACTION_POINTER_DOWN, listOf(100f to 100f, 160f to 100f), time + 1000, 1)
            val history = List(5) { ScrollbackRow.newBuilder().addAllRunes("older".padEnd(live.cols).map { it.code }).build() }
            update(view, buildScrollbackSnapshot(live, history, 5), 5, 2)
            assertEquals("history prepend must preserve selected cells", "row00", view.selectedText())
            event(view, MotionEvent.ACTION_POINTER_UP, listOf(100f to 100f, 160f to 100f), time + 1016, 1)
            event(view, MotionEvent.ACTION_MOVE, listOf(30f to 20f), time + 1032)
            event(view, MotionEvent.ACTION_UP, listOf(30f to 20f), time + 1048)
            assertEquals("remaining finger must not reselect after panning", "row00", view.selectedText())
        }
    }
    @Test
    fun expandingHistoryPreservesCommittedRange() {
        InstrumentationRegistry.getInstrumentation().runOnMainSync {
            val live = snapshot()
            val view = view(live)
            select(view)
            val history = List(5) { ScrollbackRow.newBuilder().addAllRunes("older".padEnd(live.cols).map { it.code }).build() }
            update(view, buildScrollbackSnapshot(live, history, 5), 5, 2)
            assertEquals("row00", view.selectedText())
        }
    }

    @Test
    fun edgeHoldSelectsBeyondViewportAndReleaseStopsScrolling() {
        InstrumentationRegistry.getInstrumentation().runOnMainSync {
            val view = view(snapshot())
            val clock = TestCoroutineScheduler()
            view.selectionScrollDispatcher = StandardTestDispatcher(clock)
            val w = view.getScaledCellWidthForTesting()
            val h = view.getScaledCellHeightForTesting()
            val visibleRows = view.getVisibleEndRowExclusive()
            val time = SystemClock.uptimeMillis()
            event(view, MotionEvent.ACTION_DOWN, listOf(w * 0.5f to h * 0.5f), time)
            event(view, MotionEvent.ACTION_MOVE, listOf(w * 4.5f to 159f), time + 16)
            val before = view.getCameraOffsetYForTesting()
            clock.runCurrent()
            assertEquals(before, view.getCameraOffsetYForTesting(), 0f)
            clock.advanceTimeBy(12 * TerminalSelectionAutoScroll.IntervalMs + 1)
            clock.runCurrent()
            val copied = view.selectedText()!!
            assertTrue(copied.startsWith("row00"))
            assertTrue("selection did not extend beyond the original viewport", copied.lines().size > visibleRows)
            assertTrue(view.getCameraOffsetYForTesting() > before)
            event(view, MotionEvent.ACTION_UP, listOf(w * 4.5f to 159f), time + 32)
            val stopped = view.getCameraOffsetYForTesting()
            clock.advanceTimeBy(10_000)
            assertEquals(stopped, view.getCameraOffsetYForTesting(), 0f)
            assertEquals(copied, view.selectedText())
        }
    }

    @Test
    fun reverseAndHorizontalEdgeScrollingStopForCancelAndModeExit() {
        InstrumentationRegistry.getInstrumentation().runOnMainSync {
            val view = view(snapshot())
            val clock = TestCoroutineScheduler()
            view.selectionScrollDispatcher = StandardTestDispatcher(clock)
            val h = view.getScaledCellHeightForTesting()
            view.restoreViewportState(view.captureViewportState().copy(cameraOffsetYPx = h * 15, mode = TerminalViewportMode.Manual))
            val time = SystemClock.uptimeMillis()
            event(view, MotionEvent.ACTION_DOWN, listOf(80f to 80f), time)
            event(view, MotionEvent.ACTION_MOVE, listOf(239f to 1f), time + 16)
            clock.runCurrent()
            clock.advanceTimeBy(5 * TerminalSelectionAutoScroll.IntervalMs + 1)
            clock.runCurrent()
            assertTrue(view.getCameraOffsetYForTesting() < h * 15)
            assertTrue(view.getCameraOffsetXForTesting() > 0)
            assertTrue(view.selectedText()!!.lines().size > 5)
            event(view, MotionEvent.ACTION_CANCEL, listOf(239f to 1f), time + 32)
            assertEquals(null, view.selectedText())
            val stopped = view.getCameraOffsetYForTesting()
            clock.advanceTimeBy(1000)
            assertEquals(stopped, view.getCameraOffsetYForTesting(), 0f)
            event(view, MotionEvent.ACTION_DOWN, listOf(80f to 80f), time + 64)
            event(view, MotionEvent.ACTION_MOVE, listOf(239f to 159f), time + 80)
            view.setSelectionMode(false)
            clock.advanceTimeBy(1000)
            assertEquals(stopped, view.getCameraOffsetYForTesting(), 0f)
            assertEquals(null, view.selectedText())
        }
    }

    @Test
    fun secondFingerStopsEdgeRepeatAndPanningBackKeepsSelectedHistory() {
        InstrumentationRegistry.getInstrumentation().runOnMainSync {
            val live = snapshot()
            val view = view(live)
            val clock = TestCoroutineScheduler()
            view.selectionScrollDispatcher = StandardTestDispatcher(clock)
            select(view, lift = false)
            val copied = view.selectedText()
            val time = SystemClock.uptimeMillis() + 100
            event(view, MotionEvent.ACTION_POINTER_DOWN, listOf(70f to 60f, 150f to 60f), time, 1)
            clock.advanceTimeBy(1000)
            assertEquals(copied, view.selectedText())
            val history = List(5) { ScrollbackRow.newBuilder().addAllRunes("older".padEnd(live.cols).map { it.code }).build() }
            val requests = mutableListOf<Int>()
            view.setOnScrollback { requests.add(it) }
            update(view, buildScrollbackSnapshot(live, history, 5), 5, 2)
            event(view, MotionEvent.ACTION_MOVE, listOf(70f to -2000f, 150f to -2000f), time + 16)
            assertEquals(copied, view.selectedText())
            assertTrue("selected history was dropped while panning to live", requests.none { it < 0 })
            event(view, MotionEvent.ACTION_CANCEL, listOf(70f to 10f, 150f to 10f), time + 32)
            assertEquals(copied, view.selectedText())
        }
    }

    @Test
    fun pinchAndLiftFirstFingerPreserveRangeAndDoNotResumeSelection() {
        InstrumentationRegistry.getInstrumentation().runOnMainSync {
            val view = view(snapshot())
            select(view)
            var zoom = 1f
            view.setOnZoomChanged { zoom = it }
            val time = SystemClock.uptimeMillis() + 100
            event(view, MotionEvent.ACTION_DOWN, listOf(80f to 70f), time)
            event(view, MotionEvent.ACTION_POINTER_DOWN, listOf(80f to 70f, 160f to 70f), time + 16, 1)
            // Cross the device's minimum scale span over a full gesture, with
            // explicit event times rather than sleeps or recognition timeouts.
            for (step in 1..8) {
                val halfSpan = 40f + step * 25f
                event(view, MotionEvent.ACTION_MOVE, listOf(120f - halfSpan to 70f, 120f + halfSpan to 70f), time + 16 + step * 16L)
            }
            assertTrue("pinch did not adjust zoom", zoom > 1f)
            assertEquals("row00", view.selectedText())
            event(view, MotionEvent.ACTION_POINTER_UP, listOf(-120f to 70f, 360f to 70f), time + 160, 0)
            // Real pointer IDs remain stable when index zero lifts.
            val properties = arrayOf(MotionEvent.PointerProperties().apply { id = 1; toolType = MotionEvent.TOOL_TYPE_FINGER })
            val coordinates = arrayOf(MotionEvent.PointerCoords().apply { x = 20f; y = 20f; pressure = 1f; size = 1f })
            for (action in listOf(MotionEvent.ACTION_MOVE, MotionEvent.ACTION_UP)) {
                val e = MotionEvent.obtain(time, time + 176, action, 1, properties, coordinates, 0, 0, 1f, 1f, 0, 0, 0, 0)
                try { view.dispatchTouchEvent(e) } finally { e.recycle() }
            }
            assertEquals("row00", view.selectedText())
        }
    }

    @Test
    fun realGeometryChangeClearsRangeAndStopsEdgeRepeat() {
        InstrumentationRegistry.getInstrumentation().runOnMainSync {
            val view = view(snapshot())
            val clock = TestCoroutineScheduler()
            view.selectionScrollDispatcher = StandardTestDispatcher(clock)
            select(view, lift = false)
            update(view, snapshot(2), 0, 2)
            assertEquals(null, view.selectedText())
            val stopped = view.getCameraOffsetYForTesting()
            clock.advanceTimeBy(1000)
            assertEquals(stopped, view.getCameraOffsetYForTesting(), 0f)
        }
    }

    @Test
    fun preciseCellDragBelowTouchSlopCommitsOnLiftAndHoldKeepsIt() {
        InstrumentationRegistry.getInstrumentation().runOnMainSync {
            val snap = snapshot()
            val view = view(snap)
            view.update(snap, 14, 8, TerminalPalette(Color.White, Color.Black), 2,
                snap.cols, snap.rows, true, DefaultTerminalZoom, 0, 0, false, false)
            view.restoreViewportState(view.captureViewportState().copy(cameraOffsetYPx = 0f, mode = TerminalViewportMode.Manual))
            var tapped = false
            view.setOnTap { tapped = true }
            val w = view.getScaledCellWidthForTesting()
            val h = view.getScaledCellHeightForTesting()
            assertTrue(w * 2 < android.view.ViewConfiguration.get(view.context).scaledTouchSlop)
            val time = SystemClock.uptimeMillis()
            event(view, MotionEvent.ACTION_DOWN, listOf(w * 0.5f to h * 0.5f), time)
            event(view, MotionEvent.ACTION_MOVE, listOf(w * 2.5f to h * 0.5f), time + 16)
            event(view, MotionEvent.ACTION_UP, listOf(w * 2.5f to h * 0.5f), time + 32)
            assertEquals("row", view.selectedText())
            assertEquals(false, tapped)
            event(view, MotionEvent.ACTION_DOWN, listOf(100f to 100f), time + 64)
            assertEquals("row", view.selectedText())
            event(view, MotionEvent.ACTION_POINTER_DOWN, listOf(100f to 100f, 160f to 100f), time + 80, 1)
            event(view, MotionEvent.ACTION_CANCEL, listOf(100f to 100f, 160f to 100f), time + 96)
            assertEquals("row", view.selectedText())
        }
    }

    @Test
    fun heldTopEdgeWaitsForHistoryThenExtendsIntoOlderRows() {
        InstrumentationRegistry.getInstrumentation().runOnMainSync {
            val live = snapshot(3)
            val view = view(live)
            val clock = TestCoroutineScheduler()
            view.selectionScrollDispatcher = StandardTestDispatcher(clock)
            val history = List(5) { row -> ScrollbackRow.newBuilder()
                .addAllRunes("older$row".padEnd(live.cols).map { it.code }).build() }
            var requests = 0
            var pendingRows = 0
            var offset = 0
            view.setOnScrollback { rows -> requests++; pendingRows += rows }
            val w = view.getScaledCellWidthForTesting()
            val h = view.getScaledCellHeightForTesting()
            val time = SystemClock.uptimeMillis()
            event(view, MotionEvent.ACTION_DOWN, listOf(w * 0.5f to h * 1.5f), time)
            event(view, MotionEvent.ACTION_MOVE, listOf(w * 0.5f to 1f), time + 16)
            clock.runCurrent()
            clock.advanceTimeBy(4 * TerminalSelectionAutoScroll.IntervalMs + 1)
            clock.runCurrent()
            assertEquals("pending history request was repeated before acknowledgement", 1, requests)
            repeat(5) { frame ->
                offset = (offset + pendingRows).coerceAtMost(history.size)
                pendingRows = 0
                update(view, buildScrollbackSnapshot(live, history, offset), offset, frame + 2L)
                clock.runCurrent()
                clock.advanceTimeBy(4 * TerminalSelectionAutoScroll.IntervalMs + 1)
                clock.runCurrent()
            }
            assertEquals(5, offset)
            val copied = view.selectedText()!!
            assertTrue(copied.startsWith("older0\n"))
            assertTrue(copied.contains("row00"))
            val stoppedRequests = requests
            clock.advanceTimeBy(10_000)
            assertEquals("exhausted history kept generating requests", stoppedRequests, requests)
            event(view, MotionEvent.ACTION_UP, listOf(w * 0.5f to 1f), time + 32)
            assertEquals(copied, view.selectedText())
        }
    }

    @Test
    fun normalPanContinuesAcrossLiveGeometryUpdates() {
        InstrumentationRegistry.getInstrumentation().runOnMainSync {
            val view = view(snapshot())
            view.setSelectionMode(false)
            val time = SystemClock.uptimeMillis()
            event(view, MotionEvent.ACTION_DOWN, listOf(190f to 140f), time)
            event(view, MotionEvent.ACTION_MOVE, listOf(160f to 100f), time + 16)
            assertTrue(view.getCameraOffsetYForTesting() > 0f)
            update(view, snapshot(35), 0, 2)
            val before = view.getCameraOffsetYForTesting()
            event(view, MotionEvent.ACTION_MOVE, listOf(130f to 60f), time + 32)
            assertTrue("selection lifecycle reset interrupted normal panning", view.getCameraOffsetYForTesting() > before)
            event(view, MotionEvent.ACTION_UP, listOf(130f to 60f), time + 48)
        }
    }

}
