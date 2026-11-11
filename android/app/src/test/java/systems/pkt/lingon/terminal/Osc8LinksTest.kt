package systems.pkt.lingon.terminal

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import systems.pkt.lingon.protocol.Diff
import systems.pkt.lingon.protocol.DiffRow
import systems.pkt.lingon.protocol.ScrollbackRow
import systems.pkt.lingon.protocol.Snapshot

class Osc8LinksTest {
    private val target = "https://example.test/actual?q=1;two"
    private fun row(payload: String) = DiffRow.newBuilder().setRow(0)
        .addAllRunes("label ".map { it.code }).addAllHyperlinks(List(5) { payload } + "").build()
    private fun snapshot(payload: String = "id=a;$target"): TerminalSnapshot = TerminalSnapshot.fromProto(
        Snapshot.newBuilder().setCols(6).setRows(1).addAllRunes("label ".map { it.code })
            .addAllHyperlinks(List(5) { payload } + "").build(),
    )
    @Test fun snapshotLabelUsesTargetNotTextAndKeepsBoundaries() {
        val link = TerminalLinks.findHttpsLinks(snapshot()).single()
        assertEquals(target, link.url)
        assertTrue(link.contains(0, 4))
        assertFalse(link.contains(0, 5))
    }
    @Test fun diffRetargetsAndClearsUnchangedLabel() {
        var snap = snapshot()
        snap = TerminalSnapshot.applyDiff(snap, Diff.newBuilder().setCols(6).setRows(1)
            .addDiffRows(row("id=b;https://other.test")).build())
        assertEquals("https://other.test", TerminalLinks.findHttpsLinks(snap).single().url)
        snap = TerminalSnapshot.applyDiff(snap, Diff.newBuilder().setCols(6).setRows(1)
            .addDiffRows(DiffRow.newBuilder().setRow(0).addAllRunes("label ".map { it.code })).build())
        assertTrue(TerminalLinks.findHttpsLinks(snap).isEmpty())
    }
    @Test fun historyAndLiveBothRetainTargets() {
        val history = ScrollbackRow.newBuilder().addAllRunes("older ".map { it.code })
            .addAllHyperlinks(List(5) { ";https://history.test" } + "").build()
        val view = buildScrollbackSnapshot(snapshot(), listOf(history), 1)
        assertEquals(listOf("https://history.test", target), TerminalLinks.findHttpsLinks(view).map { it.url })
    }
    @Test fun newDiffAllocatesTargetsAndGraphemeExpansionPreservesThem() {
        var snap = TerminalSnapshot.applyDiff(null, Diff.newBuilder().setCols(6).setRows(1)
            .addDiffRows(row("id=b;$target")).build())
        val other = DiffRow.newBuilder().setRow(0).addAllRunes("label ".map { it.code })
            .addAllGraphemes(List(6) { "" }).addAllHyperlinks(List(5) { "id=b;$target" } + "").build()
        snap = TerminalSnapshot.applyDiff(snap, Diff.newBuilder().setCols(6).setRows(1).addDiffRows(other).build())
        assertEquals(target, TerminalLinks.findHttpsLinks(snap).single().url)
        val resized = TerminalSnapshot.applyDiff(snap, Diff.newBuilder().setCols(3).setRows(2).build())
        assertTrue(TerminalLinks.findHttpsLinks(resized).isEmpty())
    }
    @Test fun wrappedWideLabelsAndHiddenCellsHaveExactHitAreas() {
        val snap = TerminalSnapshot.fromProto(Snapshot.newBuilder().setCols(4).setRows(2)
            .addAllRunes(listOf('日'.code, 0, '本'.code, 0, 'x'.code, 'y'.code, 'z'.code, 32))
            .addAllModes(listOf(0, 0, 0, 0, 0, MODE_HIDDEN, 0, 0))
            .addAllHyperlinks(List(7) { "id=x;$target" } + "").build())
        val links = TerminalLinks.findHttpsLinks(snap)
        assertEquals(listOf(target, target, target), links.map { it.url })
        for (col in 0..3) assertTrue(links.any { it.contains(0, col) })
        assertFalse(links.any { it.contains(1, 1) })
        assertTrue(links.any { it.contains(1, 2) })
        assertFalse(links.any { it.contains(1, 3) })
    }
    @Test fun explicitHttpAndUnicodeTargetsPreserveTheirUri() {
        for (uri in listOf("http://example.test/path", "https://example.test/日本?q=1;two")) {
            assertEquals(uri, TerminalLinks.findHttpsLinks(snapshot("id=x;$uri")).single().url)
        }
    }
    @Test fun graphemeExpansionPreservesOtherRowsTargets() {
        val snap = TerminalSnapshot.fromProto(Snapshot.newBuilder().setCols(6).setRows(2)
            .addAllRunes("label other ".map { it.code })
            .addAllHyperlinks(List(5) { "id=x;$target" } + List(7) { "" }).build())
        val updated = TerminalSnapshot.applyDiff(snap, Diff.newBuilder().setCols(6).setRows(2)
            .addDiffRows(DiffRow.newBuilder().setRow(1).addAllRunes("other ".map { it.code })
                .addAllGraphemes(List(6) { "" })).build())
        assertEquals(target, TerminalLinks.findHttpsLinks(updated).single().url)
    }
    @Test fun ordinaryUnderlineDoesNotInventTarget() {
        val plain = TerminalSnapshot.fromProto(Snapshot.newBuilder().setCols(6).setRows(1)
            .addAllRunes("label ".map { it.code }).addAllModes(List(6) { 8 }).build())
        assertTrue(TerminalLinks.findHttpsLinks(plain).isEmpty())
    }
    @Test fun explicitTargetWinsOverUrlLabelAndInvalidMetadataDoesNotFallback() {
        for (payload in listOf("id=a;https://real.test", ";javascript:alert(1)", ";https://bad.test\u001b", ";", "broken")) {
            val label = "https://label.test"
            val snap = TerminalSnapshot.fromProto(Snapshot.newBuilder().setCols(label.length).setRows(1)
                .addAllRunes(label.map { it.code }).addAllHyperlinks(List(label.length) { payload }).build())
            assertEquals(if (payload.startsWith("id=a;")) listOf("https://real.test") else emptyList<String>(),
                TerminalLinks.findHttpsLinks(snap).map { it.url })
        }
    }
}
