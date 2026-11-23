package app.unison.diagnostics

import org.junit.Assert.assertEquals
import org.junit.Test

class PathRedactionTest {
    @Test
    fun unixPathIsReplaced() {
        assertEquals(
            "cannot open [path]: no such file",
            redactPaths("cannot open /Users/eugene/Documents/notes.txt: no such file"),
        )
    }

    @Test
    fun androidDataPathIsReplaced() {
        assertEquals(
            "cannot write [path]",
            redactPaths("cannot write /data/user/0/app.unison/files/notes.db"),
        )
    }

    @Test
    fun windowsPathIsReplaced() {
        assertEquals(
            "cannot write [path]",
            redactPaths("""cannot write C:\Users\eugene\AppData\Local\notes.db"""),
        )
    }

    @Test
    fun pathWithSpacesIsReplaced() {
        assertEquals(
            "cannot write [path] now",
            redactPaths("""cannot write C:\Users\John Doe\Documents\notes.txt now"""),
        )
    }

    @Test
    fun urlAfterPathIsKept() {
        assertEquals(
            "see [path] and https://example.com/Users/eugene",
            redactPaths("see /Users/name/Documents and https://example.com/Users/eugene"),
        )
    }

    @Test
    fun urlsAndPlainWordsAreKept() {
        val text = "See https://example.com/Users/eugene and https://example.com/home/user, then open /home."
        assertEquals(text, redactPaths(text))
    }
}
