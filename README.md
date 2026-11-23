# dot

A `dot` command that renders Graphviz graphs without a Graphviz
installation. The engine is
[forkcloser/go-graphviz](https://github.com/forkcloser/go-graphviz),
forkcloser's fork of goccy/go-graphviz, which ships Graphviz as
WebAssembly, so the result is one static Go binary with no C toolchain and
no system packages behind it.

```
go install github.com/forkcloser/dot/cmd/dot@latest
```

## Usage

```
dot [-K engine] [-T format] [-o path] [input]
```

| Flag | Values | Default |
|---|---|---|
| `-K` | `dot`, `neato`, `fdp`, `sfdp`, `circo`, `twopi`, `osage`, `patchwork` | `dot` |
| `-T` | `dot`, `xdot`, `svg`, `png`, `jpg` | `dot` |
| `-o` | output file | standard output |
| input | a DOT file, or `-` | standard input |

Flags take Graphviz's glued form (`-Tpng`, `-oout.png`) as well as the spaced
one. `-?`, or `-h`, prints the usage. `-V` prints the version, one line on
standard error as Graphviz's `dot` prints its own and in the same shape,
`dot - graphviz version 16.1.1 (forkcloser/dot v1.5.0)`: the Graphviz release
the engine embeds, then this command's version where Graphviz's `dot` has its
build date. The first of these flags on the command line is the whole
command, as with Graphviz's `dot`: what follows it is not read.

The exit statuses are those of Graphviz's `dot`: 0 on success, 2 when the
input cannot be read (a file that is not there or may not be opened), and 1
for every other failure: a flag, a format or a layout the command does not
know, a graph that does not parse, a rendering that fails, an output that
cannot be written. An error Graphviz reports while it goes on drawing (a
record or HTML label it cannot read) is a status 1 with the rendering written
all the same. An input with no graph in it, empty or comments alone, is
nothing to draw: status 0 and nothing written, no output file made or
replaced.

An input may hold several graphs. Each is drawn, one after the other, to
standard output or into the `-o` file. A part that does not parse ends the
drawing with status 1, the graphs before it written; a graph that fails, or
that Graphviz reports an error about, does not stop the ones after it, and
the status is 1.

These differences from Graphviz's `dot` are kept on purpose:

- `-` names standard input, where Graphviz's `dot` takes it for an unknown
  option.
- A directory given as the input is an error, with status 2 like any input
  that cannot be read, where Graphviz's `dot` reads nothing from it, says
  nothing and exits 0.
- A graph that starts on the line another ends on is drawn
  (`digraph { a } digraph { b }` is two graphs), where Graphviz's `dot` loses
  it, and an error names the line of the input, where Graphviz's `dot` counts
  from the line the graph before ended on.
- A graph that names a layout nobody has fails alone, and the graphs after it
  are drawn. Graphviz's `dot` then skips every graph after it ("Layout was
  not done") until one names a layout of its own, and draws from that one on.
- With `-o` and several graphs the file holds every rendering, as standard
  output does, where Graphviz's `dot` writes the first into the file and the
  others to standard output.

```
go tool pprof -dot cpu.prof | dot -Tpng -o profile.png
```

That is the whole interface. Graphviz's other options (`-G`, `-N`, `-E`,
`-l`, multiple outputs) are not implemented; graph attributes go in the DOT
source.

## Why

Upstream go-graphviz keeps its own `dot` command in a nested module that has never
been tagged and still requires the library at v0.2.5, five releases behind.
A tool directive on it cannot move. This module is the same thin client on
the current library, tagged, so a `tool` directive or `go install` pin
tracks releases like any other dependency.

## Releasing

A release is a signed tag on `main`, cut with `just do release vX.Y.Z` by a
repository admin; `go install` and tool directives take it from the module
proxy. Before tagging: `just lint` and `just test` green on CI, and every
merged pull request's title saying what changed for a user: the release
notes are those titles, published on the tag's release page with
`gh release create vX.Y.Z --verify-tag --generate-notes`, and one that
breaks a consumer carries the `breaking` label.

## Licence

MIT, see `LICENSE`. The binary embeds forkcloser/go-graphviz's `graphviz.wasm`,
built from Graphviz, under the Eclipse Public License 2.0 (`LICENSE-graphviz`),
and expat, under the MIT license (`LICENSE-expat`); the release archives carry
both texts.
