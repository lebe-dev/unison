package app.unison.updater

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** Tests of the changelog reader. */
internal class UpdateMarkdownTest {
    @Test
    fun `headings carry their level`() {
        val blocks = UpdateMarkdown.parse("# Release\n\n### Notes")
        assertEquals(2, blocks.size)
        assertEquals(MarkdownBlock.Heading(1, listOf(MarkdownInline.Plain("Release"))), blocks[0])
        assertEquals(3, (blocks[1] as MarkdownBlock.Heading).level)
    }

    @Test
    fun `plain lines form one paragraph`() {
        val blocks = UpdateMarkdown.parse("first line\nsecond line\n\nanother paragraph")
        assertEquals(2, blocks.size)
        assertEquals(MarkdownBlock.Paragraph(listOf(MarkdownInline.Plain("first line second line"))), blocks[0])
        assertEquals(MarkdownBlock.Paragraph(listOf(MarkdownInline.Plain("another paragraph"))), blocks[1])
    }

    @Test
    fun `bullets are blocks of their own`() {
        val blocks = UpdateMarkdown.parse("- one\n- two\n+ three\n* four")
        assertEquals(4, blocks.size)
        assertTrue(blocks.all { it is MarkdownBlock.Bullet })
        assertEquals(MarkdownBlock.Bullet(listOf(MarkdownInline.Plain("one"))), blocks[0])
    }

    @Test
    fun `a fenced block keeps its lines`() {
        val blocks = UpdateMarkdown.parse("before\n\n```\nline one\nline two\n```\n\nafter")
        assertEquals(3, blocks.size)
        assertEquals(MarkdownBlock.CodeBlock("line one\nline two"), blocks[1])
    }

    @Test
    fun `bold italic and code are inline styles`() {
        val blocks = UpdateMarkdown.parse("a **bold** and *italic* and `code` end")
        val paragraph = blocks.single() as MarkdownBlock.Paragraph
        assertEquals(
            listOf(
                MarkdownInline.Plain("a "),
                MarkdownInline.Bold(listOf(MarkdownInline.Plain("bold"))),
                MarkdownInline.Plain(" and "),
                MarkdownInline.Italic(listOf(MarkdownInline.Plain("italic"))),
                MarkdownInline.Plain(" and "),
                MarkdownInline.Code("code"),
                MarkdownInline.Plain(" end"),
            ),
            paragraph.content,
        )
    }

    @Test
    fun `an https link is a link`() {
        val paragraph =
            UpdateMarkdown
                .parse("see [the release](https://github.com/lebe-dev/unison) now")
                .single() as MarkdownBlock.Paragraph
        assertTrue(paragraph.content.any { it is MarkdownInline.Link && it.url.startsWith("https://") })
    }

    @Test
    fun `an unsafe link scheme is plain text`() {
        val paragraph =
            UpdateMarkdown
                .parse("see [the release](javascript:alert(1)) now")
                .single() as MarkdownBlock.Paragraph
        assertFalse(paragraph.content.any { it is MarkdownInline.Link })
        assertTrue(paragraph.content.any { it is MarkdownInline.Plain && it.text.contains("the release") })
    }

    @Test
    fun `a data link is plain text`() {
        val paragraph =
            UpdateMarkdown
                .parse("[open](data:text/html;base64,PHNjcmlwdD4=)")
                .single() as MarkdownBlock.Paragraph
        assertFalse(paragraph.content.any { it is MarkdownInline.Link })
    }

    @Test
    fun `raw html stays text`() {
        val paragraph =
            UpdateMarkdown
                .parse("<script>alert(1)</script>")
                .single() as MarkdownBlock.Paragraph
        assertEquals(listOf(MarkdownInline.Plain("<script>alert(1)</script>")), paragraph.content)
    }

    @Test
    fun `snake case underscores stay plain text`() {
        val paragraph =
            UpdateMarkdown
                .parse("renamed operating_mode_changed to _italic_ text")
                .single() as MarkdownBlock.Paragraph
        assertEquals(
            listOf(
                MarkdownInline.Plain("renamed operating_mode_changed to "),
                MarkdownInline.Italic(listOf(MarkdownInline.Plain("italic"))),
                MarkdownInline.Plain(" text"),
            ),
            paragraph.content,
        )
    }

    @Test
    fun `brackets before a link do not swallow text into the link`() {
        val paragraph =
            UpdateMarkdown
                .parse("[note] see [docs](https://example.com) now")
                .single() as MarkdownBlock.Paragraph
        assertEquals(
            listOf(
                MarkdownInline.Plain("[note] see "),
                MarkdownInline.Link(listOf(MarkdownInline.Plain("docs")), "https://example.com"),
                MarkdownInline.Plain(" now"),
            ),
            paragraph.content,
        )
    }

    @Test
    fun `an empty text has no blocks`() {
        assertTrue(UpdateMarkdown.parse("").isEmpty())
        assertTrue(UpdateMarkdown.parse("   \n\n").isEmpty())
    }
}
