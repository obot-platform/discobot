export const categoryLabels = {
	layout: "Layout",
	form: "Forms",
	navigation: "Navigation",
	overlay: "Overlays",
	data: "Data display",
	feedback: "Feedback",
	utility: "Utilities",
} as const;

export type UiComponentCategory = keyof typeof categoryLabels;
export type UiComponentFilter = "all" | UiComponentCategory;

export type UiComponentCatalogEntry = {
	category: UiComponentCategory;
	description: string;
	exportName: string;
	featured: boolean;
	importPath: string;
	label: string;
	name: string;
};

const componentNames = [
	"accordion",
	"alert-dialog",
	"alert",
	"aspect-ratio",
	"avatar",
	"badge",
	"breadcrumb",
	"button-group",
	"button",
	"calendar",
	"card",
	"carousel",
	"chart",
	"checkbox",
	"collapsible",
	"command",
	"context-menu",
	"data-table",
	"dialog",
	"drawer",
	"dropdown-menu",
	"empty",
	"field",
	"form",
	"hover-card",
	"input-group",
	"input-otp",
	"input",
	"item",
	"kbd",
	"label",
	"menubar",
	"native-select",
	"navigation-menu",
	"pagination",
	"popover",
	"progress",
	"radio-group",
	"range-calendar",
	"resizable",
	"scroll-area",
	"select",
	"separator",
	"sheet",
	"sidebar",
	"skeleton",
	"slider",
	"sonner",
	"spinner",
	"split-dropdown-button",
	"switch",
	"table",
	"tabs",
	"textarea",
	"toggle-group",
	"toggle",
	"tooltip",
] as const;

const categoryOrder: UiComponentCategory[] = [
	"layout",
	"form",
	"navigation",
	"overlay",
	"data",
	"feedback",
	"utility",
];

const featuredNames = new Set([
	"accordion",
	"badge",
	"button",
	"card",
	"dialog",
	"dropdown-menu",
	"input",
	"select",
	"sheet",
	"sidebar",
	"switch",
	"tabs",
	"textarea",
	"tooltip",
]);

const descriptions: Partial<Record<string, string>> = {
	accordion:
		"Reveal stacked content sections without leaving the current view.",
	"alert-dialog":
		"Ask for confirmation before destructive or high-friction actions.",
	alert: "Present inline status, warnings, and contextual feedback.",
	"aspect-ratio":
		"Lock media containers to a predictable width-to-height ratio.",
	avatar: "Display a user or workspace identity image with a fallback.",
	badge: "Attach a compact label for state, tags, or counts.",
	breadcrumb:
		"Show the current location within a nested information hierarchy.",
	button: "Trigger a primary, secondary, or contextual action.",
	"button-group": "Keep related actions visually and semantically grouped.",
	calendar: "Pick a single date from a visual calendar view.",
	card: "Wrap related content and actions in a consistent surface.",
	carousel: "Present horizontally paged content like onboarding or previews.",
	chart: "Render charts and chart tooltips for dashboard-style views.",
	checkbox: "Capture boolean choices or multi-select options.",
	collapsible: "Expand or collapse a region while staying in the same layout.",
	command: "Create a command palette or searchable action launcher.",
	"context-menu":
		"Expose secondary actions at the cursor or long-press position.",
	"data-table":
		"Compose sortable, filterable table views from structured data.",
	dialog: "Open a centered modal workflow that blocks the rest of the app.",
	drawer: "Slide a panel from the edge for mobile-first or transient tasks.",
	"dropdown-menu": "Offer a compact menu of contextual actions.",
	empty: "Explain and decorate an empty state before real content exists.",
	field: "Assemble labels, descriptions, and validation around form controls.",
	form: "Connect form controls with validation and error presentation helpers.",
	"hover-card": "Preview extra context when someone hovers a trigger.",
	input: "Capture short single-line text or search queries.",
	"input-group": "Combine inputs with icons, prefixes, or inline actions.",
	"input-otp": "Capture one-time codes in segmented inputs.",
	item: "Lay out dense list rows with media, text, and trailing actions.",
	kbd: "Show keyboard shortcuts in a compact visual token.",
	label: "Describe an associated form control accessibly.",
	menubar: "Provide app-like menu bars with nested items and shortcuts.",
	"native-select": "Use the platform select element with consistent styling.",
	"navigation-menu":
		"Build top-level product navigation with expandable content.",
	pagination: "Move between pages of data or content.",
	popover:
		"Anchor a floating panel to a trigger without taking over the screen.",
	progress: "Communicate completion progress for an ongoing task.",
	"radio-group": "Choose one option from a small exclusive set.",
	"range-calendar": "Pick a date range from a calendar interface.",
	resizable: "Let the user drag panes to change layout proportions.",
	"scroll-area": "Constrain overflow while keeping custom scroll styling.",
	select: "Pick from a styled listbox-style set of options.",
	separator: "Visually divide related content or toolbar groups.",
	sheet: "Slide in supporting content from a screen edge.",
	sidebar: "Compose multi-section desktop navigation and work panels.",
	skeleton: "Reserve space while content is loading.",
	slider: "Select a numeric value across a continuous range.",
	sonner: "Display toast notifications and transient system messages.",
	spinner: "Show an indeterminate loading state.",
	switch: "Toggle a setting on and off.",
	table: "Present tabular data with semantic table markup.",
	tabs: "Switch between sibling views within the same surface.",
	textarea: "Capture longer multi-line text input.",
	toggle: "Turn a compact pressed state on or off.",
	"toggle-group": "Manage one-or-many pressed button selections.",
	tooltip: "Reveal short help text for controls and icons.",
};

function toLabel(name: string): string {
	return name
		.split("-")
		.map((part) => part.charAt(0).toUpperCase() + part.slice(1))
		.join(" ");
}

function toExportName(name: string): string {
	return toLabel(name).replaceAll(" ", "");
}

function categorizeComponent(name: string): UiComponentCategory {
	if (
		[
			"calendar",
			"checkbox",
			"field",
			"form",
			"input",
			"input-group",
			"input-otp",
			"label",
			"native-select",
			"radio-group",
			"range-calendar",
			"select",
			"slider",
			"switch",
			"textarea",
			"toggle",
			"toggle-group",
		].includes(name)
	) {
		return "form";
	}

	if (
		[
			"breadcrumb",
			"command",
			"context-menu",
			"dropdown-menu",
			"menubar",
			"navigation-menu",
			"pagination",
			"tabs",
		].includes(name)
	) {
		return "navigation";
	}

	if (
		[
			"alert-dialog",
			"dialog",
			"drawer",
			"hover-card",
			"popover",
			"sheet",
			"tooltip",
		].includes(name)
	) {
		return "overlay";
	}

	if (["chart", "data-table", "table"].includes(name)) {
		return "data";
	}

	if (
		["alert", "empty", "progress", "skeleton", "sonner", "spinner"].includes(
			name,
		)
	) {
		return "feedback";
	}

	if (["avatar", "badge", "kbd", "separator"].includes(name)) {
		return "utility";
	}

	return "layout";
}

function defaultDescription(category: UiComponentCategory): string {
	switch (category) {
		case "layout":
			return "Structure the shell and arrange content within a page or panel.";
		case "form":
			return "Capture user input and control interactive settings.";
		case "navigation":
			return "Move through views, menus, and grouped destinations.";
		case "overlay":
			return "Present transient UI above the current screen.";
		case "data":
			return "Show structured information, tables, and charts.";
		case "feedback":
			return "Communicate loading, status, and empty states.";
		case "utility":
			return "Support the rest of the system with compact helper UI.";
	}
}

export const uiComponentCatalog: UiComponentCatalogEntry[] = componentNames
	.map((path) => {
		const name = path.split("/").at(-2);
		if (!name) {
			throw new Error(`Unable to derive component name from path: ${path}`);
		}

		const category = categorizeComponent(name);
		return {
			category,
			description: descriptions[name] ?? defaultDescription(category),
			exportName: toExportName(name),
			featured: featuredNames.has(name),
			importPath: `$lib/components/ui/${name}`,
			label: toLabel(name),
			name,
		};
	})
	.sort((left, right) => {
		return (
			categoryOrder.indexOf(left.category) -
				categoryOrder.indexOf(right.category) ||
			left.label.localeCompare(right.label)
		);
	});

export const uiComponentFilters: UiComponentFilter[] = [
	"all",
	...categoryOrder,
];
