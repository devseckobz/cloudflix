<script lang="ts">
	import { onMount } from 'svelte'
	import { Button } from '$lib/components/ui/button/index.js'
	import AlertDialogRoot from './alert-dialog.svelte'
	import AlertDialogContent from './alert-dialog-content.svelte'
	import AlertDialogHeader from './alert-dialog-header.svelte'
	import AlertDialogTitle from './alert-dialog-title.svelte'
	import AlertDialogDescription from './alert-dialog-description.svelte'
	import AlertDialogFooter from './alert-dialog-footer.svelte'
	import { settleConfirm, subscribeConfirm, type ConfirmRequest, type ConfirmResult } from './confirm.js'

	let request = $state<ConfirmRequest | null>(null)
	let open = $state(false)
	let answered = false

	onMount(() =>
		subscribeConfirm((next) => {
			// Keep the last request around while the dialog animates closed so the
			// title/description do not blank out mid-transition.
			if (next) {
				request = next
				answered = false
			}
			open = next !== null
		})
	)

	function answer(choice: ConfirmResult) {
		if (answered) return
		answered = true
		// settleConfirm() resolves the promise and notifies the subscriber, which
		// closes the dialog (or swaps in the next queued request).
		settleConfirm(choice)
	}
</script>

<AlertDialogRoot bind:open onOpenChange={(next) => { if (!next) answer('cancel') }}>
	<AlertDialogContent>
		<AlertDialogHeader>
			<AlertDialogTitle>{request?.title ?? ''}</AlertDialogTitle>
			{#if request?.description}
				<AlertDialogDescription>{request.description}</AlertDialogDescription>
			{/if}
		</AlertDialogHeader>
		<AlertDialogFooter>
			<Button variant="outline" onclick={() => answer('cancel')}>
				{request?.cancelLabel ?? 'Cancel'}
			</Button>
			<Button
				variant={request?.destructive ? 'destructive' : 'default'}
				onclick={() => answer('confirm')}
			>
				{request?.confirmLabel ?? 'Continue'}
			</Button>
			{#if request?.alternateLabel}
				<Button
					variant={request.alternateDestructive ? 'destructive' : 'default'}
					onclick={() => answer('alternate')}
					title={request.alternateHint}
				>
					{request.alternateLabel}
				</Button>
			{/if}
		</AlertDialogFooter>
	</AlertDialogContent>
</AlertDialogRoot>
