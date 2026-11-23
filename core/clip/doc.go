// Package clip runs clipboard content through the local pipeline: sensitive
// content is dropped, own writes are ignored, changes closer than the debounce
// window are merged, and the result becomes a clip item. Text, images and files
// are captured; images are normalized to PNG and payloads above the inline
// limit go to the payload store.
//
// Items announced by peers follow the planner: an applied item is written to
// the clipboard, a metadata item only reaches the history, and an item that
// needs confirmation is reported for the user. A pause silences both
// directions.
package clip
