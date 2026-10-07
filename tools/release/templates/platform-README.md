# Eco

The memory of ARN, as a compiled binary for {{.OS}}/{{.Arch}}.

This package holds the binary and nothing else. The launcher lives in
`@{{.Scope}}/eco`, which depends on this package as an optional dependency
and resolves it by `os` and `cpu`.

Install the launcher, not this package:

```
npm i @{{.Scope}}/eco
eco --version
```

Version {{.Version}}.