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
 * if (!ok) return
 * ```
 *
 * A destructive action that can also discard data can declare a third choice.
 * `confirmDialog` then resolves to `'cancel' | 'confirm' | 'alternate'`, so the
 * caller can tell "keep the files" apart from "delete the files":
 *
 * ```ts
 * const choice = await confirmDialog({
 *   title: 'Delete this torrent?',
 *   alternateLabel: 'Delete files too',
 *   alternateDestructive: true,
 * })
 * if (choice === 'cancel') return
 * await remove(hash, choice === 'alternate')
 * ```
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
	/**
	 * Optional third, more destructive action. When present it renders last and
	 * the dialog resolves to a `ConfirmResult` instead of a boolean.
	 */
	alternateLabel?: string
	/** Renders the alternate button with the destructive variant. */
	alternateDestructive?: boolean
	/** Supporting detail shown under the alternate button, e.g. what it deletes. */
	alternateHint?: string
}

/** `true` when a two-choice dialog is confirmed, `false` otherwise. */
export type ConfirmAnswer = ConfirmResult | boolean

export type ConfirmResult = 'cancel' | 'confirm' | 'alternate'

export type ConfirmRequest = ConfirmOptions & {
	id: number
	/** Resolves a tri-state answer for three-button dialogs, else a boolean. */
	resolve: (answer: ConfirmAnswer) => void
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
 * Opens a modal confirmation dialog and resolves when the user answers.
 *
 * Resolves `true`/`false` for a normal two-button dialog, or
 * `'cancel' | 'confirm' | 'alternate'` when `alternateLabel` is provided.
 * Cancelling, pressing Escape, or dismissing always resolves to the falsy
 * answer (`false` / `'cancel'`).
 *
 * Concurrent calls are queued and shown one at a time.
 */
export function confirmDialog(options: ConfirmOptions): Promise<ConfirmAnswer> {
	return new Promise<ConfirmAnswer>((resolve) => {
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

/**
 * @internal used by the host component.
 *
 * Settles the current request with a tri-state answer. Callers that asked for a
 * plain yes/no receive a boolean so existing call sites keep working.
 */
export function settleConfirm(answer: ConfirmResult) {
	const request = queue.shift()
	if (request) {
		request.resolve(
			request.alternateLabel
				? answer
				: answer === 'confirm',
		)
	}
	notify()
}
