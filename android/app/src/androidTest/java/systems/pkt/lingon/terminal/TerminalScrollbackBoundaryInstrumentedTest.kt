package systems.pkt.lingon.terminal

import android.graphics.Bitmap
import android.graphics.Canvas
import android.os.SystemClock
import android.view.MotionEvent
import android.view.View
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.toArgb
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import systems.pkt.lingon.protocol.ScrollbackRow
import kotlin.math.floor

@RunWith(AndroidJUnit4::class)
class TerminalScrollbackBoundaryInstrumentedTest {
    @Test
    fun boundaryOccupiesOneHistoryPixelAtEveryZoomAndHorizontalPan() = onMain {
        for ((fg, bg) in listOf(Color.White to Color.Black, Color.Black to Color.White)) {
            for (zoom in listOf(1f, 1.5f, 2f, 4f)) {
                val view = grid(zoom, fg, bg)
                for (boundary in listOf(80f, 80.25f, 80.75f)) {
                    positionBoundary(view, boundary, x = 20f)
                    val before = view.captureViewportState()
                    assertBoundary(view, bg.toArgb(), if (bg == Color.Black) 0xff666666.toInt() else 0xff999999.toInt())
                    assertEquals("painting must not change viewport geometry", before, view.captureViewportState())
                }
            }
        }
    }

    @Test
    fun boundaryIsAbsentWithoutHistoryOrWhenOutsideViewport() = onMain {
        val live = grid(historyRows = 0)
        assertPixels(live, boundaryRow = null, background = android.graphics.Color.BLACK)
        val view = grid(zoom = 3f)
        for (boundary in listOf(-1f, 0f, 0.75f, view.height + 0.25f, view.height + 1f)) {
            positionBoundary(view, boundary)
            assertPixels(view, boundaryRow = null, background = android.graphics.Color.BLACK)
        }
        positionBoundary(view, view.height.toFloat())
        assertBoundary(view, android.graphics.Color.BLACK, 0xff666666.toInt())
    }

    @Test
    fun boundaryMovesWithHistoryAndDisappearsOnReturnToLive() = onMain {
        val view = grid()
        positionBoundary(view, 120f)
        assertBoundary(view, android.graphics.Color.BLACK, 0xff666666.toInt())
        positionBoundary(view, 60f)
        assertBoundary(view, android.graphics.Color.BLACK, 0xff666666.toInt())
        update(view, zoom = 1f, historyRows = 4, frameSeq = 2)
        positionBoundary(view, 40f, historyRows = 4)
        assertBoundary(view, android.graphics.Color.BLACK, 0xff666666.toInt(), historyRows = 4)
        update(view, zoom = 1f, historyRows = 0, frameSeq = 3)
        assertPixels(view, boundaryRow = null, background = android.graphics.Color.BLACK)
    }

    @Test
    fun boundaryRemainsVisibleOverSelectionWithoutChangingCopiedText() = onMain {
        val view = grid()
        positionBoundary(view, 100f)
        view.setSelectionMode(true)
        val y = 100f - view.getScaledCellHeightForTesting() * 0.5f
        val time = SystemClock.uptimeMillis()
        for ((action, x) in listOf(MotionEvent.ACTION_DOWN to 20f, MotionEvent.ACTION_MOVE to 200f, MotionEvent.ACTION_UP to 200f)) {
            val event = MotionEvent.obtain(time, time, action, x, y, 0)
            try { view.dispatchTouchEvent(event) } finally { event.recycle() }
        }
        val selection = view.selectedText()
        val before = view.captureViewportState()
        val row = floor(8 * view.getScaledCellHeightForTesting() - view.getCameraOffsetYForTesting()).toInt() - 1
        draw(view) { bitmap ->
            assertEquals("selection is present above the boundary", android.graphics.Color.WHITE, bitmap.getPixel(100, row - 1))
            for (x in 0 until view.width) {
                assertEquals("line stays visible over selected cells", 0xff666666.toInt(), bitmap.getPixel(x, row))
            }
            assertEquals("line does not spill into live cells", android.graphics.Color.BLACK, bitmap.getPixel(100, row + 1))
        }
        assertEquals(selection, view.selectedText())
        assertEquals(before, view.captureViewportState())
    }

    private fun grid(zoom: Float = 1f, fg: Color = Color.White, bg: Color = Color.Black, historyRows: Int = 8): TerminalGridView {
        val view = TerminalGridView(InstrumentationRegistry.getInstrumentation().targetContext)
        view.measure(View.MeasureSpec.makeMeasureSpec(320, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(240, View.MeasureSpec.EXACTLY))
        view.layout(0, 0, 320, 240)
        update(view, zoom, historyRows, 1, fg, bg)
        return view
    }

    private fun update(view: TerminalGridView, zoom: Float, historyRows: Int, frameSeq: Long, fg: Color = Color.White, bg: Color = Color.Black) {
        val live = TerminalSnapshot(cols = 80, rows = 40, runes = IntArray(80 * 40) { ' '.code },
            modes = IntArray(80 * 40), fg = IntArray(80 * 40), bg = IntArray(80 * 40),
            graphemes = null, cursorX = 0, cursorY = 0, cursorVisible = false, mode = 0, title = "")
        val history = List(historyRows) { ScrollbackRow.newBuilder().addAllRunes(List(80) { ' '.code }).build() }
        view.update(snapshot = buildScrollbackSnapshot(live, history, historyRows), fontSizeSp = 14, minFontSizeSp = 8,
            palette = TerminalPalette(defaultFg = fg, defaultBg = bg), frameSeq = frameSeq,
            hostCols = 80, hostRows = 40, fitToViewWidth = false, zoomFactor = zoom, panResetNonce = 0,
            scrollbackOffsetRows = historyRows, imeVisible = false, isLoading = false)
    }

    private fun positionBoundary(view: TerminalGridView, boundary: Float, x: Float = 0f, historyRows: Int = 8) {
        view.restoreViewportState(view.captureViewportState().copy(cameraOffsetXPx = x, preferredCameraOffsetXPx = x,
            cameraOffsetYPx = historyRows * view.getScaledCellHeightForTesting() - boundary, mode = TerminalViewportMode.Manual))
        val actual = historyRows * view.getScaledCellHeightForTesting() - view.getCameraOffsetYForTesting()
        assertEquals("fixture must reach requested boundary", boundary, actual, 0.001f)
    }

    private fun assertBoundary(view: TerminalGridView, background: Int, color: Int, historyRows: Int = 8) {
        val y = historyRows * view.getScaledCellHeightForTesting() - view.getCameraOffsetYForTesting()
        assertTrue(y >= 1f && y <= view.height)
        assertPixels(view, floor(y).toInt() - 1, background, color)
    }

    private fun assertPixels(view: TerminalGridView, boundaryRow: Int?, background: Int, color: Int = 0) = draw(view) { bitmap ->
        for (y in 0 until view.height) {
            for (x in 0 until view.width) {
                assertEquals("pixel ($x,$y); boundary row $boundaryRow", if (y == boundaryRow) color else background, bitmap.getPixel(x, y))
            }
        }
    }

    private fun draw(view: TerminalGridView, check: (Bitmap) -> Unit) {
        val bitmap = Bitmap.createBitmap(view.width, view.height, Bitmap.Config.ARGB_8888)
        try { view.draw(Canvas(bitmap)); check(bitmap) } finally { bitmap.recycle() }
    }

    private fun onMain(block: () -> Unit) = InstrumentationRegistry.getInstrumentation().runOnMainSync(block)
}
