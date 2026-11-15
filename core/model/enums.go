package model

// Role is the trust role of a paired device.
type Role uint8

// Device roles.
const (
	// RoleUnspecified is the zero value and marks a missing role.
	RoleUnspecified Role = iota
	// RolePersonal grants full synchronization.
	RolePersonal
	// RoleFriends grants sending after confirmation only.
	RoleFriends
)

// String returns the lower case name of the role.
func (r Role) String() string {
	switch r {
	case RolePersonal:
		return "personal"
	case RoleFriends:
		return "friends"
	default:
		return "unspecified"
	}
}

// Platform is the operating system of a device.
type Platform uint8

// Device platforms.
const (
	// PlatformUnknown is the zero value and marks an unknown platform.
	PlatformUnknown Platform = iota
	// PlatformMacOS is macOS.
	PlatformMacOS
	// PlatformWindows is Windows.
	PlatformWindows
	// PlatformAndroid is Android.
	PlatformAndroid
)

// String returns the lower case name of the platform.
func (p Platform) String() string {
	switch p {
	case PlatformMacOS:
		return "macos"
	case PlatformWindows:
		return "windows"
	case PlatformAndroid:
		return "android"
	default:
		return "unknown"
	}
}

// Kind is the content kind of a clip item.
type Kind uint8

// Content kinds.
const (
	// KindUnspecified is the zero value and marks a missing kind.
	KindUnspecified Kind = iota
	// KindText is plain or rich text.
	KindText
	// KindImage is an image.
	KindImage
	// KindFiles is one or more files.
	KindFiles
)

// String returns the lower case name of the kind.
func (k Kind) String() string {
	switch k {
	case KindText:
		return "text"
	case KindImage:
		return "image"
	case KindFiles:
		return "files"
	default:
		return "unspecified"
	}
}

// Source is where a clip item was captured.
type Source uint8

// Clip item sources.
const (
	// SourceUnspecified is the zero value and marks a missing source.
	SourceUnspecified Source = iota
	// SourceClipboard is the local clipboard.
	SourceClipboard
	// SourceShare is a share request from another application.
	SourceShare
	// SourceCLI is an explicit command.
	SourceCLI
)

// String returns the lower case name of the source.
func (s Source) String() string {
	switch s {
	case SourceClipboard:
		return "clipboard"
	case SourceShare:
		return "share"
	case SourceCLI:
		return "cli"
	default:
		return "unspecified"
	}
}
