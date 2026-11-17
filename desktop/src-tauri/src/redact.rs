//! Removal of the personal data that must not leave the device.

/// PATH_PLACEHOLDER marks where an absolute path was removed.
const PATH_PLACEHOLDER: &str = "[path]";

/// UNIX_ROOTS are the roots of the absolute paths that a text may carry.
const UNIX_ROOTS: [&str; 10] = [
    "/Users/",
    "/home/",
    "/root/",
    "/tmp/",
    "/private/",
    "/var/",
    "/opt/",
    "/mnt/",
    "/data/",
    "/storage/",
];

/// paths replaces the absolute paths of a text with a placeholder, so a report
/// or a log never carries the files of the user. A path can contain spaces, so
/// a token with a separator after a path still belongs to it.
pub fn paths(text: &str) -> String {
    let tokens = tokens(text);
    let mut redacted = String::with_capacity(text.len());
    let mut index = 0;
    while index < tokens.len() {
        let (token, space) = tokens[index];
        index += 1;
        let Some((start, end)) = path_range(token) else {
            redacted.push_str(token);
            redacted.push_str(space);
            continue;
        };
        redacted.push_str(&token[..start]);
        redacted.push_str(PATH_PLACEHOLDER);
        let mut suffix = &token[end..];
        redacted.push_str(suffix);
        let mut after = space;
        // A path that ends with punctuation is complete, and a token without a
        // separator is a word after it.
        while suffix.is_empty() && index < tokens.len() && continues_path(tokens[index].0) {
            let (next, next_space) = tokens[index];
            index += 1;
            suffix = closing(next);
            redacted.push_str(suffix);
            after = next_space;
        }
        redacted.push_str(after);
    }
    redacted
}

/// tokens returns the whitespace separated tokens of a text with the
/// whitespace that follows each of them.
fn tokens(text: &str) -> Vec<(&str, &str)> {
    let mut tokens = Vec::new();
    let mut rest = text;
    while !rest.is_empty() {
        let end = rest.find(char::is_whitespace).unwrap_or(rest.len());
        let (token, tail) = rest.split_at(end);
        let space = tail.len() - tail.trim_start_matches(char::is_whitespace).len();
        let (space, tail) = tail.split_at(space);
        tokens.push((token, space));
        rest = tail;
    }
    tokens
}

/// path_range returns the byte range of a path inside a token. The range
/// leaves the punctuation that opens and closes the token out.
fn path_range(token: &str) -> Option<(usize, usize)> {
    let start = token
        .char_indices()
        .find(|(_, c)| !matches!(c, '(' | '[' | '{' | '\'' | '"' | '<'))
        .map_or(0, |(index, _)| index);
    is_path(&token[start..]).then(|| (start, token.len() - closing(token).len()))
}

/// closing returns the punctuation at the end of a token, which stays after a
/// path.
fn closing(token: &str) -> &str {
    let end = token
        .char_indices()
        .rev()
        .find(|(_, c)| !is_closing(*c))
        .map_or(0, |(index, c)| index + c.len_utf8());
    &token[end..]
}

/// is_closing reports whether a character closes a path in a text.
fn is_closing(c: char) -> bool {
    matches!(c, ')' | ']' | '}' | ',' | ';' | ':' | '\'' | '"' | '>')
}

/// continues_path reports whether a token is the next piece of a path with
/// spaces rather than a word or a URL after it.
fn continues_path(token: &str) -> bool {
    (token.contains('/') || token.contains('\\'))
        && !token.starts_with("http://")
        && !token.starts_with("https://")
}

/// is_path reports whether a token starts an absolute path of the computer.
fn is_path(token: &str) -> bool {
    UNIX_ROOTS.iter().any(|root| token.starts_with(root)) || is_windows_path(token)
}

/// is_windows_path reports whether a token starts a path with a drive letter.
fn is_windows_path(token: &str) -> bool {
    let mut chars = token.chars();
    matches!(chars.next(), Some(letter) if letter.is_ascii_alphabetic())
        && chars.next() == Some(':')
        && matches!(chars.next(), Some('\\') | Some('/'))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn covers_a_unix_path() {
        assert_eq!(
            paths("cannot open /tmp/unison/build.log now"),
            "cannot open [path] now"
        );
        assert_eq!(
            paths("the store at /var/lib/unison is busy"),
            "the store at [path] is busy"
        );
    }

    #[test]
    fn covers_a_macos_home() {
        assert_eq!(
            paths("cannot open /Users/name/Library/Application Support/Unison/file.log now"),
            "cannot open [path] now"
        );
        assert_eq!(
            paths("the file (/Users/someone/Documents/report.pdf) is missing"),
            "the file ([path]) is missing"
        );
    }

    #[test]
    fn covers_windows_drives() {
        assert_eq!(
            paths(r"cannot open C:\Users\John Doe\Documents\file.txt now"),
            "cannot open [path] now"
        );
        assert_eq!(
            paths(r"cannot open C:\Users\Someone\AppData\Local\Unison\unison.log"),
            "cannot open [path]"
        );
        assert_eq!(
            paths("cannot open D:/data/transfers/report.pdf"),
            "cannot open [path]"
        );
    }

    #[test]
    fn stops_before_the_words_after_a_path() {
        assert_eq!(
            paths("open /tmp/unison/file.log and read it"),
            "open [path] and read it"
        );
    }

    #[test]
    fn leaves_words_and_urls_alone() {
        let text = "the home page at https://example.com/home/page is open";
        assert_eq!(paths(text), text);
        assert_eq!(
            paths("the daemon did not start"),
            "the daemon did not start"
        );
    }
}
