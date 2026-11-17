//! Locale of the native shell.
//!
//! The language is picked once at startup from the system settings, like the
//! webview picks it from its language tags: a Russian primary tag becomes
//! Russian, anything else English.

/// Locale of the texts the shell draws itself, such as the tray menu.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Locale {
    En,
    Ru,
}

impl Locale {
    /// from_tag picks the locale of a language tag by its primary subtag.
    pub fn from_tag(tag: &str) -> Self {
        let primary = tag
            .split(['-', '_'])
            .next()
            .unwrap_or_default()
            .to_ascii_lowercase();
        if primary == "ru" { Self::Ru } else { Self::En }
    }

    /// detect reads the locale of the system and falls back to English.
    pub fn detect() -> Self {
        sys_locale::get_locale()
            .map(|tag| Self::from_tag(&tag))
            .unwrap_or(Self::En)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_russian_primary_tag_selects_russian() {
        assert_eq!(Locale::from_tag("ru"), Locale::Ru);
        assert_eq!(Locale::from_tag("ru-RU"), Locale::Ru);
        assert_eq!(Locale::from_tag("RU_ru"), Locale::Ru);
    }

    #[test]
    fn every_other_tag_selects_english() {
        for tag in ["en-US", "de-DE", "uk-UA", "", "rux"] {
            assert_eq!(Locale::from_tag(tag), Locale::En, "{tag:?}");
        }
    }
}
