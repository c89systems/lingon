package systems.pkt.lingon.terminal

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestCoroutineScheduler
import org.junit.Assert.assertEquals
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class TerminalSelectionAutoScrollTest {
    @Test
    fun repeatClockIsExplicitCancellableAndStopsAtBufferBoundary() {
        val clock = TestCoroutineScheduler()
        var steps = 0
        val scroller = TerminalSelectionAutoScroll(StandardTestDispatcher(clock)) { ++steps < 3 }
        scroller.start()
        scroller.start()
        clock.runCurrent()
        clock.advanceTimeBy(59)
        assertEquals(0, steps)
        clock.advanceTimeBy(1)
        clock.runCurrent()
        assertEquals(1, steps)
        scroller.stop()
        clock.advanceTimeBy(1000)
        assertEquals(1, steps)
        scroller.start()
        clock.runCurrent()
        clock.advanceTimeBy(1000)
        assertEquals(3, steps)
        scroller.stop()
    }
    @Test
    fun edgeBandsDoNotOverlapAndSupportDraggingOutsideTheView() {
        assertEquals(-1, TerminalSelectionAutoScroll.edgeDirection(-50f, 100f, 40f))
        assertEquals(0, TerminalSelectionAutoScroll.edgeDirection(50f, 100f, 40f))
        assertEquals(1, TerminalSelectionAutoScroll.edgeDirection(150f, 100f, 40f))
        assertEquals(0, TerminalSelectionAutoScroll.edgeDirection(0f, 0f, 40f))
    }
}
