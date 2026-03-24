import type { IdeOption } from "$lib/shell-types";

export const windowControls = ["_", "□", "×"];

export const ideOptions: IdeOption[] = [
	{ id: "cursor", label: "Cursor", family: "standard" },
	{ id: "vscode", label: "VS Code", family: "standard" },
	{ id: "zed", label: "Zed", family: "standard" },
	{
		id: "jetbrains-intellij-idea",
		label: "IntelliJ IDEA Ultimate",
		family: "jetbrains",
		productCode: "IU",
	},
	{
		id: "jetbrains-webstorm",
		label: "WebStorm",
		family: "jetbrains",
		productCode: "WS",
	},
	{
		id: "jetbrains-goland",
		label: "GoLand",
		family: "jetbrains",
		productCode: "GO",
	},
	{
		id: "jetbrains-pycharm",
		label: "PyCharm Professional",
		family: "jetbrains",
		productCode: "PY",
	},
	{
		id: "jetbrains-phpstorm",
		label: "PhpStorm",
		family: "jetbrains",
		productCode: "PS",
	},
	{
		id: "jetbrains-clion",
		label: "CLion",
		family: "jetbrains",
		productCode: "CL",
	},
	{
		id: "jetbrains-rubymine",
		label: "RubyMine",
		family: "jetbrains",
		productCode: "RM",
	},
	{
		id: "jetbrains-rider",
		label: "Rider",
		family: "jetbrains",
		productCode: "RD",
	},
];
