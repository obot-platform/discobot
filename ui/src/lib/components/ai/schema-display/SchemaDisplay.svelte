<script lang="ts">
	import { cn } from "$lib/utils";
	import {
		setSchemaDisplayContext,
		type HttpMethod,
		type SchemaParameter,
		type SchemaProperty,
	} from "./context";

	type Props = {
		method: HttpMethod;
		path: string;
		description?: string;
		parameters?: SchemaParameter[];
		requestBody?: SchemaProperty[];
		responseBody?: SchemaProperty[];
		class?: string;
		children?: () => any;
	};

	let {
		method,
		path,
		description,
		parameters,
		requestBody,
		responseBody,
		class: className,
		children,
		...restProps
	}: Props = $props();

	const schemaDisplay = $state({
		method: "GET" as HttpMethod,
		path: "",
		description: undefined as string | undefined,
		parameters: undefined as SchemaParameter[] | undefined,
		requestBody: undefined as SchemaProperty[] | undefined,
		responseBody: undefined as SchemaProperty[] | undefined,
	});

	$effect(() => {
		schemaDisplay.method = method;
		schemaDisplay.path = path;
		schemaDisplay.description = description;
		schemaDisplay.parameters = parameters;
		schemaDisplay.requestBody = requestBody;
		schemaDisplay.responseBody = responseBody;
	});

	setSchemaDisplayContext(schemaDisplay);
</script>

<div class={cn("overflow-hidden rounded-lg border bg-background", className)} {...restProps}>
	{@render children?.()}
</div>
