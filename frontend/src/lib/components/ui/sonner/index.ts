export { default as Toaster } from "./sonner.svelte";
// Re-exported so app code can `import { toast } from '$lib/components/ui/sonner/index.js'`
// instead of reaching into the svelte-sonner package directly.
export { toast } from "svelte-sonner";
export type { ExternalToast, ToastT, ToasterProps } from "svelte-sonner";
