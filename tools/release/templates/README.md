# Eco

The memory of ARN: one binary per platform, chosen at install time, exactly
like esbuild.

```
npm i @{{.Scope}}/eco
eco --version
```

The platform binary lives in an optional dependency
(`@{{.Scope}}/eco-win32-x64`, `@{{.Scope}}/eco-linux-x64`,
`@{{.Scope}}/eco-darwin-arm64`) that npm picks by `os` and `cpu`. This package
has no dependencies of its own, no install scripts, and no network access.

Eco is also a Go module: `go get {{.Module}}@v{{.Version}}`.

Version {{.Version}}.