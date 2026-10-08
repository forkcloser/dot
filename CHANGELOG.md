# Changelog

All notable changes are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and versions follow
[Semantic Versioning](https://semver.org/): the versioned surface is the
command line interface described in `README.md`.

## [Unreleased]

## [1.2.0] - 2026-10-08

### Added

- `-Txdot`: the layout with its drawing operations, as Graphviz's xdot.
  `-Tdot` stays the layout with positions only.

### Changed

- The engine is forkcloser/go-graphviz v0.5.0. Text is measured with the
  fonts it is drawn in, the installed ones, as Graphviz's own `dot` does:
  labels fit their nodes, and the same graph can lay out slightly
  differently, in every output format, on machines with different fonts.
- PNG and JPEG are drawn the way Graphviz's Cairo renderer draws: lines
  meet in mitred corners and end flat, where they were round; dashed lines
  are six points on, six off, and dotted lines two on, six off, Graphviz's
  lengths. A PNG render of a graph of about 40 nodes allocates 7 MB
  instead of 320 MB and takes half the time. The binary no longer carries
  `fogleman/gg` or `golang/freetype`.
- Font names resolve as Graphviz resolves them (PostScript names, family
  and style words, metric-compatible substitutes), falling back to
  embedded Go fonts in regular, bold, italic and monospace.
- Lines scale with the page as in Graphviz's renderers: at the default
  96 dpi a pen width of 1 is a third wider than before.
- A node image is drawn in the box `imagescale` and `imagepos` give it,
  and may be JPEG, GIF, BMP or WebP.
- A PNG or JPEG page over 256 megapixels, or a node image over 64
  megapixels, is refused instead of allocated.

### Fixed

- `rotate=90` and `landscape=true` graphs render in PNG and JPEG instead of
  a blank page.
- Characters the label's font lacks (Japanese, Chinese, Korean in a Latin
  font) are drawn from an installed font that has them, not as boxes.
- A node image named by an absolute path is drawn; it was silently left
  out. A lossless or extended WebP node image, any WebP with alpha, is
  sized as it is instead of failing the render as a page too large.
- An attribute the engine could not store (out of WebAssembly memory)
  fails the render instead of being dropped.

Details in go-graphviz's
[CHANGELOG](https://github.com/forkcloser/go-graphviz/blob/v0.5.0/CHANGELOG.md).

## [1.1.1] - 2026-10-05

### Fixed

- PNG and JPEG output, through forkcloser/go-graphviz v0.3.1: edges are
  drawn as lines on amd64 instead of filled shapes, labels in a TrueType or
  fallback font are drawn instead of left blank (Linux and Windows), text
  sits on Graphviz's baseline instead of one font size too high, and a
  node's explicit empty label stays empty. Details in go-graphviz's
  [CHANGELOG](https://github.com/forkcloser/go-graphviz/blob/v0.3.1/CHANGELOG.md).

## [1.1.0] - 2026-10-05

### Changed

- The engine is forkcloser/go-graphviz v0.3.0, forkcloser's fork of
  goccy/go-graphviz, in place of goccy/go-graphviz v0.2.10. It carries
  Graphviz 16.1.0 where v0.2.10 carried 12.1.2, so layouts and renders follow
  Graphviz's own changes between the two, and the fork's own fixes are in its
  [CHANGELOG](https://github.com/forkcloser/go-graphviz/blob/v0.3.0/CHANGELOG.md);
  the command, its flags and its output formats are unchanged. The fork no
  longer pulls in disintegration/imaging or flopp/go-findfont.

## [1.0.1] - 2026-09-20

### Fixed

- `-o` rendered straight into the output path, so a failed render, an
  interrupt or a full disk left a truncated file there; a plain parse error
  emptied a previously good output. The render now goes to a temporary file
  beside the target, renamed onto it only on success.

## [1.0.0] - 2026-09-13

### Added

- The `dot` command: `-K` layout, `-T` format (dot, svg, png, jpg), `-o`
  output, file or standard input, file or standard output, Graphviz's glued
  and spaced flag forms, `-V` from the build info. Rendering by go-graphviz
  v0.2.10, Graphviz as WebAssembly.
- Empty or whitespace-only input is reported as "no graph found" instead of
  faulting inside the engine.
