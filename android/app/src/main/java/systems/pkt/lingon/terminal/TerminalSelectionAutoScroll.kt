package systems.pkt.lingon.terminal

import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

// Owns just the selection repeat clock; geometry and touch state stay in the view.
internal class TerminalSelectionAutoScroll(
    private val dispatcher: CoroutineDispatcher,
    private val step: () -> Boolean,
) {
    private var job: Job? = null

    fun start() {
        if (job?.isActive == true) return
        job = CoroutineScope(dispatcher).launch {
            while (isActive) {
                delay(IntervalMs)
                if (!step()) break
            }
        }
    }

    fun stop() {
        job?.cancel()
        job = null
    }

    companion object {
        const val IntervalMs = 60L

        fun edgeDirection(position: Float, extent: Float, band: Float): Int = when {
            extent <= 0f -> 0
            position < band.coerceAtMost(extent / 4f) -> -1
            position >= extent - band.coerceAtMost(extent / 4f) -> 1
            else -> 0
        }
    }
}
