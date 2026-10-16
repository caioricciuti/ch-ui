// The pasted preview sample, shared by every transform panel of the open
// pipeline so switching nodes keeps it. The editor clears it on unmount.
export const pastedSample = $state({
  /** The paste box is in use instead of the live sample. */
  active: false,
  text: '',
  /** The server said it has no live sample, so pasting is the only way. */
  noLive: false,
})

export function resetPastedSample(): void {
  pastedSample.active = false
  pastedSample.text = ''
  pastedSample.noLive = false
}
