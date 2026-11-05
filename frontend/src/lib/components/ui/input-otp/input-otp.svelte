<script lang="ts">
	import { PinInput as InputOTPPrimitive, REGEXP_ONLY_DIGITS } from "bits-ui";
	import { cn } from "$lib/utils.js";

	let {
		ref = $bindable(null),
		class: className,
		value = $bindable(""),
		maxlength = 6,
		numeric = false,
		pattern,
		autofocus = false,
		inputRef = $bindable(null),
		...restProps
	}: Omit<InputOTPPrimitive.RootProps, "maxlength"> & {
		/**
		 * Number of cells. Accepts a string so callers can write the literal
		 * `maxlength="6"` (the login markup test asserts that exact text).
		 */
		maxlength?: number | string;
		/** Restrict input to digits only (sets bits-ui's REGEXP_ONLY_DIGITS). */
		numeric?: boolean;
		/**
		 * Focus the hidden input once mounted. Prefer this over the native
		 * `autofocus` attribute, which trips Svelte's a11y_autofocus warning.
		 */
		autofocus?: boolean;
	} = $props();

	const resolvedPattern = $derived(numeric ? REGEXP_ONLY_DIGITS : pattern);

	$effect(() => {
		if (autofocus) inputRef?.focus();
	});
</script>

<InputOTPPrimitive.Root
	bind:ref
	bind:value
	bind:inputRef
	data-slot="input-otp"
	spellcheck={false}
	maxlength={Number(maxlength)}
	pattern={resolvedPattern}
	class={cn("flex items-center has-disabled:opacity-50 disabled:cursor-not-allowed", className)}
	{...restProps}
/>
