package systems.pkt.lingon.terminal

import java.net.URI

data class TerminalLinkRange(
    val url: String,
    val startRow: Int,
    val startCol: Int,
    val endRow: Int,
    val endColExclusive: Int,
) {
    fun contains(row: Int, col: Int): Boolean {
        if (row < startRow || row > endRow) return false
        if (startRow == endRow) return col >= startCol && col < endColExclusive
        if (row == startRow) return col >= startCol
        if (row == endRow) return col < endColExclusive
        return true
    }
}

object TerminalLinks {
    private const val httpsScheme = "https://"
    private val trailingPunctuation = setOf('.', ',', ';', ':', '!', '?')
    private val trailingFormatDelimiters = setOf('>', '"', '\'', '`')

    fun findHttpsLinks(snapshot: TerminalSnapshot): List<TerminalLinkRange> {
        if (snapshot.cols <= 0 || snapshot.rows <= 0) return emptyList()
        val text = StringBuilder()
        val cells = ArrayList<CellRef?>()

        for (row in 0 until snapshot.rows) {
            if (row > 0) {
                text.append('\n')
                cells.add(null)
            }
            for (col in 0 until snapshot.cols) {
                appendCellText(snapshot, row, col, text, cells)
            }
        }

        return explicitLinks(snapshot) + findHttpsLinks(text.toString()).mapNotNull { match ->
            val start = firstMappedCell(cells, match.start) ?: return@mapNotNull null
            val end = lastMappedCell(cells, match.endExclusive - 1) ?: return@mapNotNull null
            TerminalLinkRange(
                url = match.url,
                startRow = start.row,
                startCol = start.col,
                endRow = end.row,
                endColExclusive = end.endColExclusive,
            )
        }
    }

    internal fun refreshForUpdate(
        snapshot: TerminalSnapshot?,
        snapshotChanged: Boolean,
        frameSeqChanged: Boolean,
        finder: (TerminalSnapshot) -> List<TerminalLinkRange> = ::findHttpsLinks,
    ): List<TerminalLinkRange>? {
        if (!snapshotChanged && !frameSeqChanged) return null
        return snapshot?.let(finder) ?: emptyList()
    }

    fun findHttpsLinks(text: String): List<TextLinkRange> {
        val links = ArrayList<TextLinkRange>()
        var searchStart = 0
        while (searchStart < text.length) {
            val schemeStart = text.indexOf(httpsScheme, searchStart)
            if (schemeStart < 0) break
            var end = schemeStart + httpsScheme.length
            while (end < text.length && !isUrlDelimiter(text[end])) {
                end += 1
            }
            end = trimTrailingPunctuation(text, schemeStart, end)
            if (end > schemeStart + httpsScheme.length) {
                links.add(TextLinkRange(text.substring(schemeStart, end), schemeStart, end))
            }
            searchStart = maxOf(end, schemeStart + httpsScheme.length)
        }
        return links
    }

    // Keep explicit application targets separate from visible-URL inference. Even
    // rejected metadata suppresses inference, so a URL label cannot open a different target.
    private fun explicitLinks(snapshot: TerminalSnapshot): List<TerminalLinkRange> {
        val payloads = snapshot.hyperlinks ?: return emptyList()
        val links = ArrayList<TerminalLinkRange>()
        for (row in 0 until snapshot.rows) {
            var col = 0
            while (col < snapshot.cols) {
                val start = col
                val payload = payloads.getOrNull(row * snapshot.cols + col).orEmpty()
                val target = targetUri(payload)
                if (target == null || snapshot.modes[row * snapshot.cols + col] and MODE_HIDDEN != 0) {
                    col++
                    continue
                }
                do { col++ } while (col < snapshot.cols &&
                    payloads.getOrNull(row * snapshot.cols + col) == payload &&
                    snapshot.modes[row * snapshot.cols + col] and MODE_HIDDEN == 0)
                links.add(TerminalLinkRange(target, row, start, row, col))
            }
        }
        return links
    }

    private fun targetUri(payload: String): String? {
        val separator = payload.indexOf(';')
        if (separator < 0 || payload.any { it.isISOControl() }) return null
        val target = payload.substring(separator + 1)
        val uri = runCatching { URI(target) }.getOrNull() ?: return null
        if (!uri.scheme.equals("https", ignoreCase = true) && !uri.scheme.equals("http", ignoreCase = true)) return null
        if (uri.rawAuthority.isNullOrEmpty()) return null
        return target
    }

    private fun appendCellText(
        snapshot: TerminalSnapshot,
        row: Int,
        col: Int,
        text: StringBuilder,
        cells: MutableList<CellRef?>,
    ) {
        val idx = row * snapshot.cols + col
        if (snapshot.runes.getOrElse(idx) { 32 } == 0 &&
            snapshot.graphemes?.getOrElse(idx) { "" }.isNullOrEmpty() &&
            snapshot.modes.getOrElse(idx) { 0 } and MODE_HIDDEN == 0
        ) {
            // A continuation occupies a screen cell but contributes no text.
            // Extend the preceding character's hit area without adding a delimiter.
            cells.lastOrNull()?.let { previous ->
                if (previous.row == row && previous.col == col - 1) {
                    cells[cells.lastIndex] = previous.copy(endColExclusive = col + 1)
                }
            }
            return
        }
        val cellText = when {
            !snapshot.hyperlinks?.getOrNull(idx).isNullOrEmpty() -> " "
            snapshot.modes.getOrElse(idx) { 0 } and MODE_HIDDEN != 0 -> " "
            snapshot.graphemes?.getOrElse(idx) { "" }?.isNotEmpty() == true -> snapshot.graphemes[idx]
            else -> {
                val rune = snapshot.runes.getOrElse(idx) { 32 }
                if (rune == 0) " " else String(Character.toChars(rune))
            }
        }
        text.append(cellText)
        repeat(cellText.length) {
            cells.add(CellRef(row = row, col = col))
        }
    }

    private fun firstMappedCell(cells: List<CellRef?>, start: Int): CellRef? {
        var index = start
        while (index < cells.size) {
            cells[index]?.let { return it }
            index += 1
        }
        return null
    }

    private fun lastMappedCell(cells: List<CellRef?>, start: Int): CellRef? {
        var index = start
        while (index >= 0) {
            cells[index]?.let { return it }
            index -= 1
        }
        return null
    }

    private fun isUrlDelimiter(char: Char): Boolean {
        return char.isWhitespace() || char.isISOControl()
    }

    private fun trimTrailingPunctuation(text: String, start: Int, exclusiveEnd: Int): Int {
        var end = exclusiveEnd
        while (end > start) {
            val char = text[end - 1]
            when {
                char in trailingPunctuation -> end -= 1
                char in trailingFormatDelimiters -> end -= 1
                char == ')' && hasUnmatchedClosing(text, start, end, open = '(', close = ')') -> end -= 1
                char == ']' && hasUnmatchedClosing(text, start, end, open = '[', close = ']') -> end -= 1
                char == '}' && hasUnmatchedClosing(text, start, end, open = '{', close = '}') -> end -= 1
                else -> return end
            }
        }
        return end
    }

    private fun hasUnmatchedClosing(text: String, start: Int, exclusiveEnd: Int, open: Char, close: Char): Boolean {
        var depth = 0
        for (index in start until exclusiveEnd) {
            when (text[index]) {
                open -> depth += 1
                close -> depth -= 1
            }
        }
        return depth < 0
    }

    private data class CellRef(val row: Int, val col: Int, val endColExclusive: Int = col + 1)
}

data class TextLinkRange(
    val url: String,
    val start: Int,
    val endExclusive: Int,
)
