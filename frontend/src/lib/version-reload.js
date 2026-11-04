/**
 * Watch the version reported by the live Go server. The first valid version is
 * the baseline; a later change means the binary (and embedded frontend) has
 * been replaced, so the browser must load a fresh HTML document and JS bundle.
 *
 * This covers both Settings-triggered updates and updates run over SSH.
 * A second reload cannot be triggered from the same page instance.
 *
 * @param {() => void} reload
 */
export function createVersionReloadMonitor(reload) {
  let observed = ''
  let reloading = false
  return {
    /** @param {unknown} value */
    observe(value) {
      if (reloading || typeof value !== 'string' || !/^v\d+\.\d+\.\d+/.test(value)) return false
      if (!observed) {
        observed = value
        return false
      }
      if (observed === value) return false
      reloading = true
      reload()
      return true
    },
  }
}
