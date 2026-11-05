import { mount } from 'svelte'
import ConfirmHost from './confirm-host.svelte'

/**
 * Imperative, awaitable replacement for `window.confirm()`.
 *
 * The dialog host is mounted lazily into `document.body` the first time
 * `confirmDialog()` is called, so callers never have to render any markup:
 *
 * ```ts
 * import { confirmDialog } from '$lib/components/ui/alert-dialog/index.js'
 *
 * const ok = await confirmDialog({
 *   title: 'Remove the TMDB API token?',
 *   description: 'Metadata lookups will stop working until a new token is saved.',
 *   confirmLabel: 'Remove',
 *   destructive: true,
 * })
 * if (ok !== true) return
 * ```
 *
 * Always compare with `ok !== true` rather than `!ok`: the answer is a boolean,
 * and being explicit keeps a future non-boolean answer from reading as consent.
 */
export type ConfirmOptions = {
	/** Dialog heading. Required — it is also the accessible name. */
	title: string
	/** Optional supporting copy rendered under the heading. */
	description?: string
	/** Label for the confirming button. Defaults to `Continue`. */
	confirmLabel?: string
	/** Label for the dismissing button. Defaults to `Cancel`. */
	cancelLabel?: string
	/** Renders the confirm button with the destructive variant. */
	destructive?: boolean
}

export type ConfirmRequest = ConfirmOptions & {
	id: number
	resolve: (ok: boolean) => void
}

let nextId = 1
const queue: ConfirmRequest[] = []
let listener: ((request: ConfirmRequest | null) => void) | null = null
let hostMounted = false

function notify() {
	listener?.(queue[0] ?? null)
}

function ensureHost() {
	if (hostMounted || typeof document === 'undefined') return
	hostMounted = true
	const target = document.createElement('div')
	target.id = 'cloudflix-confirm-host'
	document.body.appendChild(target)
	mount(ConfirmHost, { target })
}

/**
 * Opens a modal confirmation dialog and resolves `true` when the user confirms,
 * `false` when they cancel, press Escape, or the dialog is otherwise dismissed.
 *
 * Concurrent calls are queued and shown one at a time.
 */
export function confirmDialog(options: ConfirmOptions): Promise<boolean> {
	return new Promise<boolean>((resolve) => {
		queue.push({ ...options, id: nextId++, resolve })
		ensureHost()
		notify()
	})
}

/** @internal used by the host component */
export function subscribeConfirm(fn: (request: ConfirmRequest | null) => void) {
	listener = fn
	notify()
	return () => {
		if (listener === fn) listener = null
	}
}

/** @internal used by the host component */
export function settleConfirm(ok: boolean) {
	const request = queue.shift()
	request?.resolve(ok)
	notify()
}
