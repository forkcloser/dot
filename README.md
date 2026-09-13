# dot

A `dot` command that renders Graphviz graphs without a Graphviz
installation. The engine is [go-graphviz](https://github.com/goccy/go-graphviz),
which ships Graphviz as WebAssembly, so the result is one static Go binary
with no C toolchain and no system packages behind it.

```
go install github.com/forkcloser/dot/cmd/dot@latest
```

## Usage

```
dot [-K layout] [-T format] [-o output] [input]
```

| Flag | Values | Default |
|---|---|---|
| `-K` | `dot`, `neato`, `fdp`, `sfdp`, `circo`, `twopi`, `osage`, `patchwork` | `dot` |
| `-T` | `dot`, `svg`, `png`, `jpg` | `dot` |
| `-o` | output file | standard output |
| input | a DOT file, or `-` | standard input |

Flags take Graphviz's glued form (`-Tpng`, `-oout.png`) as well as the spaced
one. `-V` prints the version, `-h` the usage. Exit status is 0 on success, 1
on an input or rendering error, 2 on a usage error.

```
go tool pprof -dot cpu.prof | dot -Tpng -o profile.png
```

That is the whole interface. Graphviz's other options (`-G`, `-N`, `-E`,
`-l`, multiple outputs) are not implemented; graph attributes go in the DOT
source.

## Why

go-graphviz keeps its own `dot` command in a nested module that has never
been tagged and still requires the library at v0.2.5, five releases behind.
A tool directive on it cannot move. This module is the same thin client on
the current library, tagged, so a `tool` directive or `go install` pin
tracks releases like any other dependency.

## Licence

MIT, see `LICENSE`. go-graphviz is MIT; its dependency tree includes
`github.com/golang/freetype`, which is dual-licensed FreeType or GPLv2, and
the dependency license check here allows that one module on the FreeType
terms.
