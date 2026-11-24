//! Semantic version of a release, as GitHub tags and the binary name it.
//!
//! The comparison follows SemVer 2.0.0: build metadata is ignored, a version
//! without a pre-release is newer than one with it, and the identifiers of a
//! pre-release are compared from left to right.

use std::cmp::Ordering;
use std::fmt;

/// One identifier of the pre-release part.
#[derive(Clone, Debug, PartialEq, Eq)]
enum Ident {
    /// A number, compared as an integer so `beta.10` is newer than `beta.2`.
    Numeric(u64),
    /// A word, compared lexicographically.
    Alpha(String),
}

/// Version is a parsed semantic version.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Version {
    major: u64,
    minor: u64,
    patch: u64,
    pre: Vec<Ident>,
}

impl Version {
    /// parse reads a version. A leading `v`, which some tags carry, is
    /// accepted; the build metadata after `+` is ignored.
    pub fn parse(text: &str) -> Result<Self, String> {
        let text = text.trim();
        let text = text.strip_prefix('v').unwrap_or(text);
        let text = text
            .split_once('+')
            .map(|(without_build, _)| without_build)
            .unwrap_or(text);
        let (core, pre) = match text.split_once('-') {
            Some((core, pre)) => (core, Some(pre)),
            None => (text, None),
        };

        let mut parts = core.split('.');
        let major = number(parts.next(), "major")?;
        let minor = number(parts.next(), "minor")?;
        let patch = number(parts.next(), "patch")?;
        if parts.next().is_some() {
            return Err(format!("the version {text} has more than three numbers"));
        }

        let pre = match pre {
            Some(pre) => pre
                .split('.')
                .map(identifier)
                .collect::<Result<Vec<_>, _>>()?,
            None => Vec::new(),
        };
        Ok(Self {
            major,
            minor,
            patch,
            pre,
        })
    }

    /// is_prerelease reports whether the version names a pre-release.
    pub fn is_prerelease(&self) -> bool {
        !self.pre.is_empty()
    }
}

/// number reads one numeric part without a leading zero.
fn number(part: Option<&str>, name: &str) -> Result<u64, String> {
    let part = part.ok_or_else(|| format!("the version has no {name} number"))?;
    if part.is_empty() || !part.bytes().all(|byte| byte.is_ascii_digit()) {
        return Err(format!("the {name} number {part:?} is not a number"));
    }
    if part.len() > 1 && part.starts_with('0') {
        return Err(format!("the {name} number {part:?} has a leading zero"));
    }
    part.parse()
        .map_err(|_| format!("the {name} number {part:?} is too large"))
}

/// identifier reads one identifier of the pre-release part.
fn identifier(part: &str) -> Result<Ident, String> {
    if part.is_empty() {
        return Err("a pre-release identifier is empty".to_string());
    }
    if part.bytes().all(|byte| byte.is_ascii_digit()) {
        if part.len() > 1 && part.starts_with('0') {
            return Err(format!(
                "the pre-release number {part:?} has a leading zero"
            ));
        }
        let value = part
            .parse()
            .map_err(|_| format!("the pre-release number {part:?} is too large"))?;
        return Ok(Ident::Numeric(value));
    }
    if !part
        .bytes()
        .all(|byte| byte.is_ascii_alphanumeric() || byte == b'-')
    {
        return Err(format!(
            "the pre-release identifier {part:?} has a bad character"
        ));
    }
    Ok(Ident::Alpha(part.to_string()))
}

impl Ord for Version {
    fn cmp(&self, other: &Self) -> Ordering {
        self.major
            .cmp(&other.major)
            .then_with(|| self.minor.cmp(&other.minor))
            .then_with(|| self.patch.cmp(&other.patch))
            .then_with(|| match (self.pre.is_empty(), other.pre.is_empty()) {
                // A release is newer than any pre-release of the same numbers.
                (true, true) => Ordering::Equal,
                (true, false) => Ordering::Greater,
                (false, true) => Ordering::Less,
                (false, false) => self.pre.cmp(&other.pre),
            })
    }
}

impl PartialOrd for Version {
    fn partial_cmp(&self, other: &Self) -> Option<Ordering> {
        Some(self.cmp(other))
    }
}

impl Ord for Ident {
    fn cmp(&self, other: &Self) -> Ordering {
        match (self, other) {
            (Ident::Numeric(left), Ident::Numeric(right)) => left.cmp(right),
            // Words are newer than numbers.
            (Ident::Numeric(_), Ident::Alpha(_)) => Ordering::Less,
            (Ident::Alpha(_), Ident::Numeric(_)) => Ordering::Greater,
            (Ident::Alpha(left), Ident::Alpha(right)) => left.cmp(right),
        }
    }
}

impl PartialOrd for Ident {
    fn partial_cmp(&self, other: &Self) -> Option<Ordering> {
        Some(self.cmp(other))
    }
}

impl fmt::Display for Version {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(formatter, "{}.{}.{}", self.major, self.minor, self.patch)?;
        if !self.pre.is_empty() {
            let parts: Vec<String> = self
                .pre
                .iter()
                .map(|ident| match ident {
                    Ident::Numeric(value) => value.to_string(),
                    Ident::Alpha(text) => text.clone(),
                })
                .collect();
            write!(formatter, "-{}", parts.join("."))?;
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// version parses a version of a test.
    fn version(text: &str) -> Version {
        Version::parse(text).unwrap_or_else(|err| panic!("{text}: {err}"))
    }

    #[test]
    fn orders_the_numbers() {
        assert!(version("0.3.0") > version("0.2.9"));
        assert!(version("1.0.0") > version("0.99.99"));
        assert_eq!(version("0.3.0"), version("0.3.0"));
    }

    #[test]
    fn a_release_is_newer_than_its_pre_release() {
        assert!(version("0.3.0") > version("0.3.0-beta.2"));
        assert!(version("0.3.0-rc.1") > version("0.3.0-beta.10"));
    }

    #[test]
    fn numbers_of_a_pre_release_compare_as_integers() {
        assert!(version("0.3.0-beta.10") > version("0.3.0-beta.2"));
        assert!(version("0.3.0-beta.2") > version("0.3.0-beta"));
    }

    #[test]
    fn words_are_newer_than_numbers() {
        assert!(version("0.3.0-alpha") > version("0.3.0-1"));
    }

    #[test]
    fn strips_the_leading_v() {
        assert_eq!(version("v0.3.0"), version("0.3.0"));
    }

    #[test]
    fn ignores_build_metadata() {
        assert_eq!(version("0.3.0+20261124"), version("0.3.0"));
        assert_eq!(version("0.3.0-beta.1+build.5"), version("0.3.0-beta.1"));
    }

    #[test]
    fn reads_a_dev_build_as_a_pre_release() {
        let dev = version("0.2.0-a1b2c3d");
        assert!(dev.is_prerelease());
        assert!(version("0.2.0") > dev);
    }

    #[test]
    fn refuses_a_version_that_is_not_semver() {
        for text in [
            "",
            "0.3",
            "0.3.0.1",
            "x.y.z",
            "0.03.0",
            "0.3.0-",
            "0.3.0-beta..1",
            "0.3.0-beta_1",
        ] {
            assert!(Version::parse(text).is_err(), "{text} was accepted");
        }
    }
}
