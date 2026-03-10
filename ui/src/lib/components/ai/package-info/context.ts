import { getContext, setContext } from "svelte";

const PACKAGE_INFO_CONTEXT_KEY = Symbol.for("discobot-ui-ai-package-info-context");

export type ChangeType = "major" | "minor" | "patch" | "added" | "removed";

export type PackageInfoContextValue = {
	name: string;
	currentVersion?: string;
	newVersion?: string;
	changeType?: ChangeType;
};

export function setPackageInfoContext(
	value: PackageInfoContextValue,
): PackageInfoContextValue {
	return setContext(PACKAGE_INFO_CONTEXT_KEY, value);
}

export function usePackageInfoContext(): PackageInfoContextValue {
	const context = getContext<PackageInfoContextValue | undefined>(
		PACKAGE_INFO_CONTEXT_KEY,
	);
	if (!context) {
		throw new Error("PackageInfo components must be used within PackageInfo");
	}
	return context;
}
