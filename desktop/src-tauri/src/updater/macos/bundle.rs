//! Bundle inspection, disk image mounting and staging on macOS.

use std::ffi::{CString, OsStr, OsString};
use std::fs;
use std::os::unix::ffi::OsStrExt;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};

use super::{
    BUNDLE_NAME, DAEMON_EXECUTABLE, INFO_PLIST, LOGIN_ITEM, create_private, details, sibling,
};

/// System utilities that mount, copy, inspect and verify a bundle.
const HDIUTIL: &str = "/usr/bin/hdiutil";
const DITTO: &str = "/usr/bin/ditto";
const XATTR: &str = "/usr/bin/xattr";
const CODESIGN: &str = "/usr/bin/codesign";
const PLIST_BUDDY: &str = "/usr/libexec/PlistBuddy";

/// Expected CFBundleIdentifier of the desktop application.
const BUNDLE_IDENTIFIER: &str = "app.unison";

/// StagedBundle owns a staged `.app` copy and removes it on drop unless
/// disarmed after ownership is handed to the detached helper.
pub(super) struct StagedBundle {
    path: PathBuf,
    armed: bool,
}

impl StagedBundle {
    /// new wraps a staged bundle path so it is cleaned up on failure.
    fn new(path: PathBuf) -> Self {
        Self { path, armed: true }
    }

    /// path returns the location of the staged bundle.
    pub(super) fn path(&self) -> &Path {
        &self.path
    }

    /// disarm releases ownership so the detached helper can swap the bundle.
    pub(super) fn disarm(mut self) {
        self.armed = false;
    }
}

impl Drop for StagedBundle {
    fn drop(&mut self) {
        if self.armed {
            let _ = fs::remove_dir_all(&self.path);
        }
    }
}

/// current_bundle validates that the running process is a replaceable macOS
/// bundle and returns its path.
pub(super) fn current_bundle() -> Result<PathBuf, String> {
    if cfg!(debug_assertions) {
        return Err("in-app updates are disabled in development builds".to_string());
    }
    let executable =
        std::env::current_exe().map_err(|err| format!("cannot find the running app: {err}"))?;
    let bundle = bundle_of(&executable)?;
    inspect(&bundle)?;
    Ok(bundle)
}

/// prepare_staged mounts the disk image, validates the bundle inside it,
/// copies it next to the target, removes quarantine and checks its signature.
pub(super) fn prepare_staged(
    artifact_path: &Path,
    target: &Path,
    expected_version: &str,
    mount_dir: &Path,
) -> Result<StagedBundle, String> {
    let staged = {
        let mount = Mount::attach(artifact_path, mount_dir)?;
        let bundle = mount.dir.join(BUNDLE_NAME);
        check_bundle(&bundle, expected_version)?;
        StagedBundle::new(stage(&bundle, target)?)
    };
    unquarantine(staged.path());
    verify_signature(staged.path())?;
    Ok(staged)
}

/// bundle_of returns the bundle that holds an executable of `Contents/MacOS`.
fn bundle_of(executable: &Path) -> Result<PathBuf, String> {
    let bundle = executable
        .parent()
        .and_then(Path::parent)
        .and_then(Path::parent)
        .map(Path::to_path_buf)
        .ok_or_else(|| not_a_bundle(executable))?;
    if bundle.extension() != Some(OsStr::new("app")) {
        return Err(not_a_bundle(executable));
    }
    Ok(bundle)
}

/// not_a_bundle describes an executable that does not live in a bundle.
fn not_a_bundle(executable: &Path) -> String {
    format!(
        "{} is not inside an application bundle",
        executable.display()
    )
}

/// inspect reports why the bundle cannot be replaced from here.
fn inspect(bundle: &Path) -> Result<(), String> {
    if !bundle.join(INFO_PLIST).is_file() {
        return Err("the application bundle is incomplete".to_string());
    }
    if bundle.to_string_lossy().contains("/AppTranslocation/") {
        return Err(
            "Unison runs from a disk image or a temporary location. Move Unison.app to /Applications and start it there."
                .to_string(),
        );
    }
    if is_read_only(bundle) {
        return Err(
            "Unison runs from a read-only volume. Move Unison.app to /Applications and start it there."
                .to_string(),
        );
    }
    let parent = bundle
        .parent()
        .ok_or_else(|| "the installation folder of Unison is unknown".to_string())?;
    if !is_writable(parent) {
        return Err(
            "Unison does not have write permission to the installation folder. Move Unison.app to ~/Applications or update as an administrator."
                .to_string(),
        );
    }
    Ok(())
}

/// is_read_only reports whether the file system of a path is mounted read-only.
fn is_read_only(path: &Path) -> bool {
    let Ok(path) = CString::new(path.as_os_str().as_bytes()) else {
        return false;
    };
    // SAFETY: the call fills the structure with the data of the file system.
    let mut stats: libc::statvfs = unsafe { std::mem::zeroed() };
    // SAFETY: the path is a null terminated string and the buffer is large
    // enough for the structure.
    let code = unsafe { libc::statvfs(path.as_ptr(), &mut stats) };
    code == 0 && stats.f_flag & libc::ST_RDONLY as libc::c_ulong != 0
}

/// is_writable reports whether the current user may write into a directory.
fn is_writable(dir: &Path) -> bool {
    let Ok(dir) = CString::new(dir.as_os_str().as_bytes()) else {
        return false;
    };
    // SAFETY: the path is a null terminated string.
    unsafe { libc::access(dir.as_ptr(), libc::W_OK) == 0 }
}

/// Mount is the disk image of an update. It is detached and its mount point
/// removed when it leaves the scope.
struct Mount {
    dir: PathBuf,
}

impl Mount {
    /// attach mounts the disk image read-only at a private mount point.
    fn attach(dmg: &Path, dir: &Path) -> Result<Self, String> {
        create_private(dir)?;
        let output = Command::new(HDIUTIL)
            .args(attach_args(dmg, dir))
            .stdin(Stdio::null())
            .output()
            .map_err(|err| format!("cannot mount the update: {err}"))?;
        if output.status.success() {
            return Ok(Self {
                dir: dir.to_path_buf(),
            });
        }
        let _ = fs::remove_dir_all(dir);
        Err(format!("cannot mount the update: {}", details(&output)))
    }
}

impl Drop for Mount {
    fn drop(&mut self) {
        let _ = Command::new(HDIUTIL)
            .args(detach_args(&self.dir))
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .status();
        let _ = fs::remove_dir_all(&self.dir);
    }
}

/// attach_args returns the hdiutil arguments to mount a disk image read-only.
fn attach_args(dmg: &Path, dir: &Path) -> Vec<OsString> {
    vec![
        OsString::from("attach"),
        OsString::from("-nobrowse"),
        OsString::from("-readonly"),
        OsString::from("-noverify"),
        OsString::from("-noautoopen"),
        OsString::from("-mountpoint"),
        dir.as_os_str().to_os_string(),
        dmg.as_os_str().to_os_string(),
    ]
}

/// detach_args returns the hdiutil arguments to forcibly unmount a mount point.
fn detach_args(dir: &Path) -> Vec<OsString> {
    vec![
        OsString::from("detach"),
        OsString::from("-force"),
        dir.as_os_str().to_os_string(),
    ]
}

/// check_bundle reports why the bundle of the disk image cannot replace the
/// installed app.
fn check_bundle(bundle: &Path, expected_version: &str) -> Result<(), String> {
    let metadata = fs::symlink_metadata(bundle)
        .map_err(|err| format!("the update holds no {BUNDLE_NAME}: {err}"))?;
    if metadata.file_type().is_symlink() || !metadata.is_dir() {
        return Err(format!("the update holds no {BUNDLE_NAME}"));
    }
    for relative in [INFO_PLIST, DAEMON_EXECUTABLE, LOGIN_ITEM] {
        if !bundle.join(relative).is_file() {
            return Err(format!("the update has no {relative}"));
        }
    }
    let identifier = plist_value(bundle, "CFBundleIdentifier")?;
    if identifier.trim() != BUNDLE_IDENTIFIER {
        return Err(format!(
            "the update is the app {}, not {BUNDLE_IDENTIFIER}",
            identifier.trim()
        ));
    }
    let version = plist_value(bundle, "CFBundleShortVersionString")?;
    if version.trim() != expected_version {
        return Err(format!(
            "the update is version {}, not {expected_version}",
            version.trim()
        ));
    }
    let executable = main_executable(bundle)?;
    if !bundle.join(&executable).is_file() {
        return Err(format!("the update has no {executable}"));
    }
    Ok(())
}

/// main_executable returns the executable of a bundle that Info.plist names.
pub(super) fn main_executable(bundle: &Path) -> Result<String, String> {
    let name = plist_value(bundle, "CFBundleExecutable")?;
    let name = name.trim();
    if name.is_empty() || name.contains('/') {
        return Err("the app names no executable".to_string());
    }
    Ok(format!("Contents/MacOS/{name}"))
}

/// plist_value reads a key of the Info.plist of a bundle.
fn plist_value(bundle: &Path, key: &str) -> Result<String, String> {
    let plist = bundle.join(INFO_PLIST);
    let output = Command::new(PLIST_BUDDY)
        .arg("-c")
        .arg(format!("Print :{key}"))
        .arg(&plist)
        .stdin(Stdio::null())
        .output()
        .map_err(|err| format!("cannot read {INFO_PLIST}: {err}"))?;
    if !output.status.success() {
        return Err(format!("{INFO_PLIST} has no {key}"));
    }
    Ok(String::from_utf8_lossy(&output.stdout).trim().to_string())
}

/// stage copies the bundle of the disk image next to the installed app, so the
/// swap is a rename within one file system.
fn stage(source: &Path, target: &Path) -> Result<PathBuf, String> {
    let staged = sibling(target, "new");
    let _ = fs::remove_dir_all(&staged);
    let output = Command::new(DITTO)
        .arg(source)
        .arg(&staged)
        .stdin(Stdio::null())
        .output()
        .map_err(|err| format!("cannot copy the update: {err}"))?;
    if !output.status.success() {
        let _ = fs::remove_dir_all(&staged);
        return Err(format!("cannot copy the update: {}", details(&output)));
    }
    Ok(staged)
}

/// unquarantine removes the quarantine attribute, so an update that is not
/// notarized still starts after the bundle was copied.
fn unquarantine(bundle: &Path) {
    let _ = Command::new(XATTR)
        .args(["-dr", "com.apple.quarantine"])
        .arg(bundle)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status();
}

/// verify_signature checks the copied bundle and warns about an ad-hoc
/// signature, which runs but cannot be distributed.
fn verify_signature(bundle: &Path) -> Result<(), String> {
    let output = Command::new(CODESIGN)
        .args(["--verify", "--deep", "--strict"])
        .arg(bundle)
        .stdin(Stdio::null())
        .output()
        .map_err(|err| format!("cannot check the signature of the update: {err}"))?;
    if !output.status.success() {
        return Err(format!(
            "the signature of the update is broken: {}",
            details(&output)
        ));
    }
    let details = Command::new(CODESIGN)
        .args(["-dv", "--verbose=2"])
        .arg(bundle)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .output();
    if let Ok(details) = details
        && String::from_utf8_lossy(&details.stderr).contains("Signature=adhoc")
    {
        crate::journal::record(
            "WARN",
            "the update is signed ad-hoc, so the Keychain may ask for access again",
        );
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::testutil::TempDir;

    /// bundle writes a fake app bundle with the given version and identifier.
    fn bundle(dir: &TempDir, version: &str, identifier: &str) -> PathBuf {
        let app = dir.join(BUNDLE_NAME);
        for relative in [
            "Contents/MacOS/unison-desktop",
            DAEMON_EXECUTABLE,
            LOGIN_ITEM,
        ] {
            let path = app.join(relative);
            fs::create_dir_all(path.parent().expect("a parent")).expect("create the directories");
            fs::write(&path, b"").expect("write the file");
        }
        let plist = format!(
            r#"<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleIdentifier</key>
  <string>{identifier}</string>
  <key>CFBundleShortVersionString</key>
  <string>{version}</string>
  <key>CFBundleExecutable</key>
  <string>unison-desktop</string>
</dict>
</plist>
"#
        );
        fs::create_dir_all(app.join("Contents")).expect("create the contents");
        fs::write(app.join(INFO_PLIST), plist).expect("write the plist");
        app
    }

    #[test]
    fn a_bundle_is_found_three_levels_above_the_executable() {
        let executable = Path::new("/Applications/Unison.app/Contents/MacOS/Unison");
        assert_eq!(
            bundle_of(executable),
            Ok(PathBuf::from("/Applications/Unison.app"))
        );
        assert!(bundle_of(Path::new("/usr/local/bin/unison")).is_err());
        assert!(bundle_of(Path::new("/Applications/Unison/Contents/MacOS/Unison")).is_err());
    }

    #[test]
    fn a_translocated_bundle_explains_the_move() {
        let dir = TempDir::new();
        let app = dir.join("AppTranslocation/d/Unison.app");
        fs::create_dir_all(app.join("Contents")).expect("create the contents");
        fs::write(app.join(INFO_PLIST), b"plist").expect("write the plist");
        let err = inspect(&app).expect_err("a translocated bundle");
        assert!(err.contains("Move Unison.app to /Applications"), "{err}");
    }

    #[test]
    fn a_bundle_of_the_user_needs_nothing_more() {
        let dir = TempDir::new();
        let app = dir.join("Unison.app");
        fs::create_dir_all(app.join("Contents")).expect("create the contents");
        fs::write(app.join(INFO_PLIST), b"plist").expect("write the plist");
        assert_eq!(inspect(&app), Ok(()));
    }

    #[test]
    fn an_incomplete_bundle_is_refused() {
        let dir = TempDir::new();
        let app = dir.join("Unison.app");
        assert!(inspect(&app).is_err());
    }

    #[test]
    fn the_folder_of_the_user_is_writable() {
        let dir = TempDir::new();
        assert!(is_writable(dir.path()));
        assert!(!is_writable(&dir.join("missing")));
    }

    #[test]
    fn hdiutil_commands_start_with_their_verb() {
        let dmg = Path::new("/tmp/unison.dmg");
        let mount = Path::new("/tmp/mnt");
        let attach = attach_args(dmg, mount);
        assert_eq!(attach.first(), Some(&OsString::from("attach")));
        assert_eq!(attach.last(), Some(&OsString::from("/tmp/unison.dmg")));
        assert!(attach.contains(&OsString::from("-mountpoint")));

        let detach = detach_args(mount);
        assert_eq!(
            detach,
            vec![
                OsString::from("detach"),
                OsString::from("-force"),
                OsString::from("/tmp/mnt"),
            ]
        );
    }

    #[test]
    fn staged_bundle_is_removed_on_drop_unless_disarmed() {
        let dir = TempDir::new();
        let dropped = dir.join("dropped.app");
        fs::create_dir_all(&dropped).expect("create dropped bundle");
        {
            let _guard = StagedBundle::new(dropped.clone());
        }
        assert!(!dropped.exists(), "armed guard must remove the directory");

        let kept = dir.join("kept.app");
        fs::create_dir_all(&kept).expect("create kept bundle");
        {
            let guard = StagedBundle::new(kept.clone());
            guard.disarm();
        }
        assert!(kept.exists(), "disarmed guard must keep the directory");
    }

    #[test]
    fn a_staged_bundle_must_carry_the_expected_app() {
        let dir = TempDir::new();
        let app = bundle(&dir, "0.2.1", BUNDLE_IDENTIFIER);
        assert_eq!(check_bundle(&app, "0.2.1"), Ok(()));

        let err = check_bundle(&app, "0.2.0").expect_err("another version");
        assert!(err.contains("0.2.1"), "{err}");

        let other = TempDir::new();
        let app = bundle(&other, "0.2.1", "app.other");
        let err = check_bundle(&app, "0.2.1").expect_err("another app");
        assert!(err.contains("app.other"), "{err}");
    }

    #[test]
    fn a_staged_bundle_needs_every_part_of_the_app() {
        let dir = TempDir::new();
        let app = bundle(&dir, "0.2.1", BUNDLE_IDENTIFIER);
        fs::remove_file(app.join(DAEMON_EXECUTABLE)).expect("remove the daemon");
        let err = check_bundle(&app, "0.2.1").expect_err("an app without a daemon");
        assert!(err.contains(DAEMON_EXECUTABLE), "{err}");

        let other = TempDir::new();
        let app = bundle(&other, "0.2.1", BUNDLE_IDENTIFIER);
        fs::remove_file(app.join("Contents/MacOS/unison-desktop")).expect("remove the app");
        let err = check_bundle(&app, "0.2.1").expect_err("an app without its executable");
        assert!(err.contains("unison-desktop"), "{err}");
    }

    #[test]
    fn a_missing_bundle_is_refused() {
        let dir = TempDir::new();
        let err = check_bundle(&dir.join(BUNDLE_NAME), "0.2.1").expect_err("a missing bundle");
        assert!(err.contains(BUNDLE_NAME), "{err}");
    }
}
