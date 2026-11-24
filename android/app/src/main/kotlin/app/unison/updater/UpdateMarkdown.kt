@file:Suppress("CyclomaticComplexMethod", "LoopWithTooManyJumpStatements")

package app.unison.updater

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp

/** A run of text inside a block, with the markup it carries. */
sealed interface MarkdownInline {
    data class Plain(
        val text: String,
    ) : MarkdownInline

    data class Bold(
        val children: List<MarkdownInline>,
    ) : MarkdownInline

    data class Italic(
        val children: List<MarkdownInline>,
    ) : MarkdownInline

    data class Code(
        val text: String,
    ) : MarkdownInline

    data class Link(
        val children: List<MarkdownInline>,
        val url: String,
    ) : MarkdownInline
}

/** One block of the release changelog. */
sealed interface MarkdownBlock {
    data class Heading(
        val level: Int,
        val content: List<MarkdownInline>,
    ) : MarkdownBlock

    data class Paragraph(
        val content: List<MarkdownInline>,
    ) : MarkdownBlock

    data class Bullet(
        val content: List<MarkdownInline>,
    ) : MarkdownBlock

    data class CodeBlock(
        val text: String,
    ) : MarkdownBlock
}

/**
 * A small Markdown reader for the changelog of a release. It understands the
 * headings, paragraphs, bullet lists, fenced code and inline styles GitHub uses
 * in release notes, and it never renders raw HTML.
 */
object UpdateMarkdown {
    /** Reads the release notes into blocks the screen draws with Compose. */
    fun parse(markdown: String): List<MarkdownBlock> {
        val blocks = mutableListOf<MarkdownBlock>()
        val paragraph = StringBuilder()
        val code = StringBuilder()
        var inCode = false

        fun flushParagraph() {
            if (paragraph.isNotBlank()) {
                blocks += MarkdownBlock.Paragraph(inline(paragraph.toString().trim()))
            }
            paragraph.clear()
        }

        fun flushCode() {
            blocks += MarkdownBlock.CodeBlock(code.toString().trimEnd('\n'))
            code.clear()
        }

        markdown.replace("\r\n", "\n").lineSequence().forEach { raw ->
            val line = raw.trimEnd()
            if (line.startsWith(CODE_FENCE)) {
                if (inCode) {
                    flushCode()
                } else {
                    flushParagraph()
                }
                inCode = !inCode
                return@forEach
            }
            if (inCode) {
                code.append(line).append('\n')
                return@forEach
            }
            when {
                line.isBlank() -> flushParagraph()
                headingLevel(line) > 0 -> {
                    flushParagraph()
                    val level = headingLevel(line)
                    blocks += MarkdownBlock.Heading(level, inline(line.drop(level + 1).trim()))
                }
                bulletText(line) != null -> {
                    flushParagraph()
                    blocks += MarkdownBlock.Bullet(inline(bulletText(line).orEmpty()))
                }
                else -> {
                    if (paragraph.isNotEmpty()) {
                        paragraph.append(' ')
                    }
                    paragraph.append(line.trim())
                }
            }
        }
        if (inCode) {
            flushCode()
        }
        flushParagraph()
        return blocks
    }

    /** Reads the inline markup of one line. */
    internal fun inline(text: String): List<MarkdownInline> {
        val result = mutableListOf<MarkdownInline>()
        val plain = StringBuilder()
        var index = 0

        fun flushPlain() {
            if (plain.isNotEmpty()) {
                result += MarkdownInline.Plain(plain.toString())
                plain.clear()
            }
        }

        while (index < text.length) {
            val rest = text.substring(index)
            when {
                rest.startsWith("**") -> {
                    val end = text.indexOf("**", index + HOLD)
                    if (end <= index + HOLD) {
                        plain.append(text[index])
                        index++
                        continue
                    }
                    flushPlain()
                    result += MarkdownInline.Bold(inline(text.substring(index + HOLD, end)))
                    index = end + HOLD
                }
                text[index] == '`' -> {
                    val end = text.indexOf('`', index + 1)
                    if (end < 0) {
                        plain.append(text[index])
                        index++
                        continue
                    }
                    flushPlain()
                    result += MarkdownInline.Code(text.substring(index + 1, end))
                    index = end + 1
                }
                text[index] == '[' -> {
                    val next = link(text, index)
                    if (next == null) {
                        plain.append(text[index])
                        index++
                        continue
                    }
                    flushPlain()
                    if (isSafeUrl(next.url)) {
                        result += MarkdownInline.Link(inline(next.label), next.url)
                    } else {
                        result += MarkdownInline.Plain(next.label)
                    }
                    index = next.end
                }
                text[index] == '*' || text[index] == '_' -> {
                    val marker = text[index]
                    val end = text.indexOf(marker, index + 1)
                    if (end <= index + 1 || rest.startsWith("**")) {
                        plain.append(text[index])
                        index++
                        continue
                    }
                    flushPlain()
                    result += MarkdownInline.Italic(inline(text.substring(index + 1, end)))
                    index = end + 1
                }
                else -> {
                    plain.append(text[index])
                    index++
                }
            }
        }
        flushPlain()
        return result
    }

    /** One link of the inline text. */
    private data class ParsedLink(
        val label: String,
        val url: String,
        val end: Int,
    )

    /** Reads a `[label](url)` link that starts at [start]. */
    private fun link(
        text: String,
        start: Int,
    ): ParsedLink? {
        val close = text.indexOf("](", start + 1)
        if (close < 0) {
            return null
        }
        val end = text.indexOf(')', close + 2)
        if (end < 0) {
            return null
        }
        val label = text.substring(start + 1, close)
        val url = text.substring(close + 2, end).trim()
        if (label.isEmpty() || url.isEmpty()) {
            return null
        }
        return ParsedLink(label, url, end + 1)
    }

    /** True for the link schemes the changelog may carry. */
    private fun isSafeUrl(url: String): Boolean = url.startsWith("https://") || url.startsWith("http://")

    /** Level of a heading line, 0 when the line is not a heading. */
    private fun headingLevel(line: String): Int {
        val hashes = line.takeWhile { it == '#' }.length
        if (hashes !in 1..6 || line.length <= hashes || line[hashes] != ' ') {
            return 0
        }
        return hashes
    }

    /** Text of a bullet line, null when the line is not a bullet. */
    private fun bulletText(line: String): String? {
        val trimmed = line.trimStart()
        if (trimmed.length > 2 && trimmed[0] in BULLETS && trimmed[1] == ' ') {
            return trimmed.substring(2)
        }
        return null
    }

    private const val CODE_FENCE = "```"
    private const val HOLD = 2
    private val BULLETS = setOf('-', '*', '+')
}

/** Draws the release notes with the typography of the app, without a web view. */
@Composable
fun UpdateMarkdownText(
    markdown: String,
    modifier: Modifier = Modifier,
) {
    val blocks = remember(markdown) { UpdateMarkdown.parse(markdown) }
    Column(modifier = modifier, verticalArrangement = Arrangement.spacedBy(8.dp)) {
        blocks.forEach { block ->
            when (block) {
                is MarkdownBlock.Heading ->
                    Text(
                        text = inlineText(block.content),
                        style = if (block.level <= 2) MaterialTheme.typography.titleMedium else MaterialTheme.typography.titleSmall,
                    )
                is MarkdownBlock.Paragraph ->
                    SelectionContainer {
                        Text(text = inlineText(block.content), style = MaterialTheme.typography.bodyMedium)
                    }
                is MarkdownBlock.Bullet ->
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        Text(text = "•", style = MaterialTheme.typography.bodyMedium)
                        SelectionContainer {
                            Text(
                                text = inlineText(block.content),
                                style = MaterialTheme.typography.bodyMedium,
                            )
                        }
                    }
                is MarkdownBlock.CodeBlock ->
                    SelectionContainer {
                        Text(
                            text = block.text,
                            style = MaterialTheme.typography.bodySmall,
                            fontFamily = FontFamily.Monospace,
                            modifier =
                                Modifier
                                    .fillMaxWidth()
                                    .background(
                                        MaterialTheme.colorScheme.surfaceContainerHighest,
                                        RoundedCornerShape(8.dp),
                                    ).padding(8.dp),
                        )
                    }
            }
        }
    }
}

/** Builds the styled text of one block. */
@Composable
private fun inlineText(content: List<MarkdownInline>): AnnotatedString {
    val link = MaterialTheme.colorScheme.primary
    val code = MaterialTheme.colorScheme.surfaceContainerHighest
    return buildAnnotatedString { appendInline(content, link, code) }
}

/** Appends [content] to the current text with its styles. */
private fun androidx.compose.ui.text.AnnotatedString.Builder.appendInline(
    content: List<MarkdownInline>,
    link: Color,
    code: Color,
) {
    content.forEach { item ->
        when (item) {
            is MarkdownInline.Plain -> append(item.text)
            is MarkdownInline.Bold ->
                withStyle(SpanStyle(fontWeight = FontWeight.SemiBold)) {
                    appendInline(item.children, link, code)
                }
            is MarkdownInline.Italic ->
                withStyle(SpanStyle(fontStyle = FontStyle.Italic)) {
                    appendInline(item.children, link, code)
                }
            is MarkdownInline.Code ->
                withStyle(SpanStyle(fontFamily = FontFamily.Monospace, background = code)) {
                    append(item.text)
                }
            is MarkdownInline.Link ->
                withStyle(SpanStyle(color = link, textDecoration = TextDecoration.Underline)) {
                    appendInline(item.children, link, code)
                }
        }
    }
}
