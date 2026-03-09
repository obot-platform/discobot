import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export type WithElementRef<T, U extends Element = Element> = T & {
	ref?: U | null;
};

export type WithoutChildren<T> = T extends { children?: unknown }
	? Omit<T, "children">
	: T;

export type WithoutChild<T> = T extends { child?: unknown }
	? Omit<T, "child">
	: T;

export type WithoutChildrenOrChild<T> = WithoutChild<WithoutChildren<T>>;

export function cn(...inputs: ClassValue[]) {
	return twMerge(clsx(inputs));
}
