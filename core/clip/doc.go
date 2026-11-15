// Package clip runs clipboard content through the local pipeline: sensitive
// content is dropped, own writes are ignored, changes closer than the debounce
// window are merged, and the result becomes a clip item. Items announced by
// peers reach the clipboard only when the policy applies them, and a pause
// silences both directions.
package clip
