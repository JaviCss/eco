package main

type platform struct {
	Name     string
	GoOS     string
	GoArch   string
	NodeOS   string
	NodeArch string
	Binary   string
	BinName  string
}

const scope = "javicss"

const mainPackageName = "eco"

var platforms = []platform{
	{
		Name: mainPackageName + "-win32-x64", GoOS: "windows", GoArch: "amd64",
		NodeOS: "win32", NodeArch: "x64", Binary: "eco.exe", BinName: "eco-win32-x64",
	},
	{
		Name: mainPackageName + "-linux-x64", GoOS: "linux", GoArch: "amd64",
		NodeOS: "linux", NodeArch: "x64", Binary: "eco", BinName: "eco-linux-x64",
	},
	{
		Name: mainPackageName + "-darwin-arm64", GoOS: "darwin", GoArch: "arm64",
		NodeOS: "darwin", NodeArch: "arm64", Binary: "eco", BinName: "eco-darwin-arm64",
	},
}
